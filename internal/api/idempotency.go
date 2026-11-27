// Idempotency-Key support (build register §1.9): the webmail will double-submit.
// A keyed request stores its first response per user+key; retries replay it.
package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"synkra.email/mail/internal/authn"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

// errKeyReuse: the same key arrived with a different request body.
var errKeyReuse = errors.New("idempotency key already used with a different request body")

// readBody drains the (already size-capped) request body once, so the raw bytes
// can be hashed for idempotency and decoded for the handler.
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func bodyHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// idempotentBegin resolves the key against stored responses. replay=false with
// err=nil means "no key or first sight of it — execute normally".
func (s *Server) idempotentBegin(ctx context.Context, ac *authn.AuthContext, key, endpoint string, body []byte) (status int, response []byte, replay bool, err error) {
	if key == "" {
		return 0, nil, false, nil
	}
	rec, gerr := store.GetIdempotentResponse(ctx, s.Store.Pool, ac.User.ID, key, endpoint)
	if errors.Is(gerr, store.ErrNotFound) {
		return 0, nil, false, nil
	}
	if gerr != nil {
		return 0, nil, false, gerr
	}
	if rec.RequestHash != bodyHash(body) {
		return 0, nil, false, errKeyReuse
	}
	return rec.Status, rec.Response, true, nil
}

func (s *Server) replayJSON(w http.ResponseWriter, status int, response []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Idempotent-Replay", "true")
	w.WriteHeader(status)
	_, _ = w.Write(response)
}

// idempotentFinish stores the response of the first keyed request. Storage is
// best-effort ordering-wise: the handler stores before writing so a replay can
// never double-execute the side effect, but a storage failure surfaces as an
// error response rather than a silent double execution.
func (s *Server) idempotentFinish(ctx context.Context, ac *authn.AuthContext, key, endpoint string, body []byte, status int, response any) error {
	if key == "" {
		return nil
	}
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	rec := &model.Idempotency{
		ID: uuid.New(), UserID: ac.User.ID, Key: key, Endpoint: endpoint,
		RequestHash: bodyHash(body), Status: status, Response: raw,
	}
	return s.Store.WithTx(ctx, func(tx store.Tx) error {
		return store.InsertIdempotentResponse(ctx, tx, rec)
	})
}
