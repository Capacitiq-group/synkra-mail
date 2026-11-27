// Package authn: mail-owned authentication (Sections 2.3, 7.1).
// Local password login + TOTP-ready, per-device sessions, app passwords for
// protocol ports, admin impersonation with guardrails (Section 11.2).
package authn

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"synkra.email/mail/internal/config"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

var (
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrLocked              = errors.New("account locked: too many failed logins")
	ErrSuspended           = errors.New("account suspended")
	ErrAppPasswordLimit    = errors.New("app password limit reached (10 active max)")
	ErrImpersonationDenied = errors.New("impersonation requires platform staff")
	// ErrAmbiguousLogin: the login e-mail exists in more than one organisation
	// and none can be preferred (register §1.2). Refused, never resolved by luck.
	ErrAmbiguousLogin = errors.New("login e-mail exists in multiple organisations")
	// ErrStaffImpersonation: staff accounts cannot be impersonated (register §1.7) —
	// the audit log alone is not a privilege boundary.
	ErrStaffImpersonation = errors.New("staff accounts cannot be impersonated")
)

// AuthContext is resolved from a verified session/token — never from client
// input (Section 2.3). All downstream queries scope by ctx.User.OrgID.
type AuthContext struct {
	User    *model.User
	Org     *model.Org
	Session *model.Session
}

// Impersonating reports whether this context is a staff impersonation session.
func (a *AuthContext) Impersonating() bool {
	return a.Session != nil && a.Session.Kind == "impersonation"
}

// CanChangeCredentials: impersonated sessions cannot change credentials
// (Section 11.2 — credential operations use the reset flow instead).
func (a *AuthContext) CanChangeCredentials() bool {
	return !a.Impersonating()
}

type Service struct {
	Store *store.Store
	Cfg   *config.Config
}

// Login verifies email+password with lockout (10 fails / 10 min per account and IP).
// Login e-mails are unique per organisation, not globally (schema); when two
// orgs share a login address the org that owns the address's domain wins, and a
// truly ambiguous login is refused instead of silently picking one (register §1.2).
func (s *Service) Login(ctx context.Context, email, password, ip, userAgent string) (*AuthContext, string, error) {
	fails, err := store.CountRecentFailedLogins(ctx, s.Store.Pool, email, ip, "10 minutes")
	if err != nil {
		return nil, "", err
	}
	if fails >= s.Cfg.HTTP.LoginMaxFails {
		return nil, "", ErrLocked
	}

	users, err := store.ListUsersByEmail(ctx, s.Store.Pool, email)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, "", err
	}
	if len(users) == 0 {
		store.RecordLoginAttempt(ctx, s.Store.Pool, email, ip, false)
		return nil, "", ErrInvalidCredentials
	}
	u, resolved := s.resolveLoginUser(ctx, users, email)
	if !resolved {
		store.RecordLoginAttempt(ctx, s.Store.Pool, email, ip, false)
		return nil, "", ErrAmbiguousLogin
	}
	if u.Status != "active" || u.PasswordHash == "" {
		store.RecordLoginAttempt(ctx, s.Store.Pool, email, ip, false)
		return nil, "", ErrInvalidCredentials
	}
	if err := krypto.VerifyPassword(u.PasswordHash, password); err != nil {
		store.RecordLoginAttempt(ctx, s.Store.Pool, email, ip, false)
		return nil, "", ErrInvalidCredentials
	}

	store.RecordLoginAttempt(ctx, s.Store.Pool, email, ip, true)
	ac, token, err := s.createSession(ctx, u, "web", ip, userAgent, nil, s.Cfg.HTTP.SessionTTL)
	if err != nil {
		return nil, "", err
	}
	return ac, token, nil
}

// resolveLoginUser picks the login identity deterministically: one match wins;
// with several, the org that owns the e-mail's domain does. The platform domain
// belongs to no org, so duplicates on it stay ambiguous (false).
func (s *Service) resolveLoginUser(ctx context.Context, users []model.User, email string) (*model.User, bool) {
	if len(users) == 1 {
		return &users[0], true
	}
	if i := strings.LastIndexByte(email, '@'); i >= 0 {
		orgID, err := store.GetOrgIDByDomainName(ctx, s.Store.Pool, email[i+1:])
		if err == nil && orgID != nil {
			for j := range users {
				if users[j].OrgID == *orgID {
					return &users[j], true
				}
			}
		}
	}
	return nil, false
}

func (s *Service) createSession(ctx context.Context, u *model.User, kind, ip, userAgent string, adminID *uuid.UUID, ttl time.Duration) (*AuthContext, string, error) {
	token := krypto.NewToken()
	sess := &model.Session{
		ID:             uuid.New(),
		UserID:         u.ID,
		TokenHash:      krypto.SHA256Hex(token),
		Kind:           kind,
		IP:             ip,
		UserAgent:      userAgent,
		ImpersonatedBy: adminID,
		ExpiresAt:      time.Now().Add(ttl),
	}
	err := s.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := store.CreateSession(ctx, tx, sess); err != nil {
			return err
		}
		if kind != "impersonation" {
			return store.TouchUserLogin(ctx, tx, u.ID)
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	org, err := store.GetOrg(ctx, s.Store.Pool, u.OrgID)
	if err != nil {
		return nil, "", err
	}
	return &AuthContext{User: u, Org: org, Session: sess}, token, nil
}

// VerifyToken resolves a bearer token to an AuthContext.
func (s *Service) VerifyToken(ctx context.Context, token string) (*AuthContext, error) {
	if token == "" {
		return nil, ErrInvalidCredentials
	}
	sess, err := store.GetSessionByTokenHash(ctx, s.Store.Pool, krypto.SHA256Hex(token))
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if sess.RevokedAt != nil || time.Now().After(sess.ExpiresAt) {
		return nil, ErrInvalidCredentials
	}
	u, err := store.GetUser(ctx, s.Store.Pool, sess.UserID)
	if err != nil || u.Status != "active" {
		return nil, ErrSuspended
	}
	org, err := store.GetOrg(ctx, s.Store.Pool, u.OrgID)
	if err != nil {
		return nil, err
	}
	return &AuthContext{User: u, Org: org, Session: sess}, nil
}

func (s *Service) Logout(ctx context.Context, ac *AuthContext) error {
	return s.Store.WithTx(ctx, func(tx store.Tx) error {
		return store.RevokeSession(ctx, tx, ac.Session.ID)
	})
}

// ---- app passwords (protocol ports only — Section 6.1) ----

// CreateAppPassword enforces the owner policy: max 10 active, 180-day expiry.
// Returns the one-time plaintext secret.
func (s *Service) CreateAppPassword(ctx context.Context, ac *AuthContext, name string) (secret string, ap *model.AppPassword, err error) {
	if !ac.CanChangeCredentials() {
		return "", nil, errors.New("impersonated sessions cannot manage credentials")
	}
	n, err := store.CountActiveAppPasswords(ctx, s.Store.Pool, ac.User.ID)
	if err != nil {
		return "", nil, err
	}
	if n >= s.Cfg.AppPasswordMaxActive {
		return "", nil, ErrAppPasswordLimit
	}
	secret, prefix := krypto.NewAppPassword()
	ap = &model.AppPassword{
		ID:           uuid.New(),
		UserID:       ac.User.ID,
		Name:         name,
		Prefix:       prefix,
		PasswordHash: krypto.HashPassword(secret),
		ExpiresAt:    time.Now().AddDate(0, 0, s.Cfg.AppPasswordMaxAgeDays),
	}
	err = s.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := store.CreateAppPassword(ctx, tx, ap); err != nil {
			return err
		}
		return store.RecordAudit(ctx, tx, &ac.User.ID, nil, false, "app_password.create", "user", ac.User.ID.String(), name, nil)
	})
	return secret, ap, err
}

// AuthenticateAppPassword verifies an IMAP/submission login. The main account
// password is never accepted on protocol ports (Section 6.1). Protocol-port
// brute force shares the web login's failed-attempt lockout (register §1.6),
// keyed by the login identity plus source IP when the caller knows it (ip ""
// for servers that cannot see the peer, e.g. go-imap v2 sessions).
func (s *Service) AuthenticateAppPassword(ctx context.Context, username, secret, ip string) (*model.User, *model.AppPassword, error) {
	if secret == "" || len(secret) < 5 {
		store.RecordLoginAttempt(ctx, s.Store.Pool, username, ip, false)
		return nil, nil, ErrInvalidCredentials
	}
	fails, err := store.CountRecentFailedLogins(ctx, s.Store.Pool, username, ip, "10 minutes")
	if err != nil {
		return nil, nil, err
	}
	if fails >= s.Cfg.HTTP.LoginMaxFails {
		return nil, nil, ErrLocked
	}
	prefix := secret[:4]
	candidates, err := store.FindActiveAppPasswordByPrefix(ctx, s.Store.Pool, prefix)
	if err != nil {
		return nil, nil, err
	}
	for i := range candidates {
		ap := &candidates[i]
		u, err := store.GetUser(ctx, s.Store.Pool, ap.UserID)
		if err != nil {
			continue
		}
		if u.Status != "active" {
			continue
		}
		// Login identity must match the user (or one of their addresses).
		if !stringsEqualFold(u.Email, username) {
			continue
		}
		if err := krypto.VerifyPassword(ap.PasswordHash, secret); err == nil {
			store.RecordLoginAttempt(ctx, s.Store.Pool, username, ip, true)
			store.TouchAppPassword(ctx, s.Store.Pool, ap.ID)
			return u, ap, nil
		}
	}
	store.RecordLoginAttempt(ctx, s.Store.Pool, username, ip, false)
	return nil, nil, ErrInvalidCredentials
}

func (s *Service) RevokeAppPassword(ctx context.Context, ac *AuthContext, apID uuid.UUID) error {
	if !ac.CanChangeCredentials() {
		return errors.New("impersonated sessions cannot manage credentials")
	}
	return s.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := store.RevokeAppPassword(ctx, tx, apID, ac.User.ID); err != nil {
			return err
		}
		return store.RecordAudit(ctx, tx, &ac.User.ID, nil, false, "app_password.revoke", "user", ac.User.ID.String(), "", nil)
	})
}

// ---- impersonation (Section 11.2) ----

// Impersonate creates a 60-minute, audited, guard-railed staff session.
// Staff accounts cannot be impersonated (register §1.7): a staff session in
// someone else's hands bypasses every platform-admin boundary.
func (s *Service) Impersonate(ctx context.Context, admin *model.User, targetUserID uuid.UUID, reason, ip, userAgent string) (token string, err error) {
	if !admin.IsPlatformAdmin {
		return "", ErrImpersonationDenied
	}
	target, err := store.GetUser(ctx, s.Store.Pool, targetUserID)
	if err != nil {
		return "", err
	}
	if target.IsPlatformAdmin {
		return "", ErrStaffImpersonation
	}
	ac, token, err := s.createSession(ctx, target, "impersonation", ip, userAgent, &admin.ID, 60*time.Minute)
	if err != nil {
		return "", err
	}
	err = s.Store.WithTx(ctx, func(tx store.Tx) error {
		return store.RecordAudit(ctx, tx, &target.ID, &admin.ID, true,
			"impersonation.start", "user", target.ID.String(), reason, nil)
	})
	if err != nil {
		return "", err
	}
	_ = ac
	return token, nil
}

// EndImpersonation ends an impersonation session (admin side or timeout).
func (s *Service) EndImpersonation(ctx context.Context, admin *model.User, sessionID uuid.UUID, reason string) error {
	return s.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := store.RevokeSession(ctx, tx, sessionID); err != nil {
			return err
		}
		return store.RecordAudit(ctx, tx, nil, &admin.ID, true,
			"impersonation.end", "session", sessionID.String(), reason, nil)
	})
}

// ProvisionOrg creates org + owner + mailbox + address + platform membership in
// one transaction (Section 4.1 provisioning rule 1).
func (s *Service) ProvisionOrg(ctx context.Context, orgName, ownerEmail, ownerName, passwordHash, mailboxDomain string, plan *model.Plan) (*model.Org, *model.User, error) {
	org := &model.Org{ID: uuid.New(), Name: orgName, PlanID: plan.ID}
	user := &model.User{
		ID:           uuid.New(),
		OrgID:        org.ID,
		Email:        ownerEmail,
		PasswordHash: passwordHash,
		DisplayName:  ownerName,
		Role:         "owner",
	}
	err := s.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := store.CreateOrg(ctx, tx, org, s.Cfg.TrialDays); err != nil {
			return err
		}
		if err := store.CreateUser(ctx, tx, user); err != nil {
			return err
		}
		// Mailbox + address on the given domain (platform domain by default).
		var domainID uuid.UUID
		if err := tx.QueryRow(ctx, `select id from mail.domain where lower(name::text)=lower($1)`, mailboxDomain).Scan(&domainID); err != nil {
			return err
		}
		mb := &model.Mailbox{ID: uuid.New(), OrgID: org.ID, OwnerUserID: &user.ID, Kind: "user", Name: ownerEmail}
		if err := store.CreateMailbox(ctx, tx, mb); err != nil {
			return err
		}
		local, _, err := splitAddress(ownerEmail)
		if err != nil {
			return err
		}
		adr := &model.Address{ID: uuid.New(), OrgID: org.ID, DomainID: domainID, MailboxID: mb.ID, Local: local, Kind: "user", IsPrimary: true}
		if err := store.CreateAddress(ctx, tx, adr); err != nil {
			return err
		}
		return store.RecordAudit(ctx, tx, &user.ID, nil, false, "org.provision", "org", org.ID.String(), orgName, nil)
	})
	if err != nil {
		return nil, nil, err
	}
	orgFull, err := store.GetOrg(ctx, s.Store.Pool, org.ID)
	if err != nil {
		return nil, nil, err
	}
	userFull, err := store.GetUser(ctx, s.Store.Pool, user.ID)
	return orgFull, userFull, err
}

func splitAddress(addr string) (local, domain string, err error) {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == '@' {
			return addr[:i], addr[i+1:], nil
		}
	}
	return "", "", errors.New("invalid address")
}

func stringsEqualFold(a, b string) bool { return strings.EqualFold(a, b) }
