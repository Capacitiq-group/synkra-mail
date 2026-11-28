// domainreg.go — registrar-managed domain lifecycle (phase 11, migration 009).
// The registrar fields live beside the core domain row; this file owns them
// so the phase-8 domain code stays untouched.
package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RegistrarInfo is the reseller + registry view of a domain row.
type RegistrarInfo struct {
	Registrar         string          `json:"registrar"` // manual | oner
	RegistrarDomainID string          `json:"registrar_domain_id,omitempty"`
	RegistryStatus    string          `json:"registry_status"` // ok | pending_create | pending_update | client_hold | server_hold
	Lifecycle         string          `json:"lifecycle"`       // ok | add_grace | renew_grace | redemption | pending_delete | expired
	RegisteredAt      *time.Time      `json:"registered_at"`
	RenewsUntil       *time.Time      `json:"renews_until"`
	AutoRenew         bool            `json:"auto_renew"`
	TransferLock      bool            `json:"transfer_lock"`
	WhoisPrivacy      bool            `json:"whois_privacy"`
	EPPCodeLastAt     *time.Time      `json:"epp_code_last_at,omitempty"`
	Registrant        json.RawMessage `json:"registrant"`
	Nameservers       []string        `json:"nameservers"`
	ManagedMode       string          `json:"managed_mode"` // unmanaged | full | partial
	LastStatusSyncAt  *time.Time      `json:"last_status_sync_at,omitempty"`
}

const registrarSelect = `select registrar, coalesce(registrar_domain_id,''), registry_status, lifecycle_state,
	registered_at, renews_until, auto_renew, transfer_lock, whois_privacy, epp_code_last_at,
	coalesce(registrant,'null'::jsonb), coalesce(nameservers,'{}'::text[]), managed_mode, last_status_sync_at
	from mail.domain where id=$1`

func scanRegistrarInfo(row pgx.Row) (*RegistrarInfo, error) {
	var r RegistrarInfo
	var registrant json.RawMessage
	err := row.Scan(&r.Registrar, &r.RegistrarDomainID, &r.RegistryStatus, &r.Lifecycle,
		&r.RegisteredAt, &r.RenewsUntil, &r.AutoRenew, &r.TransferLock, &r.WhoisPrivacy, &r.EPPCodeLastAt,
		&registrant, &r.Nameservers, &r.ManagedMode, &r.LastStatusSyncAt)
	if err != nil {
		return nil, err
	}
	r.Registrant = registrant
	return &r, nil
}

func GetRegistrarInfo(ctx context.Context, q Querier, domainID uuid.UUID) (*RegistrarInfo, error) {
	return scanRegistrarInfo(q.QueryRow(ctx, registrarSelect, domainID))
}

// UpdateRegistrarInfo writes the columns a status sync or user action changes.
func UpdateRegistrarInfo(ctx context.Context, tx pgx.Tx, domainID uuid.UUID, patch *RegistrarInfo) error {
	const q = `update mail.domain set registrar=$2, registrar_domain_id=$3, registry_status=$4, lifecycle_state=$5,
		registered_at=$6, renews_until=$7, auto_renew=$8, transfer_lock=$9, whois_privacy=$10,
		registrant=$11, nameservers=$12, managed_mode=$13, last_status_sync_at=now(), updated_at=now()
		where id=$1`
	_, err := tx.Exec(ctx, q, domainID,
		patch.Registrar, patch.RegistrarDomainID, patch.RegistryStatus, patch.Lifecycle,
		patch.RegisteredAt, patch.RenewsUntil, patch.AutoRenew, patch.TransferLock, patch.WhoisPrivacy,
		patch.Registrant, patch.Nameservers, patch.ManagedMode)
	return err
}

func SetEPPRetrieved(ctx context.Context, tx pgx.Tx, domainID uuid.UUID) error {
	_, err := tx.Exec(ctx, `update mail.domain set epp_code_last_at=now(), updated_at=now() where id=$1`, domainID)
	return err
}

func SetNameserversDB(ctx context.Context, tx pgx.Tx, domainID uuid.UUID, ns []string, managedMode string) error {
	_, err := tx.Exec(ctx, `update mail.domain set nameservers=$2, managed_mode=$3, updated_at=now() where id=$1`,
		domainID, ns, managedMode)
	return err
}
