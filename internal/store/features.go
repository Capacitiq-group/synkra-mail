package store

// Feature + integration surfaces for the register's §12/§13 endpoints:
// signatures, autoresponders, inbound routing rules, webhooks and API keys.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---- signatures (register §12b) ----

type Signature struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Name      string
	HTML      string
	Text      string
	IsDefault bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

func ListSignatures(ctx context.Context, q Querier, userID uuid.UUID) ([]Signature, error) {
	rows, err := q.Query(ctx, `select id, user_id, name, html, text, is_default, created_at, updated_at
		from mail.signature where user_id=$1 order by created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Signature
	for rows.Next() {
		var s Signature
		if err := rows.Scan(&s.ID, &s.UserID, &s.Name, &s.HTML, &s.Text, &s.IsDefault, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func CreateSignature(ctx context.Context, tx pgx.Tx, s *Signature) error {
	if s.IsDefault {
		// One default per user: clear the rest inside the same transaction.
		if _, err := tx.Exec(ctx, `update mail.signature set is_default=false where user_id=$1`, s.UserID); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `insert into mail.signature (id, user_id, name, html, text, is_default)
		values ($1,$2,$3,$4,$5,$6)`,
		s.ID, s.UserID, s.Name, s.HTML, s.Text, s.IsDefault)
	return err
}

func DeleteSignature(ctx context.Context, tx pgx.Tx, id, userID uuid.UUID) error {
	ct, err := tx.Exec(ctx, `delete from mail.signature where id=$1 and user_id=$2`, id, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- autoresponder (register §12c) ----

type Autoresponder struct {
	ID        uuid.UUID
	MailboxID uuid.UUID
	Enabled   bool
	Subject   string
	Body      string
	StartsAt  *time.Time
	EndsAt    *time.Time
	UpdatedAt time.Time
}

func GetAutoresponder(ctx context.Context, q Querier, mailboxID uuid.UUID) (*Autoresponder, error) {
	row := q.QueryRow(ctx, `select id, mailbox_id, enabled, subject, body, starts_at, ends_at, updated_at
		from mail.autoresponder where mailbox_id=$1`, mailboxID)
	var a Autoresponder
	err := row.Scan(&a.ID, &a.MailboxID, &a.Enabled, &a.Subject, &a.Body, &a.StartsAt, &a.EndsAt, &a.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &a, nil
}

func UpsertAutoresponder(ctx context.Context, tx pgx.Tx, a *Autoresponder) error {
	_, err := tx.Exec(ctx, `insert into mail.autoresponder (id, mailbox_id, enabled, subject, body, starts_at, ends_at, updated_at)
		values ($1,$2,$3,$4,$5,$6,$7,now())
		on conflict (mailbox_id) do update set enabled=$3, subject=$4, body=$5, starts_at=$6, ends_at=$7, updated_at=now()`,
		a.ID, a.MailboxID, a.Enabled, a.Subject, a.Body, a.StartsAt, a.EndsAt)
	return err
}

// ShouldAutoReply applies the window and the loop guards (register §12c): the
// sender must not be a daemon path, and one reply per sender per mailbox per day.
func ShouldAutoReply(ctx context.Context, q Querier, mailboxID uuid.UUID, now time.Time, envFrom string) (bool, error) {
	a, err := GetAutoresponder(ctx, q, mailboxID)
	if err != nil {
		return false, nil // none configured
	}
	if !a.Enabled {
		return false, nil
	}
	if a.StartsAt != nil && now.Before(*a.StartsAt) {
		return false, nil
	}
	if a.EndsAt != nil && now.After(*a.EndsAt) {
		return false, nil
	}
	var sent bool
	err = q.QueryRow(ctx, `select true from mail.autoresponder_log
		where mailbox_id=$1 and sender=$2 and reply_date = ($3::timestamptz)::date`,
		mailboxID, envFrom, now).Scan(&sent)
	if err == nil && sent {
		return false, nil
	}
	return true, nil
}

// RecordAutoReply claims the one-reply-per-sender-per-day slot; a concurrent
// delivery that claims first wins (on conflict do nothing).
func RecordAutoReply(ctx context.Context, tx pgx.Tx, mailboxID uuid.UUID, sender string) error {
	_, err := tx.Exec(ctx, `insert into mail.autoresponder_log (mailbox_id, sender) values ($1,$2)
		on conflict (mailbox_id, sender, reply_date) do nothing`, mailboxID, sender)
	return err
}

// ---- inbound routing rules (register §12d) ----

type MailboxRule struct {
	ID              uuid.UUID
	MailboxID       uuid.UUID
	Position        int
	Enabled         bool
	MatchFrom       string
	MatchSubject    string
	ActionFolderID  *uuid.UUID
	ActionMarkRead  bool
	CreatedAt       time.Time
}

func ListRules(ctx context.Context, q Querier, mailboxID uuid.UUID) ([]MailboxRule, error) {
	rows, err := q.Query(ctx, `select id, mailbox_id, position, enabled, match_from, match_subject, action_folder_id, action_mark_read, created_at
		from mail.mailbox_rule where mailbox_id=$1 order by position, created_at`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MailboxRule
	for rows.Next() {
		var r MailboxRule
		if err := rows.Scan(&r.ID, &r.MailboxID, &r.Position, &r.Enabled, &r.MatchFrom, &r.MatchSubject, &r.ActionFolderID, &r.ActionMarkRead, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func CreateRule(ctx context.Context, tx pgx.Tx, r *MailboxRule) error {
	_, err := tx.Exec(ctx, `insert into mail.mailbox_rule (id, mailbox_id, position, enabled, match_from, match_subject, action_folder_id, action_mark_read)
		values ($1,$2,$3,$4,$5,$6,$7,$8)`,
		r.ID, r.MailboxID, r.Position, r.Enabled, r.MatchFrom, r.MatchSubject, optUUID(r.ActionFolderID), r.ActionMarkRead)
	return err
}

func DeleteRule(ctx context.Context, tx pgx.Tx, id, mailboxID uuid.UUID) error {
	ct, err := tx.Exec(ctx, `delete from mail.mailbox_rule where id=$1 and mailbox_id=$2`, id, mailboxID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FirstMatchingRule: position order, first match wins; at least one non-empty
// condition must match for the rule to fire.
func FirstMatchingRule(ctx context.Context, q Querier, mailboxID uuid.UUID, fromEmail, subject string) (*MailboxRule, error) {
	rules, err := ListRules(ctx, q, mailboxID)
	if err != nil {
		return nil, err
	}
	for i := range rules {
		r := &rules[i]
		if !r.Enabled {
			continue
		}
		matched := false
		if r.MatchFrom != "" && containsFold(fromEmail, r.MatchFrom) {
			matched = true
		}
		if r.MatchSubject != "" && containsFold(subject, r.MatchSubject) {
			matched = true
		}
		if matched {
			return r, nil
		}
	}
	return nil, nil
}

func containsFold(haystack, needle string) bool {
	n := len(needle)
	if n == 0 {
		return false
	}
	for i := 0; i+n <= len(haystack); i++ {
		if equalFold(haystack[i:i+n], needle) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// ---- webhooks (register §13f) ----

type Webhook struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	URL       string
	Secret    string
	Events    []string
	Active    bool
	CreatedAt time.Time
}

func ListWebhooks(ctx context.Context, q Querier, orgID uuid.UUID) ([]Webhook, error) {
	rows, err := q.Query(ctx, `select id, org_id, url, secret, events, active, created_at
		from mail.webhook where org_id=$1 order by created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Webhook
	for rows.Next() {
		var w Webhook
		if err := rows.Scan(&w.ID, &w.OrgID, &w.URL, &w.Secret, &w.Events, &w.Active, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func CreateWebhook(ctx context.Context, tx pgx.Tx, w *Webhook) error {
	_, err := tx.Exec(ctx, `insert into mail.webhook (id, org_id, url, secret, events, active)
		values ($1,$2,$3,$4,$5,$6)`,
		w.ID, w.OrgID, w.URL, w.Secret, w.Events, w.Active)
	return err
}

func DeleteWebhook(ctx context.Context, tx pgx.Tx, id, orgID uuid.UUID) error {
	ct, err := tx.Exec(ctx, `delete from mail.webhook where id=$1 and org_id=$2`, id, orgID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ActiveWebhooksForEvent lists the org's active webhooks subscribed to a state.
func ActiveWebhooksForEvent(ctx context.Context, q Querier, orgID uuid.UUID, state string) ([]Webhook, error) {
	rows, err := q.Query(ctx, `select id, org_id, url, secret, events, active, created_at
		from mail.webhook where org_id=$1 and active and $2 = any(events)`, orgID, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Webhook
	for rows.Next() {
		var w Webhook
		if err := rows.Scan(&w.ID, &w.OrgID, &w.URL, &w.Secret, &w.Events, &w.Active, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

type WebhookDelivery struct {
	ID        uuid.UUID
	WebhookID uuid.UUID
	EventID   uuid.UUID
	Payload   []byte
	Status    string
	Attempts  int
	LastError string
	NextAt    time.Time
	CreatedAt time.Time
}

func QueueWebhookDelivery(ctx context.Context, tx pgx.Tx, wd *WebhookDelivery) error {
	_, err := tx.Exec(ctx, `insert into mail.webhook_delivery (id, webhook_id, event_id, payload)
		values ($1,$2,$3,$4)`, wd.ID, wd.WebhookID, wd.EventID, wd.Payload)
	return err
}

func ClaimDueWebhookDeliveries(ctx context.Context, q Querier, limit int) ([]WebhookDelivery, error) {
	rows, err := q.Query(ctx, `update mail.webhook_delivery wd
		set status='in_flight', attempts = wd.attempts + 1
		where wd.id in (select id from mail.webhook_delivery
			where status='queued' and next_at < now() order by created_at limit $1)
		returning id, webhook_id, event_id, payload, status, attempts, coalesce(last_error,''), next_at, created_at`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookDelivery
	for rows.Next() {
		var wd WebhookDelivery
		if err := rows.Scan(&wd.ID, &wd.WebhookID, &wd.EventID, &wd.Payload, &wd.Status, &wd.Attempts, &wd.LastError, &wd.NextAt, &wd.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, wd)
	}
	return out, rows.Err()
}

func SetWebhookDeliveryOutcome(ctx context.Context, tx pgx.Tx, id uuid.UUID, delivered bool, lastErr string) error {
	if delivered {
		_, err := tx.Exec(ctx, `update mail.webhook_delivery set status='delivered', last_error='' where id=$1`, id)
		return err
	}
	// Retry with backoff: 1m * attempts, give up after 5 attempts (register
	// §13f: webhooks are best-effort with retries, never blocking delivery).
	_, err := tx.Exec(ctx, `update mail.webhook_delivery
		set status = case when attempts >= 5 then 'failed' else 'queued' end,
		    last_error=$2,
		    next_at = now() + make_interval(secs => 60 * attempts)
		where id=$1`, id, lastErr)
	return err
}

// ---- API keys (register §13f: the API is the product) ----

type APIKey struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	CreatedBy  uuid.UUID
	Name       string
	TokenHash  string
	Prefix     string
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

func ListAPIKeys(ctx context.Context, q Querier, orgID uuid.UUID) ([]APIKey, error) {
	rows, err := q.Query(ctx, `select id, org_id, created_by, name, token_hash, prefix, last_used_at, expires_at, revoked_at, created_at
		from mail.api_key where org_id=$1 order by created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.OrgID, &k.CreatedBy, &k.Name, &k.TokenHash, &k.Prefix, &k.LastUsedAt, &k.ExpiresAt, &k.RevokedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func CreateAPIKey(ctx context.Context, tx pgx.Tx, k *APIKey) error {
	_, err := tx.Exec(ctx, `insert into mail.api_key (id, org_id, created_by, name, token_hash, prefix, expires_at)
		values ($1,$2,$3,$4,$5,$6,$7)`,
		k.ID, k.OrgID, k.CreatedBy, k.Name, k.TokenHash, k.Prefix, k.ExpiresAt)
	return err
}

func RevokeAPIKey(ctx context.Context, tx pgx.Tx, id, orgID uuid.UUID) error {
	ct, err := tx.Exec(ctx, `update mail.api_key set revoked_at=now() where id=$1 and org_id=$2 and revoked_at is null`, id, orgID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func GetAPIKeyByTokenHash(ctx context.Context, q Querier, tokenHash string) (*APIKey, error) {
	row := q.QueryRow(ctx, `select id, org_id, created_by, name, token_hash, prefix, last_used_at, expires_at, revoked_at, created_at
		from mail.api_key where token_hash=$1`, tokenHash)
	var k APIKey
	err := row.Scan(&k.ID, &k.OrgID, &k.CreatedBy, &k.Name, &k.TokenHash, &k.Prefix, &k.LastUsedAt, &k.ExpiresAt, &k.RevokedAt, &k.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &k, nil
}

func TouchAPIKey(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `update mail.api_key set last_used_at=now() where id=$1`, id)
	return err
}

// GetPrimaryAddressByMailbox: the mailbox's primary address string (local@domain).
func GetPrimaryAddressByMailbox(ctx context.Context, q Querier, mailboxID uuid.UUID) (string, error) {
	row := q.QueryRow(ctx, `select a.local, d.name from mail.address a
		join mail.domain d on d.id = a.domain_id
		where a.mailbox_id=$1 and a.is_primary limit 1`, mailboxID)
	var local, dom string
	if err := row.Scan(&local, &dom); err != nil {
		return "", notFound(err)
	}
	return local + "@" + dom, nil
}

// GetOrgOwnerID: the org's first owner-role user (shared mailboxes have no
// owner; their autoresponder replies bill to the org's owner).
func GetOrgOwnerID(ctx context.Context, q Querier, orgID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `select id from mail.user where org_id=$1 and role='owner' order by created_at limit 1`, orgID).Scan(&id)
	if err != nil {
		return uuid.Nil, notFound(err)
	}
	return id, nil
}

// GetWebhookByID (dispatch needs the secret to sign).
func GetWebhookByID(ctx context.Context, q Querier, id uuid.UUID) (*Webhook, error) {
	row := q.QueryRow(ctx, `select id, org_id, url, secret, events, active, created_at
		from mail.webhook where id=$1`, id)
	var w Webhook
	err := row.Scan(&w.ID, &w.OrgID, &w.URL, &w.Secret, &w.Events, &w.Active, &w.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &w, nil
}
