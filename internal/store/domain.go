package store

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"synkra.email/mail/internal/model"
)

// ---- domains ----

// ValidDomainName checks what we will actually publish and delegate for
// (audit item 11: "contains ." was the whole rule before). A lowercase FQDN,
// 1-253 octets, letter-leading labels of 1-63 octets, hyphens not at the
// edges, and a non-numeric TLD of at least two letters.
func ValidDomainName(name string) bool {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	if len(name) < 4 || len(name) > 253 || !strings.Contains(name, ".") {
		return false
	}
	labels := strings.Split(name, ".")
	top := len(labels) - 1
	for n, label := range labels {
		if label == "" || len(label) > 63 {
			return false
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		hasLetter := false
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z':
				hasLetter = true
			case r >= '0' && r <= '9':
			case r == '-':
			case r == '_':
				// Underscores appear in service labels (e.g. _dmarc) but not
				// in registrable domain names.
				if n != 0 {
					return false
				}
			default:
				return false
			}
		}
		if n == top && (!hasLetter || len(label) < 2) {
			return false
		}
	}
	return true
}

// GenerateDKIMKeyPair creates the RSA-2048 signing material (Section 8.1).
// Key generation is deliberately OUTSIDE any database transaction (audit item
// 11: an RSA-2048 key inside the caller's tx held a pool connection hostage).
// The caller seals the private PEM before it touches the database (audit
// item 4); CreateDomain stores whatever it is given.
func GenerateDKIMKeyPair() (privatePEM, publicTXT, selector string, err error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", "", err
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	pub, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return "", "", "", err
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	return string(privPEM), "v=DKIM1; k=rsa; p=" + pubB64, "sk1", nil
}

// CreateDomain provisions a domain row with caller-supplied DKIM material.
// Customer domains MUST pass sealed material ("enc1." prefix) — the platform
// seed may pass raw on first bootstrap (it is sealed on the next boot when a
// KMS key exists; dev runs honest-plaintext).
func CreateDomain(ctx context.Context, tx pgx.Tx, d *model.Domain) error {
	if !ValidDomainName(d.Name) {
		return ErrInvalidDomain
	}
	if d.DKIMPrivatePEM == "" {
		// Bootstrap path only (platform domain seed). Customer rows get keys
		// from the handler, generated outside the transaction and sealed.
		privPEM, pubTXT, selector, err := GenerateDKIMKeyPair()
		if err != nil {
			return err
		}
		d.DKIMPrivatePEM, d.DKIMPublicTXT, d.DKIMSelector = privPEM, pubTXT, selector
	}
	if d.DKIMSelector == "" {
		d.DKIMSelector = "sk1"
	}
	if d.OwnershipToken == "" {
		d.OwnershipToken = "synkra-verify-" + uuid.NewString()
	}
	if d.ExpiresAt == nil && !d.IsPlatform && d.Status != "verified" {
		// Squatting window (audit item 11): a pending registration that is
		// never verified releases the name.
		exp := time.Now().Add(30 * 24 * time.Hour)
		d.ExpiresAt = &exp
	}
	_, err := tx.Exec(ctx, `insert into mail.domain (id, org_id, name, is_platform, status, ownership_token,
			dkim_selector, dkim_private_pem, dkim_public_txt, expires_at, reverify_due_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		d.ID, optUUID(d.OrgID), d.Name, d.IsPlatform, d.Status, d.OwnershipToken,
		d.DKIMSelector, d.DKIMPrivatePEM, d.DKIMPublicTXT, optTimePtr(d.ExpiresAt), optTimePtr(d.ReverifyDueAt))
	return err
}

func domainSelect() string {
	return `select id, org_id, name, is_platform, status, ownership_token, dkim_selector,
		coalesce(dkim_private_pem,''), coalesce(dkim_public_txt,''), verified_at, created_at,
		coalesce(dkim_next_selector,''), coalesce(dkim_next_private_enc,''), coalesce(dkim_next_public_txt,''),
		reverify_due_at, expires_at
		from mail.domain`
}

func GetDomainByName(ctx context.Context, q Querier, name string) (*model.Domain, error) {
	// Expired rows may share a name with a live registration; prefer live.
	row := q.QueryRow(ctx, domainSelect()+` where name=$1 order by (status='expired') limit 1`, name)
	return scanDomain(row)
}

func GetDomain(ctx context.Context, q Querier, id uuid.UUID) (*model.Domain, error) {
	row := q.QueryRow(ctx, domainSelect()+` where id=$1`, id)
	return scanDomain(row)
}

// GetOrgIDByDomainName returns the org that owns a domain name (nil for the
// platform domain, which belongs to no org). Login uses this to resolve a
// shared login e-mail to the organisation that owns its domain (register §1.2).
func GetOrgIDByDomainName(ctx context.Context, q Querier, name string) (*uuid.UUID, error) {
	var orgID *uuid.UUID
	err := q.QueryRow(ctx, `select org_id from mail.domain where lower(name::text)=lower($1) and status <> 'expired'`, name).Scan(&orgID)
	if err != nil {
		return nil, notFound(err)
	}
	return orgID, nil
}

func ListDomainsByOrg(ctx context.Context, q Querier, orgID uuid.UUID) ([]model.Domain, error) {
	rows, err := q.Query(ctx, domainSelect()+` where org_id=$1 and status <> 'expired' order by created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Domain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func GetPlatformDomain(ctx context.Context, q Querier) (*model.Domain, error) {
	row := q.QueryRow(ctx, domainSelect()+` where is_platform=true limit 1`)
	return scanDomain(row)
}

func scanDomain(row pgx.Row) (*model.Domain, error) {
	var d model.Domain
	err := row.Scan(&d.ID, &d.OrgID, &d.Name, &d.IsPlatform, &d.Status, &d.OwnershipToken,
		&d.DKIMSelector, &d.DKIMPrivatePEM, &d.DKIMPublicTXT, &d.VerifiedAt, &d.CreatedAt,
		&d.NextSelector, &d.NextPrivateEnc, &d.NextPublicTXT, &d.ReverifyDueAt, &d.ExpiresAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &d, nil
}

func VerifyDomain(ctx context.Context, tx pgx.Tx, domainID uuid.UUID) error {
	ct, err := tx.Exec(ctx, `update mail.domain set status='verified', verified_at=now(),
		reverify_due_at = now() + interval '365 days', expires_at=null, updated_at=now() where id=$1`, domainID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func SetDomainStatus(ctx context.Context, tx pgx.Tx, domainID uuid.UUID, status string) error {
	_, err := tx.Exec(ctx, `update mail.domain set status=$2, updated_at=now() where id=$1`, domainID, status)
	return err
}

// DeleteDomain removes a domain the org no longer hosts. Refuses when
// addresses still route through it (decommission those first) — the delete
// endpoint was missing entirely (audit-sweep finding: domains could be
// registered but never decommissioned).
func DeleteDomain(ctx context.Context, tx pgx.Tx, domainID uuid.UUID) error {
	var n int
	if err := tx.QueryRow(ctx, `select count(*) from mail.address where domain_id=$1`, domainID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrDomainInUse
	}
	ct, err := tx.Exec(ctx, `delete from mail.domain where id=$1 and is_platform=false`, domainID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ErrDomainInUse: addresses still route through the domain.
var ErrDomainInUse = errors.New("domain still has addresses routed through it")

// CountDomainsByOrg: the per-org registration cap (audit item 11: an unlimited
// loop of RSA key generation inside requests is a CPU/pool DoS).
func CountDomainsByOrg(ctx context.Context, q Querier, orgID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from mail.domain where org_id=$1 and status <> 'expired'`, orgID).Scan(&n)
	return n, err
}

// ReleaseExpiredDomains marks stale unverified registrations expired and frees
// the names (audit item 11). Returns the released names for logging.
func ReleaseExpiredDomains(ctx context.Context, q Querier, now time.Time) ([]string, error) {
	rows, err := q.Query(ctx, `update mail.domain set status='expired', updated_at=now()
		where status='pending' and expires_at is not null and expires_at < $1
		returning name`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// FlagDomainsForReverify schedules the ownership re-check (audit item 11: a
// lapsed domain stays with its old owner forever otherwise). Verified domains
// past their reverify deadline flip to status 'reverify' — routing keeps
// working during the grace window; the janitor alerts and, after a further
// grace, the domain is disabled.
func FlagDomainsForReverify(ctx context.Context, q Querier, now time.Time) ([]string, error) {
	rows, err := q.Query(ctx, `update mail.domain set status='reverify', updated_at=now()
		where status='verified' and is_platform=false and verified_at is not null
		  and (reverify_due_at is null or reverify_due_at < $1)
		returning name`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// ---- DKIM rotation (audit item 4: no rotation existed) ----

// StageDKIMRotation stores the sealed next key + selector; the worker keeps
// signing with the active key until ActivateDKIMRotation.
func StageDKIMRotation(ctx context.Context, tx pgx.Tx, domainID uuid.UUID, selector, sealedPriv, publicTXT string) error {
	ct, err := tx.Exec(ctx, `update mail.domain set dkim_next_selector=$2, dkim_next_private_enc=$3,
		dkim_next_public_txt=$4, updated_at=now() where id=$1`,
		domainID, selector, sealedPriv, publicTXT)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ActivateDKIMRotation promotes the staged key. The caller has verified the
// new selector's TXT record resolves first, so signing never leads DNS.
func ActivateDKIMRotation(ctx context.Context, tx pgx.Tx, domainID uuid.UUID) error {
	ct, err := tx.Exec(ctx, `update mail.domain set
		dkim_selector = dkim_next_selector,
		dkim_private_pem = dkim_next_private_enc,
		dkim_public_txt = dkim_next_public_txt,
		dkim_next_selector = null, dkim_next_private_enc = null, dkim_next_public_txt = null,
		updated_at=now()
		where id=$1 and dkim_next_selector is not null`, domainID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ErrInvalidDomain: the name failed ValidDomainName.
var ErrInvalidDomain = errors.New("invalid domain name")

// EnsurePlatformDomain seeds the platform domain row (dev bootstrap; prod seed does this too).
func EnsurePlatformDomain(ctx context.Context, tx pgx.Tx, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `select id from mail.domain where name=$1`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	d := &model.Domain{ID: uuid.New(), Name: name, IsPlatform: true, Status: "verified"}
	if err := CreateDomain(ctx, tx, d); err != nil {
		return uuid.Nil, err
	}
	return d.ID, nil
}

// optTimePtr adapts a nullable timestamp for pgx.
func optTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}
