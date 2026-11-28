// Package api: the internal webmail/admin API (Section 15.1).
// JSON, UTF-8; error envelope { error: { code, message, request_id } }.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"synkra.email/mail/internal/authn"
	"synkra.email/mail/internal/blob"
	"synkra.email/mail/internal/config"
	"synkra.email/mail/internal/outbound"
	"synkra.email/mail/internal/store"
)

type Server struct {
	Cfg      *config.Config
	Auth     *authn.Service
	Store    *store.Store
	Pipeline *outbound.Pipeline
	Blobs    blob.Store // probe target for /readyz
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	// Ops: liveness and readiness (systemd/monitoring, Section 13.2).
	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", s.handleReadyz)

	r.Route("/api/v1", func(r chi.Router) {
		// Public
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/verify-email", s.handleVerifyEmail)

		// Session-authenticated. A default request-body ceiling applies
		// (audit item 5: /auth/login and the tracker endpoint read bodies
		// with no limit at all); /send caps itself higher.
		r.Group(func(r chi.Router) {
			r.Use(s.requireSession)
			r.Use(s.limitBody(1 << 20)) // 1 MiB default JSON envelope
			r.Get("/me", s.handleMe)
			r.Post("/auth/logout", s.handleLogout)

			// MFA (audit item 3), sessions, verification.
			r.Post("/me/mfa/enrol", s.handleMFAEnrol)
			r.Post("/me/mfa/activate", s.handleMFAActivate)
			r.Post("/me/mfa/disable", s.handleMFADisable)
			r.Get("/me/sessions", s.handleListSessions)
			r.Post("/me/sessions/{id}/revoke", s.handleRevokeSession)
			r.Post("/me/verify-email", s.handleRequestEmailVerification)

			// Suppression management + owner-side resume (audit item 18).
			r.Get("/suppression", s.handleListSuppression)
			r.Delete("/suppression/{email}", s.handleDeleteSuppression)
			r.Post("/mailboxes/{id}/resume", s.handleResumeOwnMailbox)

			// Org users and shared mailboxes (register §2.6-2.9).
			r.Get("/org/users", s.handleListOrgUsers)
			r.Post("/org/users/{id}/suspend", s.handleSuspendOrgUser)
			r.Post("/org/users/{id}/resume", s.handleResumeOrgUser)
			r.Post("/org/users/{id}/revoke-sessions", s.handleRevokeUserSessions)
			r.Get("/org/shared-mailboxes/{id}/members", s.handleListSharedMailboxMembers)

			// Quarantine (register §2.11): the spam verdict engines are hook
			// points only for now; the console surface ships with them.
			r.Get("/org/quarantine", s.handleListQuarantine)
			r.Post("/org/quarantine/{id}/release", s.handleReleaseQuarantine)

			// Attachments (register §2.10): blobs stored and referenced; these
			// routes serve them (★ hard webmail prerequisite).
			r.Get("/messages/{id}/attachments", s.handleListAttachments)
			r.Get("/messages/{id}/attachments/{att_id}", s.handleGetAttachment)

			// Feature register §12: signatures, autoresponder, routing rules,
			// mail trace, deliverability dashboard.
			r.Get("/me/signatures", s.handleListSignatures)
			r.Post("/me/signatures", s.handleCreateSignature)
			r.Delete("/me/signatures/{id}", s.handleDeleteSignature)
			r.Get("/mailboxes/{id}/autoresponder", s.handleGetAutoresponder)
			r.Put("/mailboxes/{id}/autoresponder", s.handleSetAutoresponder)
			r.Get("/mailboxes/{id}/rules", s.handleListRules)
			r.Post("/mailboxes/{id}/rules", s.handleCreateRule)
			r.Delete("/mailboxes/{id}/rules/{rid}", s.handleDeleteRule)
			r.Get("/messages/{id}/trace", s.handleMessageTrace)
			r.Get("/org/deliverability", s.handleOrgDeliverability)

			// Differentiators §13: health score, reliability monitor, webhooks,
			// API keys (the API is the product). Mutating routes live in the
			// backdoor-guard group below — single registration each.
			r.Get("/org/health", s.handleOrgHealth)
			r.Get("/org/reliability", s.handleOrgReliability)
			r.Get("/org/webhooks", s.handleListWebhooks)
			r.Delete("/org/webhooks/{id}", s.handleDeleteWebhook)
			r.Post("/org/webhooks/{id}/test", s.handleTestWebhook)
			r.Get("/org/api-keys", s.handleListAPIKeys)
			r.Delete("/org/api-keys/{id}", s.handleRevokeAPIKey)

			r.Get("/app-passwords", s.handleListAppPasswords)
			r.Delete("/app-passwords/{id}", s.handleRevokeAppPassword)

			r.Get("/mailboxes", s.handleListMailboxes)
			r.Get("/folders", s.handleListFolders)
			r.Post("/folders", s.handleCreateFolder)
			r.Delete("/folders/{id}", s.handleDeleteFolder)

			r.Get("/messages", s.handleListMessages)
			r.Get("/messages/{id}", s.handleGetMessage)
			r.Get("/messages/{id}/raw", s.handleGetMessageRaw)
			r.Get("/messages/{id}/tracking", s.handleMessageTracking)
			r.Patch("/messages/{id}/flags", s.handleSetFlags)
			r.Post("/messages/{id}/move", s.handleMoveMessage)
			r.Delete("/messages/{id}", s.handleDeleteMessage)

			r.Post("/send", s.handleSend)
			r.Get("/search", s.handleSearch)
			r.Get("/storage", s.handleStorage)
			r.Get("/org/storage/usage", s.handleStorageUsage)

			// Billing (phase 11)
			r.Get("/org/billing", s.handleBillingSummary)
			r.Put("/org/billing/contact", s.handleBillingContact)
			r.Get("/org/billing/preview", s.handleBillingPreview)
			r.Post("/org/billing/invoices", s.handleCreateInvoice)
			r.Get("/org/billing/invoices", s.handleListInvoices)
			r.Get("/org/billing/invoices/{id}", s.handleGetInvoice)
			r.Post("/org/billing/invoices/{id}/pay", s.handlePayInvoice)
			r.Post("/org/billing/invoices/{id}/verify", s.handleVerifyInvoice)
			r.Post("/org/billing/invoices/{id}/mark-paid", s.handleMarkInvoicePaid)
			r.Delete("/org/billing/payment-methods/{id}", s.handleDeletePaymentMethod)

			// IMAP migration assistant (phase 11)
			r.Post("/org/migrations/source-folders", s.handleMigrationProbe)
			r.Post("/org/migrations", s.handleCreateMigration)
			r.Get("/org/migrations", s.handleListMigrations)
			r.Get("/org/migrations/{id}", s.handleGetMigration)
			r.Post("/org/migrations/{id}/pause", s.handleMigrationPause)
			r.Post("/org/migrations/{id}/resume", s.handleMigrationResume)
			r.Post("/org/migrations/{id}/cancel", s.handleMigrationCancel)
			r.Delete("/org/migrations/{id}", s.handleDeleteMigration)

			r.Get("/addresses", s.handleListAddresses)
			r.Delete("/addresses/{id}", s.handleDeleteAddress)
			r.Get("/domains", s.handleListDomains)
			r.Post("/domains/check", s.handleDomainCheck)
			r.Post("/domains/register", s.handleRegisterDomain)
			r.Post("/domains/{id}/verify", s.handleVerifyDomain)
			r.Get("/domains/{id}/dns", s.handleDomainDNS)
			r.Get("/domains/{id}/doctor", s.handleDomainDoctor)
			r.Get("/domains/{id}", s.handleDomainDetail)
			r.Put("/domains/{id}/nameservers", s.handleDomainNameservers)
			r.Post("/domains/{id}/transfer-lock", s.handleDomainTransferLock)
			r.Post("/domains/{id}/whois-privacy", s.handleDomainWhoisPrivacy)
			r.Post("/domains/{id}/auto-renew", s.handleDomainAutoRenew)
			r.Put("/domains/{id}/registrant", s.handleDomainRegistrant)
			r.Post("/domains/{id}/epp", s.handleDomainEPP)
			r.Post("/domains/{id}/transfer-out", s.handleDomainTransferOut)
			r.Post("/domains/{id}/renew", s.handleDomainRenew)
			r.Post("/domains/{id}/sync", s.handleDomainSync)
			r.Post("/domains/{id}/rotate-dkim", s.handleStageDKIMRotation)
			r.Post("/domains/{id}/activate-dkim", s.handleActivateDKIMRotation)

			r.Get("/org", s.handleGetOrg)
			r.Post("/threads/{id}/note", s.handleThreadNote)
			r.Post("/threads/{id}/assign", s.handleThreadAssign)

			// Backdoor closures (audit item 3): impersonated sessions never
			// mint credentials, forward mail, register or delete domains,
			// grant mailbox access, wire exfil webhooks or flip tracking.
			// Each route is registered exactly once, behind the guard.
			r.Group(func(r chi.Router) {
				r.Use(s.requireNotImpersonating)
				r.Post("/users/invite", s.handleCreateUser)
				r.Post("/org/webhooks", s.handleCreateWebhook)
				r.Put("/org/tracking", s.handleSetTracking)
				r.Post("/addresses", s.handleCreateAddress)
				r.Post("/domains", s.handleCreateDomain)
				r.Delete("/domains/{id}", s.handleDeleteDomain)
				r.Post("/org/api-keys", s.handleCreateAPIKey)
				r.Post("/app-passwords", s.handleCreateAppPassword)
				r.Post("/me/password", s.handleChangeOwnPassword)
				r.Post("/org/shared-mailboxes/{id}/members", s.handleAddSharedMailboxMember)
				r.Delete("/org/shared-mailboxes/{id}/members", s.handleRemoveSharedMailboxMember)
			})
		})

		// Platform staff (admin portal — Section 11). Impersonation and
		// API-key sessions never pass: staff actions must come from a real
		// staff login, attributed to the right human (audit item 3).
		r.Group(func(r chi.Router) {
			r.Use(s.requireStaff)
			r.Get("/admin/orgs", s.handleAdminListOrgs)
			r.Post("/admin/orgs", s.handleAdminCreateOrg)
			r.Post("/admin/orgs/{id}/approve", s.handleAdminApproveOrg)
			r.Post("/admin/orgs/{id}/decline", s.handleAdminDeclineOrg)
			r.Post("/admin/orgs/{id}/suspend", s.handleAdminSuspendOrg)
			r.Post("/admin/orgs/{id}/resume", s.handleAdminResumeOrg)
			r.Post("/admin/orgs/{id}/impersonate", s.handleAdminImpersonate)
			r.Post("/admin/impersonation/{id}/end", s.handleAdminEndImpersonation)
			r.Post("/admin/mailboxes/{id}/resume", s.handleAdminResumeMailbox)
			r.Get("/admin/suppression", s.handleAdminListSuppression)
			r.Get("/admin/audit", s.handleAdminAudit)
			r.Get("/admin/queue", s.handleAdminQueue)
			r.Get("/admin/stats", s.handleAdminStats)
			r.Get("/admin/orgs/{id}/jobs", s.handleAdminOrgJobs)
		})

		// Internal: the isolated tracker posts events here (HMAC-authenticated —
		// it holds no database credentials, Section 9.1). Body capped BEFORE
		// the signature check reads it (audit item 5).
		r.Post("/internal/tracking/events", s.handleInternalTrackingEvent)
	})

	return r
}

// ---- middleware ----

type ctxKey int

const ctxAuth ctxKey = 1

func authFrom(ctx context.Context) *authn.AuthContext {
	ac, _ := ctx.Value(ctxAuth).(*authn.AuthContext)
	return ac
}

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "missing bearer token")
			return
		}
		ac, err := s.Auth.VerifyToken(r.Context(), token)
		if err != nil {
			// API keys (register §13f): sk_ tokens authenticate machine access
			// through the same middleware and tenancy checks.
			if akac, aerr := s.Auth.AuthenticateAPIKey(r.Context(), token); aerr == nil {
				ac, err = akac, nil
			}
		}
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid or expired session")
			return
		}
		if ac.Org.Status == "suspended" {
			// A suspended org keeps exactly logout and the status read (register
			// §1.3). The old exemption tested for an "/admin/" prefix that never
			// occurs under /api/v1, so it was dead code and suspended orgs were
			// over-blocked (unable even to log out).
			p := r.URL.Path
			if p != "/api/v1/auth/logout" && p != "/api/v1/me" {
				writeErrorHint(w, http.StatusForbidden, "forbidden", "organisation suspended",
					"the organisation is suspended; only logout and the status view are available until an administrator resumes it")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxAuth, ac)))
	})
}

func (s *Server) requireStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "missing bearer token")
			return
		}
		ac, err := s.Auth.VerifyToken(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid or expired session")
			return
		}
		if !ac.User.IsPlatformAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "staff only")
			return
		}
		// Staff actions must be attributed to a real staff login (audit item 3):
		// an impersonation session acts as the TARGET user (never staff, but
		// the attribution would still be wrong), and API keys are machine
		// identities — neither may drive the admin portal.
		if ac.Session != nil && ac.Session.Kind != "web" {
			writeError(w, http.StatusForbidden, "forbidden", "admin actions require a staff web session")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxAuth, ac)))
	})
}

// requireNotImpersonating: the backdoor-closure middleware (audit item 3).
// Impersonated sessions are support windows into one tenant; they must never
// mint credentials, forward mail, register domains, grant mailbox access,
// wire exfil webhooks or flip tracking.
func (s *Server) requireNotImpersonating(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ac := authFrom(r.Context()); ac != nil && ac.Impersonating() {
			writeError(w, http.StatusForbidden, "forbidden",
				"impersonated sessions cannot perform this action — end impersonation or use the admin console")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limitBody caps request bodies for everything that reads one (audit item 5:
// login and the internal tracker endpoint read unbounded bodies). /send
// raises its own cap around the message limit.
func (s *Server) limitBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("synkra_mail_session"); err == nil {
		return c.Value
	}
	return ""
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errBody struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
		Hint      string `json:"hint,omitempty"` // what to do about it (register §1.10)
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeErrorHint(w, status, code, msg, "")
}

func writeErrorHint(w http.ResponseWriter, status int, code, msg, hint string) {
	var b errBody
	b.Error.Code = code
	b.Error.Message = msg
	b.Error.RequestID = uuid.NewString()
	b.Error.Hint = hint
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(b)
}

func mapError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, outbound.ErrNotSendable):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, outbound.ErrSendingDisabled):
		writeError(w, http.StatusForbidden, "mailbox_paused", err.Error())
	case errors.Is(err, outbound.ErrSendingPaused):
		writeError(w, http.StatusForbidden, "mailbox_paused", err.Error())
	case errors.Is(err, outbound.ErrMailboxFull):
		writeError(w, http.StatusForbidden, "mailbox_full", err.Error())
	case errors.Is(err, outbound.ErrMessageTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "message_too_large", err.Error())
	case errors.Is(err, outbound.ErrInvalidAddress):
		writeError(w, http.StatusBadRequest, "invalid_address", err.Error())
	case errors.Is(err, outbound.ErrNoRecipients):
		writeError(w, http.StatusBadRequest, "no_recipients", err.Error())
	case errors.Is(err, outbound.ErrLimitExceeded):
		writeError(w, http.StatusTooManyRequests, "limit_exceeded", err.Error())
	case errors.Is(err, authn.ErrLocked):
		writeErrorHint(w, http.StatusTooManyRequests, "account_locked", err.Error(),
			"wait for the lockout to expire; further failures extend the window")
	case errors.Is(err, authn.ErrRateLimited):
		writeErrorHint(w, http.StatusTooManyRequests, "rate_limited", err.Error(),
			"too many failed attempts from this source address; wait before retrying")
	case errors.Is(err, authn.ErrTOTPRequired):
		writeErrorHint(w, http.StatusUnauthorized, "mfa_required", err.Error(),
			"repeat the login with the totp_code field from your authenticator app")
	case errors.Is(err, authn.ErrTOTPInvalid):
		writeError(w, http.StatusUnauthorized, "mfa_invalid", err.Error())
	case errors.Is(err, authn.ErrMFANotEnrolled), errors.Is(err, authn.ErrMFAAlreadyEnabled):
		writeError(w, http.StatusConflict, "mfa_state", err.Error())
	case errors.Is(err, authn.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", err.Error())
	case errors.Is(err, authn.ErrAmbiguousLogin):
		writeErrorHint(w, http.StatusConflict, "ambiguous_login", err.Error(),
			"this login e-mail exists in more than one organisation; use the full mailbox address of your organisation or contact support")
	case errors.Is(err, authn.ErrStaffImpersonation):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, authn.ErrCredentialChangeNotAllowed):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, authn.ErrAppPasswordLimit):
		writeError(w, http.StatusBadRequest, "limit_exceeded", err.Error())
	case errors.Is(err, store.ErrReservedLocal):
		writeErrorHint(w, http.StatusConflict, "reserved_address", err.Error(),
			"platform role addresses (postmaster, abuse, admin, dmarc-reports, noreply, mailer-daemon and friends) are platform-operated; choose another local part")
	case errors.Is(err, store.ErrInvalidDomain):
		writeErrorHint(w, http.StatusBadRequest, "invalid_domain", err.Error(),
			"use a registrable FQDN: lowercase labels, hyphens not leading/trailing, a lettered TLD")
	case errors.Is(err, store.ErrDomainInUse):
		writeErrorHint(w, http.StatusConflict, "domain_in_use", err.Error(),
			"delete the aliases and forwarding addresses on this domain first")
	case errors.Is(err, errKeyReuse):
		writeErrorHint(w, http.StatusUnprocessableEntity, "idempotency_key_reuse", err.Error(),
			"repeat the exact same request body with this key, or send a fresh key")
	default:
		log.Printf("[api] internal error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}
