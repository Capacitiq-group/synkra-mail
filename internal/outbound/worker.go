// Worker: claims due delivery jobs (SKIP LOCKED), DKIM-signs, delivers via the
// configured transport, records DSNs, and enforces the bounce circuit breaker
// (Section 5.2 retry table + Section 7.3 owner rules).
package outbound

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/emersion/go-msgauth/dkim"
	"github.com/google/uuid"

	"synkra.email/mail/internal/ingest"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

// Retry schedule (Section 5.2): attempt → delay. Index 0 unused (attempt 1 is immediate).
var retryDelays = []time.Duration{
	0,
	1 * time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	6 * time.Hour,
	12 * time.Hour,
	24 * time.Hour,
	48 * time.Hour,
}

const maxAttempts = 9 // expire at 72h → final bounce DSN
const delayWarningAttempt = 6

// Worker processes the queue.
type Worker struct {
	ID        string
	Pipeline  *Pipeline
	Deps      *ingest.Deps
	Transport Transport
}

func NewWorker(p *Pipeline, t Transport) *Worker {
	return &Worker{
		ID:        "worker-" + uuid.NewString()[:8],
		Pipeline:  p,
		Deps:      &ingest.Deps{Store: p.Store, Blobs: p.Blobs, Cfg: p.Cfg},
		Transport: t,
	}
}

// Run polls until ctx is done.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.Pipeline.Cfg.WorkerPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.Tick(ctx); err != nil {
				log.Printf("[worker] tick: %v", err)
			}
		}
	}
}

// Tick claims and processes one batch.
func (w *Worker) Tick(ctx context.Context) error {
	jobs, err := store.ClaimDueJobs(ctx, w.Pipeline.Store.Pool, w.ID, 10*time.Minute, 20)
	if err != nil {
		return err
	}
	for i := range jobs {
		if err := w.processJob(ctx, &jobs[i]); err != nil {
			log.Printf("[worker] job %s: %v", jobs[i].ID, err)
		}
	}
	return nil
}

// processJob handles one claimed job end-to-end.
func (w *Worker) processJob(ctx context.Context, j *model.DeliveryJob) error {
	// Re-check org state: suspension/billing pauses mid-flight (Section 7.1).
	org, err := store.GetOrg(ctx, w.Pipeline.Store.Pool, j.OrgID)
	if err != nil {
		return w.deferJob(ctx, j, j.Attempts, 5*time.Minute, "451", "4.3.0 org lookup failed")
	}
	if org.Status == "suspended" {
		// Defer without burning an attempt; reactivation resumes the queue.
		return w.deferJob(ctx, j, j.Attempts, 1*time.Hour, "451", "4.4.1 org suspended")
	}

	full, err := w.loadSignedMessage(ctx, j)
	if err != nil {
		return w.deferJob(ctx, j, j.Attempts, 5*time.Minute, "451", "4.3.0 message load failed: "+err.Error())
	}

	code, msg, err := w.Transport.Deliver(ctx, j.EnvelopeFrom, j.EnvelopeTo, full)
	if err == nil && code >= 200 && code < 300 {
		return w.completeJob(ctx, j, code, msg)
	}
	if err != nil && code == 0 {
		// Transport-level failure (network): transient by definition.
		code, msg = 451, "4.4.1 "+err.Error()
	}

	if code >= 500 {
		return w.hardBounce(ctx, j, org, code, msg)
	}
	return w.deferJob(ctx, j, j.Attempts+1, 0, fmt.Sprintf("%d", code), msg)
}

// loadSignedMessage loads the composed RFC822 and applies our DKIM signature
// for the From domain (key lives on the domain row — Section 7.6).
func (w *Worker) loadSignedMessage(ctx context.Context, j *model.DeliveryJob) ([]byte, error) {
	full, err := store.GetMessage(ctx, w.Pipeline.Store.Pool, j.MessageID)
	if err != nil {
		return nil, err
	}
	rc, err := w.Pipeline.Blobs.Open(ctx, full.Message.BlobSHA256)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	_, domain, err := splitAddr(j.EnvelopeFrom)
	if err != nil {
		return raw, nil
	}
	dom, derr := store.GetDomainByName(ctx, w.Pipeline.Store.Pool, domain)
	if derr != nil || dom.DKIMPrivatePEM == "" {
		return raw, nil // unsigned (dev domains); prod seeds keys for every hosted domain
	}
	signed, err := dkimSign(raw, domain, dom.DKIMSelector, dom.DKIMPrivatePEM)
	if err != nil {
		return nil, fmt.Errorf("dkim sign: %w", err)
	}
	return signed, nil
}

func dkimSign(raw []byte, domain, selector, privatePEM string) ([]byte, error) {
	block, _ := pem.Decode([]byte(privatePEM))
	if block == nil {
		return nil, errors.New("bad dkim pem")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	err = dkim.Sign(&out, bytes.NewReader(raw), &dkim.SignOptions{
		Domain:                 domain,
		Selector:               selector,
		Signer:                 key,
		HeaderCanonicalization: dkim.CanonicalizationRelaxed,
		BodyCanonicalization:   dkim.CanonicalizationRelaxed,
		HeaderKeys:             []string{"From", "To", "Cc", "Subject", "Date", "Message-ID", "MIME-Version", "In-Reply-To", "References", "Reply-To", "List-Unsubscribe"},
	})
	if err != nil {
		return nil, err
	}
	// dkim.Sign writes the full message (headers + DKIM-Signature + body).
	return out.Bytes(), nil
}

func (w *Worker) completeJob(ctx context.Context, j *model.DeliveryJob, code int, msg string) error {
	return w.Pipeline.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := store.MarkJobDelivered(ctx, tx, j.ID, fmt.Sprintf("%d", code), msg); err != nil {
			return err
		}
		return store.RecordDSN(ctx, tx, &model.DSN{
			ID: uuid.New(), DeliveryJobID: j.ID, Kind: "success",
			SMTPCode: fmt.Sprintf("%d", code), Reason: msg,
		})
	})
}

func (w *Worker) deferJob(ctx context.Context, j *model.DeliveryJob, attempts int, fixedDelay time.Duration, code, msg string) error {
	if attempts >= maxAttempts {
		return w.finalBounce(ctx, j, code, "retry schedule exhausted (72h): "+msg)
	}
	var delay time.Duration
	if fixedDelay > 0 {
		delay = fixedDelay
	} else if attempts-1 < len(retryDelays) {
		delay = retryDelays[attempts-1]
	} else {
		delay = retryDelays[len(retryDelays)-1]
	}
	next := time.Now().Add(delay)
	err := w.Pipeline.Store.WithTx(ctx, func(tx store.Tx) error {
		return store.DeferJob(ctx, tx, j.ID, attempts, next, code, msg)
	})
	if err != nil {
		return err
	}
	// Delay warning to the sender at attempt 6 (Section 5.2: 6h mark).
	if attempts == delayWarningAttempt {
		w.injectDSN(ctx, j, "delay", false, code, "delivery delayed: "+msg)
	}
	return nil
}

func (w *Worker) hardBounce(ctx context.Context, j *model.DeliveryJob, org *model.Org, code int, msg string) error {
	err := w.Pipeline.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := store.BounceJob(ctx, tx, j.ID, j.Attempts+1, fmt.Sprintf("%d", code), msg); err != nil {
			return err
		}
		hard := true
		if err := store.RecordDSN(ctx, tx, &model.DSN{
			ID: uuid.New(), DeliveryJobID: j.ID, Kind: "bounce", IsHard: &hard,
			SMTPCode: fmt.Sprintf("%d", code), Reason: msg,
		}); err != nil {
			return err
		}
		// Suppression on hard bounce (Section 7.3).
		if err := store.Suppress(ctx, tx, j.OrgID, j.EnvelopeTo, "hard_bounce"); err != nil {
			return err
		}
		// Circuit breaker (owner decision: 5% hard-bounce rate, rolling window).
		return w.Pipeline.checkBreakerLocked(ctx, tx, org, j.MailboxID)
	})
	if err != nil {
		return err
	}
	// Backscatter-safe bounce notice to our own sender, post-commit (Section 5.1).
	w.injectDSN(ctx, j, "bounce", true, fmt.Sprintf("%d", code), msg)
	return nil
}

// finalBounce: retries exhausted → soft-expiry bounce.
func (w *Worker) finalBounce(ctx context.Context, j *model.DeliveryJob, code, msg string) error {
	err := w.Pipeline.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := store.BounceJob(ctx, tx, j.ID, j.Attempts, code, msg); err != nil {
			return err
		}
		hard := false
		return store.RecordDSN(ctx, tx, &model.DSN{
			ID: uuid.New(), DeliveryJobID: j.ID, Kind: "bounce", IsHard: &hard,
			SMTPCode: code, Reason: msg,
		})
	})
	if err != nil {
		return err
	}
	w.injectDSN(ctx, j, "bounce", false, code, msg)
	return nil
}

// injectDSN delivers a bounce/delay notice into the original sender's mailbox
// (our own user — the anti-backscatter rule, Section 5.1). Envelope from is the
// null path; delivery is via the loopback pipeline.
func (w *Worker) injectDSN(ctx context.Context, j *model.DeliveryJob, kind string, isHard bool, code, reason string) {
	if w.Deps == nil {
		return
	}
	raw := buildDSNMessage(j, kind, isHard, code, reason)
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ingest.DeliverLoopback(dctx, w.Deps, raw, "", j.EnvelopeFrom); err != nil {
		log.Printf("[worker] dsn inject to %s: %v", j.EnvelopeFrom, err)
	}
}

// buildDSNMessage renders an RFC 3464 multipart/report notice.
func buildDSNMessage(j *model.DeliveryJob, kind string, isHard bool, code, reason string) []byte {
	var b strings.Builder
	action := "delayed"
	if kind == "bounce" {
		action = "failed"
	}
	status := "4.0.0"
	if isHard {
		status = "5.0.0"
	}
	fmt.Fprintf(&b, "From: Mail Delivery System <postmaster@synkra.email>\r\n")
	fmt.Fprintf(&b, "To: %s\r\n", j.EnvelopeFrom)
	subject := "Mail delivery delayed: " + j.EnvelopeTo
	if kind == "bounce" {
		subject = "Undelivered Mail Returned to Sender: " + j.EnvelopeTo
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/report; report-type=delivery-status;\r\n\tboundary=\"dsn-%s\"\r\n", j.ID)
	fmt.Fprintf(&b, "\r\n")
	fmt.Fprintf(&b, "--dsn-%s\r\n", j.ID)
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n\r\n")
	if kind == "bounce" {
		fmt.Fprintf(&b, "Your message to %s could not be delivered.\r\n\r\nServer said: %s %s\r\n", j.EnvelopeTo, code, reason)
	} else {
		fmt.Fprintf(&b, "Your message to %s is being retried.\r\n\r\nServer said: %s %s\r\nDelivery will be attempted for up to 72 hours.\r\n", j.EnvelopeTo, code, reason)
	}
	fmt.Fprintf(&b, "\r\n--dsn-%s\r\n", j.ID)
	fmt.Fprintf(&b, "Content-Type: message/delivery-status\r\n\r\n")
	fmt.Fprintf(&b, "Reporting-MTA: dns; mx1.synkra.email\r\n\r\n")
	fmt.Fprintf(&b, "Original-Envelope-Id: %s\r\n", j.ID)
	fmt.Fprintf(&b, "Action: %s\r\nStatus: %s\r\nDiagnostic-Code: smtp; %s %s\r\n", action, status, code, reason)
	fmt.Fprintf(&b, "\r\n--dsn-%s--\r\n", j.ID)
	return []byte(b.String())
}

// checkBreakerLocked: hard-bounce rate over the rolling window trips a mailbox
// pause (owner decision: hard bounces only, minimum volume, 5%).
func (p *Pipeline) checkBreakerLocked(ctx context.Context, tx store.Tx, org *model.Org, mailboxID uuid.UUID) error {
	attempts, hard, pct, err := store.BounceRatePct(ctx, tx, mailboxID, p.Cfg.Breaker.Window)
	if err != nil {
		return err
	}
	if attempts < p.Cfg.Breaker.MinAttempts {
		return nil
	}
	if pct < p.Cfg.Breaker.HardBounceRatePct {
		return nil
	}
	detail, _ := json.Marshal(map[string]any{
		"attempts": attempts, "hard_bounces": hard, "rate_pct": pct,
		"threshold_pct": p.Cfg.Breaker.HardBounceRatePct, "window": p.Cfg.Breaker.Window.String(),
	})
	if err := store.SetMailboxSendStatus(ctx, tx, mailboxID, "paused_bounces",
		fmt.Sprintf("hard-bounce rate %.1f%% over %s (%d/%d)", pct, p.Cfg.Breaker.Window, hard, attempts)); err != nil {
		return err
	}
	return store.RecordRateEvent(ctx, tx, org.ID, &mailboxID, "bounce_breaker", string(detail))
}

// ---- transports ----

// LoopbackTransport delivers into the local inbound pipeline (dev + tests).
type LoopbackTransport struct {
	Deps *ingest.Deps
}

func (t *LoopbackTransport) Deliver(ctx context.Context, envFrom, rcptTo string, raw []byte) (int, string, error) {
	if err := ingest.DeliverLoopback(ctx, t.Deps, raw, envFrom, rcptTo); err != nil {
		var smtpErr *ingest.SMTPError
		if errors.As(err, &smtpErr) {
			return smtpErr.Code, smtpErr.Message, err
		}
		return 451, err.Error(), err
	}
	return 250, "delivered (loopback)", nil
}

// SMTPTransport: real outbound delivery (production). MX lookup + STARTTLS.
type SMTPTransport struct {
	// HostPort override (staging/tests point at a local fake server); empty = MX lookup.
	HostPort string
	Timeout  time.Duration
}

func (t *SMTPTransport) Deliver(ctx context.Context, envFrom, rcptTo string, raw []byte) (int, string, error) {
	host := t.HostPort
	if host == "" {
		domain := rcptTo[strings.LastIndex(rcptTo, "@")+1:]
		mxs, err := net.LookupMX(domain)
		if err != nil || len(mxs) == 0 {
			// No MX → A/AAAA fallback per RFC 5321.
			mxs = []*net.MX{{Host: domain + ".", Pref: 0}}
		}
		host = strings.TrimSuffix(mxs[0].Host, ".") + ":25"
	}
	d := t.Timeout
	if d == 0 {
		d = 60 * time.Second
	}
	conn, err := net.DialTimeout("tcp", host, d)
	if err != nil {
		return 0, "", err
	}
	c, err := smtp.NewClient(conn, hostOnly(host))
	if err != nil {
		conn.Close()
		return 0, "", err
	}
	defer c.Close()
	_ = c.Hello("mx1.synkra.email")
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: hostOnly(host), MinVersion: tls.VersionTLS12}); err != nil {
			return 0, "", err
		}
	}
	if err := c.Mail(envFrom); err != nil {
		return smtpCodeOf(err), err.Error(), err
	}
	if err := c.Rcpt(rcptTo); err != nil {
		return smtpCodeOf(err), err.Error(), err
	}
	wr, err := c.Data()
	if err != nil {
		return smtpCodeOf(err), err.Error(), err
	}
	if _, err := wr.Write(raw); err != nil {
		return 0, "", err
	}
	if err := wr.Close(); err != nil {
		return smtpCodeOf(err), err.Error(), err
	}
	_ = c.Quit()
	return 250, "accepted", nil
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

func smtpCodeOf(err error) int {
	var serr *textproto.Error
	if errors.As(err, &serr) {
		return serr.Code
	}
	return 451
}
