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
	"net/http"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-msgauth/dkim"
	"github.com/google/uuid"

	"synkra.email/mail/internal/ingest"
	"synkra.email/mail/internal/krypto"
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

// Tick claims and processes one batch. Webhook deliveries (§13f) go first:
// they are small, best-effort, and never block the delivery batch.
//
// Jobs run concurrently (audit item 19: four single-threaded workers meant a
// few tarpitting destinations stalled ALL outbound mail), bounded by
// SYNKRA_MAIL_WORKER_CONCURRENCY with a per-destination-domain gate so one
// slow domain cannot occupy every slot.
func (w *Worker) Tick(ctx context.Context) error {
	w.dispatchWebhooks(ctx)
	jobs, err := store.ClaimDueJobs(ctx, w.Pipeline.Store.Pool, w.ID, 10*time.Minute, 20)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}
	conc := w.Pipeline.Cfg.WorkerConcurrency
	if conc < 1 {
		conc = 1
	}
	sem := make(chan struct{}, conc)
	gate := newDomainGate(2) // max 2 in flight per recipient domain
	var wg sync.WaitGroup
	for i := range jobs {
		j := jobs[i]
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		}
		if err := gate.acquire(ctx, rcptDomain(j.EnvelopeTo)); err != nil {
			<-sem
			wg.Wait()
			return err
		}
		wg.Add(1)
		go func(j model.DeliveryJob) {
			defer wg.Done()
			defer func() { <-sem; gate.release(rcptDomain(j.EnvelopeTo)) }()
			if err := w.processJob(ctx, &j); err != nil {
				if errors.Is(err, store.ErrLeaseLost) {
					// Another worker reclaimed the expired lease and will
					// deliver it; writing our result would duplicate (audit item 17).
					log.Printf("[worker] job %s lease lost (reclaimed by another worker); skipped", j.ID)
					return
				}
				log.Printf("[worker] job %s: %v", j.ID, err)
			}
		}(j)
	}
	wg.Wait()
	return nil
}

// domainGate bounds concurrent deliveries per recipient domain.
type domainGate struct {
	mu    sync.Mutex
	max   int
	inUse map[string]int
}

func newDomainGate(max int) *domainGate {
	return &domainGate{max: max, inUse: map[string]int{}}
}

func (g *domainGate) acquire(ctx context.Context, domain string) error {
	for {
		g.mu.Lock()
		if g.inUse[domain] < g.max {
			g.inUse[domain]++
			g.mu.Unlock()
			return nil
		}
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (g *domainGate) release(domain string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inUse[domain] <= 1 {
		delete(g.inUse, domain)
		return
	}
	g.inUse[domain]--
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

	// Envelope identity: VERP per job (audit item 21 — bounce correlation) so
	// an async bounce or an ARF complaint names the exact recipient/job. The
	// loopback transport keeps the sender address (there is no wire).
	envFrom := j.EnvelopeFrom
	if w.VERPEnabled() {
		envFrom = "bnc+" + j.ID.String() + "@" + w.Pipeline.Cfg.Hostname
	}
	code, msg, err := w.Transport.Deliver(ctx, envFrom, j.EnvelopeTo, full)
	if err == nil && code >= 200 && code < 300 {
		return w.completeJob(ctx, j, code, msg)
	}
	if err != nil && code == 0 {
		// Transport-level failure (network): transient by definition.
		code, msg = 451, "4.4.1 "+err.Error()
	}

	if code >= 500 {
		return w.rejectJob(ctx, j, org, code, msg)
	}
	return w.deferJob(ctx, j, j.Attempts+1, 0, fmt.Sprintf("%d", code), msg)
}

// VERPEnabled: real-SMTP transport only; the loopback path has no bounce wire.
func (w *Worker) VERPEnabled() bool {
	_, isLoop := w.Transport.(*LoopbackTransport)
	return !isLoop
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
	// The private key is stored sealed ("enc1.") — audit item 4; legacy
	// plaintext rows still open (dev bootstrap).
	privatePEM, oerr := krypto.OpenSealedString(w.Pipeline.Cfg.KMSKeyBytes(), dom.DKIMPrivatePEM)
	if oerr != nil {
		return nil, fmt.Errorf("dkim key: %w", oerr)
	}
	signed, err := dkimSign(raw, domain, dom.DKIMSelector, privatePEM)
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
		// Oversigning (audit item 9): listing From/To/Cc/Subject twice signs
		// the bottom two instances of each, so an attacker-injected duplicate
		// header breaks the signature instead of silently winning. Content-Type
		// was not signed at all before (a body-part swap changed meaning).
		HeaderKeys: []string{
			"From", "From", "To", "To", "Cc", "Cc", "Subject", "Subject",
			"Content-Type", "Content-Type",
			"Date", "Message-ID", "MIME-Version", "In-Reply-To", "References",
			"Reply-To", "List-Unsubscribe",
		},
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
		if err := store.RecordDSN(ctx, tx, &model.DSN{
			ID: uuid.New(), DeliveryJobID: j.ID, Kind: "success",
			SMTPCode: fmt.Sprintf("%d", code), Reason: msg,
		}); err != nil {
			return err
		}
		return recordJobEvent(ctx, tx, j, "delivered", fmt.Sprintf("%d", code), msg)
	})
}

// recordJobEvent appends the delivery event log row inside the same
// transaction that moves the job (register §7.3) — trace and truth stay one.
// Webhooks subscribed to this state fan out in the same transaction (§13f).
func recordJobEvent(ctx context.Context, tx store.Tx, j *model.DeliveryJob, state, code, msg string) error {
	detail, _ := json.Marshal(map[string]any{"code": code, "message": msg})
	mbID := j.MailboxID
	event := &model.DeliveryEvent{
		ID: uuid.New(), MessageID: j.MessageID, OrgID: j.OrgID, MailboxID: &mbID,
		State: state, Detail: detail,
	}
	if err := store.RecordDeliveryEvent(ctx, tx, event); err != nil {
		return err
	}
	hooks, err := store.ActiveWebhooksForEvent(ctx, tx, j.OrgID, state)
	if err != nil {
		return err
	}
	for _, h := range hooks {
		payload, _ := json.Marshal(map[string]any{
			"event":      state,
			"message_id": j.MessageID,
			"org_id":     j.OrgID,
			"mailbox_id": j.MailboxID,
			"rcpt":       j.EnvelopeTo,
			"code":       code,
			"message":    msg,
		})
		if err := store.QueueWebhookDelivery(ctx, tx, &store.WebhookDelivery{
			ID: uuid.New(), WebhookID: h.ID, EventID: event.ID, Payload: payload,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) deferJob(ctx context.Context, j *model.DeliveryJob, attempts int, fixedDelay time.Duration, code, msg string) error {
	if attempts >= maxAttempts {
		return w.finalBounce(ctx, j, code, "retry schedule exhausted (72h): "+msg)
	}
	var delay time.Duration
	if fixedDelay > 0 {
		delay = fixedDelay
	} else if attempts < len(retryDelays) {
		// Attempt N gets the Nth delay of the table: attempt 1 → 1m,
		// attempt 8 → 48h (Section 5.2). Index 0 is the immediate first send.
		delay = retryDelays[attempts]
	} else {
		delay = retryDelays[len(retryDelays)-1]
	}
	next := time.Now().Add(delay)
	err := w.Pipeline.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := store.DeferJob(ctx, tx, j.ID, attempts, next, code, msg); err != nil {
			return err
		}
		return recordJobEvent(ctx, tx, j, "deferred", code, "next "+next.Format(time.RFC3339))
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

// bounceClass is what a 5xx reply actually means for THIS recipient (audit
// item 18: every 5xx was treated as "bad recipient", so an IP blocklist hit
// permanently suppressed healthy mailboxes and fed the breaker).
type bounceClass string

const (
	bounceRecipientUnknown bounceClass = "recipient_unknown" // suppress + breaker
	bouncePolicyReject     bounceClass = "policy_reject"     // our IP/reputation: never suppress
	bounceMailboxFull      bounceClass = "mailbox_full"      // recipient-side storage: soft
	bounceOtherHard        bounceClass = "other_hard"        // unknown 5xx: suppress (conservative)
)

// classifyBounce reads the enhanced status code (and, failing that, the reply
// text) out of a 5xx rejection.
func classifyBounce(code int, msg string) bounceClass {
	enh := enhancedCodeOf(code, msg)
	switch {
	case strings.HasPrefix(enh, "5.1."):
		// 5.1.1 bad destination, 5.1.6 user moved, 5.1.10 bad recipient mailbox.
		return bounceRecipientUnknown
	case strings.HasPrefix(enh, "5.7."):
		// 5.7.1 delivery-not-authorized / policy reasons: blocklists, spam
		// filters, reputation walls. The recipient's address is fine.
		return bouncePolicyReject
	case enh == "5.2.2":
		return bounceMailboxFull
	}
	m := strings.ToLower(msg)
	policyHints := []string{"blocked", "blacklist", "blocklist", "policy", "reputation", "spam", "abuse", "rbl", "dnsbl"}
	for _, h := range policyHints {
		if strings.Contains(m, h) {
			return bouncePolicyReject
		}
	}
	fullHints := []string{"mailbox full", "mailbox is full", "over quota", "storage", "insufficient system storage"}
	for _, h := range fullHints {
		if strings.Contains(m, h) {
			return bounceMailboxFull
		}
	}
	unknownHints := []string{"unknown user", "user unknown", "no such user", "no such mailbox", "does not exist", "recipient rejected", "invalid recipient", "no such domain", "address invalid"}
	for _, h := range unknownHints {
		if strings.Contains(m, h) {
			return bounceRecipientUnknown
		}
	}
	return bounceOtherHard
}

// enhancedCodeOf pulls "x.y.z" out of the reply text (RFC 3463), falling back
// to a class-only "x.y.0" derived from the numeric reply code.
func enhancedCodeOf(code int, msg string) string {
	for _, f := range strings.Fields(msg) {
		if n := len(f); n >= 5 && n <= 7 && f[0] >= '2' && f[0] <= '5' && f[1] == '.' && f[3] == '.' {
			return f
		}
	}
	if code >= 500 {
		return fmt.Sprintf("5.%d.0", (code/10)%10)
	}
	return fmt.Sprintf("%d.0.0", code/100)
}

// rejectJob: a 5xx reply — dispatch on what it actually means.
func (w *Worker) rejectJob(ctx context.Context, j *model.DeliveryJob, org *model.Org, code int, msg string) error {
	switch classifyBounce(code, msg) {
	case bouncePolicyReject:
		return w.policyReject(ctx, j, org, code, msg)
	case bounceMailboxFull:
		return w.softFinalBounce(ctx, j, code, msg)
	default: // recipient_unknown + other_hard: suppress + breaker + owner rules
		return w.hardBounce(ctx, j, org, code, msg)
	}
}

// policyReject: our sending identity was refused (blocklist/reputation). The
// recipient address is healthy — suppressing it would poison the customer's
// list (audit item 18). Bounce to the sender, alert the owner, never suppress,
// never feed the hard-bounce breaker.
func (w *Worker) policyReject(ctx context.Context, j *model.DeliveryJob, org *model.Org, code int, msg string) error {
	err := w.Pipeline.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := store.BounceJob(ctx, tx, j.ID, j.Attempts+1, fmt.Sprintf("%d", code), msg); err != nil {
			return err
		}
		hard := false
		if err := store.RecordDSN(ctx, tx, &model.DSN{
			ID: uuid.New(), DeliveryJobID: j.ID, Kind: "bounce", IsHard: &hard,
			SMTPCode: fmt.Sprintf("%d", code), Reason: "policy: " + msg,
		}); err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]any{
			"rcpt": j.EnvelopeTo, "code": code, "message": msg,
			"note": "remote policy rejection — check IP reputation; recipient NOT suppressed",
		})
		if err := store.RecordRateEvent(ctx, tx, org.ID, &j.MailboxID, "policy_reject", string(detail)); err != nil {
			return err
		}
		return recordJobEvent(ctx, tx, j, "bounced", fmt.Sprintf("%d", code), "policy: "+msg)
	})
	if err != nil {
		return err
	}
	w.injectDSN(ctx, j, "bounce", false, fmt.Sprintf("%d", code), "delivery refused by the receiving system's policy: "+msg)
	w.injectOwnerAlert(ctx, org, "Outbound policy rejections",
		fmt.Sprintf("Mail to %s was refused by the receiving system (policy/reputation). Recipients were not suppressed; check the sending IP reputation.", j.EnvelopeTo))
	return nil
}

// softFinalBounce: recipient-side condition (mailbox full) — no suppression,
// no breaker, one soft bounce after the retry budget.
func (w *Worker) softFinalBounce(ctx context.Context, j *model.DeliveryJob, code int, msg string) error {
	err := w.Pipeline.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := store.BounceJob(ctx, tx, j.ID, j.Attempts+1, fmt.Sprintf("%d", code), msg); err != nil {
			return err
		}
		hard := false
		if err := store.RecordDSN(ctx, tx, &model.DSN{
			ID: uuid.New(), DeliveryJobID: j.ID, Kind: "bounce", IsHard: &hard,
			SMTPCode: fmt.Sprintf("%d", code), Reason: msg,
		}); err != nil {
			return err
		}
		return recordJobEvent(ctx, tx, j, "bounced", fmt.Sprintf("%d", code), msg)
	})
	if err != nil {
		return err
	}
	w.injectDSN(ctx, j, "bounce", false, fmt.Sprintf("%d", code), msg)
	return nil
}

func (w *Worker) hardBounce(ctx context.Context, j *model.DeliveryJob, org *model.Org, code int, msg string) error {
	err := w.Pipeline.Store.WithTx(ctx, func(tx store.Tx) error {
		// One hard bounce per mailbox at a time: the breaker counts completed
		// attempts inside this transaction, and READ COMMITTED would let two
		// workers each miss the other's uncommitted DSN and both skip a trip.
		if err := store.LockMailboxRow(ctx, tx, j.MailboxID); err != nil {
			return err
		}
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
		if err := w.Pipeline.checkBreakerLocked(ctx, tx, org, j.MailboxID); err != nil {
			return err
		}
		return recordJobEvent(ctx, tx, j, "bounced", fmt.Sprintf("%d", code), msg)
	})
	if err != nil {
		return err
	}
	// Backscatter-safe bounce notice to our own sender, post-commit (Section 5.1).
	w.injectDSN(ctx, j, "bounce", true, fmt.Sprintf("%d", code), msg)
	// Owner alert when the breaker paused this mailbox (Section 7.3 says one
	// is sent; audit item 18: nothing ever sent it).
	if mb, merr := store.GetMailbox(ctx, w.Pipeline.Store.Pool, j.MailboxID); merr == nil && mb.SendStatus == "paused_bounces" {
		reason := mb.PausedReason
		if reason == "" {
			reason = "hard-bounce rate over the breaker threshold"
		}
		w.injectOwnerAlert(ctx, org, "Sending paused: hard-bounce breaker tripped",
			"Mailbox sending was paused: "+reason+" Review the suppression list, then resume the mailbox from the console.")
	}
	return nil
}

// finalBounce: retries exhausted → soft-expiry bounce.
func (w *Worker) finalBounce(ctx context.Context, j *model.DeliveryJob, code, msg string) error {
	err := w.Pipeline.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := store.BounceJob(ctx, tx, j.ID, j.Attempts, code, msg); err != nil {
			return err
		}
		hard := false
		if err := store.RecordDSN(ctx, tx, &model.DSN{
			ID: uuid.New(), DeliveryJobID: j.ID, Kind: "bounce", IsHard: &hard,
			SMTPCode: code, Reason: msg,
		}); err != nil {
			return err
		}
		return recordJobEvent(ctx, tx, j, "bounced", code, msg)
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
	raw := buildDSNMessage(j, kind, isHard, code, reason, w.Pipeline.Cfg.Hostname)
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ingest.DeliverLoopback(dctx, w.Deps, raw, "", j.EnvelopeFrom); err != nil {
		log.Printf("[worker] dsn inject to %s: %v", j.EnvelopeFrom, err)
	}
}

// injectOwnerAlert delivers a platform notification into the org owner's
// mailbox through the loopback pipeline (best effort — never blocks delivery).
func (w *Worker) injectOwnerAlert(ctx context.Context, org *model.Org, subject, body string) {
	if w.Deps == nil {
		return
	}
	addr, err := store.GetOrgOwnerAddress(ctx, w.Pipeline.Store.Pool, org.ID)
	if err != nil || addr == "" {
		log.Printf("[worker] owner alert routing for org %s: %v", org.ID, err)
		return
	}
	raw := fmt.Sprintf("From: Synkra Mail Platform <postmaster@%s>\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"Date: %s\r\n"+
		"Content-Type: text/plain; charset=utf-8\r\n"+
		"\r\n%s\r\n",
		w.Pipeline.Cfg.Hostname, addr,
		strings.Join(strings.Fields(subject), " "),
		time.Now().Format(time.RFC1123Z), body)
	actx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ingest.DeliverLoopback(actx, w.Deps, []byte(raw), "", addr); err != nil {
		log.Printf("[worker] owner alert to %s: %v", addr, err)
	}
}

// buildDSNMessage renders an RFC 3464 multipart/report notice. The Reporting-MTA
// and the postmaster address come from the configured server identity.
func buildDSNMessage(j *model.DeliveryJob, kind string, isHard bool, code, reason, hostname string) []byte {
	var b strings.Builder
	action := "delayed"
	if kind == "bounce" {
		action = "failed"
	}
	status := "4.0.0"
	if isHard {
		status = "5.0.0"
	}
	fmt.Fprintf(&b, "From: Mail Delivery System <postmaster@%s>\r\n", hostname)
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
	fmt.Fprintf(&b, "Reporting-MTA: dns; %s\r\n\r\n", hostname)
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

// SMTPTransport: real outbound delivery (production). MX hosts are tried in
// preference order (audit item 20: only the first MX was used, so a down
// primary bounced mail the backup could have taken), the connection speaks a
// correct RFC 3207 sequence — EHLO, the STARTTLS command, the 220 reply, and
// only THEN the TLS handshake (audit item 1: the handshake was attempted
// directly, so every real server saw a ClientHello where an SMTP command
// belongs and mail retried for 72h before bouncing) — and egress is filtered:
// no address literals, no MX targets on private ranges (audit item 10).
// TLS is opportunistic but never silently downgraded: a certificate that
// fails verification gets exactly one unverified-TLS attempt (still
// encrypted); a host that does not advertise STARTTLS is logged and used in
// cleartext.
type SMTPTransport struct {
	// HostPort override (staging/tests point at a local fake server); empty = MX lookup.
	HostPort string
	// HeloName is our EHLO identity; empty: the configured hostname.
	HeloName string
	Timeout  time.Duration
	// SourceIP binds the outbound connection to a local address (audit item 20).
	SourceIP string
	// SkipEgressFilter is for tests against loopback fakes (HostPort set).
	SkipEgressFilter bool
}

func (t *SMTPTransport) Deliver(ctx context.Context, envFrom, rcptTo string, raw []byte) (int, string, error) {
	if t.HostPort != "" {
		return t.deliverTo(ctx, t.HostPort, envFrom, rcptTo, raw)
	}
	hosts, code, msg, err := pickMXHosts(rcptTo)
	if err != nil {
		return code, msg, err
	}
	var lastErr error
	for _, host := range hosts {
		c, ext, err := t.connect(ctx, host, t.helo(), true)
		if err != nil && isCertError(err) {
			// One unverified-TLS retry (encrypted but unauthenticated). After
			// that: abort — never fall back to cleartext mid-flight (Section 7.5).
			c, ext, err = t.connect(ctx, host, t.helo(), false)
		}
		if err != nil {
			lastErr = err
			continue // next MX by preference
		}
		code, msg, err := t.converse(ctx, c, ext, host, envFrom, rcptTo, raw)
		if err != nil && code == 0 && lastErr == nil {
			lastErr = err
			continue // network-level failure on this host: try the next MX
		}
		return code, msg, err
	}
	if lastErr != nil {
		return 0, "", lastErr
	}
	return 451, "4.4.4 no usable MX host", errors.New("no usable MX host")
}

func (t *SMTPTransport) helo() string {
	if t.HeloName != "" {
		return t.HeloName
	}
	return "localhost"
}

// deliverTo runs connect + converse against one fixed host (HostPort override
// used by staging and tests).
func (t *SMTPTransport) deliverTo(ctx context.Context, host, envFrom, rcptTo string, raw []byte) (int, string, error) {
	c, ext, err := t.connect(ctx, host, t.helo(), true)
	if err != nil && isCertError(err) {
		c, ext, err = t.connect(ctx, host, t.helo(), false)
	}
	if err != nil {
		return 0, "", err
	}
	return t.converse(ctx, c, ext, host, envFrom, rcptTo, raw)
}

// converse runs the MAIL/RCPT/DATA dialogue on an established connection.
func (t *SMTPTransport) converse(ctx context.Context, c *textproto.Conn, ext map[string]bool, host, envFrom, rcptTo string, raw []byte) (int, string, error) {
	defer c.Close()
	if !ext["STARTTLS"] && t.HostPort == "" {
		log.Printf("[outbound] %s does not offer STARTTLS; sending in cleartext", hostOnly(host))
	}

	cmd := func(format string, args ...any) (int, string, error) {
		id, cerr := c.Cmd(format, args...)
		if cerr != nil {
			return 0, "", cerr
		}
		c.StartResponse(id)
		defer c.EndResponse(id)
		code, msg, rerr := c.ReadResponse(-1)
		if rerr != nil {
			return 0, "", rerr
		}
		return code, msg, nil
	}

	code, msg, err := cmd("MAIL FROM:<%s>", envFrom)
	if err != nil {
		return 0, "", err
	}
	if code != 250 {
		return code, msg, nil // protocol refusal: 5xx hard, 4xx transient
	}
	code, msg, err = cmd("RCPT TO:<%s>", rcptTo)
	if err != nil {
		return 0, "", err
	}
	if code != 250 {
		return code, msg, nil
	}

	id, err := c.Cmd("DATA")
	if err != nil {
		return 0, "", err
	}
	c.StartResponse(id)
	_, _, err = c.ReadResponse(354)
	c.EndResponse(id)
	if err != nil {
		return 0, "", err
	}
	wr := c.DotWriter()
	if _, werr := wr.Write(raw); werr != nil {
		wr.Close()
		return 0, "", werr
	}
	if werr := wr.Close(); werr != nil {
		return 0, "", werr
	}
	fcode, fmsg, err := c.ReadResponse(250)
	if err != nil {
		return 0, "", err
	}
	_, _, _ = cmd("QUIT")
	return fcode, fmsg, nil
}

// pickMXHosts resolves where to deliver for rcptTo, in try order: MX records
// by preference (RFC 5321 5.1), implicit MX when no record exists, RFC 7505
// null MX (hard reject — the domain explicitly accepts no mail). NXDOMAIN
// fails fast as a permanent bounce instead of retrying 72 hours (audit item
// 20); transient DNS errors stay deferrable. Address literals are refused
// outright (audit item 10).
func pickMXHosts(rcptTo string) (hosts []string, code int, msg string, err error) {
	domain := rcptTo[strings.LastIndex(rcptTo, "@")+1:]
	if strings.HasPrefix(domain, "[") {
		return nil, 550, "5.1.3 address literals are not accepted", errors.New("address literal recipient refused")
	}
	mxs, lerr := net.LookupMX(domain)
	if lerr != nil {
		var de *net.DNSError
		if errors.As(lerr, &de) && de.IsNotFound {
			// The domain does not exist: retrying for 72h only queues
			// backscatter (audit item 20) — fail fast, permanently.
			return nil, 550, "5.1.2 domain does not exist", lerr
		}
		// Transient resolver trouble: deferrable.
		return nil, 451, "4.4.3 temporary DNS failure: " + lerr.Error(), lerr
	}
	for _, m := range mxs {
		h := strings.TrimSuffix(m.Host, ".")
		if h == "" || h == "." {
			return nil, 550, "5.1.2 null MX: domain does not accept mail", errors.New("null MX")
		}
		hosts = append(hosts, h)
	}
	if len(hosts) == 0 {
		// No MX record → the domain itself is the implicit MX (A/AAAA).
		hosts = []string{domain}
	}
	return hosts, 0, "", nil
}

// connect dials host:25 (a HostPort keeps its own port), greets, upgrades via
// the RFC 3207 sequence when advertised, and refuses to dial private or
// loopback targets (audit item 10: MX records pointing at 127.0.0.1, RFC 1918
// or link-local space were dialled happily — an SSRF door for any tenant).
// verify controls certificate verification (Section 7.5).
func (t *SMTPTransport) connect(ctx context.Context, host, helo string, verify bool) (*textproto.Conn, map[string]bool, error) {
	d := t.Timeout
	if d == 0 {
		d = 60 * time.Second
	}
	addr := host
	if _, _, perr := net.SplitHostPort(host); perr != nil {
		addr = host + ":25"
	}
	if !t.SkipEgressFilter && t.HostPort == "" {
		if err := egressAllowed(ctx, host); err != nil {
			return nil, nil, err
		}
	}
	dialer := &net.Dialer{Timeout: d}
	if t.SourceIP != "" {
		if src := net.ParseIP(t.SourceIP); src != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: src}
		}
	}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	// One deadline for the whole dialogue; the per-command reads share it.
	_ = conn.SetDeadline(time.Now().Add(d))
	c := textproto.NewConn(conn)
	if _, _, gerr := c.ReadResponse(220); gerr != nil {
		c.Close()
		return nil, nil, fmt.Errorf("greeting: %w", gerr)
	}
	ext, err := ehlo(c, helo)
	if err != nil {
		c.Close()
		return nil, nil, err
	}
	if !ext["STARTTLS"] {
		return c, ext, nil
	}
	// RFC 3207 4: send STARTTLS, read the 220 Go-ahead, and only then
	// handshake. The old code skipped the command entirely, so real servers
	// answered "502/503" to a TLS ClientHello and every send retried to death.
	id, serr := c.Cmd("STARTTLS")
	if serr != nil {
		c.Close()
		return nil, nil, serr
	}
	c.StartResponse(id)
	_, _, aerr := c.ReadResponse(220)
	c.EndResponse(id)
	if aerr != nil {
		c.Close()
		return nil, nil, fmt.Errorf("starttls: %w", aerr)
	}
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: hostOnly(host), MinVersion: tls.VersionTLS12,
		InsecureSkipVerify: !verify,
	})
	hctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := tlsConn.HandshakeContext(hctx); err != nil {
		c.Close()
		return nil, nil, err
	}
	nc := textproto.NewConn(tlsConn)
	// The cipher suite changes the connection identity: greet again (RFC 3207 4).
	ext, err = ehlo(nc, helo)
	if err != nil {
		nc.Close()
		return nil, nil, err
	}
	return nc, ext, nil
}

// egressAllowed refuses MX hosts that resolve into private, loopback,
// link-local or otherwise non-routable space. It also blocks a hostname whose
// first lookup fails transiently (fail closed: we cannot verify where it goes).
func egressAllowed(ctx context.Context, host string) error {
	hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resolver := &net.Resolver{}
	addrs, err := resolver.LookupIPAddr(hctx, host)
	if err != nil {
		return fmt.Errorf("4.4.3 cannot resolve %s for egress check: %w", host, err)
	}
	for _, ia := range addrs {
		ip := ia.IP
		if !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Errorf("5.7.1 refusing to deliver to non-public address space (%s)", ip)
		}
	}
	// Carrier-grade NAT (100.64/10) and benchmark space (198.18/15) are not
	// IsPrivate() but are still not public destinations.
	for _, ia := range addrs {
		if v4 := ia.IP.To4(); v4 != nil {
			if v4[0] == 100 || (v4[0] == 198 && v4[1] == 18) || v4[0] == 0 {
				return fmt.Errorf("5.7.1 refusing to deliver to non-public address space (%s)", ia.IP)
			}
		}
	}
	return nil
}

// ehlo greets (falling back to HELO for ancient servers) and returns the
// advertised extension keywords.
func ehlo(c *textproto.Conn, helo string) (map[string]bool, error) {
	id, err := c.Cmd("EHLO %s", helo)
	if err != nil {
		return nil, err
	}
	c.StartResponse(id)
	_, msg, err := c.ReadResponse(250)
	c.EndResponse(id)
	if err != nil {
		id2, herr := c.Cmd("HELO %s", helo)
		if herr != nil {
			return nil, herr
		}
		c.StartResponse(id2)
		defer c.EndResponse(id2)
		if _, _, err := c.ReadResponse(250); err != nil {
			return nil, fmt.Errorf("helo: %w", err)
		}
		return map[string]bool{}, nil
	}
	ext := map[string]bool{}
	// msg is the multiline response joined with \n: line 0 is the server
	// banner, the rest are the advertised extensions.
	for _, line := range strings.Split(msg, "\n") {
		f := strings.Fields(line)
		if len(f) > 0 {
			ext[strings.ToUpper(f[0])] = true
		}
	}
	return ext, nil
}

// isCertError reports whether err is a TLS certificate-verification failure
// (as opposed to a protocol or network error).
func isCertError(err error) bool {
	if err == nil {
		return false
	}
	var ua x509.UnknownAuthorityError
	var hn x509.HostnameError
	var ci x509.CertificateInvalidError
	return errors.As(err, &ua) || errors.As(err, &hn) || errors.As(err, &ci)
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

// dispatchWebhooks POSTs due webhook deliveries with an HMAC signature
// (X-Synkra-Signature over the raw body). 2xx counts as delivered; failures
// retry with backoff and are dropped after 5 attempts.
func (w *Worker) dispatchWebhooks(ctx context.Context) {
	claims, err := store.ClaimDueWebhookDeliveries(ctx, w.Pipeline.Store.Pool, 10)
	if err != nil {
		log.Printf("[worker] webhook claim: %v", err)
		return
	}
	for i := range claims {
		c := &claims[i]
		hook, err := store.GetWebhookByID(ctx, w.Pipeline.Store.Pool, c.WebhookID)
		if err != nil {
			_ = w.Pipeline.Store.WithTx(ctx, func(tx store.Tx) error {
				return store.SetWebhookDeliveryOutcome(ctx, tx, c.ID, false, "webhook deleted")
			})
			continue
		}
		ok, lastErr := postWebhook(ctx, hook.URL, hook.Secret, c.Payload)
		if err := w.Pipeline.Store.WithTx(ctx, func(tx store.Tx) error {
			return store.SetWebhookDeliveryOutcome(ctx, tx, c.ID, ok, lastErr)
		}); err != nil {
			log.Printf("[worker] webhook outcome: %v", err)
		}
	}
}

func postWebhook(ctx context.Context, url, secret string, body []byte) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Synkra-Signature", "sha256="+krypto.HMACSign([]byte(secret), string(body)))
	resp, err := webhookClient.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, ""
	}
	return false, "http " + resp.Status
}

var webhookClient = &http.Client{Timeout: 10 * time.Second}
