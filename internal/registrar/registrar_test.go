package registrar

import (
	"context"
	"testing"
	"time"
)

// The lifecycle derivation drives the customer-facing banner; the registry
// windows for .ZA are: 30 days redemption, then pending-delete.
func TestDeriveLifecycle(t *testing.T) {
	now := time.Now().UTC()
	future := now.AddDate(0, 0, 30)
	if got := DeriveLifecycle(now, &future, "ok"); got != "ok" {
		t.Fatalf("future expiry: got %s want ok", got)
	}
	past := now.AddDate(0, 0, -10)
	if got := DeriveLifecycle(now, &past, "ok"); got != "redemption" {
		t.Fatalf("10d past: got %s want redemption", got)
	}
	past60 := now.AddDate(0, 0, -45)
	if got := DeriveLifecycle(now, &past60, "ok"); got != "pending_delete" {
		t.Fatalf("45d past: got %s want pending_delete", got)
	}
	past90 := now.AddDate(0, 0, -90)
	if got := DeriveLifecycle(now, &past90, "ok"); got != "expired" {
		t.Fatalf("90d past: got %s want expired", got)
	}
	if got := DeriveLifecycle(now, &future, "client_hold"); got != "expired" {
		t.Fatalf("client_hold: got %s want expired", got)
	}
}

// Dry-run: the no-credential registrar used by dev/demo/tests.
func TestDryRunRegisterAndCheck(t *testing.T) {
	ctx := context.Background()
	var r Registrar = DryRun{}
	checks, err := r.CheckAvailable(ctx, []string{"acme.co.za", "taken.co.za"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(checks) != 2 || !checks[0].Available || checks[1].Available {
		t.Fatalf("check results: %+v", checks)
	}
	info, err := r.Register(ctx, "acme.co.za", 1, RegistrantContact{
		FirstName: "T", LastName: "M", Email: "t@acme.co.za", CountryCode: "ZA",
	}, []string{"ns1.synkra.email", "ns2.synkra.email"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if info.RegistryStatus != "pending_create" || info.Lifecycle != "add_grace" {
		t.Fatalf("registered state: %+v", info)
	}
	if _, err := r.Register(ctx, "acme.co.za", 0, RegistrantContact{Email: "t@acme.co.za"}, nil); err == nil {
		t.Fatal("years=0 must be refused")
	}
	if _, err := r.Register(ctx, "acme.co.za", 1, RegistrantContact{}, nil); err == nil {
		t.Fatal("missing registrant email must be refused")
	}
	code, err := r.EPPCode(ctx, "acme.co.za")
	if err != nil || code == "" {
		t.Fatalf("epp: %v %q", err, code)
	}
}
