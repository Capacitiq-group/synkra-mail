package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"synkra.email/mail/internal/model"
)

// Querier is satisfied by *pgxpool.Pool and pgx.Tx.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (commandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ---- users ----

func CreateUser(ctx context.Context, tx pgx.Tx, u *model.User) error {
	_, err := tx.Exec(ctx, `insert into mail.user (id, org_id, email, password_hash, display_name, role, is_platform_admin, status)
		values ($1,$2,$3,$4,$5,$6,$7,'active')`,
		u.ID, u.OrgID, u.Email, optText(u.PasswordHash), u.DisplayName, u.Role, u.IsPlatformAdmin)
	return err
}

func GetUser(ctx context.Context, q Querier, id uuid.UUID) (*model.User, error) {
	row := q.QueryRow(ctx, `select id, org_id, email, coalesce(password_hash,''),
		(totp_secret_enc is not null), display_name, role, is_platform_admin, status, created_at, updated_at
		from mail.user where id=$1`, id)
	return scanUser(row)
}

func GetUserByEmail(ctx context.Context, q Querier, email string) (*model.User, error) {
	row := q.QueryRow(ctx, `select id, org_id, email, coalesce(password_hash,''),
		(totp_secret_enc is not null), display_name, role, is_platform_admin, status, created_at, updated_at
		from mail.user where email=$1`, email)
	return scanUser(row)
}

func ListUsersByOrg(ctx context.Context, q Querier, orgID uuid.UUID) ([]model.User, error) {
	rows, err := q.Query(ctx, `select id, org_id, email, coalesce(password_hash,''),
		(totp_secret_enc is not null), display_name, role, is_platform_admin, status, created_at, updated_at
		from mail.user where org_id=$1 order by created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func scanUser(row pgx.Row) (*model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.HasTOTP, &u.DisplayName,
		&u.Role, &u.IsPlatformAdmin, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &u, nil
}

func SetUserPassword(ctx context.Context, tx pgx.Tx, userID uuid.UUID, hash string) error {
	_, err := tx.Exec(ctx, `update mail.user set password_hash=$2, updated_at=now() where id=$1`, userID, hash)
	return err
}

func SetUserStatus(ctx context.Context, tx pgx.Tx, userID uuid.UUID, status string) error {
	_, err := tx.Exec(ctx, `update mail.user set status=$2, updated_at=now() where id=$1`, userID, status)
	return err
}

func TouchUserLogin(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	_, err := tx.Exec(ctx, `update mail.user set last_login_at=now() where id=$1`, userID)
	return err
}

// ---- sessions ----

func CreateSession(ctx context.Context, tx pgx.Tx, s *model.Session) error {
	_, err := tx.Exec(ctx, `insert into mail.session (id, user_id, token_hash, kind, ip, user_agent, impersonated_by, expires_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8)`,
		s.ID, s.UserID, s.TokenHash, s.Kind, optText(s.IP), optText(s.UserAgent), optUUID(s.ImpersonatedBy), s.ExpiresAt)
	return err
}

func GetSessionByTokenHash(ctx context.Context, q Querier, tokenHash string) (*model.Session, error) {
	row := q.QueryRow(ctx, `select id, user_id, token_hash, kind, coalesce(ip,''), coalesce(user_agent,''),
		impersonated_by, expires_at, revoked_at, created_at
		from mail.session where token_hash=$1`, tokenHash)
	var s model.Session
	err := row.Scan(&s.ID, &s.UserID, &s.TokenHash, &s.Kind, &s.IP, &s.UserAgent,
		&s.ImpersonatedBy, &s.ExpiresAt, &s.RevokedAt, &s.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &s, nil
}

func RevokeSession(ctx context.Context, tx pgx.Tx, sessionID uuid.UUID) error {
	_, err := tx.Exec(ctx, `update mail.session set revoked_at=now() where id=$1 and revoked_at is null`, sessionID)
	return err
}

func RevokeAllUserSessions(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	_, err := tx.Exec(ctx, `update mail.session set revoked_at=now() where user_id=$1 and revoked_at is null`, userID)
	return err
}

// ---- app passwords (owner decision: max 10 active, 180-day expiry) ----

func CreateAppPassword(ctx context.Context, tx pgx.Tx, ap *model.AppPassword) error {
	_, err := tx.Exec(ctx, `insert into mail.app_password (id, user_id, name, prefix, password_hash, expires_at)
		values ($1,$2,$3,$4,$5,$6)`,
		ap.ID, ap.UserID, ap.Name, ap.Prefix, ap.PasswordHash, ap.ExpiresAt)
	return err
}

func ListAppPasswords(ctx context.Context, q Querier, userID uuid.UUID) ([]model.AppPassword, error) {
	rows, err := q.Query(ctx, `select id, user_id, name, prefix, password_hash, expires_at, last_used_at, revoked_at, created_at
		from mail.app_password where user_id=$1 order by created_at desc`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AppPassword
	for rows.Next() {
		var a model.AppPassword
		if err := rows.Scan(&a.ID, &a.UserID, &a.Name, &a.Prefix, &a.PasswordHash, &a.ExpiresAt, &a.LastUsedAt, &a.RevokedAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func CountActiveAppPasswords(ctx context.Context, q Querier, userID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from mail.app_password where user_id=$1 and revoked_at is null and expires_at > now()`, userID).Scan(&n)
	return n, err
}

func GetActiveAppPasswordByID(ctx context.Context, q Querier, id uuid.UUID) (*model.AppPassword, error) {
	row := q.QueryRow(ctx, `select id, user_id, name, prefix, password_hash, expires_at, last_used_at, revoked_at, created_at
		from mail.app_password where id=$1 and revoked_at is null and expires_at > now()`, id)
	var a model.AppPassword
	err := row.Scan(&a.ID, &a.UserID, &a.Name, &a.Prefix, &a.PasswordHash, &a.ExpiresAt, &a.LastUsedAt, &a.RevokedAt, &a.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &a, nil
}

func FindActiveAppPasswordByPrefix(ctx context.Context, q Querier, prefix string) ([]model.AppPassword, error) {
	rows, err := q.Query(ctx, `select id, user_id, name, prefix, password_hash, expires_at, last_used_at, revoked_at, created_at
		from mail.app_password where prefix=$1 and revoked_at is null and expires_at > now()`, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AppPassword
	for rows.Next() {
		var a model.AppPassword
		if err := rows.Scan(&a.ID, &a.UserID, &a.Name, &a.Prefix, &a.PasswordHash, &a.ExpiresAt, &a.LastUsedAt, &a.RevokedAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func TouchAppPassword(ctx context.Context, q Querier, id uuid.UUID) {
	// Best-effort; last_used_at is advisory.
	_, _ = q.Exec(ctx, `update mail.app_password set last_used_at=now() where id=$1`, id)
}

func RevokeAppPassword(ctx context.Context, tx pgx.Tx, id, userID uuid.UUID) error {
	ct, err := tx.Exec(ctx, `update mail.app_password set revoked_at=now() where id=$1 and user_id=$2 and revoked_at is null`, id, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func RevokeAllUserAppPasswords(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	// Compromised-account playbook (Section 7.3).
	_, err := tx.Exec(ctx, `update mail.app_password set revoked_at=now() where user_id=$1 and revoked_at is null`, userID)
	return err
}

// ---- login rate limiting (Section 7.1: 10 fails / 10 min per account and per IP) ----

func RecordLoginAttempt(ctx context.Context, q Querier, email, ip string, ok bool) {
	_, _ = q.Exec(ctx, `insert into mail.login_attempt (email, ip, ok) values ($1,$2,$3)`, optText(email), optText(ip), ok)
}

func CountRecentFailedLogins(ctx context.Context, q Querier, email, ip string, window string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from mail.login_attempt
		where ok=false and created_at > now() - $1::interval and (email=$2 or ip=$3)`, window, optText(email), optText(ip)).Scan(&n)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}
