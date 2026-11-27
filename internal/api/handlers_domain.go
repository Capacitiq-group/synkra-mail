// handlers_domain.go: domain lifecycle endpoints added by the 27 November
// sweep (DELETE was missing entirely — domains could be registered but never
// decommissioned).
package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/store"
)

func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	ac := authFrom(r.Context())
	id, ok := parseIDParam(chi.URLParam(r, "id"), ids.Domain)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "bad domain id")
		return
	}
	dom, err := store.GetDomain(r.Context(), s.Store.Pool, id)
	if err != nil || dom.OrgID == nil || *dom.OrgID != ac.User.OrgID {
		writeError(w, http.StatusForbidden, "forbidden", "domain not owned by org")
		return
	}
	if dom.IsPlatform {
		writeError(w, http.StatusForbidden, "forbidden", "the platform domain cannot be deleted")
		return
	}
	err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
		if err := store.DeleteDomain(r.Context(), tx, dom.ID); err != nil {
			return err
		}
		return store.RecordAudit(r.Context(), tx, &ac.User.ID, nil, ac.Impersonating(),
			"domain.delete", "domain", dom.ID.String(), dom.Name, nil)
	})
	if err != nil {
		if errors.Is(err, store.ErrDomainInUse) {
			writeErrorHint(w, http.StatusConflict, "domain_in_use", err.Error(),
				"delete the aliases and forwarding addresses on this domain first")
			return
		}
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
