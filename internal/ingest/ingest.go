// Package ingest: the inbound pipeline (Section 5.1) — routing, storage policy,
// verdicts → Inbox/Junk/Quarantine, and the storage-full rule (Section 4.4).
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"synkra.email/mail/internal/blob"
	"synkra.email/mail/internal/config"
	"synkra.email/mail/internal/mimeutil"
	"synkra.email/mail/internal/model"
	"synkra.email/mail/internal/store"
)

// Verdicts (Section 5.2 matrix). Scores are our own policy on top of library
// verifications — libraries parse and verify, they never decide (Section 1.2).
const (
	SpamRejectScore     = 10.0
	SpamQuarantineScore = 5.0
)

type Deps struct {
	Store *store.Store
	// AutoReply sends the mailbox's autoresponder message (register §12c).
	// Wired to the outbound pipeline; ingest decides WHEN, not HOW.
	AutoReply func(ctx context.Context, mailboxID, orgID uuid.UUID, to, inSubject string) error
	Blobs     blob.Store
	Cfg       *config.Config
}

// AuthVerdicts carries pipeline stage results (Section 7.2).
type AuthVerdicts struct {
	SPF        string // pass | fail | softfail | none | neutral | permerror | temperror
	DKIM       string // pass | fail | none
	DKIMDomain string
	// DKIMPassDomains: EVERY domain with a passing signature — DMARC needs
	// the aligned one, not the first (audit item 8).
	DKIMPassDomains []string
	DMARC           string // pass | fail | none
	DMARCPolicy     string // none | quarantine | reject (as published; empty = none)
	DMARCRecord     string // the _dmarc record as published (trace/debug)
	FromDomain      string // From domain per the standard address parser
	Malware         bool
	DNSBL           string // "" = not checked / clean
}

// Result reports what happened to a delivered message.
type Result struct {
	MessageID  uuid.UUID
	MailboxID  uuid.UUID
	FolderID   uuid.UUID
	UID        uint32
	Quarantine bool
	SpamScore  float64
}

// SMTP-shaped error carrying a code for the server layer to return verbatim.
type SMTPError struct {
	Code    int
	Message string
}

func (e *SMTPError) Error() string { return fmt.Sprintf("%d %s", e.Code, e.Message) }

// QuotaState summarizes org storage against the plan pool (Section 4.4).
type QuotaState struct {
	UsedBytes     int64
	IncludedBytes int64
	GraceBytes    int64
	Full          bool // at/over 100%
	BeyondGrace   bool // beyond 110% — reject inbound, block sending
	UsedPercent   int
}

// CheckQuota computes the org storage state and records warnings (80/90/100).
func (d *Deps) CheckQuota(ctx context.Context, orgID uuid.UUID) (*QuotaState, error) {
	org, err := store.GetOrg(ctx, d.Store.Pool, orgID)
	if err != nil {
		return nil, err
	}
	plan, err := store.GetPlanByID(ctx, d.Store.Pool, org.PlanID)
	if err != nil {
		return nil, err
	}
	used, err := store.OrgUsageBytes(ctx, d.Store.Pool, orgID)
	if err != nil {
		return nil, err
	}
	included := int64(plan.StorageTotalMB) * 1024 * 1024
	q := &QuotaState{
		UsedBytes:     used,
		IncludedBytes: included,
		GraceBytes:    included / 10, // 10% grace buffer (owner decision)
		UsedPercent:   int(used * 100 / max64(included, 1)),
	}
	q.Full = used >= included
	q.BeyondGrace = used >= included+q.GraceBytes

	// Warnings at 80/90/100 — one per crossing (unique(org, level)), never delete.
	var warns []int
	if used*100 >= included*80 {
		warns = append(warns, 80)
	}
	if used*100 >= included*90 {
		warns = append(warns, 90)
	}
	if q.Full {
		warns = append(warns, 100)
	}
	if len(warns) > 0 {
		_ = d.Store.WithTx(ctx, func(tx store.Tx) error {
			for _, lvl := range warns {
				if err := store.RecordQuotaEvent(ctx, tx, orgID, lvl, fmt.Sprintf("usage %d/%d bytes", used, included)); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return q, nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// DeliverInbound runs the full inbound path for one recipient copy.
// Errors of type *SMTPError carry the code the MX must return.
func (d *Deps) DeliverInbound(ctx context.Context, raw []byte, envFrom, rcptTo string, av *AuthVerdicts) (*Result, error) {
	// Wire normalisation (build register 3.7): bare LF becomes CRLF before
	// anything is stored or re-queued.
	raw = mimeutil.NormalizeWire(raw)
	local, domain, err := splitRcpt(rcptTo)
	if err != nil {
		return nil, &SMTPError{Code: 501, Message: "bad recipient address"}
	}

	// Routing (Section 5.1): hosted domain + known local part, else reject.
	addr, dom, rerr := store.RouteAddress(ctx, d.Store.Pool, domain, local)
	if rerr != nil {
		if errors.Is(rerr, store.ErrNotFound) {
			if dom == nil {
				// We are never a relay (Section 7.4.1).
				return nil, &SMTPError{Code: 554, Message: "5.1.1 domain not hosted here"}
			}
			return nil, &SMTPError{Code: 550, Message: "5.1.1 no such mailbox"}
		}
		return nil, &SMTPError{Code: 451, Message: "4.3.0 routing lookup failed"}
	}
	if addr.Kind == "forward" && addr.ForwardTarget != "" {
		// Forwarding aliases (ADR sealing lands with ARC work; direct delivery now).
		return nil, &SMTPError{Code: 451, Message: "4.3.0 forwarding not enabled yet"}
	}

	mb, err := store.GetMailbox(ctx, d.Store.Pool, addr.MailboxID)
	if err != nil {
		return nil, &SMTPError{Code: 451, Message: "4.3.0 mailbox lookup failed"}
	}

	// Storage-full policy: accept up to the 10% grace buffer, then 552 (Section 4.4).
	q, err := d.CheckQuota(ctx, mb.OrgID)
	if err != nil {
		return nil, &SMTPError{Code: 451, Message: "4.3.0 quota check failed"}
	}
	if q.BeyondGrace {
		return nil, &SMTPError{Code: 552, Message: "5.2.2 mailbox full — storage grace buffer exhausted"}
	}

	// Spam verdict (our rule engine v1 — Section 7.2).
	score := Score(av)
	if av.Malware {
		// Anti-backscatter: quarantine, never bounce content back (Section 5.1).
		return d.quarantine(ctx, raw, envFrom, mb, dom, av, score, "malware")
	}
	if score >= SpamRejectScore || (av.DMARC == "fail" && av.DMARCPolicy == "reject") {
		return nil, &SMTPError{Code: 550, Message: "5.7.1 message rejected by policy"}
	}

	folderSpecial := model.FolderInbox
	if score >= SpamQuarantineScore || (av.DMARC == "fail" && av.DMARCPolicy == "quarantine") {
		folderSpecial = model.FolderJunk
	}

	res, err := d.storeMessage(ctx, raw, envFrom, mb, dom, av, score, folderSpecial)
	if err != nil {
		return nil, err
	}
	if folderSpecial == model.FolderJunk {
		res.Quarantine = true
	}
	return res, nil
}

func (d *Deps) quarantine(ctx context.Context, raw []byte, envFrom string, mb *model.Mailbox, dom *model.Domain, av *AuthVerdicts, score float64, reason string) (*Result, error) {
	// Quarantine holds the blob for digest review without a mailbox_item (Section 5.1).
	parsed, perr := mimeutil.Parse(raw)
	if perr != nil {
		return nil, &SMTPError{Code: 451, Message: "4.3.0 parse failed"}
	}
	sha, _, err := d.Blobs.Put(ctx, NewReader(raw))
	if err != nil {
		return nil, &SMTPError{Code: 451, Message: "4.3.0 storage failed"}
	}
	err = d.Store.WithTx(ctx, func(tx store.Tx) error {
		if err := store.PutBlob(ctx, tx, sha, int64(len(raw))); err != nil {
			return err
		}
		return store.QuarantineMessage(ctx, tx, &model.Quarantine{
			ID:        uuid.New(),
			OrgID:     mb.OrgID,
			MailboxID: &mb.ID,
			Reason:    reason,
			Score:     float32(score),
			ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		})
	})
	if err != nil {
		return nil, &SMTPError{Code: 451, Message: "4.3.0 quarantine failed"}
	}
	_ = parsed
	_ = dom
	_ = av
	_ = envFrom
	return &Result{Quarantine: true, SpamScore: score}, nil
}

// storeMessage writes blob + message + mailbox_item in one transaction.
func (d *Deps) storeMessage(ctx context.Context, raw []byte, envFrom string, mb *model.Mailbox, dom *model.Domain, av *AuthVerdicts, score float64, folderSpecial string) (*Result, error) {
	parsed, err := mimeutil.Parse(raw)
	if err != nil {
		return nil, &SMTPError{Code: 451, Message: "4.3.0 parse failed"}
	}
	sha, size, err := d.Blobs.Put(ctx, NewReader(raw))
	if err != nil {
		return nil, &SMTPError{Code: 451, Message: "4.3.0 storage failed"}
	}

	// Inbound routing rules (register §12d): first match in position order
	// overrides the destination folder. System injections (null envelope path)
	// skip the rule engine.
	var rule *store.MailboxRule
	if envFrom != "" {
		if r, rerr := store.FirstMatchingRule(ctx, d.Store.Pool, mb.ID, parsed.FromEmail, parsed.Subject); rerr == nil && r != nil {
			rule = r
			log.Printf("[ingest] rule %s matched for mailbox %s", r.ID, mb.ID)
		}
	}
	var folder *model.Folder
	if rule != nil && rule.ActionFolderID != nil {
		folder, err = store.GetFolder(ctx, d.Store.Pool, *rule.ActionFolderID)
		if err != nil {
			return nil, &SMTPError{Code: 451, Message: "4.3.0 rule folder lookup failed"}
		}
	} else {
		folder, err = store.GetFolderBySpecialUse(ctx, d.Store.Pool, mb.ID, folderSpecial)
		if err != nil {
			return nil, &SMTPError{Code: 451, Message: "4.3.0 folder lookup failed"}
		}
	}

	msgID := uuid.New()
	authResults := map[string]any{
		"spf":   av.SPF,
		"dkim":  av.DKIM,
		"dmarc": av.DMARC,
		"score": score,
	}
	if av.DKIMDomain != "" {
		authResults["dkim_domain"] = av.DKIMDomain
	}

	msg := &model.Message{
		ID:             msgID,
		OrgID:          mb.OrgID,
		MailboxID:      mb.ID,
		BlobSHA256:     sha,
		Direction:      "inbound",
		MessageID:      parsed.MessageID,
		InReplyTo:      parsed.InReplyTo,
		Refs:           parsed.Refs,
		FromEmail:      parsed.FromEmail,
		FromName:       parsed.FromName,
		Subject:        parsed.Subject,
		Snippet:        mimeutil.Snippet(parsed.Text),
		BodyText:       parsed.Text,
		HasAttachments: len(parsed.Attachments) > 0,
		SizeBytes:      size,
		InternalDate:   time.Now(),
		AuthResults:    authResults,
		SpamScore:      float32(score),
	}

	flags := []string(nil)
	if rule != nil && rule.ActionMarkRead {
		flags = append(flags, model.FlagSeen)
	}
	var uid uint32
	err = d.Store.WithTx(ctx, func(tx store.Tx) error {
		subjNorm := mimeutil.NormalizeSubject(parsed.Subject)
		threadID, terr := store.FindOrCreateThread(ctx, tx, mb.OrgID, parsed.Refs, parsed.InReplyTo, subjNorm)
		if terr != nil {
			return terr
		}
		msg.ThreadID = &threadID
		_, uid, err = store.InsertMessage(ctx, tx, msg, folder.ID, flags)
		if err != nil {
			return err
		}
		for _, a := range parsed.Attachments {
			asha, asize, aerr := d.Blobs.Put(ctx, NewReader(a.Content))
			if aerr != nil {
				return aerr
			}
			if aerr := store.InsertAttachment(ctx, tx, &model.Attachment{
				ID: uuid.New(), MessageID: msgID, BlobSHA256: asha,
				Filename: a.Filename, ContentType: a.ContentType,
				SizeBytes: asize, IsInline: a.IsInline, ContentID: a.ContentID,
			}); aerr != nil {
				return aerr
			}
		}
		return nil
	})
	if err != nil {
		return nil, &SMTPError{Code: 451, Message: "4.3.0 store failed"}
	}
	// Autoresponder (register §12c): window + one-reply-per-sender-per-day
	// guards run here; the reply itself goes out through the wired sender.
	if d.AutoReply != nil && envFrom != "" && folder.SpecialUse == model.FolderInbox &&
		!isDaemonPath(envFrom) {
		log.Printf("[ingest] autoresponder check for mailbox %s from %s", mb.ID, logDomain(envFrom))
		if ok, _ := store.ShouldAutoReply(ctx, d.Store.Pool, mb.ID, time.Now(), envFrom); ok {
			if cerr := d.Store.WithTx(ctx, func(tx store.Tx) error {
				return store.RecordAutoReply(ctx, tx, mb.ID, envFrom)
			}); cerr == nil {
				if aerr := d.AutoReply(ctx, mb.ID, mb.OrgID, envFrom, parsed.Subject); aerr != nil {
					log.Printf("[ingest] auto-reply to %s: %v", envFrom, aerr)
				}
			}
		}
	}
	return &Result{MessageID: msgID, MailboxID: mb.ID, FolderID: folder.ID, UID: uid, SpamScore: score}, nil
}

// isDaemonPath: never reply to the null-path cousins (DSNs arrive with an empty
// envelope from and are skipped earlier; these are the machine senders).
// logDomain: the sender's domain for logs (no local part, no content).
func logDomain(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return addr[i+1:]
	}
	return "(unknown)"
}

func isDaemonPath(envFrom string) bool {
	local := envFrom
	if i := strings.IndexByte(envFrom, '@'); i >= 0 {
		local = envFrom[:i]
	}
	local = strings.ToLower(local)
	for _, d := range []string{"postmaster", "mailer-daemon", "noreply", "no-reply", "donotreply", "bounce", "bounces", "auto"} {
		if local == d || strings.HasPrefix(local, d) {
			return true
		}
	}
	return false
}

// Score is the v1 rule engine (auth results + heuristics — Section 7.2),
// aligned with the Section 5.1 verdict matrix: standalone SPF fail only tags;
// quarantine-grade scores come from DMARC policy failures and accumulated signals.
func Score(av *AuthVerdicts) float64 {
	s := 0.0
	switch av.SPF {
	case "fail":
		s += 2 // tag (Section 5.1: reject requires DMARC policy behind it)
	case "softfail":
		s += 1
	}
	if av.DKIM == "fail" {
		s += 3
	}
	if av.SPF == "none" && av.DKIM == "none" {
		s += 3 // unauthenticated mail
	}
	if av.DMARC == "fail" {
		switch av.DMARCPolicy {
		case "quarantine":
			s += 4
		case "reject":
			s += 8
		default:
			s += 2
		}
	}
	if av.DNSBL != "" {
		s += 6
	}
	return s
}

// DeliverLoopback injects a message directly into the pipeline without TCP —
// used by the outbound worker's dev/loopback transport (deterministic tests).
func DeliverLoopback(ctx context.Context, deps *Deps, raw []byte, envFrom, rcptTo string) error {
	av := &AuthVerdicts{SPF: "pass", DKIM: "pass", DMARC: "pass"}
	_, err := deps.DeliverInbound(ctx, raw, envFrom, rcptTo, av)
	return err
}

func splitRcpt(addr string) (local, domain string, err error) {
	at := -1
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == '@' {
			at = i
			break
		}
	}
	if at <= 0 || at == len(addr)-1 {
		return "", "", errors.New("invalid address")
	}
	return addr[:at], addr[at+1:], nil
}
