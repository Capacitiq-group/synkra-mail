// Package model: domain types shared across services. IDs are uuid; API layers
// add type prefixes via ids.Format.
package model

import (
	"time"

	"github.com/google/uuid"
)

type Org struct {
	ID                   uuid.UUID
	Name                 string
	PlanID               uuid.UUID
	Status               string // active | suspended
	Tier                 string // new | verified
	SendingEnabled       bool
	SendingBlockedReason string
	ReviewStatus         string // pending | approved | declined
	TrackingEnabled      bool   // per-org tracking toggle (off by default — owner decision)
	StorageOverageGB     float64
	SynkraOrgID          *uuid.UUID
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type Plan struct {
	ID                       uuid.UUID
	Slug                     string
	Name                     string
	PriceCents               int
	Currency                 string
	Interval                 string
	MailboxLimit             int
	AliasLimit               int
	SharedMailboxLimit       int
	StorageTotalMB           int
	StorageOverageCentsPerGB int
}

type Subscription struct {
	ID               uuid.UUID
	OrgID            uuid.UUID
	PlanID           uuid.UUID
	Status           string // trial | active | past_due | canceled
	TrialEndsAt      *time.Time
	PaystackRef      string
	CurrentPeriodEnd *time.Time
	CreatedAt        time.Time
}

type User struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	Email           string
	PasswordHash    string
	HasTOTP         bool
	TotpEnabled     bool       // MFA active: login demands a valid code (audit item 3)
	EmailVerifiedAt *time.Time // e-mail verification (audit item 13)
	DisplayName     string
	Role            string // owner | admin | member
	IsPlatformAdmin bool
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Session struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	TokenHash      string
	Kind           string // web | admin | impersonation
	IP             string
	UserAgent      string
	ImpersonatedBy *uuid.UUID
	ExpiresAt      time.Time
	RevokedAt      *time.Time
	CreatedAt      time.Time
}

type AppPassword struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Name         string
	Prefix       string
	PasswordHash string
	ExpiresAt    time.Time
	LastUsedAt   *time.Time
	RevokedAt    *time.Time
	CreatedAt    time.Time
}

type Domain struct {
	ID             uuid.UUID
	OrgID          *uuid.UUID // nil = platform domain
	Name           string
	IsPlatform     bool
	Status         string // pending | verified | disabled | expired | reverify
	OwnershipToken string
	DKIMSelector   string
	DKIMPrivatePEM string // sealed at rest ("enc1." prefix) — Section 7.6 / audit item 4
	DKIMPublicTXT  string
	VerifiedAt     *time.Time
	// Staged rotation (audit item 4): the next key is generated and published
	// first; ActivateDKIMRotation swaps it in once its DNS record resolves.
	NextSelector   string
	NextPrivateEnc string
	NextPublicTXT  string
	ReverifyDueAt  *time.Time // ownership re-check deadline (audit item 11)
	ExpiresAt      *time.Time // unverified registrations expire (audit item 11)
	CreatedAt      time.Time
}

type Mailbox struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	OwnerUserID  *uuid.UUID
	Kind         string // user | shared
	Name         string
	SendStatus   string // active | paused_bounces | paused_abuse | paused_admin
	PausedReason string
	CreatedAt    time.Time
}

type Address struct {
	ID            uuid.UUID
	OrgID         uuid.UUID
	DomainID      uuid.UUID
	MailboxID     uuid.UUID
	Local         string
	Kind          string // user | alias | forward
	ForwardTarget string
	IsPrimary     bool
	AllowSendAs   bool
	CreatedAt     time.Time
}

// Addr returns the full email address string. Domain name is resolved by the caller.
func (a Address) Addr(domainName string) string { return a.Local + "@" + domainName }

type MailboxMember struct {
	ID        uuid.UUID
	MailboxID uuid.UUID
	UserID    uuid.UUID
	Role      string // owner | member | viewer
	CreatedAt time.Time
}

type Folder struct {
	ID            uuid.UUID
	MailboxID     uuid.UUID
	Name          string
	SpecialUse    string // inbox|sent|drafts|trash|junk|archive|""
	UIDValidity   uint32
	UIDNext       uint32
	TotalMessages uint32
	Unread        uint32
	Subscribed    bool
	CreatedAt     time.Time
}

type Blob struct {
	SHA256     string
	SizeBytes  int64
	Refcount   int
	StoredAt   time.Time
	PurgeAfter *time.Time
}

type Thread struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	SubjectNorm string
	AssigneeID  *uuid.UUID
	Note        string // internal note (Section 10.3) — never transmitted
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Message struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	MailboxID       uuid.UUID
	BlobSHA256      string
	Direction       string // inbound | outbound | internal
	MessageID       string // RFC Message-ID header
	InReplyTo       string
	Refs            []string
	ThreadID        *uuid.UUID
	FromEmail       string
	FromName        string
	ToAddrs         []EmailAddr // jsonb
	CcAddrs         []EmailAddr
	Subject         string
	Snippet         string
	BodyText        string
	HasAttachments  bool
	SizeBytes       int64
	InternalDate    time.Time
	AuthResults     map[string]any
	SpamScore       float32
	TrackingEnabled bool
	CreatedAt       time.Time
}

// EmailAddr is the jsonb-friendly form stored in message.to_addrs / cc_addrs.
type EmailAddr struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type MailboxItem struct {
	ID        uuid.UUID
	FolderID  uuid.UUID
	MessageID uuid.UUID
	UID       uint32
	Flags     []string
	DeletedAt *time.Time
	CreatedAt time.Time
}

type Attachment struct {
	ID          uuid.UUID
	MessageID   uuid.UUID
	BlobSHA256  string
	Filename    string
	ContentType string
	SizeBytes   int64
	IsInline    bool
	ContentID   string
}

type DeliveryJob struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	MailboxID       uuid.UUID
	MessageID       uuid.UUID
	EnvelopeFrom    string
	EnvelopeTo      string
	RecipientDomain string
	IsDSN           bool
	Status          string // queued|in_flight|delivered|deferred|bounced|canceled
	Attempts        int
	NextAttemptAt   time.Time
	LeaseUntil      *time.Time
	LeasedBy        string
	LastCode        string
	LastMsg         string
	ScheduledSendAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type DSN struct {
	ID            uuid.UUID
	DeliveryJobID uuid.UUID
	Kind          string // bounce | delay | success
	IsHard        *bool
	SMTPCode      string
	Reason        string
	CreatedAt     time.Time
}

type Suppression struct {
	OrgID     uuid.UUID
	Email     string
	Reason    string // hard_bounce | complaint | unsub
	CreatedAt time.Time
}

type Quarantine struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	MailboxID *uuid.UUID
	MessageID *uuid.UUID
	Reason    string
	Score     float32
	ExpiresAt time.Time
	CreatedAt time.Time
}

// TrackingPixel, OpenEvent, TrackedLink, ClickEvent — Section 9.
type TrackingPixel struct {
	ID        uuid.UUID
	MessageID uuid.UUID
	Recipient string
	CreatedAt time.Time
}

type OpenEvent struct {
	ID           uuid.UUID
	PixelID      uuid.UUID
	OccurredAt   time.Time
	Confidence   string // opened | likely_opened | possibly_automated
	ClientFamily string
	IPHash       string
}

type TrackedLink struct {
	ID        uuid.UUID
	MessageID uuid.UUID
	URL       string
	CreatedAt time.Time
}

type ClickEvent struct {
	ID           uuid.UUID
	LinkID       uuid.UUID
	OccurredAt   time.Time
	ClientFamily string
	IPHash       string
}

// System folder special-use values.
const (
	FolderInbox   = "inbox"
	FolderSent    = "sent"
	FolderDrafts  = "drafts"
	FolderTrash   = "trash"
	FolderJunk    = "junk"
	FolderArchive = "archive"
)

// Standard IMAP system flags we store verbatim in mailbox_item.flags.
const (
	FlagSeen     = "\\Seen"
	FlagAnswered = "\\Answered"
	FlagFlagged  = "\\Flagged"
	FlagDeleted  = "\\Deleted"
	FlagDraft    = "\\Draft"
	FlagRecent   = "\\Recent"
)

// Idempotency is a stored first response for a keyed request (build register
// §1.9): keyed retries replay it instead of executing twice.
type Idempotency struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Key         string
	Endpoint    string
	RequestHash string
	Status      int
	Response    []byte // raw JSON, stored as jsonb
	CreatedAt   time.Time
}

// DeliveryEvent is one row of the delivery state machine (build register §7.3):
// queued → delivered | bounced | deferred, per message, in order.
type DeliveryEvent struct {
	ID         uuid.UUID
	MessageID  uuid.UUID
	OrgID      uuid.UUID
	MailboxID  *uuid.UUID
	State      string
	Detail     []byte // jsonb
	OccurredAt time.Time
}
