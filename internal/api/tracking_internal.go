// Internal tracking-event ingest: the isolated tracker posts verified events
// here (HMAC-authenticated). The tracker holds no database credentials —
// Section 9.1 isolation rule. Classifier implements the three-label honesty
// model (Section 9.2).
package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"synkra.email/mail/internal/ids"
	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

type internalEventReq struct {
		OrgID      string    `json:"org_id,omitempty"`
		MailboxID  string    `json:"mailbox_id,omitempty"`
		Rcpt       string    `json:"rcpt,omitempty"`
	Kind         string    `json:"kind"` // open | click | unsubscribe
	ID           string    `json:"id"`   // pixel id or link id
	OccurredAt   time.Time `json:"occurred_at"`
	ClientFamily string    `json:"client_family"`
	IPHash       string    `json:"ip_hash"`
	Recipient    string    `json:"recipient"` // resolved by the tracker from request context
}

// handleInternalTrackingEvent consumes verified tracker events.
func (s *Server) handleInternalTrackingEvent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "unreadable body")
		return
	}
	// HMAC service authentication (tracker ↔ core share a secret).
	sig := r.Header.Get("X-Synkra-Tracker-Signature")
	if sig == "" || !krypto.HMACVerify([]byte(s.Cfg.TrackingSecret), string(body), sig) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "bad signature")
		return
	}
	var req internalEventReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_credentials", "bad body")
		return
	}
	if req.OccurredAt.IsZero() {
		req.OccurredAt = time.Now()
	}

	switch req.Kind {
	case "open":
		pixelID, ok := parseIDParam(req.ID, ids.Pixel)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_credentials", "bad pixel id")
			return
		}
		pixel, err := store.GetPixelForEvent(r.Context(), s.Store.Pool, pixelID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "pixel not found")
			return
		}
		firstAt, hasFirst := s.firstOpenAt(r.Context(), pixelID)
		conf := ClassifyOpen(req.ClientFamily, firstAt, hasFirst, s.isInternalRecipient(r.Context(), pixel.Recipient))
		err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
			return store.InsertOpenEvent(r.Context(), tx, &model.OpenEvent{
				ID: uuid.New(), PixelID: pixel.ID, OccurredAt: req.OccurredAt,
				Confidence: conf, ClientFamily: req.ClientFamily, IPHash: req.IPHash,
			})
		})
		if err != nil {
			mapError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "confidence": conf})

	case "click":
		linkID, ok := parseIDParam(req.ID, ids.Link)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_credentials", "bad link id")
			return
		}
		link, err := store.GetLinkForEvent(r.Context(), s.Store.Pool, linkID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "link not found")
			return
		}
		err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
			return store.InsertClickEvent(r.Context(), tx, &model.ClickEvent{
				ID: uuid.New(), LinkID: link.ID, OccurredAt: req.OccurredAt,
				ClientFamily: req.ClientFamily, IPHash: req.IPHash,
			})
		})
		if err != nil {
			mapError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "target": link.URL})

	case "unsubscribe":
		orgID, ok := parseIDParam(req.OrgID, ids.Org)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_credentials", "bad org id")
			return
		}
		if req.Rcpt == "" {
			writeError(w, http.StatusBadRequest, "invalid_credentials", "rcpt required")
			return
		}
		err = s.Store.WithTx(r.Context(), func(tx store.Tx) error {
			if err := store.Suppress(r.Context(), tx, orgID, req.Rcpt, "unsubscribe"); err != nil {
				return err
			}
			return store.RecordAudit(r.Context(), tx, nil, nil, false,
				"unsubscribe", "org", orgID.String(), req.Rcpt, nil)
		})
		if err != nil {
			mapError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unsubscribed": req.Rcpt})

	default:
		writeError(w, http.StatusBadRequest, "invalid_credentials", "kind must be open|click|unsubscribe")
	}
}

// firstOpenAt: a pixel with an existing event means this is a repeat load.
func (s *Server) firstOpenAt(ctx context.Context, pixelID uuid.UUID) (t time.Time, has bool) {
	pixel, err := store.GetPixelForEvent(ctx, s.Store.Pool, pixelID)
	if err != nil {
		return
	}
	v, err := store.TrackingEventsForMessage(ctx, s.Store.Pool, pixel.MessageID)
	if err != nil {
		return
	}
	for _, e := range v.OpenEvents {
		if e.PixelID == pixelID {
			return e.OccurredAt, true
		}
	}
	return
}

func (s *Server) isInternalRecipient(ctx context.Context, recipient string) bool {
	if recipient == "" {
		return false
	}
	dom := recipient
	if i := strings.LastIndex(recipient, "@"); i >= 0 {
		dom = recipient[i+1:]
	}
	_, err := store.GetDomainByName(ctx, s.Store.Pool, dom)
	return err == nil
}

// ClassifyOpen implements the three-label model (Section 9.2):
//
//	Opened             — confirmed human open, exact day/time (Synkra→Synkra)
//	Likely opened      — first open day/time reliable (external, proxy-filtered)
//	Possibly automated — machine prefetch or no reliable signal (Apple MPP etc.)
func ClassifyOpen(clientFamily string, firstOpen time.Time, hasFirstOpen bool, internalRecipient bool) string {
	fam := strings.ToLower(clientFamily)
	switch {
	case fam == "":
		return "possibly_automated"
	case strings.Contains(fam, "googleimageproxy"),
		strings.Contains(fam, "google httpsproxy"),
		strings.Contains(fam, "mpp"),
		strings.Contains(fam, "prefetch"),
		strings.Contains(fam, "bot"),
		strings.Contains(fam, "spider"),
		strings.Contains(fam, "fetcher"):
		return "possibly_automated"
	}
	// Apple Mail Privacy Protection pattern: repeat loads from an anonymized relay.
	if strings.Contains(fam, "macos") || strings.Contains(fam, "ios") || strings.Contains(fam, "macintosh") {
		if hasFirstOpen && time.Since(firstOpen) < 2*time.Minute {
			return "possibly_automated"
		}
	}
	if hasFirstOpen && time.Since(firstOpen) < 30*time.Second {
		// Rapid repeat loads from the same client: prefetch behaviour.
		return "possibly_automated"
	}
	if internalRecipient {
		return "opened"
	}
	return "likely_opened"
}
