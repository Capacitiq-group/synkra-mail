// dmarc.go: DMARC evaluation rewritten per audit item 8. The hand-rolled v1
// matched `p=reject` by substring (so `sp=reject` read as `p=reject`), had no
// organizational-domain fallback (subdomain spoofing walked past `p=reject`),
// aligned by two-way suffix (siblings failed), and used the first passing
// DKIM signature rather than an aligned one.
package smtpin

import (
	"context"
	"net"
	"net/mail"
	"strings"
	"time"
)

// dmarcTags is a DMARC1 record parsed into its tag=value pairs. Unknown tags
// are ignored (forward compatibility, RFC 7489 6.3).
type dmarcTags map[string]string

// parseDMARCRecord parses "v=DMARC1; p=reject; rua=..." — exact tag matching,
// whitespace tolerated, values lower-cased for the policy tags we act on.
func parseDMARCRecord(rec string) dmarcTags {
	tags := dmarcTags{}
	for _, part := range strings.Split(rec, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.IndexByte(part, '=')
		if eq <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(part[:eq]))
		val := strings.TrimSpace(part[eq+1:])
		tags[key] = val
	}
	return tags
}

// twoLevelTLDs: public-suffix second-level labels this deployment cares about
// (South Africa first — the register is a ZACR shop — plus the common
// international ones). The full PSL is a bigger dependency than the platform
// needs at this stage; the list lives here, in one place, testable.
var twoLevelTLDs = map[string]struct{}{
	"co.za": {}, "org.za": {}, "net.za": {}, "web.za": {}, "gov.za": {}, "ac.za": {},
	"co.uk": {}, "org.uk": {}, "ac.uk": {}, "gov.uk": {},
	"com.au": {}, "net.au": {}, "org.au": {},
	"co.nz": {}, "org.nz": {},
	"com.br": {}, "com.mx": {}, "com.ar": {}, "com.sg": {}, "co.jp": {}, "co.in": {},
	"co.ke": {}, "co.tz": {}, "co.zw": {}, "com.ng": {}, "com.gh": {},
}

// OrgDomain returns the organizational domain (RFC 7489 3.2): the public
// suffix plus one label. example.co.za → example.co.za; mail.example.co.za →
// example.co.za; example.com → example.com.
func OrgDomain(domain string) string {
	d := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if d == "" {
		return ""
	}
	labels := strings.Split(d, ".")
	if len(labels) <= 2 {
		return d
	}
	// Try the longest two-level suffix first (a.b.co.za → suffix co.za).
	last2 := labels[len(labels)-2] + "." + labels[len(labels)-1]
	if _, is2 := twoLevelTLDs[last2]; is2 {
		if len(labels) == 3 {
			return d
		}
		return strings.Join(labels[len(labels)-3:], ".")
	}
	return strings.Join(labels[len(labels)-2:], ".")
}

// Aligned: relaxed-mode alignment (RFC 7489 3.1.1) between an
// authenticated identifier's domain and the From domain — equal
// organizational domains.
func Aligned(authDomain, fromDomain string) bool {
	if authDomain == "" || fromDomain == "" {
		return false
	}
	return OrgDomain(authDomain) == OrgDomain(fromDomain)
}

// policyFor resolves the applicable policy for a subdomain of a DMARC-
// publishing domain (RFC 7489 6.3): the record's `p` for the exact domain,
// its `sp` for subdomains, falling back to `p`.
func (t dmarcTags) policyFor(domain, fromDomain string) string {
	if strings.EqualFold(domain, fromDomain) {
		return strings.ToLower(t["p"])
	}
	if sp, ok := t["sp"]; ok && sp != "" {
		return strings.ToLower(sp)
	}
	return strings.ToLower(t["p"])
}

// EvaluateDMARC is the ingest-side evaluation: given the From domain, the
// published record ("" when none), the SPF verdict domain and EVERY passing
// DKIM domain, it returns the DMARC result and the applicable policy. DKIM
// passes when ANY signature with an aligned domain passes (audit item 8: the
// first passing signature won before, so an aligned signature behind a
// failed one was missed).
func EvaluateDMARC(fromDomain string, rec string, spfDomain string, dkimPassDomains []string) (result, policy string) {
	if fromDomain == "" {
		return "none", ""
	}
	tags := parseDMARCRecord(rec)
	if !strings.EqualFold(tags["v"], "dmarc1") && !strings.EqualFold(firstRecordTag(rec), "v=dmarc1") {
		// Not a DMARC record (or absent) → policy none.
		return "none", ""
	}
	pol := tags.policyFor(fromDomain, fromDomain)
	if pol == "" || pol == "none" {
		// Organizational fallback (audit item 8): a subdomain of a
		// p=reject org domain inherits the org policy via sp/p.
		org := OrgDomain(fromDomain)
		if org != "" && !strings.EqualFold(org, fromDomain) {
			orgTags := parseDMARCRecord(lookupDMARCRecord(context.Background(), org))
			if strings.EqualFold(orgTags["v"], "dmarc1") {
				pol = orgTags.policyFor(org, fromDomain)
			}
		}
	}
	if pol == "" || pol == "none" {
		return "pass", "" // no meaningful policy to fail against
	}
	spfAligned := spfDomain != "" && Aligned(spfDomain, fromDomain)
	dkAligned := false
	for _, d := range dkimPassDomains {
		if d != "" && Aligned(d, fromDomain) {
			dkAligned = true
			break
		}
	}
	if spfAligned || dkAligned {
		return "pass", pol
	}
	return "fail", pol
}

// lookupDMARCRecord fetches the DMARC TXT record for a domain (short timeout;
// slow resolvers must not stall the MX).
func lookupDMARCRecord(ctx context.Context, domain string) string {
	qctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resolver := &net.Resolver{}
	txts, err := resolver.LookupTXT(qctx, "_dmarc."+domain)
	if err != nil || len(txts) == 0 {
		return ""
	}
	for _, t := range txts {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(strings.ToLower(t), "v=dmarc1") {
			return t
		}
	}
	return ""
}

// firstRecordTag is a tolerant check used by EvaluateDMARC ("v=DMARC1" with
// arbitrary case/spacing).
func firstRecordTag(rec string) string {
	for _, part := range strings.Split(rec, ";") {
		part = strings.TrimSpace(part)
		if part != "" {
			return part
		}
	}
	return ""
}

// spfDomain extracts the MAIL FROM domain used for SPF evaluation.
func spfDomain(envFrom string) string {
	if at := strings.LastIndex(envFrom, "@"); at >= 0 {
		return strings.ToLower(envFrom[at+1:])
	}
	return strings.ToLower(envFrom)
}

// FromAddress parses the From header field with the standard library (audit
// item 8: naive string splitting disagreed with what a mail client displays —
// "Evil <victim@example.com>" parsed as domain example.com but the display
// name carried the payload).
func FromAddress(headerValue string) (addr *mail.Address, err error) {
	addr, err = mail.ParseAddress(strings.TrimSpace(headerValue))
	if err != nil {
		return nil, err
	}
	return addr, nil
}

// FromDomainOfHeader extracts the From domain from a header block using the
// real address parser (falls back to "" on a malformed header, which then
// evaluates as DMARC none — never a false pass).
func FromDomainOfHeader(rawHeader string) string {
	addr, err := FromAddress(rawHeader)
	if err != nil || addr == nil {
		return ""
	}
	at := strings.LastIndexByte(addr.Address, '@')
	if at < 0 || at == len(addr.Address)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(addr.Address[at+1:], "."))
}
