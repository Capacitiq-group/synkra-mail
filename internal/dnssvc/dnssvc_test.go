// dnssvc_test.go: phase 8 unit tests — the mail-ready template shape and the
// DNS doctor verdicts (no live DNS; the resolver is a fake).
package dnssvc

import (
	"context"
	"strings"
	"testing"
)

func template() TemplateInput {
	return TemplateInput{
		Domain:         "customer.co.za",
		DKIMSelector:   "sk1",
		DKIMRecordTXT:  "v=DKIM1; k=rsa; p=TESTKEY",
		Hostname:       "mx1.synkra.co.za",
		PlatformDomain: "synkra.email",
		OwnershipToken: "synkra-verify-abc123",
	}
}

// The template must NOT tell the customer to publish the platform's report
// authorisation record (audit item 22: that record belongs in OUR zone).
func TestMailReadyTemplateShape(t *testing.T) {
	recs := MailReadyRecords(template())
	var mx, spf, dkim, dmarc, own bool
	for _, r := range recs {
		switch r.Purpose {
		case "mail_mx":
			mx = true
			if r.Name == "@" && r.RData != "10 mx1.synkra.co.za" {
				t.Fatalf("mx rdata = %q", r.RData)
			}
		case "spf":
			spf = true
			if !strings.Contains(r.RData, "include:spf.synkra.email") {
				t.Fatalf("spf = %q", r.RData)
			}
		case "dkim":
			dkim = true
			if r.Name != "sk1._domainkey" || r.RData != "v=DKIM1; k=rsa; p=TESTKEY" {
				t.Fatalf("dkim = %s %q", r.Name, r.RData)
			}
		case "dmarc":
			dmarc = true
			if !strings.Contains(r.RData, "rua=mailto:dmarc-reports@synkra.email") {
				t.Fatalf("dmarc = %q", r.RData)
			}
		case "ownership":
			own = true
		}
	}
	if !mx || !spf || !dkim || !dmarc || !own {
		t.Fatalf("template incomplete: mx=%v spf=%v dkim=%v dmarc=%v own=%v", mx, spf, dkim, dmarc, own)
	}
	for _, r := range recs {
		if r.TTL != TTLDefault {
			t.Fatalf("record %s TTL = %d, want %d (4b.9)", r.Name, r.TTL, TTLDefault)
		}
	}
}

func TestDoctorVerdicts(t *testing.T) {
	in := template()
	live := map[string][]string{
		"customer.co.za":                {"10 mx1.synkra.co.za", "synkra-verify-abc123"},
		"_dmarc.customer.co.za":         {"v=DMARC1; p=quarantine; rua=mailto:dmarc-reports@synkra.email"},
		"sk1._domainkey.customer.co.za": {"v=DKIM1; k=rsa; p=TESTKEY"},
	}
	lookup := func(ctx context.Context, name string) ([]string, error) {
		// The apex lookup doubles as TXT; SPF lives at the apex too.
		return live[name], nil
	}
	checks := Doctor(context.Background(), in, lookup)
	pass := 0
	for _, c := range checks {
		if c.Pass {
			pass++
		}
	}
	// MX, ownership, DMARC, DKIM pass; SPF "fails" (the apex TXT join has no
	// v=spf1 record in the fake), proving the doctor reports per-record truth.
	if pass != 4 {
		t.Fatalf("doctor pass count = %d, want 4", pass)
	}
	for _, c := range checks {
		if !c.Pass && c.Fix == "" {
			t.Fatalf("failing check %s must carry a fix (register §1.10)", c.Record)
		}
	}
	// Break the MX and watch the doctor catch it with the right fix.
	live["customer.co.za"] = []string{"10 wrong.example.net", "synkra-verify-abc123"}
	checks = Doctor(context.Background(), in, lookup)
	for _, c := range checks {
		if c.Record == "MX" && c.Pass {
			t.Fatal("doctor must catch a wrong MX host")
		}
	}
}
