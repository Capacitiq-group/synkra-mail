// Package config loads Synkra Email configuration from the environment.
// Every knob has a dev-safe default; production values arrive via env on the VPS.
// Fail-closed rule (2026-11-27 audit item 7): unknown SYNKRA_MAIL_ENV values are
// refused, prod requires strong secrets, and the outbound transport never
// silently falls back to loopback — it must be named.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"synkra.email/mail/internal/krypto"
)

// Config is the full process configuration. `maild` and `tracker` both consume it.
type Config struct {
	Env  string // dev | prod
	HTTP HTTP
	DB   DB

	PlatformDomain string // synkra.email (owner decision: pending DNS verification)
	TrackingHost   string // trk.synkra.email

	// Hostname is THIS server's mail identity: the EHLO name, the bounce
	// Reporting-MTA and the Message-ID domain, all in one knob. It must be a
	// resolvable A/AAAA with matching PTR (FCrDNS) — the sending VPS's mail name.
	Hostname string

	TrackingSecret string // HMAC secret shared between mail core and the tracker (rotatable)

	BlobMode string // fs | s3   (s3 adapter is the Stage-A production mode)
	BlobDir  string // fs mode root

	// S3 blob storage (BlobMode = "s3"). Endpoint-driven so any
	// S3-compatible provider works (Contabo Object Storage in Stage A,
	// Section 14.1). Endpoint/region/bucket are deployment configuration —
	// choosing them is the owner's residency sign-off, not a code decision.
	S3 S3

	SMTPInAddr     string // inbound MX listener (port 25 prod)
	SubmissionAddr string // submission listener (ports 465/587 prod; STARTTLS dev)
	IMAPAddr       string // IMAP listener (port 993 prod)
	HTTPAddr       string // internal API (webmail + admin clients)
	TrackerAddr    string // isolated tracking service listener
	TrackerURL     string // public base URL of the tracker (used for URL rewriting)

	// TLS: dev runs plaintext; prod terminates via ACME certs (Section 13.1).
	TLSCertFile string
	TLSKeyFile  string
	TLSEnabled  bool

	SubmissionTLSAddr string // implicit-TLS submission (465 prod). Empty = disabled (dev).

	// OutboundTransport names the delivery transport explicitly (audit item 7):
	// "auto" = smtp in prod, loopback in dev; "loopback" | "smtp" force it in
	// any env. The worker logs which one is live at startup.
	OutboundTransport string

	// OutboundSourceIP binds outbound SMTP connections to this local address
	// (audit item 20: the "additional sending IP" must actually be usable).
	// Empty = default source.
	OutboundSourceIP string

	// TrustedProxy: when true, X-Forwarded-For is honoured for rate limiting
	// (audit item 6: an untrusted XFF is a spray bypass). Dev default false.
	TrustedProxy bool

	// Secrets: at-rest encryption for DKIM private keys and TOTP seeds
	// (audit item 4). Required in prod; base64, 32 bytes when raw.
	KMSKey string

	// Domain policy (audit item 11).
	MaxDomainsPerOrg       int
	UnverifiedDomainExpiry time.Duration // janitor releases unverified domains after this
	ReverifyIntervalDays   int           // verified domains re-check ownership this often

	// Inbound protection (audit item 5 / 29).
	MaxInboundConns int // concurrent inbound SMTP connections (0 = 256 default)

	// Authoritative DNS edge (§4b — the dnsd process).
	DNSUDPAddr     string        // UDP listener (port 53 prod)
	DNSTCPAddr     string        // TCP listener (port 53 prod)
	DNSHealthAddr  string        // dnsd health endpoint (empty = none)
	DNSSecondaries []string      // CIDRs allowed to AXFR; EMPTY = deny all (4b.7)
	DNSNSHosts     []string      // authoritative NS hostnames (4b.2)
	DNSZoneRefresh time.Duration // zone re-read cadence

	// Limits (owner policy, Section 7.3).
	Limits Limits

	// Storage policy (Section 4.4 + owner decision).
	Storage Storage

	// Bounce circuit breaker (owner decision: 5% hard-bounce rate on a rolling window).
	Breaker Breaker

	// Warmup: per-IP daily outbound cap during IP warmup (Section 8.3). 0 = uncapped.
	// Counted across ALL orgs on the IP (audit item 20), not per org.
	WarmupDailyCap int

	// App password policy (owner decision): max 10 active, 180-day expiry.
	AppPasswordMaxActive  int
	AppPasswordMaxAgeDays int

	// Trial (owner decision): 14 days, no outbound sending until payment or admin approval.
	TrialDays int

	// Message size cap (plan cap; default 25 MB, Section 6.1).
	MaxMessageBytes int64

	// Outbound worker tuning.
	WorkerCount               int
	WorkerConcurrency         int // concurrent deliveries per worker (audit item 19: head-of-line blocking)
	WorkerPollInterval        time.Duration
	JanitorInterval           time.Duration
	LoginAttemptRetentionDays int
	RateEventRetentionDays    int
	IdempotencyRetentionDays  int
	BlobGCBatch               int
	QueueEscapeHatchJobs      int // Section 5.3: documented, not acted on at this scale
}

type HTTP struct {
	SessionTTL      time.Duration
	CookieName      string
	CookieSecret    string // HMAC key for signed cookies / internal service auth
	LoginMaxFails   int
	LoginWindow     time.Duration
	LoginLockoutDur time.Duration
	LoginIPMaxFails int // per-IP spray gate within the window (audit item 6)
	IMAPMaxConnUser int // per-user connection cap (Section 6.2)
}

type DB struct {
	URL string
}

// S3 holds the object-storage connection. Keys are secrets: never log them.
type S3 struct {
	Endpoint  string // e.g. https://eu2.contabostorage.com (scheme required)
	Region    string // SigV4 scope region; most S3-compatible providers accept any non-empty value
	Bucket    string
	AccessKey string
	SecretKey string
	PathStyle bool // bucket-in-path URLs (required by Contabo/MinIO; optional on AWS)
}

type Limits struct {
	// New-org tier (first 7 days / until verified): Section 7.3.
	NewOrgDaily      int // 100 messages/day per org
	NewMailboxHourly int // 20/hour per mailbox
	MaxRecipients    int // 50 recipients per message
	// Verified + paid tier.
	VerifiedMailboxDaily int // 500/day per mailbox
	VerifiedOrgDaily     int // 2000/day per org
	NewOrgDays           int // days the new tier applies
}

type Storage struct {
	WarnPercent     int  // 80
	Warn2Percent    int  // 90
	FullPercent     int  // 100
	GracePercent    int  // 110 = 100% + 10% grace buffer
	IncludedBytesMB int  // from plan, this is the fallback default (10 GB)
	NeverAutoDelete bool // owner policy: never delete anything automatically
}

type Breaker struct {
	HardBounceRatePct float64 // 5.0
	Window            time.Duration
	MinAttempts       int // minimum attempts in window before the breaker can trip
}

// ErrProdConfig is returned with a precise message when prod startup would
// run with an unsafe default (fail-closed, audit item 7).
var ErrProdConfig = errors.New("unsafe production configuration")

func Load() (*Config, error) {
	c := &Config{
		Env: envStr("SYNKRA_MAIL_ENV", "dev"),

		PlatformDomain: envStr("SYNKRA_MAIL_PLATFORM_DOMAIN", "synkra.email"),
		TrackingHost:   envStr("SYNKRA_MAIL_TRACKING_HOST", "trk.synkra.email"),
		// Default follows the planned Stage-A VPS (mx1.synkra.email did not
		// resolve at the 2026-11-25 DNS check; mx1.synkra.co.za does).
		Hostname:       envStr("SYNKRA_MAIL_HOSTNAME", "mx1.synkra.co.za"),
		TrackingSecret: envStr("SYNKRA_MAIL_TRACKING_SECRET", "dev-tracking-secret"),

		BlobMode: envStr("SYNKRA_MAIL_BLOB_MODE", "fs"),
		BlobDir:  envStr("SYNKRA_MAIL_BLOB_DIR", "./devdata/blobs"),

		S3: S3{
			Endpoint:  envStr("SYNKRA_MAIL_S3_ENDPOINT", ""),
			Region:    envStr("SYNKRA_MAIL_S3_REGION", "us-east-1"),
			Bucket:    envStr("SYNKRA_MAIL_S3_BUCKET", ""),
			AccessKey: envStr("SYNKRA_MAIL_S3_ACCESS_KEY", ""),
			SecretKey: envStr("SYNKRA_MAIL_S3_SECRET_KEY", ""),
			PathStyle: envStr("SYNKRA_MAIL_S3_PATH_STYLE", "true") == "true",
		},

		SMTPInAddr:     envStr("SYNKRA_MAIL_SMTP_IN_ADDR", "127.0.0.1:2025"),
		SubmissionAddr: envStr("SYNKRA_MAIL_SUBMISSION_ADDR", "127.0.0.1:2587"),
		IMAPAddr:       envStr("SYNKRA_MAIL_IMAP_ADDR", "127.0.0.1:1143"),
		HTTPAddr:       envStr("SYNKRA_MAIL_HTTP_ADDR", "127.0.0.1:8080"),
		TrackerAddr:    envStr("SYNKRA_MAIL_TRACKER_ADDR", "127.0.0.1:8081"),
		TrackerURL:     envStr("SYNKRA_MAIL_TRACKER_URL", "http://127.0.0.1:8081"),

		TLSCertFile:       envStr("SYNKRA_MAIL_TLS_CERT", ""),
		TLSKeyFile:        envStr("SYNKRA_MAIL_TLS_KEY", ""),
		SubmissionTLSAddr: envStr("SYNKRA_MAIL_SUBMISSION_TLS_ADDR", ""),

		OutboundTransport: envStr("SYNKRA_MAIL_OUTBOUND_TRANSPORT", "auto"),
		OutboundSourceIP:  envStr("SYNKRA_MAIL_OUTBOUND_SOURCE_IP", ""),
		TrustedProxy:      envStr("SYNKRA_MAIL_TRUSTED_PROXY", "") == "true",
		KMSKey:            envStr("SYNKRA_MAIL_KMS_KEY", ""),

		MaxDomainsPerOrg:       envInt("SYNKRA_MAIL_MAX_DOMAINS", 20),
		UnverifiedDomainExpiry: envDur("SYNKRA_MAIL_UNVERIFIED_DOMAIN_EXPIRY", 30*24*time.Hour),
		ReverifyIntervalDays:   envInt("SYNKRA_MAIL_REVERIFY_DAYS", 365),

		MaxInboundConns: envInt("SYNKRA_MAIL_MAX_INBOUND_CONNS", 256),

		DNSUDPAddr:     envStr("SYNKRA_MAIL_DNS_UDP_ADDR", "127.0.0.1:5353"),
		DNSTCPAddr:     envStr("SYNKRA_MAIL_DNS_TCP_ADDR", "127.0.0.1:5353"),
		DNSHealthAddr:  envStr("SYNKRA_MAIL_DNS_HEALTH_ADDR", ""),
		DNSSecondaries: envList("SYNKRA_MAIL_DNS_SECONDARIES"), // empty = AXFR denied (4b.7)
		DNSNSHosts:     envListDefault("SYNKRA_MAIL_DNS_NS_HOSTS", "ns1.synkra.email,ns2.synkra.email"),
		DNSZoneRefresh: envDur("SYNKRA_MAIL_DNS_ZONE_REFRESH", 15*time.Second),

		Limits: Limits{
			NewOrgDaily:          envInt("SYNKRA_MAIL_NEW_ORG_DAILY", 100),
			NewMailboxHourly:     envInt("SYNKRA_MAIL_NEW_MAILBOX_HOURLY", 20),
			MaxRecipients:        envInt("SYNKRA_MAIL_MAX_RECIPIENTS", 50),
			VerifiedMailboxDaily: envInt("SYNKRA_MAIL_VERIFIED_MAILBOX_DAILY", 500),
			VerifiedOrgDaily:     envInt("SYNKRA_MAIL_VERIFIED_ORG_DAILY", 2000),
			NewOrgDays:           envInt("SYNKRA_MAIL_NEW_ORG_DAYS", 7),
		},

		Storage: Storage{
			WarnPercent:     80,
			Warn2Percent:    90,
			FullPercent:     100,
			GracePercent:    110,
			IncludedBytesMB: 10240,
			NeverAutoDelete: true,
		},

		Breaker: Breaker{
			HardBounceRatePct: envFloat("SYNKRA_MAIL_BREAKER_PCT", 5.0),
			Window:            envDur("SYNKRA_MAIL_BREAKER_WINDOW", 24*time.Hour),
			MinAttempts:       envInt("SYNKRA_MAIL_BREAKER_MIN", 20),
		},

		WarmupDailyCap: envInt("SYNKRA_MAIL_WARMUP_DAILY_CAP", 0),

		AppPasswordMaxActive:  envInt("SYNKRA_MAIL_APP_PW_MAX", 10),
		AppPasswordMaxAgeDays: envInt("SYNKRA_MAIL_APP_PW_AGE_DAYS", 180),

		TrialDays: envInt("SYNKRA_MAIL_TRIAL_DAYS", 14),

		MaxMessageBytes: int64(envInt("SYNKRA_MAIL_MAX_MESSAGE_MB", 25)) * 1024 * 1024,

		WorkerCount:               envInt("SYNKRA_MAIL_WORKERS", 4),
		WorkerConcurrency:         envInt("SYNKRA_MAIL_WORKER_CONCURRENCY", 8),
		WorkerPollInterval:        envDur("SYNKRA_MAIL_WORKER_POLL", 2*time.Second),
		JanitorInterval:           envDur("SYNKRA_MAIL_JANITOR_INTERVAL", time.Hour),
		LoginAttemptRetentionDays: envInt("SYNKRA_MAIL_LOG_RETENTION_DAYS", 30),
		RateEventRetentionDays:    envInt("SYNKRA_MAIL_RATE_RETENTION_DAYS", 90),
		IdempotencyRetentionDays:  envInt("SYNKRA_MAIL_IDEMPOTENCY_RETENTION_DAYS", 7),
		BlobGCBatch:               envInt("SYNKRA_MAIL_BLOB_GC_BATCH", 500),
		QueueEscapeHatchJobs:      5000,
	}

	c.HTTP = HTTP{
		SessionTTL:      envDur("SYNKRA_MAIL_SESSION_TTL", 14*24*time.Hour),
		CookieName:      "synkra_mail_session",
		CookieSecret:    envStr("SYNKRA_MAIL_COOKIE_SECRET", "dev-cookie-secret"),
		LoginMaxFails:   envInt("SYNKRA_MAIL_LOGIN_MAX_FAILS", 10),
		LoginWindow:     envDur("SYNKRA_MAIL_LOGIN_WINDOW", 10*time.Minute),
		LoginLockoutDur: envDur("SYNKRA_MAIL_LOGIN_LOCKOUT", 15*time.Minute),
		LoginIPMaxFails: envInt("SYNKRA_MAIL_LOGIN_IP_MAX_FAILS", 30),
		IMAPMaxConnUser: envInt("SYNKRA_MAIL_IMAP_MAX_CONN", 8),
	}

	c.DB.URL = os.Getenv("SYNKRA_MAIL_DATABASE_URL")
	if c.DB.URL == "" {
		return nil, fmt.Errorf("SYNKRA_MAIL_DATABASE_URL is required")
	}

	// One identity per machine, canonicalised: lowercase, no trailing dot
	// (RFC 1035 2.3.1 — a trailing dot breaks HELO comparisons and PTR match).
	c.Hostname = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.Hostname)), ".")
	c.PlatformDomain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.PlatformDomain)), ".")
	c.TrackingHost = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.TrackingHost)), ".")
	if c.Hostname == "" {
		return nil, fmt.Errorf("SYNKRA_MAIL_HOSTNAME must not be empty")
	}

	// Fail-closed environment (audit item 7): the exact strings "dev" and
	// "prod" are the only valid values. Anything else — unset is fine (dev),
	// a typo or a different spelling — is refused at startup rather than
	// silently behaving like dev.
	switch c.Env {
	case "dev", "prod":
	default:
		return nil, fmt.Errorf("%w: SYNKRA_MAIL_ENV=%q is not one of dev|prod — refusing to guess", ErrProdConfig, c.Env)
	}

	if err := c.validateSecrets(); err != nil {
		return nil, err
	}

	// Outbound transport must be named, never inferred from a mistyped env.
	switch c.OutboundTransport {
	case "auto", "loopback", "smtp":
	default:
		return nil, fmt.Errorf("%w: SYNKRA_MAIL_OUTBOUND_TRANSPORT=%q is not one of auto|loopback|smtp", ErrProdConfig, c.OutboundTransport)
	}

	if c.BlobMode == "s3" {
		if c.S3.Endpoint == "" || c.S3.Bucket == "" || c.S3.AccessKey == "" || c.S3.SecretKey == "" {
			return nil, fmt.Errorf("SYNKRA_MAIL_BLOB_MODE=s3 requires SYNKRA_MAIL_S3_ENDPOINT, _BUCKET, _ACCESS_KEY and _SECRET_KEY")
		}
		if !strings.HasPrefix(c.S3.Endpoint, "http://") && !strings.HasPrefix(c.S3.Endpoint, "https://") {
			return nil, fmt.Errorf("SYNKRA_MAIL_S3_ENDPOINT must include a scheme (http:// or https://)")
		}
	}

	if c.TLSCertFile != "" && c.TLSKeyFile != "" {
		c.TLSEnabled = true
	}
	if err := c.validateProd(); err != nil {
		return nil, err
	}
	return c, nil
}

// validateSecrets enforces the secret-strength rule everywhere, not only in
// prod (audit item 7: "no length or entropy check"). Minimum 32 characters,
// never the shipped default, never a single repeated character.
func (c *Config) validateSecrets() error {
	for name, val := range map[string]string{
		"SYNKRA_MAIL_TRACKING_SECRET": c.TrackingSecret,
		"SYNKRA_MAIL_COOKIE_SECRET":   c.HTTP.CookieSecret,
	} {
		if err := checkSecret(name, val); err != nil {
			if c.Env == "prod" {
				return err
			}
			// Dev keeps running (tests need fixed secrets) but the value must
			// still be one of the two shipped dev defaults, not a weak custom one.
			if val != "dev-tracking-secret" && val != "dev-cookie-secret" {
				fmt.Fprintf(os.Stderr, "[config] warning: %v\n", err)
			}
		}
	}
	return nil
}

func checkSecret(name, val string) error {
	if val == "dev-tracking-secret" || val == "dev-cookie-secret" {
		return fmt.Errorf("%w: %s must be changed from the shipped default", ErrProdConfig, name)
	}
	if len(val) < 32 {
		return fmt.Errorf("%w: %s must be at least 32 characters (got %d)", ErrProdConfig, name, len(val))
	}
	first := val[0]
	mono := true
	for i := 1; i < len(val); i++ {
		if val[i] != first {
			mono = false
			break
		}
	}
	if mono {
		return fmt.Errorf("%w: %s has no entropy (all characters identical)", ErrProdConfig, name)
	}
	return nil
}

// validateProd refuses to start prod with unsafe knobs: no encryption key, no
// TLS, or a loopback outbound transport.
func (c *Config) validateProd() error {
	if c.Env != "prod" {
		return nil
	}
	if c.KMSKey == "" {
		return fmt.Errorf("%w: SYNKRA_MAIL_KMS_KEY is required in prod (DKIM keys and TOTP seeds are encrypted at rest)", ErrProdConfig)
	}
	if _, err := decodeKMSKey(c.KMSKey); err != nil {
		return fmt.Errorf("SYNKRA_MAIL_KMS_KEY: %v", err)
	}
	if c.OutboundTransport == "loopback" {
		return fmt.Errorf("%w: SYNKRA_MAIL_OUTBOUND_TRANSPORT=loopback would silently discard prod mail", ErrProdConfig)
	}
	if !c.TLSEnabled {
		return fmt.Errorf("%w: SYNKRA_MAIL_TLS_CERT/SYNKRA_MAIL_TLS_KEY are required in prod (AUTH must never be offered in cleartext)", ErrProdConfig)
	}
	return nil
}

// decodeKMSKey accepts 32 raw bytes base64-encoded, or a passphrase of at
// least 32 characters (SHA-256 derived, mirrored in krypto.DeriveKey).
func decodeKMSKey(v string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(v); err == nil && len(b) == 32 {
		return b, nil
	}
	if len(v) >= 32 {
		return nil, nil // passphrase form — accepted, derived on use
	}
	return nil, fmt.Errorf("must be 32 base64-encoded bytes or a >=32 character passphrase")
}

// KMSKeyBytes materialises the at-rest encryption key (32 bytes). Raw base64
// is used verbatim; a passphrase is SHA-256-derived. Dev without a key gets
// the dev-derived default so tests run; prod refuses at Load().
func (c *Config) KMSKeyBytes() []byte {
	if c.KMSKey == "" {
		return krypto.DeriveKey("synkra-dev-kms-key")
	}
	if b, err := base64.StdEncoding.DecodeString(c.KMSKey); err == nil && len(b) == 32 {
		return b
	}
	return krypto.DeriveKey(c.KMSKey)
}

// DevDefaults returns a config suitable for tests: ephemeral-ish ports, fs blobs.
func DevDefaults(dbURL string) *Config {
	c, err := Load()
	if err != nil {
		panic(err)
	}
	c.Env = "dev"
	c.DB.URL = dbURL
	return c
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// envList parses a comma-separated env value into a list ("" → nil).
func envList(key string) []string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// envListDefault parses a comma-separated env value with a default list.
func envListDefault(key, def string) []string {
	if os.Getenv(key) == "" {
		return envList(def)
	}
	return envList(os.Getenv(key))
}
