// Package outbound: submission admission + delivery queue worker (Section 5.2,
// 5.3, 7.3). Durable truth is mail.delivery_job; workers claim with SKIP LOCKED.
package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"synkra.email/mail/internal/blob"
	"synkra.email/mail/internal/config"
	"synkra.email/mail/internal/ingest"
	"synkra.email/mail/internal/mimeutil"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

var (
	ErrNotSendable     = errors.New("from address not sendable by this user")
	ErrSendingPaused   = errors.New("sending paused")
	ErrSendingDisabled = errors.New("sending not enabled: payment or admin approval required")
	ErrMailboxFull     = errors.New("mailbox full: storage grace buffer exhausted")
	ErrLimitExceeded   = errors.New("sending limit exceeded")
	ErrNoRecipients    = errors.New("no recipients")
	ErrInvalidAddress  = errors.New("invalid address")
	ErrMessageTooLarge = errors.New("message exceeds the size limit")
)

// Transport delivers one message to one recipient. Loopback (dev/tests) and
// real SMTP (production) implement this.
type Transport interface {
	Deliver(ctx context.Context, envFrom, rcptTo string, raw []byte) (code int, msg string, err error)
}

// Pipeline carries the dependencies for admission + queueing.
type Pipeline struct {
	Store *store.Store
	Blobs blob.Store
	Cfg   *config.Config
	Deps  *ingest.Deps // loopback transport target
	// TrackEnabled resolves the per-org tracking toggle (Section 9.1: off by default).
	TrackEnabled func(ctx context.Context, orgID uuid.UUID) bool
	TrackerURL   string
	TrackingKey  []byte
}

// ComposeInput mirrors mimeutil.ComposeInput plus tracking + draft context.
type ComposeInput struct {
	From        mimeutil.Addr
	To          []mimeutil.Addr
	Cc          []mimeutil.Addr
	Bcc         []mimeutil.Addr
	Subject     string
	Text        string
	HTML        string
	InReplyTo   string
	References  []string
	Attachments []mimeutil.Att
	// Date is the submission time (InternalDate of the Sent copy); zero = now.
	Date            time.Time
	Track           bool // per-message opt-in (compose UI shows a disclosure — Section 9.4)
	Newsletter      bool // RFC 2369 bulk mail: adds List-Unsubscribe (register §12e)
	ListUnsubscribe string
	ListID          string
}

// RecipientView is the API response: what happened per recipient.
type RecipientView struct {
	Email      string `json:"email"`
	Suppressed bool   `json:"suppressed,omitempty"`
	Queued     bool   `json:"queued"`
}

// admitted carries what admission decided into the storage step: the built
// raw message, its Message-ID, the tracking rows to create and the suppressed
// set (skipped recipients — reported, not fatal).
type admitted struct {
	raw        []byte
	messageID  string
	track      *pendingTrack
	suppressed map[string]bool
}

// ComposeAndQueue: full submission path from the webmail API.
func (p *Pipeline) ComposeAndQueue(ctx context.Context, userID, orgID uuid.UUID, in ComposeInput) (messageID uuid.UUID, recipients []RecipientView, err error) {
	// From ownership (Section 7.4.2): own mailbox, team membership, or alias routed to them.
	local, domain, serr := splitAddr(in.From.Email)
	if serr != nil || !mimeutil.ValidAddress(in.From.Email) {
		return uuid.Nil, nil, fmt.Errorf("%w: %q", ErrInvalidAddress, in.From.Email)
	}
	ok, err := store.SendAsOK(ctx, p.Store.Pool, userID, local, domain)
	if err != nil {
		return uuid.Nil, nil, err
	}
	if !ok {
		return uuid.Nil, nil, ErrNotSendable
	}
	fromMailbox, err := p.mailboxForAddress(ctx, local, domain)
	if err != nil {
		return uuid.Nil, nil, err
	}

	adm, err := p.admissionAndBuild(ctx, fromMailbox, in)
	if err != nil {
		return uuid.Nil, nil, err
	}
	recipients, messageID, err = p.storeOutgoing(ctx, orgID, fromMailbox.ID, userID, adm.raw, adm.messageID, in, nil, adm)
	return messageID, recipients, err
}

// QueueSubmission handles messages arriving over the submission SMTP port —
// the raw message is already built by the client. What the client wrote is
// kept verbatim; only what it omitted (Message-ID, Date) is supplied, and
// admission runs against the same rules as the webmail compose.
//
// Envelope recipients are ALWAYS merged into admission (audit item 2): the
// old code merged them only when the client sent no To/Cc headers, so Bcc and
// envelope-only recipients skipped suppression, address validation, limit
// counting — and went onto the wire unvalidated.
func (p *Pipeline) QueueSubmission(ctx context.Context, userID, orgID uuid.UUID, raw []byte, envFrom string, rcpts []string) error {
	// Second-From (and friends) is the known signature/authorisation bypass:
	// the client's message may name exactly one From header (audit item 9).
	if mimeutil.CountHeader(raw, "From") > 1 {
		return fmt.Errorf("%w: 5.6.0 multiple From headers rejected", ErrInvalidAddress)
	}
	for _, h := range []string{"Subject", "Date"} {
		if mimeutil.CountHeader(raw, h) > 1 {
			return fmt.Errorf("%w: 5.6.0 duplicate %s header rejected", ErrInvalidAddress, h)
		}
	}
	parsed, err := mimeutil.Parse(raw)
	if err != nil {
		return fmt.Errorf("cannot parse submitted message: %w", err)
	}
	fromEmail := parsed.FromEmail
	if fromEmail == "" {
		fromEmail = envFrom
	}
	local, domain, serr := splitAddr(fromEmail)
	if serr != nil || !mimeutil.ValidAddress(fromEmail) {
		return fmt.Errorf("%w: 5.1.7 bad sender address", ErrInvalidAddress)
	}
	ok, err := store.SendAsOK(ctx, p.Store.Pool, userID, local, domain)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotSendable
	}
	fromMailbox, err := p.mailboxForAddress(ctx, local, domain)
	if err != nil {
		return err
	}

	in := ComposeInput{
		From:        mimeutil.Addr{Email: fromEmail, Name: parsed.FromName},
		To:          parsed.To,
		Cc:          parsed.Cc,
		Subject:     parsed.Subject,
		Text:        parsed.Text,
		HTML:        parsed.HTML,
		InReplyTo:   parsed.InReplyTo,
		References:  parsed.Refs,
		Attachments: parsed.Attachments,
		Date:        parsed.Date,
		Track:       false, // submission clients don't get rewrites unless the org toggle + webmail path
	}
	// Envelope recipients (Bcc comes envelope-only from normal mail clients)
	// join the admission set as Bcc: validated, suppressed and counted like
	// every other recipient. The stored To/Cc stay what the client wrote and
	// the message body is not touched.
	headerEmails := map[string]struct{}{}
	for _, l := range [][]mimeutil.Addr{parsed.To, parsed.Cc} {
		for _, a := range l {
			headerEmails[strings.ToLower(strings.TrimSpace(a.Email))] = struct{}{}
		}
	}
	in.Bcc = make([]mimeutil.Addr, 0, len(rcpts))
	for _, r := range dedupe(rcpts) {
		e := strings.TrimSpace(r)
		if !mimeutil.ValidAddress(e) {
			// Never let an unvalidated string reach `RCPT TO:<%s>` (audit item 2).
			return fmt.Errorf("%w: 5.1.3 bad envelope recipient %q", ErrInvalidAddress, r)
		}
		if _, dup := headerEmails[strings.ToLower(e)]; dup {
			continue
		}
		in.Bcc = append(in.Bcc, mimeutil.Addr{Email: e})
	}
	// The client's raw message is the deliverable (Sent copy + jobs reference it);
	// admission re-uses the build path only for validation and tracking metadata.
	adm, err := p.admissionAndBuild(ctx, fromMailbox, in)
	if err != nil {
		return err
	}

	// A message leaving without a Message-ID or Date breaks threading at the
	// receiver (Section 4.2). What the client left out is supplied, prefixed
	// to its headers; nothing it wrote is rewritten.
	msgID := strings.TrimSpace(parsed.MessageID)
	if msgID == "" {
		msgID = mimeutil.NewMessageID(p.Cfg.Hostname)
	}
	var missing []mimeutil.Header
	if !mimeutil.HasHeader(raw, "Message-ID") {
		missing = append(missing, mimeutil.Header{Name: "Message-ID", Value: msgID})
	}
	if !mimeutil.HasHeader(raw, "Date") {
		missing = append(missing, mimeutil.Header{Name: "Date", Value: time.Now().Format(time.RFC1123Z)})
	}
	raw = mimeutil.AddMissingHeaders(raw, missing...)

	_, _, err = p.storeOutgoing(ctx, orgID, fromMailbox.ID, userID, raw, msgID, in, rcpts, adm)
	return err
}

// admissionAndBuild: sending state, limits, suppression, tracking rewrite, MIME build.
func (p *Pipeline) admissionAndBuild(ctx context.Context, fromMailbox *model.Mailbox, in ComposeInput) (*admitted, error) {
	// List-Unsubscribe (register §12e): one-click for bulk mail. The token is
	// per recipient; with several recipients the header cannot name them all,
	// so bulk sends to a single recipient carry it (a newsletter to a list is
	// one send per recipient anyway) and batch sends carry none.
	if in.Newsletter {
		if rcpts := gatherRecipients(in); len(rcpts) == 1 {
			in.ListUnsubscribe = UnsubscribeURL(p.TrackerURL, p.TrackingKey, fromMailbox.OrgID, fromMailbox.ID, rcpts[0])
			in.ListID = "<" + strings.ToLower(strings.ReplaceAll(fromMailbox.Name, " ", "-")) +
				"." + fromMailbox.OrgID.String()[:8] + "@" + p.Cfg.Hostname + ">"
		}
	}
	org, err := store.GetOrg(ctx, p.Store.Pool, fromMailbox.OrgID)
	if err != nil {
		return nil, err
	}

	// Org suspension defers outbound (Section 7.1); storage-full blocks sending (Section 4.4).
	if org.Status == "suspended" {
		return nil, ErrSendingPaused
	}
	qd := &ingest.Deps{Store: p.Store, Blobs: p.Blobs, Cfg: p.Cfg}
	q, err := qd.CheckQuota(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	if q.BeyondGrace {
		return nil, ErrMailboxFull
	}

	// Sending gate (owner decision): payment or admin approval, else no outbound.
	state, err := store.ResolveSendingState(ctx, p.Store.Pool, org)
	if err != nil {
		return nil, err
	}
	if !state.Enabled {
		return nil, ErrSendingDisabled
	}
	if fromMailbox.SendStatus != "active" {
		return nil, fmt.Errorf("%w (%s)", ErrSendingPaused, fromMailbox.SendStatus)
	}

	rcpts := gatherRecipients(in)
	if len(rcpts) == 0 {
		return nil, ErrNoRecipients
	}
	if len(rcpts) > p.Cfg.Limits.MaxRecipients {
		return nil, fmt.Errorf("%w: max %d recipients per message", ErrLimitExceeded, p.Cfg.Limits.MaxRecipients)
	}
	if !mimeutil.ValidAddress(in.From.Email) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidAddress, in.From.Email)
	}
	for _, r := range rcpts {
		if !mimeutil.ValidAddress(r) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidAddress, r)
		}
	}

	// Suppressed recipients (hard bounce or unsubscribe, Section 7.3) are
	// skipped and reported instead of aborting the send: one poisoned address
	// must not stop a message to thirty good ones. All suppressed = refuse.
	suppressed := map[string]bool{}
	sendable := make([]string, 0, len(rcpts))
	for _, r := range rcpts {
		sup, err := store.IsSuppressed(ctx, p.Store.Pool, org.ID, r)
		if err != nil {
			return nil, err
		}
		if sup {
			suppressed[strings.ToLower(strings.TrimSpace(r))] = true
			continue
		}
		sendable = append(sendable, r)
	}
	if len(sendable) == 0 {
		return nil, fmt.Errorf("%w: every recipient is suppressed (hard bounce or unsubscribe)", ErrLimitExceeded)
	}

	// Compose headers reflect what will actually be delivered.
	in.To = withoutSuppressed(in.To, suppressed)
	in.Cc = withoutSuppressed(in.Cc, suppressed)
	in.Bcc = withoutSuppressed(in.Bcc, suppressed)

	adm := &admitted{suppressed: suppressed}
	// Tracking (Section 9): org toggle + per-message opt-in; off by default.
	if in.Track && p.TrackEnabled != nil && p.TrackEnabled(ctx, org.ID) {
		raw, msgID, pt, terr := p.buildTracked(ctx, org, fromMailbox, in)
		if terr != nil {
			return nil, terr
		}
		adm.raw, adm.messageID, adm.track = raw, msgID, pt
	} else {
		raw, msgID, berr := p.build(in)
		if berr != nil {
			return nil, berr
		}
		adm.raw, adm.messageID = raw, msgID
	}
	if int64(len(adm.raw)) > p.Cfg.MaxMessageBytes {
		return nil, fmt.Errorf("%w (%d MB)", ErrMessageTooLarge, p.Cfg.MaxMessageBytes>>20)
	}
	// Tiered limits (Section 7.3) reserve atomically for exactly the
	// recipients we will attempt (audit item 20: COUNT-then-send raced under
	// concurrency and every cap could be overshot).
	if err := p.reserveLimits(ctx, org, fromMailbox, len(sendable)); err != nil {
		return nil, err
	}
	return adm, nil
}

// reserveLimits consumes the org/mailbox/warmup budgets in single-statement
// atomic reservations. A failed message build after a reservation leaks a few
// reserved slots — the conservative direction (never over the cap).
func (p *Pipeline) reserveLimits(ctx context.Context, org *model.Org, fromMailbox *model.Mailbox, sendable int) error {
	day := time.Now().Format("20060102")
	hour := time.Now().Format("2006010215")
	var orgCap, mboxCap int
	var mboxWindow string
	if org.Tier == "verified" {
		orgCap, mboxCap, mboxWindow = p.Cfg.Limits.VerifiedOrgDaily, p.Cfg.Limits.VerifiedMailboxDaily, day
	} else {
		// New-org tier: 100/day org, 20/hour mailbox (Section 7.3).
		orgCap, mboxCap, mboxWindow = p.Cfg.Limits.NewOrgDaily, p.Cfg.Limits.NewMailboxHourly, hour
	}
	if ok, err := store.ReserveSend(ctx, p.Store.Pool, "org:"+org.ID.String()+":"+day, sendable, orgCap); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: org %s cap %d reached", ErrLimitExceeded, org.Tier, orgCap)
	}
	if ok, err := store.ReserveSend(ctx, p.Store.Pool, "mb:"+fromMailbox.ID.String()+":"+mboxWindow, sendable, mboxCap); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: mailbox cap %d reached", ErrLimitExceeded, mboxCap)
	}
	// Warmup (owner decision, Section 8.3): a per-IP budget shared by every
	// org on the sending IP — the old check was per-org, so N tenants each
	// got the whole IP's budget (audit item 20).
	if p.Cfg.WarmupDailyCap > 0 {
		if ok, err := store.ReserveSend(ctx, p.Store.Pool, "ip:warmup:"+day, sendable, p.Cfg.WarmupDailyCap); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("%w: IP warmup cap %d reached", ErrLimitExceeded, p.Cfg.WarmupDailyCap)
		}
	}
	return nil
}

// SendAutoReply is the ingest hook target (register §12c): ingest decides WHEN
// (window, rate limit, daemon guard); this builds and queues the reply through
// the normal admission path, so a suspended or blocked org simply stays quiet.
func (p *Pipeline) SendAutoReply(ctx context.Context, mailboxID, orgID uuid.UUID, to, inSubject string) error {
	ar, err := store.GetAutoresponder(ctx, p.Store.Pool, mailboxID)
	if err != nil || !ar.Enabled || ar.Body == "" {
		return nil
	}
	from, err := store.GetPrimaryAddressByMailbox(ctx, p.Store.Pool, mailboxID)
	if err != nil {
		return err
	}
	mb, err := store.GetMailbox(ctx, p.Store.Pool, mailboxID)
	if err != nil {
		return err
	}
	var fromName string
	if mb.OwnerUserID == nil {
		fromName = mb.Name
	}
	subject := ar.Subject
	if subject == "" {
		subject = "Re: " + inSubject
	}
	in := ComposeInput{
		From:    mimeutil.Addr{Email: from, Name: fromName},
		To:      []mimeutil.Addr{{Email: to}},
		Subject: subject, Text: ar.Body,
	}
	userID := uuid.Nil
	if mb.OwnerUserID != nil {
		userID = *mb.OwnerUserID
	} else {
		userID, err = store.GetOrgOwnerID(ctx, p.Store.Pool, orgID)
		if err != nil {
			return err
		}
	}
	_, _, err = p.ComposeAndQueue(ctx, userID, orgID, in)
	return err
}

func (p *Pipeline) build(in ComposeInput) ([]byte, string, error) {
	return mimeutil.Build(mimeutil.ComposeInput{
		From: in.From, To: in.To, Cc: in.Cc, Bcc: in.Bcc, Subject: in.Subject,
		Text: in.Text, HTML: in.HTML, InReplyTo: in.InReplyTo, References: in.References,
		Attachments: in.Attachments, Date: in.Date,
		MessageIDDomain: p.Cfg.Hostname,
		ListUnsubscribe: in.ListUnsubscribe, ListID: in.ListID,
	})
}

// buildTracked rewrites links + injects the pixel, creating tracking rows in the
// same transaction as the message (tracker stays isolated — Section 9.1).
func (p *Pipeline) buildTracked(ctx context.Context, org *model.Org, fromMailbox *model.Mailbox, in ComposeInput) ([]byte, string, *pendingTrack, error) {
	pixelID := uuid.New()
	raw, msgID, err := mimeutil.Build(mimeutil.ComposeInput{
		From: in.From, To: in.To, Cc: in.Cc, Bcc: in.Bcc, Subject: in.Subject,
		Text: in.Text, HTML: in.HTML, InReplyTo: in.InReplyTo, References: in.References,
		Attachments: in.Attachments, Date: in.Date,
		MessageIDDomain: p.Cfg.Hostname,
		ListUnsubscribe: in.ListUnsubscribe, ListID: in.ListID,
	})
	if err != nil {
		return nil, "", nil, err
	}

	pt := &pendingTrack{pixelID: pixelID, linkIDs: map[string]uuid.UUID{}}
	if len(in.HTML) > 0 {
		newHTML := RewriteLinks(in.HTML, func(target string) string {
			id, ok := pt.linkIDs[target]
			if !ok {
				id = uuid.New()
				pt.linkIDs[target] = id
			}
			return ClickURL(p.TrackerURL, p.TrackingKey, id, target)
		})
		raw = ReplaceHTMLBody(raw, newHTML)
	}
	raw = AppendPixel(raw, PixelURL(p.TrackerURL, p.TrackingKey, pixelID))
	return raw, msgID, pt, nil
}

type pendingTrack struct {
	pixelID uuid.UUID
	linkIDs map[string]uuid.UUID
}

// storeOutgoing: Sent copy + delivery_job rows, one transaction.
func (p *Pipeline) storeOutgoing(ctx context.Context, orgID, fromMailboxID, userID uuid.UUID, raw []byte, msgID string, in ComposeInput, envRcpts []string, adm *admitted) ([]RecipientView, uuid.UUID, error) {
	sha, size, err := p.Blobs.Put(ctx, bytes.NewReader(raw))
	if err != nil {
		return nil, uuid.Nil, err
	}

	var suppressed map[string]bool
	if adm != nil {
		suppressed = adm.suppressed
	}
	held := func(r string) bool { return suppressed[strings.ToLower(strings.TrimSpace(r))] }

	var envelopeRcpts []string
	if envRcpts != nil {
		envelopeRcpts = dedupe(envRcpts)
	} else {
		envelopeRcpts = gatherRecipients(in) // suppression already removed from in
	}
	deliver := make([]string, 0, len(envelopeRcpts))
	for _, rcpt := range envelopeRcpts {
		if !held(rcpt) {
			deliver = append(deliver, rcpt)
		}
	}

	var outID uuid.UUID
	var views []RecipientView
	err = p.Store.WithTx(ctx, func(tx store.Tx) error {
		sent, ferr := store.GetFolderBySpecialUse(ctx, tx, fromMailboxID, model.FolderSent)
		if ferr != nil {
			return ferr
		}
		msg := &model.Message{
			ID: uuid.New(), OrgID: orgID, MailboxID: fromMailboxID,
			BlobSHA256: sha, Direction: "outbound",
			MessageID: msgID, InReplyTo: in.InReplyTo, Refs: in.References,
			FromEmail: in.From.Email, FromName: in.From.Name,
			ToAddrs: toModelAddrs(withoutSuppressed(in.To, suppressed)),
			CcAddrs: toModelAddrs(withoutSuppressed(in.Cc, suppressed)),
			Subject: in.Subject, Snippet: mimeutil.Snippet(in.Text), BodyText: in.Text,
			HasAttachments: hasAttachments(in.Attachments), SizeBytes: size,
			TrackingEnabled: adm != nil && adm.track != nil,
		}
		date := in.Date
		if date.IsZero() {
			date = time.Now()
		}
		msg.InternalDate = date
		subjNorm := mimeutil.NormalizeSubject(in.Subject)
		threadID, terr := store.FindOrCreateThread(ctx, tx, orgID, in.References, in.InReplyTo, subjNorm)
		if terr != nil {
			return terr
		}
		msg.ThreadID = &threadID
		if _, _, ierr := store.InsertMessage(ctx, tx, msg, sent.ID, []string{model.FlagSeen}); ierr != nil {
			return ierr
		}
		outID = msg.ID
		// Delivery event log (register §7.3): admission and queueing share this
		// transaction, so "queued" is the machine's first locked-in state.
		detail, _ := json.Marshal(map[string]any{"recipients": len(deliver)})
		if err := store.RecordDeliveryEvent(ctx, tx, &model.DeliveryEvent{
			ID: uuid.New(), MessageID: outID, OrgID: orgID, MailboxID: &fromMailboxID,
			State: "queued", Detail: detail,
		}); err != nil {
			return err
		}

		for _, a := range in.Attachments {
			asha, asize, aerr := p.Blobs.Put(ctx, bytes.NewReader(a.Content))
			if aerr != nil {
				return aerr
			}
			if aerr := store.InsertAttachment(ctx, tx, &model.Attachment{
				ID: uuid.New(), MessageID: msg.ID, BlobSHA256: asha,
				Filename: a.Filename, ContentType: a.ContentType, SizeBytes: asize,
				IsInline: a.IsInline, ContentID: a.ContentID,
			}); aerr != nil {
				return aerr
			}
		}

		// Tracking rows (if this compose was tracked).
		if adm != nil && adm.track != nil {
			// The pixel names the sole recipient; with several recipients the
			// pixel would leak who opened what, so it stays unattributed.
			recipient := ""
			if len(deliver) == 1 {
				recipient = deliver[0]
			}
			if err := store.CreateTrackingPixel(ctx, tx, &model.TrackingPixel{
				ID: adm.track.pixelID, MessageID: msg.ID, Recipient: recipient,
			}); err != nil {
				return err
			}
			for target, lid := range adm.track.linkIDs {
				if err := store.CreateTrackedLink(ctx, tx, &model.TrackedLink{
					ID: lid, MessageID: msg.ID, URL: target,
				}); err != nil {
					return err
				}
			}
		}

		// One durable job per recipient we will attempt (Section 5.2).
		for _, rcpt := range deliver {
			domain := rcptDomain(rcpt)
			j := &model.DeliveryJob{
				ID: uuid.New(), OrgID: orgID, MailboxID: fromMailboxID, MessageID: msg.ID,
				EnvelopeFrom: in.From.Email, EnvelopeTo: rcpt, RecipientDomain: domain,
			}
			if err := store.CreateDeliveryJob(ctx, tx, j); err != nil {
				return err
			}
		}
		for _, rcpt := range envelopeRcpts {
			views = append(views, RecipientView{Email: rcpt, Queued: !held(rcpt), Suppressed: held(rcpt)})
		}
		return nil
	})
	if err != nil {
		return nil, uuid.Nil, err
	}
	return views, outID, nil
}

func (p *Pipeline) mailboxForAddress(ctx context.Context, local, domain string) (*model.Mailbox, error) {
	addr, _, err := store.RouteAddress(ctx, p.Store.Pool, domain, local)
	if err != nil {
		return nil, ErrNotSendable
	}
	return store.GetMailbox(ctx, p.Store.Pool, addr.MailboxID)
}

func gatherRecipients(in ComposeInput) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(l []mimeutil.Addr) {
		for _, a := range l {
			e := strings.ToLower(strings.TrimSpace(a.Email))
			if e == "" {
				continue
			}
			if _, ok := seen[e]; ok {
				continue
			}
			seen[e] = struct{}{}
			out = append(out, a.Email)
		}
	}
	add(in.To)
	add(in.Cc)
	add(in.Bcc)
	return out
}

func withoutSuppressed(list []mimeutil.Addr, suppressed map[string]bool) []mimeutil.Addr {
	if len(suppressed) == 0 || len(list) == 0 {
		return list
	}
	out := make([]mimeutil.Addr, 0, len(list))
	for _, a := range list {
		if !suppressed[strings.ToLower(strings.TrimSpace(a.Email))] {
			out = append(out, a)
		}
	}
	return out
}

func dedupe(list []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(list))
	for _, s := range list {
		e := strings.ToLower(strings.TrimSpace(s))
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, strings.TrimSpace(s))
	}
	return out
}

func toModelAddrs(l []mimeutil.Addr) []model.EmailAddr {
	out := make([]model.EmailAddr, 0, len(l))
	for _, a := range l {
		out = append(out, model.EmailAddr{Name: a.Name, Email: a.Email})
	}
	return out
}

func hasAttachments(atts []mimeutil.Att) bool {
	for _, a := range atts {
		if !a.IsInline {
			return true
		}
	}
	return false
}

func splitAddr(addr string) (local, domain string, err error) {
	at := strings.LastIndex(addr, "@")
	if at <= 0 || at == len(addr)-1 {
		return "", "", errors.New("invalid address")
	}
	return addr[:at], addr[at+1:], nil
}

func rcptDomain(rcpt string) string {
	if at := strings.LastIndex(rcpt, "@"); at >= 0 && at < len(rcpt)-1 {
		return rcpt[at+1:]
	}
	return ""
}
