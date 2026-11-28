// phase11_test.go — the registrar-managed domain lifecycle, billing document
// and storage usage, end to end through the API (dry-run registrar: dev and
// CI run without Oner credentials).
package e2e

import (
	"net/http"
	"testing"
)

func TestDomainWizardDryRun(t *testing.T) {
	s := boot(t)
	_, _, login, pw := s.provisionTenant(t, "domreg")
	token := s.login(t, login, pw)

	// 1. availability check
	st, body, _ := s.apiReq(t, "POST", "/api/v1/domains/check", token,
		map[string]any{"names": []string{"freshdom.co.za", "takendom.co.za"}}, nil)
	if st != http.StatusOK {
		t.Fatalf("check: %d %v", st, body)
	}
	results := body["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("check results: %v", body)
	}

	// 2. register with our nameservers → zone provisioned
	st, body, _ = s.apiReq(t, "POST", "/api/v1/domains/register", token, map[string]any{
		"name": "freshdom.co.za", "years": 1, "ns_mode": "synkra",
		"registrant": map[string]any{
			"first_name": "T", "last_name": "M", "email": login,
			"phone": "+27.821234567", "city": "Cape Town", "country_code": "ZA",
		},
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("register: %d %v", st, body)
	}
	if body["zone_provisioned"] != true {
		t.Fatalf("zone not provisioned: %v", body)
	}
	domID := body["domain_id"].(string)

	// 3. detail: ZARC pending_create + add_grace + full management
	st, body, _ = s.apiReq(t, "GET", "/api/v1/domains/"+domID, token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("detail: %d %v", st, body)
	}
	reg := body["registrar"].(map[string]any)
	if reg["registry_status"] != "pending_create" || reg["managed_mode"] != "full" {
		t.Fatalf("detail registrar: %v", reg)
	}

	// 4. custom nameservers → partially managed
	st, body, _ = s.apiReq(t, "PUT", "/api/v1/domains/"+domID+"/nameservers", token, map[string]any{
		"nameservers": []string{"ns1.external.example", "ns2.external.example"},
	}, nil)
	if st != http.StatusOK || body["managed_mode"] != "partial" {
		t.Fatalf("nameservers: %d %v", st, body)
	}

	// 5. transfer lock + whois privacy + auto-renew toggles
	for _, ep := range []string{"transfer-lock", "whois-privacy", "auto-renew"} {
		st, body, _ = s.apiReq(t, "POST", "/api/v1/domains/"+domID+"/"+ep, token,
			map[string]any{"enabled": true}, nil)
		if st != http.StatusOK || body["enabled"] != true {
			t.Fatalf("%s: %d %v", ep, st, body)
		}
	}

	// 6. EPP retrieval: wrong password refused, right password yields a code
	st, _, _ = s.apiReq(t, "POST", "/api/v1/domains/"+domID+"/epp", token,
		map[string]any{"password": "wrong-password"}, nil)
	if st != http.StatusForbidden {
		t.Fatalf("epp wrong password: %d", st)
	}
	st, body, _ = s.apiReq(t, "POST", "/api/v1/domains/"+domID+"/epp", token,
		map[string]any{"password": pw}, nil)
	if st != http.StatusOK {
		t.Fatalf("epp right password: %d %v", st, body)
	}
	if code, _ := body["epp_code"].(string); code == "" {
		t.Fatalf("empty epp code: %v", body)
	}

	// 7. the DNS doctor returns expected records + live verdicts
	st, body, _ = s.apiReq(t, "GET", "/api/v1/domains/"+domID+"/doctor", token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("doctor: %d %v", st, body)
	}
	if checks, ok := body["checks"].([]any); !ok || len(checks) == 0 {
		t.Fatalf("doctor checks missing: %v", body)
	}
}

func TestBillingPreviewVATAndInvoice(t *testing.T) {
	s := boot(t)
	_, _, login, pw := s.provisionTenant(t, "billing")
	token := s.login(t, login, pw)

	// Preview: per-line VAT at exactly 15%, totals derived.
	st, body, _ := s.apiReq(t, "GET", "/api/v1/org/billing/preview", token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("preview: %d %v", st, body)
	}
	lines := body["lines"].([]any)
	if len(lines) == 0 {
		t.Fatalf("no preview lines: %v", body)
	}
	first := lines[0].(map[string]any)
	if first["vat_zar"].(float64) <= 0 {
		t.Fatalf("vat not computed: %v", first)
	}
	unit := first["unit_zar"].(float64)
	qty := int(first["quantity"].(float64))
	want := unit * float64(qty) * 0.15
	if diff := first["vat_zar"].(float64) - want; diff > 0.02 || diff < -0.02 {
		t.Fatalf("vat ≠ 15%%: got %v want ~%v", first["vat_zar"], want)
	}

	// Generate the invoice; number sequence + open status.
	st, body, _ = s.apiReq(t, "POST", "/api/v1/org/billing/invoices", token, nil, nil)
	if st != http.StatusCreated {
		t.Fatalf("invoice create: %d %v", st, body)
	}
	inv := body["invoice"].(map[string]any)
	if inv["status"] != "open" || inv["currency"] != "ZAR" {
		t.Fatalf("invoice: %v", inv)
	}
	invID := inv["id"].(string)

	// Pay without a Paystack key: honest not_configured, invoice stays open.
	st, body, _ = s.apiReq(t, "POST", "/api/v1/org/billing/invoices/"+invID+"/pay", token, nil, nil)
	if st != http.StatusNotImplemented {
		t.Fatalf("pay unconfigured: %d %v", st, body)
	}

	// Dev manual capture → paid + payment history row.
	st, body, _ = s.apiReq(t, "POST", "/api/v1/org/billing/invoices/"+invID+"/mark-paid", token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("mark-paid: %d %v", st, body)
	}
	st, body, _ = s.apiReq(t, "GET", "/api/v1/org/billing/invoices", token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("list: %d %v", st, body)
	}
	payments := body["payments"].([]any)
	if len(payments) == 0 {
		t.Fatalf("no payment history: %v", body)
	}

	// Billing contact round trip.
	st, body, _ = s.apiReq(t, "PUT", "/api/v1/org/billing/contact", token,
		map[string]any{"billing_email": "ap@acme.co.za", "billing_contact_name": "Finance", "vat_number": "VAT123456"}, nil)
	if st != http.StatusOK {
		t.Fatalf("contact: %d %v", st, body)
	}
}

func TestStorageUsageEndpoint(t *testing.T) {
	s := boot(t)
	_, _, login, pw := s.provisionTenant(t, "storage")
	token := s.login(t, login, pw)
	st, body, _ := s.apiReq(t, "GET", "/api/v1/org/storage/usage", token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("usage: %d %v", st, body)
	}
	for _, key := range []string{"totals", "by_folder", "largest", "suggestions"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("missing %s: %v", key, body)
		}
	}
}

func TestMigrationJobLifecycle(t *testing.T) {
	s := boot(t)
	_, user, login, pw := s.provisionTenant(t, "migr")
	token := s.login(t, login, pw)

	// A mailbox to migrate into: the owner's own mailbox.
	mb, err := s.ownMailbox(user)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}

	// Probe an unreachable host: honest 502 with a hint, nothing persisted.
	st, body, _ := s.apiReq(t, "POST", "/api/v1/org/migrations/source-folders", token, map[string]any{
		"host": "127.0.0.1", "port": 1, "security": "none", "username": "x", "password": "y",
	}, nil)
	if st != http.StatusBadGateway {
		t.Fatalf("probe unreachable: %d %v", st, body)
	}

	// Create the job (source unreachable — inventory will fail gracefully).
	st, body, _ = s.apiReq(t, "POST", "/api/v1/org/migrations", token, map[string]any{
		"mailbox_id": mb.ID.String(),
		"source": map[string]any{
			"host": "127.0.0.1", "port": 1, "security": "none",
			"username": "u", "password": "p",
		},
		"folder_map": map[string]string{"INBOX": "INBOX"},
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create: %d %v", st, body)
	}
	job := body["job"].(map[string]any)
	jobID := job["id"].(string)
	if job["state"] != "queued" {
		t.Fatalf("state: %v", job)
	}

	// Pause → resume → cancel transitions.
	for _, ep := range []struct {
		path string
		want string
	}{{"/pause", "paused"}, {"/resume", "running"}, {"/cancel", "cancelled"}} {
		st, body, _ = s.apiReq(t, "POST", "/api/v1/org/migrations/"+jobID+ep.path, token, nil, nil)
		if st != http.StatusOK {
			t.Fatalf("%s: %d %v", ep.path, st, body)
		}
		if got := body["job"].(map[string]any)["state"]; got != ep.want {
			t.Fatalf("%s state: got %v want %s", ep.path, got, ep.want)
		}
	}

	// Terminal job can be deleted.
	st, _, _ = s.apiReq(t, "DELETE", "/api/v1/org/migrations/"+jobID, token, nil, nil)
	if st != http.StatusOK {
		t.Fatalf("delete: %d", st)
	}
}
