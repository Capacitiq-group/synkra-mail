package store

import (
	"context"
	"errors"
	"time"

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
//
// Every user read carries totp_enabled and email_verified_at (audit items 3
// and 13): MFA enforcement and verification need them on every path, not just
// login.

func CreateUser(ctx context.Context, tx pgx.Tx, u *model.User) error {
	_, err := tx.Exec(ctx, `insert into mail.user (id, org_id, email, password_hash, display_name, role, is_platform_admin, status)
		values ($1,$2,$3,$4,$5,$6,$7,'active')`,
		u.ID, u.OrgID, u.Email, optText(u.PasswordHash), u.DisplayName, u.Role, u.IsPlatformAdmin)
	return err
}

func GetUser(ctx context.Context, q Querier, id uuid.UUID) (*model.User, error) {
	row := q.QueryRow(ctx, `select id, org_id, email, coalesce(password_hash,''),
		(totp_secret_enc is not null), totp_enabled, email_verified_at, display_name, role, is_platform_admin, status, created_at, updated_at
		from mail.user where id=$1`, id)
	return scanUser(row)
}

func GetUserByEmail(ctx context.Context, q Querier, email string) (*model.User, error) {
	row := q.QueryRow(ctx, `select id, org_id, email, coalesce(password_hash,''),
		(totp_secret_enc is not null), totp_enabled, email_verified_at, display_name, role, is_platform_admin, status, created_at, updated_at
		from mail.user where email=$1`, email)
	return scanUser(row)
}

// ListUsersByEmail returns every user with this login e-mail. The schema keeps
// e-mail uniqueness per organisation (unique (org_id, email)), so two orgs can
// legitimately share a login address; login resolves that explicitly (authn).
func ListUsersByEmail(ctx context.Context, q Querier, email string) ([]model.User, error) {
	rows, err := q.Query(ctx, `select id, org_id, email, coalesce(password_hash,''),
		(totp_secret_enc is not null), totp_enabled, email_verified_at, display_name, role, is_platform_admin, status, created_at, updated_at
		from mail.user where email=$1 order by created_at`, email)
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

func ListUsersByOrg(ctx context.Context, q Querier, orgID uuid.UUID) ([]model.User, error) {
	rows, err := q.Query(ctx, `select id, org_id, email, coalesce(password_hash,''),
		(totp_secret_enc is not null), totp_enabled, email_verified_at, display_name, role, is_platform_admin, status, created_at, updated_at
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
	err := row.Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.HasTOTP, &u.TotpEnabled, &u.EmailVerifiedAt,
		&u.DisplayName, &u.Role, &u.IsPlatformAdmin, &u.Status, &u.CreatedAt, &u.UpdatedAt)
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

// ---- login rate limiting (Section 7.1; reworked per audit item 6) ----
//
// The old query counted failures by `email OR ip` in one number: any third
// party could lock any account by failing logins from a spoofed XFF, and the
// same failure fed both limits. Now the two limits are independent:
//
//   - the ACCOUNT lockout counts that e-mail's own failures only (brute force
//     against one identity);
//   - the IP gate counts that source address's failures only (password
//     spraying) — a soft 429, never an account lock, and only trustworthy when
//     the caller can see the real peer address.

func RecordLoginAttempt(ctx context.Context, q Querier, email, ip string, ok bool) {
	_, _ = q.Exec(ctx, `insert into mail.login_attempt (email, ip, ok) values ($1,$2,$3)`, optText(email), optText(ip), ok)
}

// CountFailedLoginsByEmail: failures of one identity inside the window.
func CountFailedLoginsByEmail(ctx context.Context, q Querier, email string, window string) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from mail.login_attempt
		where ok=false and created_at > now() - $1::interval and email=$2`, window, optText(email)).Scan(&n)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// CountFailedLoginsByIP: failures from one source address inside the window.
func CountFailedLoginsByIP(ctx context.Context, q Querier, ip string, window string) (int, error) {
	if ip == "" {
		return 0, nil
	}
	var n int
	err := q.QueryRow(ctx, `select count(*) from mail.login_attempt
		where ok=false and created_at > now() - $1::interval and ip=$2`, window, ip).Scan(&n)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// RevokeOtherUserSessions revokes every session of a user except one (the
// session that performed a credential change keeps working).
func RevokeOtherUserSessions(ctx context.Context, tx pgx.Tx, userID, keepSessionID uuid.UUID) error {
	_, err := tx.Exec(ctx, `update mail.session set revoked_at=now() where user_id=$1 and revoked_at is null and id <> $2`, userID, keepSessionID)
	return err
}

// ---- TOTP enrolment (audit item 3: one phished staff password must not
// expose every tenant; the seed column existed, nothing used it) ----

// SetTOTPEnrolment stores the sealed seed; enabled=false until the user proves
// possession of the authenticator with a valid code (activation step).
func SetTOTPEnrolment(ctx context.Context, tx pgx.Tx, userID uuid.UUID, sealedSecret string, enabled bool) error {
	_, err := tx.Exec(ctx, `update mail.user set totp_secret_enc=$2, totp_enabled=$3, updated_at=now() where id=$1`,
		userID, sealedSecret, enabled)
	return err
}

// ClearTOTP disables MFA and erases the seed.
func ClearTOTP(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	_, err := tx.Exec(ctx, `update mail.user set totp_secret_enc=null, totp_enabled=false, updated_at=now() where id=$1`, userID)
	return err
}

// GetTOTPSealed returns the sealed seed ("" when none).
func GetTOTPSealed(ctx context.Context, q Querier, userID uuid.UUID) (string, error) {
	var enc *string
	err := q.QueryRow(ctx, `select totp_secret_enc from mail.user where id=$1`, userID).Scan(&enc)
	if err != nil {
		return "", notFound(err)
	}
	if enc == nil {
		return "", nil
	}
	return *enc, nil
}

// ---- e-mail verification (audit item 13) ----

func CreateVerificationToken(ctx context.Context, tx pgx.Tx, id uuid.UUID, userID uuid.UUID, tokenHash, kind string, expiresAt time.Time) error {
	_, err := tx.Exec(ctx, `insert into mail.verification_token (id, user_id, token_hash, kind, expires_at)
		values ($1,$2,$3,$4,$5)`, id, userID, tokenHash, kind, expiresAt)
	return err
}

// ConsumeVerificationToken marks the token used and verifies the user's
// e-mail in the same transaction. Returns ErrNotFound when the token is
// unknown, used, or expired.
func ConsumeVerificationToken(ctx context.Context, tx pgx.Tx, tokenHash string) (userID uuid.UUID, kind string, err error) {
	err = tx.QueryRow(ctx, `update mail.verification_token set used_at=now()
		where token_hash=$1 and used_at is null and expires_at > now()
		returning user_id, kind`, tokenHash).Scan(&userID, &kind)
	if err != nil {
		return uuid.Nil, "", notFound(err)
	}
	if kind == "email" {
		if _, err = tx.Exec(ctx, `update mail.user set email_verified_at=now(), updated_at=now() where id=$1`, userID); err != nil {
			return uuid.Nil, "", err
		}
	}
	return userID, kind, nil
}

// ---- session management surface (missing-features audit: list + revoke) ----

func ListSessionsByUser(ctx context.Context, q Querier, userID uuid.UUID) ([]SessionView, error) {
	rows, err := q.Query(ctx, `select id, user_id, token_hash, kind, coalesce(ip,''), coalesce(user_agent,''),
		impersonated_by, expires_at, revoked_at, created_at
		from mail.session where user_id=$1 and revoked_at is null and expires_at > now()
		order by created_at desc`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionView
	for rows.Next() {
		var s model.Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.TokenHash, &s.Kind, &s.IP, &s.UserAgent,
			&s.ImpersonatedBy, &s.ExpiresAt, &s.RevokedAt, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, SessionView{Session: s})
	}
	return out, rows.Err()
}

// SessionView is a session row prepared for the API surface.
type SessionView struct {
	Session model.Session
}

// RevokeSessionForUser revokes one of the user's own sessions (never another
// user's); ErrNotFound when it is not theirs.
func RevokeSessionForUser(ctx context.Context, tx pgx.Tx, userID, sessionID uuid.UUID) error {
	ct, err := tx.Exec(ctx, `update mail.session set revoked_at=now()
		where id=$1 and user_id=$2 and revoked_at is null`, sessionID, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
