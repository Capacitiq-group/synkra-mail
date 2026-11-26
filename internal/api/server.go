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

		// Session-authenticated
		r.Group(func(r chi.Router) {
			r.Use(s.requireSession)
			r.Get("/me", s.handleMe)
			r.Post("/auth/logout", s.handleLogout)

			r.Get("/app-passwords", s.handleListAppPasswords)
			r.Post("/app-passwords", s.handleCreateAppPassword)
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

			r.Get("/addresses", s.handleListAddresses)
			r.Post("/addresses", s.handleCreateAddress)
			r.Delete("/addresses/{id}", s.handleDeleteAddress)
			r.Get("/domains", s.handleListDomains)
			r.Post("/domains", s.handleCreateDomain)
			r.Post("/domains/{id}/verify", s.handleVerifyDomain)
			r.Get("/domains/{id}/dns", s.handleDomainDNS)

			r.Get("/org", s.handleGetOrg)
			r.Put("/org/tracking", s.handleSetTracking)
			r.Post("/threads/{id}/note", s.handleThreadNote)
			r.Post("/threads/{id}/assign", s.handleThreadAssign)

			r.Post("/users/invite", s.handleCreateUser) // org admin adds mailboxes
		})

		// Platform staff (admin portal — Section 11)
		r.Group(func(r chi.Router) {
			r.Use(s.requireStaff)
			r.Get("/admin/orgs", s.handleAdminListOrgs)
			r.Post("/admin/orgs", s.handleAdminCreateOrg)
			r.Post("/admin/orgs/{id}/approve", s.handleAdminApproveOrg)
			r.Post("/admin/orgs/{id}/suspend", s.handleAdminSuspendOrg)
			r.Post("/admin/orgs/{id}/resume", s.handleAdminResumeOrg)
			r.Post("/admin/orgs/{id}/impersonate", s.handleAdminImpersonate)
			r.Get("/admin/audit", s.handleAdminAudit)
			r.Get("/admin/queue", s.handleAdminQueue)
			r.Get("/admin/stats", s.handleAdminStats)
			r.Get("/admin/orgs/{id}/jobs", s.handleAdminOrgJobs)
		})

		// Internal: the isolated tracker posts events here (HMAC-authenticated —
		// it holds no database credentials, Section 9.1).
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
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid or expired session")
			return
		}
		if ac.Org.Status == "suspended" && !strings.HasPrefix(r.URL.Path, "/admin/") {
			writeError(w, http.StatusForbidden, "forbidden", "organisation suspended")
			return
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
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxAuth, ac)))
	})
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
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	var b errBody
	b.Error.Code = code
	b.Error.Message = msg
	b.Error.RequestID = uuid.NewString()
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
		writeError(w, http.StatusTooManyRequests, "account_locked", err.Error())
	case errors.Is(err, authn.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", err.Error())
	case errors.Is(err, authn.ErrAppPasswordLimit):
		writeError(w, http.StatusBadRequest, "limit_exceeded", err.Error())
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
