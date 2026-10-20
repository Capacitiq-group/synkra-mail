// Package outbound: submission admission + delivery queue worker (Section 5.2,
// 5.3, 7.3). Durable truth is mail.delivery_job; workers claim with SKIP LOCKED.
package outbound

import (
	"bytes"
	"context"
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
	Track       bool // per-message opt-in (compose UI shows a disclosure — Section 9.4)
}

// RecipientView is the API response: what happened per recipient.
type RecipientView struct {
	Email      string `json:"email"`
	Suppressed bool   `json:"suppressed,omitempty"`
	Queued     bool   `json:"queued"`
}

// ComposeAndQueue: full submission path from the webmail API.
func (p *Pipeline) ComposeAndQueue(ctx context.Context, userID, orgID uuid.UUID, in ComposeInput) (messageID uuid.UUID, recipients []RecipientView, err error) {
	// From ownership (Section 7.4.2): own mailbox, team membership, or alias routed to them.
	local, domain, serr := splitAddr(in.From.Email)
	if serr != nil {
		return uuid.Nil, nil, fmt.Errorf("invalid from address")
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

	raw, msgID, pt, err := p.admissionAndBuild(ctx, fromMailbox, in)
	if err != nil {
		return uuid.Nil, nil, err
	}

	recipients, messageID, err = p.storeOutgoing(ctx, orgID, fromMailbox.ID, userID, raw, msgID, in, nil, pt)
	return messageID, recipients, err
}

// QueueSubmission handles messages arriving over the submission SMTP port —
// the raw message is already built by the client.
func (p *Pipeline) QueueSubmission(ctx context.Context, userID, orgID uuid.UUID, raw []byte, envFrom string, rcpts []string) error {
	parsed, err := mimeutil.Parse(raw)
	if err != nil {
		return fmt.Errorf("cannot parse submitted message: %w", err)
	}
	fromEmail := parsed.FromEmail
	if fromEmail == "" {
		fromEmail = envFrom
	}
	local, domain, serr := splitAddr(fromEmail)
	if serr != nil {
		return errors.New("5.1.7 bad sender address")
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
		From:    mimeutil.Addr{Email: fromEmail, Name: parsed.FromName},
		Subject: parsed.Subject,
		Text:    parsed.Text,
		HTML:    parsed.HTML,
		Track:   false, // submission clients don't get rewrites unless the org toggle + webmail path
	}
	for _, r := range rcpts {
		in.To = append(in.To, mimeutil.Addr{Email: r})
	}
	// The client's raw message is the deliverable (Sent copy + jobs reference it);
	// admission re-uses the build path only for validation and tracking metadata.
	_, _, pt, err := p.admissionAndBuild(ctx, fromMailbox, in)
	if err != nil {
		return err
	}
	_, _, err = p.storeOutgoing(ctx, orgID, fromMailbox.ID, userID, raw, parsed.MessageID, in, rcpts, pt)
	return err
}

// admissionAndBuild: sending state, limits, suppression, tracking rewrite, MIME build.
func (p *Pipeline) admissionAndBuild(ctx context.Context, fromMailbox *model.Mailbox, in ComposeInput) ([]byte, string, *pendingTrack, error) {
	org, err := store.GetOrg(ctx, p.Store.Pool, fromMailbox.OrgID)
	if err != nil {
		return nil, "", nil, err
	}

	// Org suspension defers outbound (Section 7.1); storage-full blocks sending (Section 4.4).
	if org.Status == "suspended" {
		return nil, "", nil, ErrSendingPaused
	}
	qd := &ingest.Deps{Store: p.Store, Blobs: p.Blobs, Cfg: p.Cfg}
	q, err := qd.CheckQuota(ctx, org.ID)
	if err != nil {
		return nil, "", nil, err
	}
	if q.BeyondGrace {
		return nil, "", nil, ErrMailboxFull
	}

	// Sending gate (owner decision): payment or admin approval, else no outbound.
	state, err := store.ResolveSendingState(ctx, p.Store.Pool, org)
	if err != nil {
		return nil, "", nil, err
	}
	if !state.Enabled {
		return nil, "", nil, ErrSendingDisabled
	}
	if fromMailbox.SendStatus != "active" {
		return nil, "", nil, fmt.Errorf("%w (%s)", ErrSendingPaused, fromMailbox.SendStatus)
	}

	rcpts := gatherRecipients(in)
	if len(rcpts) == 0 {
		return nil, "", nil, errors.New("no recipients")
	}
	if len(rcpts) > p.Cfg.Limits.MaxRecipients {
		return nil, "", nil, fmt.Errorf("%w: max %d recipients per message", ErrLimitExceeded, p.Cfg.Limits.MaxRecipients)
	}

	// Tiered limits (Section 7.3). Suppressed recipients are surfaced, not dropped silently.
	if err := p.checkLimits(ctx, org, fromMailbox, len(rcpts)); err != nil {
		return nil, "", nil, err
	}
	for _, r := range rcpts {
		sup, err := store.IsSuppressed(ctx, p.Store.Pool, org.ID, r)
		if err == nil && sup {
			return nil, "", nil, fmt.Errorf("%w: %s is suppressed (hard bounce or unsubscribe)", ErrLimitExceeded, r)
		}
	}

	// Tracking (Section 9): org toggle + per-message opt-in; off by default.
	track := in.Track && p.TrackEnabled != nil && p.TrackEnabled(ctx, org.ID)
	if track {
		raw, msgID, pt, terr := p.buildTracked(ctx, org, fromMailbox, in)
		return raw, msgID, pt, terr
	}

	raw, msgID, err := mimeutil.Build(mimeutil.ComposeInput{
		From: in.From, To: in.To, Cc: in.Cc, Subject: in.Subject,
		Text: in.Text, HTML: in.HTML, InReplyTo: in.InReplyTo, References: in.References,
		Attachments: in.Attachments,
	})
	return raw, msgID, nil, err
}

func (p *Pipeline) checkLimits(ctx context.Context, org *model.Org, fromMailbox *model.Mailbox, rcptCount int) error {
	var dailyOrg int
	var err error
	if org.Tier == "verified" {
		dailyOrg, err = store.CountSentSince(ctx, p.Store.Pool, org.ID, nil, time.Now().Add(-24*time.Hour))
		if err != nil {
			return err
		}
		if dailyOrg+rcptCount > p.Cfg.Limits.VerifiedOrgDaily {
			return fmt.Errorf("%w: org daily cap %d", ErrLimitExceeded, p.Cfg.Limits.VerifiedOrgDaily)
		}
		mbDaily, err := store.CountSentSince(ctx, p.Store.Pool, org.ID, &fromMailbox.ID, time.Now().Add(-24*time.Hour))
		if err != nil {
			return err
		}
		if mbDaily+rcptCount > p.Cfg.Limits.VerifiedMailboxDaily {
			return fmt.Errorf("%w: mailbox daily cap %d", ErrLimitExceeded, p.Cfg.Limits.VerifiedMailboxDaily)
		}
		return nil
	}

	// New-org tier: 100/day org, 20/hour mailbox, 50 recipients (Section 7.3).
	dailyOrg, err = store.CountSentSince(ctx, p.Store.Pool, org.ID, nil, time.Now().Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if dailyOrg+rcptCount > p.Cfg.Limits.NewOrgDaily {
		return fmt.Errorf("%w: new-org daily cap %d", ErrLimitExceeded, p.Cfg.Limits.NewOrgDaily)
	}
	mbHourly, err := store.CountSentSince(ctx, p.Store.Pool, org.ID, &fromMailbox.ID, time.Now().Add(-1*time.Hour))
	if err != nil {
		return err
	}
	if mbHourly+rcptCount > p.Cfg.Limits.NewMailboxHourly {
		return fmt.Errorf("%w: new-org hourly cap %d", ErrLimitExceeded, p.Cfg.Limits.NewMailboxHourly)
	}
	if p.Cfg.WarmupDailyCap > 0 && dailyOrg+rcptCount > p.Cfg.WarmupDailyCap {
		return fmt.Errorf("%w: warmup cap %d", ErrLimitExceeded, p.Cfg.WarmupDailyCap)
	}
	return nil
}

// buildTracked rewrites links + injects the pixel, creating tracking rows in the
// same transaction as the message (tracker stays isolated — Section 9.1).
func (p *Pipeline) buildTracked(ctx context.Context, org *model.Org, fromMailbox *model.Mailbox, in ComposeInput) ([]byte, string, *pendingTrack, error) {
	pixelID := uuid.New()
	raw, msgID, err := mimeutil.Build(mimeutil.ComposeInput{
		From: in.From, To: in.To, Cc: in.Cc, Subject: in.Subject,
		Text: in.Text, HTML: in.HTML, InReplyTo: in.InReplyTo, References: in.References,
		Attachments: in.Attachments,
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
func (p *Pipeline) storeOutgoing(ctx context.Context, orgID, fromMailboxID, userID uuid.UUID, raw []byte, msgID string, in ComposeInput, envRcpts []string, pt *pendingTrack) ([]RecipientView, uuid.UUID, error) {
	sha, size, err := p.Blobs.Put(ctx, bytes.NewReader(raw))
	if err != nil {
		return nil, uuid.Nil, err
	}

	var envelopeRcpts []string
	if envRcpts != nil {
		envelopeRcpts = envRcpts
	} else {
		for _, a := range in.To {
			envelopeRcpts = append(envelopeRcpts, a.Email)
		}
		for _, a := range in.Cc {
			envelopeRcpts = append(envelopeRcpts, a.Email)
		}
		for _, a := range in.Bcc {
			envelopeRcpts = append(envelopeRcpts, a.Email)
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
			ToAddrs: toModelAddrs(in.To), CcAddrs: toModelAddrs(in.Cc),
			Subject: in.Subject, Snippet: mimeutil.Snippet(in.Text), BodyText: in.Text,
			HasAttachments: hasAttachments(in.Attachments), SizeBytes: size,
			InternalDate: time.Now(), TrackingEnabled: pt != nil,
		}
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

		for _, a := range in.Attachments {
			asha, asize, aerr := p.Blobs.Put(ctx, bytes.NewReader(a.Content))
			if aerr != nil {
				return aerr
			}
			if aerr := store.InsertAttachment(ctx, tx, &model.Attachment{
				ID: uuid.New(), MessageID: msg.ID, BlobSHA256: asha,
				Filename: a.Filename, ContentType: a.ContentType, SizeBytes: asize, IsInline: a.IsInline,
			}); aerr != nil {
				return aerr
			}
		}

		// Tracking rows (if this compose was tracked).
		if pt != nil {
			if err := store.CreateTrackingPixel(ctx, tx, &model.TrackingPixel{
				ID: pt.pixelID, MessageID: msg.ID, Recipient: in.From.Email,
			}); err != nil {
				return err
			}
			// Pixel recipient is rewritten per job below; one pixel per message
			// (recipient-level pixels are a post-v1 refinement).
			for target, lid := range pt.linkIDs {
				if err := store.CreateTrackedLink(ctx, tx, &model.TrackedLink{
					ID: lid, MessageID: msg.ID, URL: target,
				}); err != nil {
					return err
				}
			}
		}

		// One durable job per recipient (Section 5.2).
		for _, rcpt := range envelopeRcpts {
			domain := rcptDomain(rcpt)
			j := &model.DeliveryJob{
				ID: uuid.New(), OrgID: orgID, MailboxID: fromMailboxID, MessageID: msg.ID,
				EnvelopeFrom: in.From.Email, EnvelopeTo: rcpt, RecipientDomain: domain,
			}
			if err := store.CreateDeliveryJob(ctx, tx, j); err != nil {
				return err
			}
			views = append(views, RecipientView{Email: rcpt, Queued: true})
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
