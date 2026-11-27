// Package dnssvc: the authoritative DNS service behind DnsService (build
// register §4b — phase 8, tier-0 dependency for every inbound and outbound
// mail flow).
//
// Scope in this build: the zone data model + internal zone API (4b.4), the
// mail-ready zone templates (4b.5), TTL policy (4b.9), the zone-change audit
// log (4b.10), the registration workflow's zero-cache path (createZone before
// delegation), and the DNS doctor validation job. The anycast edge, DNSSEC
// signing, AXFR and response-rate limiting are OPERATOR infrastructure around
// this API: the interface is deliberately small enough to sit in front of
// them (see the posture notes at the bottom).
package dnssvc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	"synkra.email/mail/internal/store"
)

// Record is one DNS record row (mail.dns_record).
type Record struct {
	ID      uuid.UUID
	ZoneID  uuid.UUID
	Name    string // relative to the zone origin; "@" is the apex
	RRType  string // MX | TXT | A | AAAA | CNAME | NS | SRV | CAA
	RData   string
	TTL     int    // seconds; TTLDefault for mail-ready records (4b.9)
	Purpose string // mail_mx | spf | dkim | dmarc | ownership | mta_sts | tls_rpt | custom
}

// TTL policy (4b.9): onboarding-speed values, documented, fixed.
const (
	TTLDefault = 300 // mail-ready records: corrections land within minutes
	TTLNS      = 3600
)

// DnsService is the API the rest of the platform talks to (4b.4). A separate
// process owns the DNS edge; this interface is the contract.
type DnsService interface {
	CreateZone(ctx context.Context, zone string, kind string, ownerOrgID *uuid.UUID) (uuid.UUID, error)
	PutRecord(ctx context.Context, zone string, rec Record) (uuid.UUID, error)
	DeleteRecord(ctx context.Context, recordID uuid.UUID) error
	ListRecords(ctx context.Context, zone string) ([]Record, error)
	// BatchApply writes the mail-ready set in one audited transaction — the
	// registration workflow's createZone step calls exactly this.
	BatchApply(ctx context.Context, zone string, records []Record) error
}

// PGDnsService is the Postgres-backed implementation (mail.dns_zone /
// mail.dns_record / mail.dns_audit).
type PGDnsService struct {
	Store  *store.Store
	Actor  *uuid.UUID // acting staff/admin for the audit trail (4b.10)
	Resolv func(ctx context.Context, name string) ([]string, error)
}

func NewPG(s *store.Store, actor *uuid.UUID) *PGDnsService {
	return &PGDnsService{Store: s, Actor: actor}
}

func (d *PGDnsService) CreateZone(ctx context.Context, zone string, kind string, ownerOrgID *uuid.UUID) (uuid.UUID, error) {
	zone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zone), "."))
	if !store.ValidDomainName(zone) {
		return uuid.Nil, store.ErrInvalidDomain
	}
	var id uuid.UUID
	err := d.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := tx.QueryRow(ctx, `insert into mail.dns_zone (id, zone, kind, owner_org_id)
			values ($1,$2,$3,$4)
			on conflict (zone) do update set kind=excluded.kind, updated_at=now()
			returning id`, uuid.New(), zone, kind, ownerOrgID).Scan(&id); err != nil {
			return err
		}
		return d.audit(ctx, tx, id, "create_zone", map[string]any{"zone": zone, "kind": kind})
	})
	return id, err
}

func (d *PGDnsService) zoneID(ctx context.Context, q store.Querier, zone string) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `select id from mail.dns_zone where zone=$1`, strings.ToLower(strings.TrimSuffix(zone, "."))).Scan(&id)
	return id, err
}

func (d *PGDnsService) PutRecord(ctx context.Context, zone string, rec Record) (uuid.UUID, error) {
	var out uuid.UUID
	err := d.Store.WithTx(ctx, func(tx store.Tx) error {
		zid, err := d.zoneID(ctx, tx, zone)
		if err != nil {
			return err
		}
		if rec.TTL <= 0 {
			rec.TTL = TTLDefault
		}
		if err := tx.QueryRow(ctx, `insert into mail.dns_record (id, zone_id, name, rrtype, rdata, ttl, purpose)
			values ($1,$2,$3,$4,$5,$6,$7)
			on conflict (zone_id, name, rrtype, rdata) do update set ttl=excluded.ttl, purpose=excluded.purpose
			returning id`, uuid.New(), zid, rec.Name, rec.RRType, rec.RData, rec.TTL, rec.Purpose).Scan(&out); err != nil {
			return err
		}
		bumpSerial(ctx, tx, zid)
		return d.audit(ctx, tx, zid, "put_record", map[string]any{
			"name": rec.Name, "type": rec.RRType, "purpose": rec.Purpose,
		})
	})
	return out, err
}

func (d *PGDnsService) DeleteRecord(ctx context.Context, recordID uuid.UUID) error {
	return d.Store.WithTx(ctx, func(tx store.Tx) error {
		var zid uuid.UUID
		if err := tx.QueryRow(ctx, `delete from mail.dns_record where id=$1 returning zone_id`, recordID).Scan(&zid); err != nil {
			return err
		}
		bumpSerial(ctx, tx, zid)
		return d.audit(ctx, tx, zid, "delete_record", map[string]any{"record": recordID.String()})
	})
}

func (d *PGDnsService) ListRecords(ctx context.Context, zone string) ([]Record, error) {
	rows, err := d.Store.Pool.Query(ctx, `select r.id, r.zone_id, r.name, r.rrtype, r.rdata, r.ttl, r.purpose
		from mail.dns_record r join mail.dns_zone z on z.id = r.zone_id
		where z.zone=$1 order by r.name, r.rrtype`, strings.ToLower(strings.TrimSuffix(zone, ".")))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.ZoneID, &r.Name, &r.RRType, &r.RData, &r.TTL, &r.Purpose); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *PGDnsService) BatchApply(ctx context.Context, zone string, records []Record) error {
	return d.Store.WithTx(ctx, func(tx store.Tx) error {
		zid, err := d.zoneID(ctx, tx, zone)
		if err != nil {
			return err
		}
		for _, rec := range records {
			if rec.TTL <= 0 {
				rec.TTL = TTLDefault
			}
			if _, err := tx.Exec(ctx, `insert into mail.dns_record (id, zone_id, name, rrtype, rdata, ttl, purpose)
				values ($1,$2,$3,$4,$5,$6,$7)
				on conflict (zone_id, name, rrtype, rdata) do update set ttl=excluded.ttl, purpose=excluded.purpose`,
				uuid.New(), zid, rec.Name, rec.RRType, rec.RData, rec.TTL, rec.Purpose); err != nil {
				return err
			}
		}
		bumpSerial(ctx, tx, zid)
		return d.audit(ctx, tx, zid, "batch_apply", map[string]any{"records": len(records)})
	})
}

func bumpSerial(ctx context.Context, tx store.Tx, zoneID uuid.UUID) {
	_, _ = tx.Exec(ctx, `update mail.dns_zone set serial = serial + 1, updated_at=now() where id=$1`, zoneID)
}

func (d *PGDnsService) audit(ctx context.Context, tx store.Tx, zoneID uuid.UUID, action string, detail map[string]any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		raw = []byte("{}")
	}
	_, err = tx.Exec(ctx, `insert into mail.dns_audit (zone_id, actor_id, action, detail)
		values ($1,$2,$3,$4::jsonb)`, zoneID, d.Actor, action, string(raw))
	return err
}

// ---- mail-ready zone templates (4b.5): one call writes the whole set ----

// TemplateInput names the knobs a mail-ready zone is built from.
type TemplateInput struct {
	Domain         string // the customer domain (the zone origin)
	DKIMSelector   string // the domain row's active selector
	DKIMRecordTXT  string // the DKIM public record (from the domain row)
	Hostname       string // our sending/receiving host (MX target)
	PlatformDomain string // synkra.email — rua destination + SPF include
	OwnershipToken string // the domain row's verification token
	HostIP         string // our A record for the MX host (empty: skip)
}

// dkimValue: the record value, or an explicit placeholder when the caller did
// not bring the domain row along (tests).
func (t TemplateInput) dkimValue() string {
	if t.DKIMRecordTXT != "" {
		return t.DKIMRecordTXT
	}
	return "v=DKIM1; k=rsa; p=<publish-the-domain-row-value>"
}

// MailReadyRecords is the complete record set for a hosted domain. The
// DMARC authorisation record (RFC 7489 §7.1) is deliberately NOT here: it is
// OUR zone's job (`<customer>._report._dmarc.<platform>`), published once per
// customer by the platform zone — the audit item 22 correction.
func MailReadyRecords(in TemplateInput) []Record {
	recs := []Record{
		{Name: "@", RRType: "MX", RData: "10 " + in.Hostname, TTL: TTLDefault, Purpose: "mail_mx"},
		{Name: "@", RRType: "TXT",
			RData:   "v=spf1 include:spf." + in.PlatformDomain + " ~all",
			TTL:     TTLDefault,
			Purpose: "spf"},
		{Name: in.DKIMSelector + "._domainkey", RRType: "TXT", RData: in.dkimValue(), TTL: TTLDefault, Purpose: "dkim"},
		{Name: "_dmarc", RRType: "TXT",
			RData:   "v=DMARC1; p=quarantine; rua=mailto:dmarc-reports@" + in.PlatformDomain,
			TTL:     TTLDefault,
			Purpose: "dmarc"},
		{Name: "@", RRType: "TXT", RData: in.OwnershipToken, TTL: TTLDefault, Purpose: "ownership"},
		// MTA-STS + TLS-RPT (deliberately on: inbound policy is cheap and
		// demonstrates the TLS posture the register demands).
		{Name: "_mta-sts", RRType: "TXT",
			RData:   "v=STSv1; id=" + mtaStsID(),
			TTL:     TTLDefault,
			Purpose: "mta_sts"},
		{Name: "_smtp._tls", RRType: "TXT",
			RData:   "v=TLSRPTv1; rua=mailto:tls-rpt@" + in.PlatformDomain,
			TTL:     TTLDefault,
			Purpose: "tls_rpt"},
	}
	if in.HostIP != "" {
		recs = append(recs, Record{Name: in.HostnameToName(), RRType: "A", RData: in.HostIP, TTL: TTLDefault, Purpose: "mail_mx"})
	}
	return recs
}

// HostnameToName: the A record for the MX host lives in OUR zone, not the
// customer's; the template writes it only for self-hosted-style zones.
func (t TemplateInput) HostnameToName() string {
	if strings.HasSuffix(t.Hostname, "."+t.Domain) {
		return strings.TrimSuffix(t.Hostname, "."+t.Domain)
	}
	return t.Hostname + "."
}

func mtaStsID() string { return time.Now().UTC().Format("20060102") }

// ---- DNS doctor (registration workflow step 3: validation surfaced pass/fail) ----

// DoctorCheck is one record's verdict for the onboarding UI.
type DoctorCheck struct {
	Record   string `json:"record"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Pass     bool   `json:"pass"`
	Fix      string `json:"fix"`
}

// Doctor resolves the live DNS and compares against the expected set. Every
// verdict carries what-to-do (register §1.10 honesty rule).
func Doctor(ctx context.Context, in TemplateInput, lookup func(ctx context.Context, name string) ([]string, error)) []DoctorCheck {
	if lookup == nil {
		lookup = defaultLookup
	}
	mxWant := "10 " + in.Hostname
	spfWant := "v=spf1 include:spf." + in.PlatformDomain + " ~all"
	dkimWant := in.dkimValue()
	dmarcWant := "v=DMARC1; p=quarantine; rua=mailto:dmarc-reports@" + in.PlatformDomain

	mx := lookupMXString(ctx, lookup, in.Domain)
	spf := lookupTXTJoined(ctx, lookup, in.Domain)
	dkim := lookupTXTJoined(ctx, lookup, in.DKIMSelector+"._domainkey."+in.Domain)
	dmarc := lookupTXTJoined(ctx, lookup, "_dmarc."+in.Domain)
	own := lookupTXTJoined(ctx, lookup, in.Domain)

	return []DoctorCheck{
		{Record: "MX", Expected: mxWant, Actual: mx, Pass: mxContains(mx, in.Hostname),
			Fix: "publish MX 10 " + in.Hostname + " on " + in.Domain},
		{Record: "TXT @ (SPF)", Expected: spfWant, Actual: spf,
			Pass: strings.Contains(spf, "include:spf."+in.PlatformDomain),
			Fix:  "merge include:spf." + in.PlatformDomain + " into your existing v=spf1 record — never a second v=spf1 record"},
		{Record: "TXT " + in.DKIMSelector + "._domainkey", Expected: dkimWant, Actual: dkim,
			Pass: strings.Contains(dkim, "v=DKIM1"),
			Fix:  "publish the DKIM TXT record shown in the console (selector " + in.DKIMSelector + ")"},
		{Record: "TXT _dmarc", Expected: dmarcWant, Actual: dmarc,
			Pass: strings.Contains(dmarc, "v=DMARC1"),
			Fix:  "publish the DMARC policy record; the report address is authorised by the platform automatically"},
		{Record: "TXT @ (ownership)", Expected: in.OwnershipToken, Actual: own,
			Pass: strings.Contains(own, in.OwnershipToken),
			Fix:  "publish TXT " + in.OwnershipToken + " on " + in.Domain + " (TTL 300-600s), wait for propagation, then verify"},
	}
}

func mxContains(actual, host string) bool {
	for _, f := range strings.Fields(actual) {
		if strings.TrimSuffix(f, ".") == strings.TrimSuffix(host, ".") {
			return true
		}
	}
	return false
}

func lookupMXString(ctx context.Context, lookup func(context.Context, string) ([]string, error), domain string) string {
	vals, err := lookup(ctx, domain)
	if err != nil {
		return ""
	}
	return strings.Join(vals, " ")
}

func lookupTXTJoined(ctx context.Context, lookup func(context.Context, string) ([]string, error), name string) string {
	vals, err := lookup(ctx, name)
	if err != nil || len(vals) == 0 {
		return ""
	}
	return strings.Join(vals, " | ")
}

func defaultLookup(ctx context.Context, name string) ([]string, error) {
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resolver := &net.Resolver{}
	// MX when the name looks like an apex of the zone; TXT otherwise. The
	// doctor passes the right expectation per check — resolve both and let
	// the caller decide.
	txts, terr := resolver.LookupTXT(qctx, name)
	if terr == nil && len(txts) > 0 {
		return txts, nil
	}
	mxs, merr := resolver.LookupMX(qctx, name)
	if merr != nil {
		return nil, merr
	}
	out := make([]string, 0, len(mxs))
	for _, m := range mxs {
		out = append(out, fmt.Sprintf("%d %s", m.Pref, strings.TrimSuffix(m.Host, ".")))
	}
	return out, nil
}

// ---- posture notes (what the operator runs AROUND this API) ----
//
// 4b.1  anycast / multi-region unicast: the API is state-backed, so edges can
//       be added without schema change.
// 4b.2  ≥2 distinct NS hostnames on distinct paths; NS records live at the
//       registrar (glue under our own zone, 4b.3).
// 4b.6  DNSSEC: OFF until the rollover runbook exists — partial DNSSEC is
//       worse than none (register's own warning).
// 4b.7  AXFR/IXFR: disabled by default; zone transfer is operator-configured
//       per-secondary with IP allowlists.
// 4b.8  response rate limiting + anycast scrubbing happen at the edge; the
//       zone API is never internet-exposed directly.
// 4b.12 reverse zones: delegated from the IP provider, managed as zones with
//       kind='reverse' through this same API.
// 4b.13 wildcards: a Record{Name:"*"} is legal through this API (catch-all).
