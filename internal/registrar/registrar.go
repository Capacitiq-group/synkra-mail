// Package registrar: domain-lifecycle operations against a reseller API.
//
// Synkra resells domains through Oner Hosting (Onerserv (Pty) Ltd, a ZARC —
// the ZA Central Registry — accredited registrar): https://oner.co.za/.
// Their reseller API (https://docs.oner.co.za) exposes "core domain lifecycle
// operations across supported TLDs". This package is the single adapter:
// everything domain-related above the API (handlers, the wizard, lifecycle
// banners) speaks these interfaces; only oner.go knows the wire details.
//
// UNVERIFIED WIRE CONTRACT. The docs site was unreachable from the build
// sandbox while this integration was written (connection timeout), so the
// endpoint paths and response shapes in oner.go are modelled on the documented
// operation set and standard registrar-API practice — they are marked
// TODO(oner-verify) and are the ONLY place to touch once the live docs are
// reachable. With no API key configured the client runs in dry-run mode:
// calls are logged and simulated successes returned, so the whole domain
// workflow is demoable and testable without touching a real registry. Dry-run
// is refused in prod config.
package registrar

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrNotConfigured = errors.New("registrar not configured")
	ErrNotSupported  = errors.New("operation not supported by this TLD")
	ErrUnavailable   = errors.New("registrar unavailable")
)

// RegistrantContact is the registry-record holder of the domain (WHOIS).
type RegistrantContact struct {
	Organisation string `json:"organisation,omitempty"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Email        string `json:"email"`
	Phone        string `json:"phone"` // E.164, e.g. +27.821234567
	Street       string `json:"street"`
	City         string `json:"city"`
	Province     string `json:"province,omitempty"`
	PostalCode   string `json:"postal_code"`
	CountryCode  string `json:"country_code"` // ISO 3166-1 alpha-2, e.g. ZA
}

// DomainInfo is the registrar's view of a domain — the source of truth for
// registry status and the lifecycle (grace/redemption) windows.
type DomainInfo struct {
	Name           string
	RegistryStatus string // ok | pending_create | pending_update | client_hold | server_hold
	Lifecycle      string // ok | add_grace | renew_grace | redemption | pending_delete | expired
	RegisteredAt   *time.Time
	RenewsUntil    *time.Time
	Nameservers    []string
	TransferLock   bool
	WhoisPrivacy   bool
	AutoRenew      bool
}

// Check reports whether a name is available for registration.
type Check struct {
	Name      string
	Available bool
	PriceZAR  float64 // per year, ex VAT
}

// Registrar is the reseller operation set. Implementations: DryRun (dev/demo
// + tests) and OnerHTTP (production wire driver).
type Registrar interface {
	CheckAvailable(ctx context.Context, names []string) ([]Check, error)
	Register(ctx context.Context, name string, years int, registrant RegistrantContact, nameservers []string) (*DomainInfo, error)
	Info(ctx context.Context, name string) (*DomainInfo, error)
	SetNameservers(ctx context.Context, name string, ns []string) error
	SetTransferLock(ctx context.Context, name string, enabled bool) error
	SetWhoisPrivacy(ctx context.Context, name string, enabled bool) error
	SetAutoRenew(ctx context.Context, name string, enabled bool) error
	SetRegistrant(ctx context.Context, name string, registrant RegistrantContact) error
	Renew(ctx context.Context, name string, years int) (*time.Time, error)
	// EPPCode returns the transfer authorisation code. Registrars rate-limit
	// and often require authenticated requests — the caller (handler) enforces
	// our own step-up (password re-confirm) before ever calling this.
	EPPCode(ctx context.Context, name string) (string, error)
	// TransferOut begins moving the domain away with the given EPP code.
	TransferOut(ctx context.Context, name, eppCode string) error
}

// DeriveLifecycle maps registry expiry + EPP status codes to the customer
// lifecycle banner. Pure function — tested.
func DeriveLifecycle(now time.Time, renewsUntil *time.Time, registryStatus string) string {
	if registryStatus == "client_hold" || registryStatus == "server_hold" {
		return "expired"
	}
	if renewsUntil == nil {
		return "ok"
	}
	switch {
	case now.Before(*renewsUntil):
		return "ok"
	case now.Before(renewsUntil.Add(30 * 24 * time.Hour)):
		// .ZA redemption-grace window
		return "redemption"
	case now.Before(renewsUntil.Add(60 * 24 * time.Hour)):
		return "pending_delete"
	default:
		return "expired"
	}
}

// GraceWindow returns a human line for a lifecycle state.
func GraceWindow(state string) string {
	switch state {
	case "add_grace":
		return "Add grace period — deleting now refunds the registration fee."
	case "renew_grace":
		return "Renew grace period — the renewal just settled."
	case "redemption":
		return "Redemption period — the domain has lapsed; restoration carries a registry fee. Act within 30 days."
	case "pending_delete":
		return "Pending delete — the name drops from the registry soon and can then be re-registered by anyone."
	case "expired":
		return "Expired — mail flow is at risk. Renew immediately to restore."
	default:
		return ""
	}
}

// DryRun is the no-credential registrar: deterministic simulated results so
// the workflow (wizard → ZACR pending → verify → lifecycle) runs end to end
// in dev, demos and tests.
type DryRun struct{}

func (DryRun) CheckAvailable(_ context.Context, names []string) ([]Check, error) {
	out := make([]Check, 0, len(names))
	for _, n := range names {
		// deterministic: names containing "taken" are unavailable
		avail := !contains(n, "taken")
		out = append(out, Check{Name: n, Available: avail, PriceZAR: 99})
	}
	return out, nil
}

func (DryRun) Register(_ context.Context, name string, years int, registrant RegistrantContact, nameservers []string) (*DomainInfo, error) {
	if years < 1 {
		return nil, fmt.Errorf("%w: years", ErrUnavailable)
	}
	if registrant.Email == "" {
		return nil, fmt.Errorf("%w: registrant", ErrUnavailable)
	}
	reg := time.Now().UTC()
	until := reg.AddDate(years, 0, 0)
	return &DomainInfo{
		Name:           name,
		RegistryStatus: "pending_create", // ZARC EPP create in flight
		Lifecycle:      "add_grace",
		RegisteredAt:   &reg,
		RenewsUntil:    &until,
		Nameservers:    nameservers,
		AutoRenew:      true,
	}, nil
}

func (DryRun) Info(_ context.Context, name string) (*DomainInfo, error) {
	reg := time.Now().AddDate(-1, 0, 0)
	until := reg.AddDate(1, 0, 0)
	return &DomainInfo{Name: name, RegistryStatus: "ok", Lifecycle: "ok", RegisteredAt: &reg, RenewsUntil: &until,
		Nameservers: []string{"ns1." + name, "ns2." + name}, TransferLock: true}, nil
}
func (DryRun) SetNameservers(_ context.Context, _ string, _ []string) error { return nil }
func (DryRun) SetTransferLock(_ context.Context, _ string, _ bool) error    { return nil }
func (DryRun) SetWhoisPrivacy(_ context.Context, _ string, _ bool) error    { return nil }
func (DryRun) SetAutoRenew(_ context.Context, _ string, _ bool) error       { return nil }
func (DryRun) SetRegistrant(_ context.Context, _ string, _ RegistrantContact) error {
	return nil
}
func (DryRun) Renew(_ context.Context, _ string, years int) (*time.Time, error) {
	until := time.Now().AddDate(years, 0, 0)
	return &until, nil
}
func (DryRun) EPPCode(_ context.Context, _ string) (string, error) {
	return "DRYRUN-EPP-" + time.Now().UTC().Format("20060102"), nil
}
func (DryRun) TransferOut(_ context.Context, _ string, _ string) error { return nil }

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
