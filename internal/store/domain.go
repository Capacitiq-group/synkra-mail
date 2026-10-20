package store

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"synkra.email/mail/internal/model"
)

// ---- domains ----

// CreateDomain provisions a domain row; DKIM keys (RSA 2048, selector sk1, Section 8.1)
// are generated here and stored on the row. Rotation uses sk2 + a new row key pair later.
func CreateDomain(ctx context.Context, tx pgx.Tx, d *model.Domain) error {
	if d.DKIMSelector == "" {
		d.DKIMSelector = "sk1"
	}
	if d.DKIMPrivatePEM == "" {
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return err
		}
		privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
		pub, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
		if err != nil {
			return err
		}
		pubB64 := base64.StdEncoding.EncodeToString(pub)
		d.DKIMPrivatePEM = string(privPEM)
		d.DKIMPublicTXT = "v=DKIM1; k=rsa; p=" + pubB64
	}
	d.OwnershipToken = "synkra-verify-" + uuid.NewString()
	_, err := tx.Exec(ctx, `insert into mail.domain (id, org_id, name, is_platform, status, ownership_token, dkim_selector, dkim_private_pem, dkim_public_txt)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		d.ID, optUUID(d.OrgID), d.Name, d.IsPlatform, d.Status, d.OwnershipToken, d.DKIMSelector, d.DKIMPrivatePEM, d.DKIMPublicTXT)
	return err
}

func GetDomainByName(ctx context.Context, q Querier, name string) (*model.Domain, error) {
	row := q.QueryRow(ctx, `select id, org_id, name, is_platform, status, ownership_token, dkim_selector,
		coalesce(dkim_private_pem,''), coalesce(dkim_public_txt,''), verified_at, created_at
		from mail.domain where name=$1`, name)
	return scanDomain(row)
}

func GetDomain(ctx context.Context, q Querier, id uuid.UUID) (*model.Domain, error) {
	row := q.QueryRow(ctx, `select id, org_id, name, is_platform, status, ownership_token, dkim_selector,
		coalesce(dkim_private_pem,''), coalesce(dkim_public_txt,''), verified_at, created_at
		from mail.domain where id=$1`, id)
	return scanDomain(row)
}

func ListDomainsByOrg(ctx context.Context, q Querier, orgID uuid.UUID) ([]model.Domain, error) {
	rows, err := q.Query(ctx, `select id, org_id, name, is_platform, status, ownership_token, dkim_selector,
		coalesce(dkim_private_pem,''), coalesce(dkim_public_txt,''), verified_at, created_at
		from mail.domain where org_id=$1 order by created_at`, orgID)
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
	row := q.QueryRow(ctx, `select id, org_id, name, is_platform, status, ownership_token, dkim_selector,
		coalesce(dkim_private_pem,''), coalesce(dkim_public_txt,''), verified_at, created_at
		from mail.domain where is_platform=true limit 1`)
	return scanDomain(row)
}

func scanDomain(row pgx.Row) (*model.Domain, error) {
	var d model.Domain
	err := row.Scan(&d.ID, &d.OrgID, &d.Name, &d.IsPlatform, &d.Status, &d.OwnershipToken,
		&d.DKIMSelector, &d.DKIMPrivatePEM, &d.DKIMPublicTXT, &d.VerifiedAt, &d.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &d, nil
}

func VerifyDomain(ctx context.Context, tx pgx.Tx, domainID uuid.UUID) error {
	ct, err := tx.Exec(ctx, `update mail.domain set status='verified', verified_at=now(), updated_at=now() where id=$1`, domainID)
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
