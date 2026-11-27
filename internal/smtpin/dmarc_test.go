// dmarc_test.go: the audit item 8 rewrite — exact tag parsing, organizational
// fallback, relaxed alignment, and the any-aligned-DKIM rule.
package smtpin

import "testing"

func TestParseDMARCRecordSpNotMisreadAsP(t *testing.T) {
	tags := parseDMARCRecord("v=DMARC1; p=none; sp=reject; rua=mailto:x@y.z")
	if tags["p"] != "none" {
		t.Fatalf("p = %q, want none (substring matching used to read sp=reject as p=reject)", tags["p"])
	}
	if tags["sp"] != "reject" {
		t.Fatalf("sp = %q, want reject", tags["sp"])
	}
	// The policy for the EXACT domain uses p; subdomains use sp.
	if got := tags.policyFor("example.co.za", "example.co.za"); got != "none" {
		t.Fatalf("policy for exact domain = %q, want none", got)
	}
	if got := tags.policyFor("example.co.za", "sub.example.co.za"); got != "reject" {
		t.Fatalf("policy for subdomain = %q, want reject", got)
	}
}

func TestOrgDomain(t *testing.T) {
	cases := map[string]string{
		"example.co.za":       "example.co.za",
		"mail.example.co.za":  "example.co.za",
		"a.b.example.co.za":   "example.co.za",
		"example.com":         "example.com",
		"mx1.example.com":     "example.com",
		"deep.mx1.example.uk": "example.uk", // co.uk-style suffixes: uk alone
	}
	for in, want := range cases {
		if got := OrgDomain(in); got != want {
			t.Errorf("OrgDomain(%q) = %q, want %q", in, got, want)
		}
	}
	// The SA two-level suffix must not be split wrong.
	if got := OrgDomain("mx1.example.co.za"); got != "example.co.za" {
		t.Errorf("co.za org domain = %q, want example.co.za", got)
	}
}

func TestAlignedSiblingSubdomains(t *testing.T) {
	// Relaxed alignment: organizational domains match. The old two-way
	// suffix match FAILED sibling subdomains (mail.a.example vs b.example).
	if !Aligned("mail.example.co.za", "other.example.co.za") {
		t.Fatal("sibling subdomains of one org domain are aligned")
	}
	if Aligned("mail.example.co.za", "example.org") {
		t.Fatal("different org domains are not aligned")
	}
	if Aligned("", "example.com") || Aligned("example.com", "") {
		t.Fatal("empty domains never align")
	}
}

func TestEvaluateDMARCAnyAlignedDKIMPass(t *testing.T) {
	rec := "v=DMARC1; p=reject"
	// First passing signature is NOT aligned; the second IS.
	result, policy := EvaluateDMARC("example.co.za", rec, "", []string{"evil.example.com", "mail.example.co.za"})
	if result != "pass" || policy != "reject" {
		t.Fatalf("result=%q policy=%q; want pass/reject (any aligned signature passes)", result, policy)
	}
	// No aligned signature → fail with the applicable policy.
	result, policy = EvaluateDMARC("example.co.za", rec, "", []string{"evil.example.com"})
	if result != "fail" || policy != "reject" {
		t.Fatalf("result=%q policy=%q; want fail/reject", result, policy)
	}
	// SPF alignment alone passes.
	result, _ = EvaluateDMARC("example.co.za", rec, "example.co.za", nil)
	if result != "pass" {
		t.Fatalf("spf-aligned result=%q, want pass", result)
	}
	// No record → none.
	result, policy = EvaluateDMARC("example.co.za", "", "", nil)
	if result != "none" || policy != "" {
		t.Fatalf("no record: result=%q policy=%q; want none/empty", result, policy)
	}
}

func TestFromDomainOfHeader(t *testing.T) {
	if d := FromDomainOfHeader("Evil Display <victim@example.co.za>"); d != "example.co.za" {
		t.Fatalf("display-name From parsed as %q, want example.co.za", d)
	}
	if d := FromDomainOfHeader("not-an-address"); d != "" {
		t.Fatalf("malformed From parsed as %q, want empty", d)
	}
}
