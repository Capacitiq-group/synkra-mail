// Package trackingd: the isolated tracking service (Section 9.1).
//
// It is a deliberately dumb, stateless public endpoint pair:
//
//	GET /o/{pixel_id}.gif?s={sig}   — 1×1 open pixel
//	GET /c/{link_id}?u={b64}&s={sig} — click redirect
//
// It verifies HMAC signatures (no enumeration / open-redirect abuse), posts
// the event to the mail core's internal endpoint, then serves. It holds NO
// database credentials and can be blocked or rate-limited without touching
// mail flow.
package trackingd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"synkra.email/mail/internal/krypto"
	"synkra.email/mail/internal/outbound"
)

const pixelGIF = "\x47\x49\x46\x38\x39\x61\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff\x21\xf9\x04\x01\x00\x00\x00\x00\x2c\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02\x44\x01\x00\x3b"

type Service struct {
	CoreInternalURL string // e.g. http://127.0.0.1:8080/api/v1/internal/tracking/events
	Secret          []byte
	HTTP            *http.Client
}

func New(coreInternalURL string, secret []byte) *Service {
	return &Service{
		CoreInternalURL: coreInternalURL,
		Secret:          secret,
		HTTP:            &http.Client{Timeout: 5 * time.Second},
	}
}

func (s *Service) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/o/{pixel}.gif", s.handleOpen)
	r.Get("/o/{pixel}", s.handleOpen)
	r.Get("/c/{link}", s.handleClick)
	r.Get("/u/{token}", s.handleUnsubscribe)
	r.Post("/u/{token}", s.handleUnsubscribe)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})
	return r
}

func (s *Service) handleOpen(w http.ResponseWriter, r *http.Request) {
	pixel := chi.URLParam(r, "pixel")
	sig := r.URL.Query().Get("s")
	if !outbound.VerifyPixelSig(s.Secret, pixel, sig) {
		http.Error(w, "bad signature", http.StatusForbidden)
		return
	}
	// Fire-and-forget: the pixel must never block on core availability.
	go s.postEvent("open", pixel, clientFamily(r.UserAgent()), ipHash(r))
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
	_, _ = w.Write([]byte(pixelGIF))
}

func (s *Service) handleClick(w http.ResponseWriter, r *http.Request) {
	link := chi.URLParam(r, "link")
	u := r.URL.Query().Get("u")
	sig := r.URL.Query().Get("s")
	target, err := base64.RawURLEncoding.DecodeString(u)
	if err != nil || len(target) == 0 {
		http.Error(w, "bad target", http.StatusBadRequest)
		return
	}
	if !outbound.VerifyClickSig(s.Secret, link, string(target), sig) {
		http.Error(w, "bad signature", http.StatusForbidden)
		return
	}
	// Open-redirect guard: only http(s) targets (Section 9.1).
	t := string(target)
	parsed, perr := url.Parse(t)
	if perr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		http.Error(w, "bad target scheme", http.StatusBadRequest)
		return
	}
	go s.postEvent("click", link, clientFamily(r.UserAgent()), ipHash(r))
	http.Redirect(w, r, t, http.StatusFound)
}

// handleUnsubscribe: one-click list unsubscribe (register §12e). The token is
// HMAC-signed by the core; the tracker verifies and forwards without holding
// database credentials, then answers with a plain HTML page.
func (s *Service) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	sig := r.URL.Query().Get("s")
	orgID, mailboxID, rcpt, ok := outbound.VerifyUnsubscribeSig(s.Secret, token, sig)
	if !ok {
		http.Error(w, "bad signature", http.StatusForbidden)
		return
	}
	go s.postUnsubscribe(orgID, mailboxID, rcpt)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<!doctype html><html><head><meta charset=\"utf-8\"><title>Unsubscribed</title></head>" +
		"<body style=\"font-family:sans-serif;max-width:28rem;margin:4rem auto\">" +
		"<h1>Unsubscribed</h1><p>This address has been removed from the mailing list. " +
		"We are sorry to see you go.</p></body></html>"))
}

func (s *Service) postUnsubscribe(orgID, mailboxID uuid.UUID, rcpt string) {
	payload := map[string]any{
		"kind":       "unsubscribe",
		"org_id":     orgID.String(),
		"mailbox_id": mailboxID.String(),
		"rcpt":       rcpt,
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.CoreInternalURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Synkra-Tracker-Signature", krypto.HMACSign([]byte(s.Secret), string(body)))
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// postEvent signs and posts the event to the core internal endpoint.
func (s *Service) postEvent(kind, id, clientFamily, ipHash string) {
	payload := map[string]any{
		"kind":          kind,
		"id":            id,
		"occurred_at":   time.Now().UTC(),
		"client_family": clientFamily,
		"ip_hash":       ipHash,
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.CoreInternalURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[tracker] build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Synkra-Tracker-Signature", krypto.HMACSign(s.Secret, string(body)))
	resp, err := s.HTTP.Do(req)
	if err != nil {
		log.Printf("[tracker] post %s event: %v", kind, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[tracker] core rejected %s event: %s", kind, resp.Status)
	}
}

// clientFamily: coarse UA family (no fingerprinting — Section 9.3).
func clientFamily(ua string) string {
	if ua == "" {
		return ""
	}
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "googleimageproxy"), strings.Contains(l, "apple"):
		if strings.Contains(l, "mail") {
			return "apple-mail"
		}
	case strings.Contains(l, "firefox"), strings.Contains(l, "gecko/"):
		return "firefox"
	case strings.Contains(l, "chrome"), strings.Contains(l, "crios"):
		return "chrome"
	case strings.Contains(l, "safari"):
		return "safari"
	case strings.Contains(l, "outlook"):
		return "outlook"
	}
	return firstToken(ua)
}

func firstToken(s string) string {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// ipHash: salted hash, never the raw IP (Section 9.3).
func ipHash(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	return shortHash(ip)
}

func shortHash(s string) string {
	return krypto.HMACSign([]byte("ip-salt-v1"), s)[:16]
}
