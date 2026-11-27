package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"synkra.email/mail/internal/model"
)

// ---- mailboxes ----

func CreateMailbox(ctx context.Context, tx pgx.Tx, m *model.Mailbox) error {
	_, err := tx.Exec(ctx, `insert into mail.mailbox (id, org_id, owner_user_id, kind, name)
                values ($1,$2,$3,$4,$5)`,
		m.ID, m.OrgID, optUUID(m.OwnerUserID), m.Kind, m.Name)
	if err != nil {
		return err
	}
	return CreateSystemFolders(ctx, tx, m.ID)
}

// CreateSystemFolders provisions INBOX, Sent, Drafts, Trash, Junk, Archive (Section 10.2).
func CreateSystemFolders(ctx context.Context, tx pgx.Tx, mailboxID uuid.UUID) error {
	systems := []struct{ name, special string }{
		{"INBOX", model.FolderInbox},
		{"Sent", model.FolderSent},
		{"Drafts", model.FolderDrafts},
		{"Trash", model.FolderTrash},
		{"Spam", model.FolderJunk},
		{"Archive", model.FolderArchive},
	}
	for _, sf := range systems {
		if _, err := tx.Exec(ctx, `insert into mail.folder (id, mailbox_id, name, special_use)
                        values ($1,$2,$3,$4)`, uuid.New(), mailboxID, sf.name, sf.special); err != nil {
			return err
		}
	}
	return nil
}

func GetMailbox(ctx context.Context, q Querier, id uuid.UUID) (*model.Mailbox, error) {
	row := q.QueryRow(ctx, `select id, org_id, owner_user_id, kind, name, send_status, coalesce(paused_reason,''), created_at
                from mail.mailbox where id=$1`, id)
	var m model.Mailbox
	err := row.Scan(&m.ID, &m.OrgID, &m.OwnerUserID, &m.Kind, &m.Name, &m.SendStatus, &m.PausedReason, &m.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &m, nil
}

func ListMailboxesByOrg(ctx context.Context, q Querier, orgID uuid.UUID) ([]model.Mailbox, error) {
	rows, err := q.Query(ctx, `select id, org_id, owner_user_id, kind, name, send_status, coalesce(paused_reason,''), created_at
                from mail.mailbox where org_id=$1 order by created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Mailbox
	for rows.Next() {
		var m model.Mailbox
		if err := rows.Scan(&m.ID, &m.OrgID, &m.OwnerUserID, &m.Kind, &m.Name, &m.SendStatus, &m.PausedReason, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func CountMailboxesByOrg(ctx context.Context, q Querier, orgID uuid.UUID, kind string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from mail.mailbox where org_id=$1 and kind=$2`, orgID, kind).Scan(&n)
	return n, err
}

func SetMailboxSendStatus(ctx context.Context, tx pgx.Tx, mailboxID uuid.UUID, status, reason string) error {
	_, err := tx.Exec(ctx, `update mail.mailbox set send_status=$2, paused_reason=$3, updated_at=now() where id=$1`,
		mailboxID, status, optText(reason))
	return err
}

// LockMailboxRow takes a row lock on the mailbox for the rest of the
// transaction. Hard bounces are counted inside the bouncing transaction, and
// under READ COMMITTED two workers bouncing for the same mailbox at once would
// each miss the other's uncommitted DSN and each see one attempt too few.
func LockMailboxRow(ctx context.Context, tx pgx.Tx, mailboxID uuid.UUID) error {
	_, err := tx.Exec(ctx, `select 1 from mail.mailbox where id=$1 for update`, mailboxID)
	return err
}

func DeleteMailbox(ctx context.Context, tx pgx.Tx, mailboxID uuid.UUID) error {
	// Hard delete is admin-only and audited (Section 11.1); ordinary deletion moves to Trash.
	_, err := tx.Exec(ctx, `delete from mail.mailbox where id=$1`, mailboxID)
	return err
}

// ---- addresses (routable addresses; aliases are address rows, not mailboxes — Section 4.1) ----

// reservedLocals: the RFC 2142 tier-0 role addresses (build register §1.5).
// They stay platform-operated: no organisation may register them on the
// platform domain, on any of its mail hosts or through the API.
var reservedLocals = map[string]struct{}{
	"postmaster": {}, "abuse": {}, "admin": {}, "hostmaster": {}, "webmaster": {}, "security": {},
}

// ErrReservedLocal is returned when an address claims one of those local parts
// on the platform domain.
var ErrReservedLocal = errors.New("local part is reserved on the platform domain")

func CreateAddress(ctx context.Context, tx pgx.Tx, a *model.Address) error {
	if a.Kind == "" {
		a.Kind = "user"
	}
	// Product invariant: a user may always send from their own primary address.
	// Aliases/forwards honour the explicit flag.
	if a.Kind == "user" {
		a.AllowSendAs = true
	}
	var isPlatform bool
	if err := tx.QueryRow(ctx, `select is_platform from mail.domain where id=$1`, a.DomainID).Scan(&isPlatform); err != nil {
		return notFound(err)
	}
	if isPlatform {
		if _, taken := reservedLocals[strings.ToLower(a.Local)]; taken {
			return ErrReservedLocal
		}
	}
	_, err := tx.Exec(ctx, `insert into mail.address (id, org_id, domain_id, mailbox_id, local, kind, forward_target, is_primary, allow_send_as)
                values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		a.ID, a.OrgID, a.DomainID, a.MailboxID, a.Local, a.Kind, optText(a.ForwardTarget), a.IsPrimary, a.AllowSendAs)
	return err
}

func GetAddressByID(ctx context.Context, q Querier, id uuid.UUID) (*model.Address, error) {
	row := q.QueryRow(ctx, `select id, org_id, domain_id, mailbox_id, local, kind, coalesce(forward_target,''), is_primary, allow_send_as, created_at
                from mail.address where id=$1`, id)
	var a model.Address
	err := row.Scan(&a.ID, &a.OrgID, &a.DomainID, &a.MailboxID, &a.Local, &a.Kind, &a.ForwardTarget, &a.IsPrimary, &a.AllowSendAs, &a.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &a, nil
}

// RouteAddress resolves where mail to local@domain must go. It is the routing
// engine's lookup (Section 5.1): hosted domain + known local part, or nothing.
func RouteAddress(ctx context.Context, q Querier, domain, local string) (*model.Address, *model.Domain, error) {
	d, err := GetDomainByName(ctx, q, domain)
	if err != nil {
		return nil, nil, err // includes ErrNotFound → 554 not-hosted
	}
	if d.Status != "verified" {
		return nil, nil, fmt.Errorf("domain %s not active: %w", domain, ErrNotFound)
	}
	row := q.QueryRow(ctx, `select id, org_id, domain_id, mailbox_id, local, kind, coalesce(forward_target,''), is_primary, allow_send_as, created_at
                from mail.address where domain_id=$1 and local=$2`, d.ID, local)
	var a model.Address
	if err := row.Scan(&a.ID, &a.OrgID, &a.DomainID, &a.MailboxID, &a.Local, &a.Kind, &a.ForwardTarget, &a.IsPrimary, &a.AllowSendAs, &a.CreatedAt); err != nil {
		return nil, nil, notFound(err)
	}
	return &a, d, nil
}

func ListAddressesByOrg(ctx context.Context, q Querier, orgID uuid.UUID) ([]model.Address, error) {
	rows, err := q.Query(ctx, `select id, org_id, domain_id, mailbox_id, local, kind, coalesce(forward_target,''), is_primary, allow_send_as, created_at
                from mail.address where org_id=$1 order by created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Address
	for rows.Next() {
		var a model.Address
		if err := rows.Scan(&a.ID, &a.OrgID, &a.DomainID, &a.MailboxID, &a.Local, &a.Kind, &a.ForwardTarget, &a.IsPrimary, &a.AllowSendAs, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func CountAddressesByOrg(ctx context.Context, q Querier, orgID uuid.UUID, kind string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from mail.address where org_id=$1 and kind=$2`, orgID, kind).Scan(&n)
	return n, err
}

func DeleteAddress(ctx context.Context, tx pgx.Tx, id, orgID uuid.UUID) error {
	ct, err := tx.Exec(ctx, `delete from mail.address where id=$1 and org_id=$2`, id, orgID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SendAsOK: user may send from this address if it belongs to their own mailbox,
// a team/shared mailbox they are member+ of, or an alias routed to such a mailbox (Section 7.4).
func SendAsOK(ctx context.Context, q Querier, userID uuid.UUID, local, domain string) (bool, error) {
	var n int
	err := q.QueryRow(ctx, `
                select count(*) from mail.address a
                join mail.domain d on d.id = a.domain_id
                join mail.mailbox mb on mb.id = a.mailbox_id
                left join mail.mailbox_member mm on mm.mailbox_id = mb.id and mm.user_id = $1 and mm.role in ('owner','member')
                join mail.user u on u.id = $1
                where lower(d.name) = lower($2) and lower(a.local) = lower($3)
                  and (mb.owner_user_id = $1 or mm.id is not null)
                  and a.allow_send_as`, userID, domain, local).Scan(&n)
	return n > 0, err
}

// ---- mailbox members (shared/team inboxes) ----

func AddMember(ctx context.Context, tx pgx.Tx, m *model.MailboxMember) error {
	_, err := tx.Exec(ctx, `insert into mail.mailbox_member (id, mailbox_id, user_id, role) values ($1,$2,$3,$4)
                on conflict (mailbox_id, user_id) do update set role=excluded.role`,
		m.ID, m.MailboxID, m.UserID, m.Role)
	return err
}

func RemoveMember(ctx context.Context, tx pgx.Tx, mailboxID, userID uuid.UUID) error {
	_, err := tx.Exec(ctx, `delete from mail.mailbox_member where mailbox_id=$1 and user_id=$2`, mailboxID, userID)
	return err
}

func MailboxesForUser(ctx context.Context, q Querier, userID uuid.UUID) ([]model.Mailbox, error) {
	rows, err := q.Query(ctx, `
                select mb.id, mb.org_id, mb.owner_user_id, mb.kind, mb.name, mb.send_status, coalesce(mb.paused_reason,''), mb.created_at
                from mail.mailbox mb
                where mb.owner_user_id = $1
                   or mb.id in (select mailbox_id from mail.mailbox_member where user_id = $1)
                order by mb.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Mailbox
	for rows.Next() {
		var m model.Mailbox
		if err := rows.Scan(&m.ID, &m.OrgID, &m.OwnerUserID, &m.Kind, &m.Name, &m.SendStatus, &m.PausedReason, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MailboxAccess: can the user read this mailbox? (own, or any membership role)
func MailboxAccess(ctx context.Context, q Querier, userID, mailboxID uuid.UUID) (bool, error) {
	var n int
	err := q.QueryRow(ctx, `
                select count(*) from mail.mailbox mb
                where mb.id=$2 and (mb.owner_user_id=$1
                  or exists (select 1 from mail.mailbox_member mm where mm.mailbox_id=mb.id and mm.user_id=$1))`,
		userID, mailboxID).Scan(&n)
	return n > 0, err
}

// MailboxMemberRole returns the user's role on a mailbox ("" when none).
func MailboxMemberRole(ctx context.Context, q Querier, userID, mailboxID uuid.UUID) (string, error) {
	var role string
	var ownerID *uuid.UUID
	err := q.QueryRow(ctx, `select owner_user_id from mail.mailbox where id=$2`, userID, mailboxID).Scan(&ownerID)
	if err == nil && ownerID != nil && *ownerID == userID {
		return "owner", nil
	}
	err = q.QueryRow(ctx, `select role from mail.mailbox_member where mailbox_id=$2 and user_id=$1`, userID, mailboxID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// ---- folders ----

func CreateFolder(ctx context.Context, tx pgx.Tx, f *model.Folder) error {
	_, err := tx.Exec(ctx, `insert into mail.folder (id, mailbox_id, name, special_use) values ($1,$2,$3,$4)`,
		f.ID, f.MailboxID, f.Name, optText(f.SpecialUse))
	return err
}

func GetFolder(ctx context.Context, q Querier, id uuid.UUID) (*model.Folder, error) {
	row := q.QueryRow(ctx, `select id, mailbox_id, name, coalesce(special_use,''), uid_validity, uid_next,
                total_messages, unread, subscribed, created_at from mail.folder where id=$1`, id)
	return scanFolder(row)
}

func GetFolderByName(ctx context.Context, q Querier, mailboxID uuid.UUID, name string) (*model.Folder, error) {
	row := q.QueryRow(ctx, `select id, mailbox_id, name, coalesce(special_use,''), uid_validity, uid_next,
                total_messages, unread, subscribed, created_at from mail.folder where mailbox_id=$1 and name=$2`, mailboxID, name)
	return scanFolder(row)
}

func GetFolderBySpecialUse(ctx context.Context, q Querier, mailboxID uuid.UUID, specialUse string) (*model.Folder, error) {
	row := q.QueryRow(ctx, `select id, mailbox_id, name, coalesce(special_use,''), uid_validity, uid_next,
                total_messages, unread, subscribed, created_at from mail.folder where mailbox_id=$1 and special_use=$2`, mailboxID, specialUse)
	return scanFolder(row)
}

func ListFolders(ctx context.Context, q Querier, mailboxID uuid.UUID) ([]model.Folder, error) {
	rows, err := q.Query(ctx, `select id, mailbox_id, name, coalesce(special_use,''), uid_validity, uid_next,
                total_messages, unread, subscribed, created_at from mail.folder where mailbox_id=$1 order by created_at`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Folder
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func scanFolder(row pgx.Row) (*model.Folder, error) {
	var f model.Folder
	err := row.Scan(&f.ID, &f.MailboxID, &f.Name, &f.SpecialUse, &f.UIDValidity, &f.UIDNext,
		&f.TotalMessages, &f.Unread, &f.Subscribed, &f.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &f, nil
}

func DeleteFolder(ctx context.Context, tx pgx.Tx, folderID uuid.UUID) error {
	var special string
	err := tx.QueryRow(ctx, `select coalesce(special_use,'') from mail.folder where id=$1`, folderID).Scan(&special)
	if err != nil {
		return notFound(err)
	}
	if special != "" {
		return errors.New("system folders cannot be deleted")
	}
	_, err = tx.Exec(ctx, `delete from mail.folder where id=$1`, folderID)
	return err
}

func RenameFolder(ctx context.Context, tx pgx.Tx, folderID uuid.UUID, name string) error {
	var special string
	err := tx.QueryRow(ctx, `select coalesce(special_use,'') from mail.folder where id=$1`, folderID).Scan(&special)
	if err != nil {
		return notFound(err)
	}
	if special != "" {
		return errors.New("system folders cannot be renamed")
	}
	_, err = tx.Exec(ctx, `update mail.folder set name=$2 where id=$1`, folderID, name)
	return err
}

func SetFolderSubscribed(ctx context.Context, tx pgx.Tx, folderID uuid.UUID, subscribed bool) error {
	_, err := tx.Exec(ctx, `update mail.folder set subscribed=$2 where id=$1`, folderID, subscribed)
	return err
}
