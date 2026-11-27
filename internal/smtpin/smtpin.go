// Package smtpin: inbound MX + submission (Section 6.1). go-smtp handles the
// wire; every policy decision (routing, verdicts, limits, tenancy) is ours.
package smtpin

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	spf "blitiri.com.ar/go/spf"
	"github.com/emersion/go-msgauth/dkim"
	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"

	"synkra.email/mail/internal/authn"
	"synkra.email/mail/internal/config"
	"synkra.email/mail/internal/ingest"
	"synkra.email/mail/internal/mimeutil"
	"synkra.email/mail/internal/store"
)

// Server wraps the go-smtp server for the MX role (port 25).
type Server struct {
	srv  *gosmtp.Server
	Deps *ingest.Deps
	Cfg  *config.Config
	Auth *store.Store
}

// New creates the inbound MX server.
func New(cfg *config.Config, deps *ingest.Deps) *Server {
	s := &Server{Deps: deps, Cfg: cfg}
	backend := gosmtp.BackendFunc(func(c *gosmtp.Conn) (gosmtp.Session, error) {
		return &inboundSession{srv: s, conn: c, remoteIP: remoteIP(c)}, nil
	})
	srv := gosmtp.NewServer(backend)
	srv.Domain = cfg.PlatformDomain
	srv.MaxMessageBytes = cfg.MaxMessageBytes
	srv.MaxRecipients = 0         // unlimited at RCPT stage; policy caps per message live in the pipeline
	srv.AllowInsecureAuth = false // auth is never required on the MX; relay is impossible by routing
	srv.ReadTimeout = 2 * time.Minute
	srv.WriteTimeout = 2 * time.Minute
	srv.EnableREQUIRETLS = false
	if cfg.TLSEnabled {
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{*mustCert(cfg)}}
	}
	s.srv = srv
	return s
}

func mustCert(cfg *config.Config) *tls.Certificate {
	cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		panic(fmt.Sprintf("tls cert: %v", err))
	}
	return &cert
}

func (s *Server) Serve(ln net.Listener) error {
	return s.srv.Serve(limitConns(ln, s.Cfg.MaxInboundConns))
}
func (s *Server) ListenAndServe() error { return s.srv.ListenAndServe() }
func (s *Server) Close() error          { return s.srv.Close() }

// limitConns caps concurrent inbound connections (audit item 29: the inbound
// SMTP listener had no cap at all — a connection flood held one goroutine and
// buffers each). In excess of the cap, connections queue in the accept
// backlog instead of being served, which back-pressures the flood.
func limitConns(ln net.Listener, max int) net.Listener {
	if max <= 0 {
		max = 256
	}
	return &cappedListener{Listener: ln, sem: make(chan struct{}, max)}
}

type cappedListener struct {
	net.Listener
	sem chan struct{}
}

func (c *cappedListener) Accept() (net.Conn, error) {
	conn, err := c.Listener.Accept()
	if err != nil {
		return nil, err
	}
	c.sem <- struct{}{}
	return &cappedConn{Conn: conn, release: func() { <-c.sem }}, nil
}

type cappedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *cappedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}

func remoteIP(c *gosmtp.Conn) net.IP {
	if nc := c.Conn(); nc != nil {
		if addr, ok := nc.RemoteAddr().(*net.TCPAddr); ok {
			return addr.IP
		}
	}
	return nil
}

// inboundSession is one SMTP transaction.
type inboundSession struct {
	srv      *Server
	conn     *gosmtp.Conn
	remoteIP net.IP
	helo     string
	from     string
	rcpts    []string
}

func (s *inboundSession) Reset() {
	s.from = ""
	s.rcpts = nil
}

func (s *inboundSession) Logout() error { return nil }

func (s *inboundSession) Mail(from string, opts *gosmtp.MailOptions) error {
	if opts != nil && opts.Size != 0 && opts.Size > s.srv.Cfg.MaxMessageBytes {
		return &gosmtp.SMTPError{Code: 552, EnhancedCode: gosmtp.EnhancedCode{5, 3, 4}, Message: "message exceeds fixed maximum size"}
	}
	s.from = from
	return nil
}

// Rcpt enforces: MX accepts RCPT only for domains we host (Section 7.4.1).
// Full validation happens again inside the pipeline at DATA time.
func (s *inboundSession) Rcpt(to string, opts *gosmtp.RcptOptions) error {
	local, domain, err := splitAddr(to)
	if err != nil {
		return &gosmtp.SMTPError{Code: 501, Message: "bad recipient"}
	}
	_, _, rerr := store.RouteAddress(context.Background(), s.srv.Deps.Store.Pool, domain, local)
	if rerr != nil {
		if errors.Is(rerr, store.ErrNotFound) {
			// Distinguish: unhosted domain → 554 (relay attempt), unknown user → 550.
			_, derr := store.GetDomainByName(context.Background(), s.srv.Deps.Store.Pool, domain)
			if derr != nil {
				return &gosmtp.SMTPError{Code: 554, Message: "5.1.1 relay access denied"}
			}
			return &gosmtp.SMTPError{Code: 550, EnhancedCode: gosmtp.EnhancedCode{5, 1, 1}, Message: "no such mailbox"}
		}
		return &gosmtp.SMTPError{Code: 451, Message: "4.3.0 temporary failure"}
	}
	if len(s.rcpts) >= 50 {
		return &gosmtp.SMTPError{Code: 452, Message: "4.5.3 too many recipients"}
	}
	s.rcpts = append(s.rcpts, to)
	return nil
}

// Data runs the filter pipeline (Section 7.2): SPF → DKIM → DMARC → score → store.
func (s *inboundSession) Data(r io.Reader) error {
	raw, err := io.ReadAll(io.LimitReader(r, s.srv.Cfg.MaxMessageBytes+1))
	if err != nil {
		return &gosmtp.SMTPError{Code: 552, Message: "read error"}
	}
	if int64(len(raw)) > s.srv.Cfg.MaxMessageBytes {
		return &gosmtp.SMTPError{Code: 552, EnhancedCode: gosmtp.EnhancedCode{5, 3, 4}, Message: "message exceeds fixed maximum size"}
	}
	if len(s.rcpts) == 0 {
		return &gosmtp.SMTPError{Code: 503, Message: "no valid recipients"}
	}

	av := s.srv.verifyAuth(context.Background(), raw, s.remoteIP, s.helo, s.from)

	ctx := context.Background()
	for _, rcpt := range s.rcpts {
		if _, err := s.srv.Deps.DeliverInbound(ctx, raw, s.from, rcpt, av); err != nil {
			var smtpErr *ingest.SMTPError
			if errors.As(err, &smtpErr) {
				return &gosmtp.SMTPError{Code: smtpErr.Code, Message: smtpErr.Message}
			}
			return &gosmtp.SMTPError{Code: 451, Message: "4.3.0 delivery failed"}
		}
	}
	return nil
}

func resScore(r *ingest.Result) float64 {
	if r == nil {
		return -1
	}
	return r.SpamScore
}

func resQuar(r *ingest.Result) bool {
	return r != nil && r.Quarantine
}

// verifyAuth runs the library verifications and returns our verdict struct.
// Verdict *decisions* are applied by ingest.Score + DeliverInbound, not here.
func (s *Server) verifyAuth(ctx context.Context, raw []byte, ip net.IP, helo, envFrom string) *ingest.AuthVerdicts {
	av := &ingest.AuthVerdicts{SPF: "none", DKIM: "none", DMARC: "none"}

	// SPF (library evaluates; we record).
	if ip != nil && envFrom != "" {
		res, _ := spf.CheckHostWithSender(ip, helo, envFrom)
		av.SPF = string(res)
		if av.SPF == "" {
			av.SPF = "none"
		}
	}

	// DKIM: verify any signatures; remember EVERY passing domain so DMARC can
	// pick the aligned one (audit item 8).
	if vs, err := dkim.Verify(bytes.NewReader(raw)); err == nil && len(vs) > 0 {
		var passing []string
		for _, v := range vs {
			if v.Err == nil {
				passing = append(passing, strings.ToLower(strings.TrimSuffix(v.Domain, ".")))
			}
		}
		if len(passing) > 0 {
			av.DKIM = "pass"
			av.DKIMDomain = passing[0]
			av.DKIMPassDomains = passing
		} else {
			av.DKIM = "fail"
		}
	}

	// DMARC: rewritten evaluation (dmarc.go) — exact tag parsing, org-domain
	// policy fallback, relaxed alignment, any aligned DKIM pass, and the From
	// domain parsed by the standard address parser (audit item 8).
	fromDomain := FromDomainOfHeader(rawHeaderValue(raw, "From"))
	av.FromDomain = fromDomain
	if fromDomain != "" {
		rec := lookupDMARCRecord(ctx, fromDomain)
		av.DMARCRecord = rec
		spfDom := ""
		if av.SPF == "pass" {
			spfDom = spfDomain(envFrom)
		}
		result, pol := EvaluateDMARC(fromDomain, rec, spfDom, av.DKIMPassDomains)
		av.DMARC = result
		av.DMARCPolicy = pol
	}
	return av
}

// rawHeaderValue extracts one header field's value from the wire form
// (case-insensitive, folded continuation lines joined) — "" when absent.
func rawHeaderValue(raw []byte, name string) string {
	nameL := strings.ToLower(name)
	var value string
	var found bool
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if len(line) == 0 {
			break // header block ended
		}
		if line[0] == ' ' || line[0] == '\t' {
			if found {
				value += " " + strings.TrimSpace(string(line))
			}
			continue
		}
		i := bytes.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		if found || !strings.EqualFold(string(line[:i]), nameL) {
			continue
		}
		found = true
		value = strings.TrimSpace(string(line[i+1:]))
	}
	return value
}

// DeliverLoopback lives in ingest (DeliverInbound is there) — alias kept for callers.
var DeliverLoopback = ingest.DeliverLoopback

func splitAddr(addr string) (local, domain string, err error) {
	at := strings.LastIndex(addr, "@")
	if at <= 0 || at == len(addr)-1 {
		return "", "", errors.New("invalid address")
	}
	return addr[:at], addr[at+1:], nil
}

// Submission server: authenticated relay for our users (ports 465/587).
// Auth is app-password only (Section 6.1); From must be sendable (Section 7.4.2);
// tier limits + breaker checked at compose admission (outbound package).
type SubmissionServer struct {
	srv  *gosmtp.Server
	Deps *ingest.Deps
	Cfg  *config.Config
	Auth *AuthAdapter
	// QueueSubmission is injected at wiring time (outbound.QueueSubmission) —
	// keeps smtpin free of the outbound import.
	QueueSubmission func(ctx context.Context, userID, orgID string, raw []byte, from string, rcpts []string) error
}

func NewSubmission(cfg *config.Config, deps *ingest.Deps, auth *AuthAdapter) *SubmissionServer {
	s := &SubmissionServer{Deps: deps, Cfg: cfg, Auth: auth}
	backend := gosmtp.BackendFunc(func(c *gosmtp.Conn) (gosmtp.Session, error) {
		return &subSession{srv: s, conn: c}, nil
	})
	srv := gosmtp.NewServer(backend)
	srv.Domain = cfg.PlatformDomain
	srv.MaxMessageBytes = cfg.MaxMessageBytes
	srv.AllowInsecureAuth = cfg.Env != "prod" // dev plaintext for tests; prod requires TLS before AUTH
	srv.EnableREQUIRETLS = false
	// Submission read/write deadlines (audit item 29: none were set — a
	// silent client held the session forever).
	srv.ReadTimeout = 3 * time.Minute
	srv.WriteTimeout = 3 * time.Minute
	if cfg.TLSEnabled {
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{*mustCert(cfg)}}
	}
	s.srv = srv
	return s
}

func (s *SubmissionServer) Serve(ln net.Listener) error { return s.srv.Serve(ln) }
func (s *SubmissionServer) Close() error                { return s.srv.Close() }

// AuthAdapter decouples smtpin from the authn package (avoid import cycles).
type AuthAdapter struct {
	AuthenticateAppPassword func(ctx context.Context, username, secret, ip string) (userID, orgID string, err error)
}

type subSession struct {
	srv    *SubmissionServer
	conn   *gosmtp.Conn
	userID string
	orgID  string
	from   string
	rcpts  []string
}

func (s *subSession) Reset()        { s.from = ""; s.rcpts = nil }
func (s *subSession) Logout() error { return nil }

func (s *subSession) AuthMechanisms() []string { return []string{"PLAIN"} }

// Auth implements go-smtp v0.25's AuthSession (sasl.Server based).
func (s *subSession) Auth(mech string) (sasl.Server, error) {
	if mech != "PLAIN" {
		return nil, errors.New("unsupported mechanism")
	}
	return sasl.NewPlainServer(func(identity, username, password string) error {
		return s.AuthPlain(username, password)
	}), nil
}

func (s *subSession) AuthPlain(username, password string) error {
	// Source IP for the failed-attempt lockout (register §1.6).
	ip := ""
	if s.conn != nil {
		if nc := s.conn.Conn(); nc != nil {
			ip = nc.RemoteAddr().String()
		}
	}
	userID, orgID, err := s.srv.Auth.AuthenticateAppPassword(context.Background(), username, password, ip)
	if err != nil {
		if errors.Is(err, authn.ErrLocked) {
			return &gosmtp.SMTPError{Code: 535, Message: "account locked: too many failed attempts"}
		}
		return &gosmtp.SMTPError{Code: 535, Message: "authentication failed (app passwords only)"}
	}
	s.userID = userID
	s.orgID = orgID
	return nil
}

func (s *subSession) Mail(from string, opts *gosmtp.MailOptions) error {
	if s.userID == "" {
		return gosmtp.ErrAuthRequired // unauthenticated relay gets 530 (Section 5.2)
	}
	s.from = from
	return nil
}

func (s *subSession) Rcpt(to string, opts *gosmtp.RcptOptions) error {
	if s.userID == "" {
		return gosmtp.ErrAuthRequired
	}
	if len(s.rcpts) >= 50 {
		return &gosmtp.SMTPError{Code: 452, Message: "4.5.3 too many recipients"}
	}
	// Validate here, not only in the pipeline (audit item 2): an
	// unvalidated string went into `RCPT TO:<%s>` on the wire, and
	// envelope-only (Bcc) recipients previously skipped validation entirely.
	if !mimeutil.ValidAddress(to) {
		return &gosmtp.SMTPError{Code: 501, Message: "5.1.3 bad recipient address syntax"}
	}
	s.rcpts = append(s.rcpts, to)
	return nil
}

func (s *subSession) Data(r io.Reader) error {
	if s.userID == "" {
		return gosmtp.ErrAuthRequired
	}
	raw, err := io.ReadAll(io.LimitReader(r, s.srv.Cfg.MaxMessageBytes+1))
	if err != nil || int64(len(raw)) > s.srv.Cfg.MaxMessageBytes {
		return &gosmtp.SMTPError{Code: 552, Message: "message exceeds size limit"}
	}
	if s.srv.QueueSubmission == nil {
		return &gosmtp.SMTPError{Code: 451, Message: "4.3.0 submission not wired"}
	}
	// Wire normalisation (build register 3.7): the queued message is stored and
	// relayed verbatim, so bare LF must become CRLF here.
	raw = mimeutil.NormalizeWire(raw)
	return s.srv.QueueSubmission(context.Background(), s.userID, s.orgID, raw, s.from, s.rcpts)
}
