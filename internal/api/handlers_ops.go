// Ops endpoints: liveness and readiness probes for systemd, monitoring and
// future load balancers (design doc Section 13.2). Unauthenticated by design:
// they expose no data, only aggregate health.
package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"synkra.email/mail/internal/blob"
)

// handleHealthz reports process liveness. Always 200 while the process runs.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// probeSHA is a well-formed-but-absent content address used to exercise the
// blob backend end to end (fs: filesystem reachable; s3: full signed
// roundtrip) without touching real mail data.
var probeSHA = strings.Repeat("0", 64)

// handleReadyz reports dependency readiness: database and blob store.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := s.Store.Pool.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "database unreachable")
		return
	}

	if s.Blobs != nil {
		if _, err := s.Blobs.Stat(ctx, probeSHA); err != nil && !errors.Is(err, blob.ErrNotFound) {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "blob store unreachable")
			return
		}
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
