// The binding API contract — every shape here mirrors the Go handlers in
// ../../internal/api/. Read together with web/README.md. When the Go side
// changes, this file changes with it.

// ---- errors ----

export interface ErrorEnvelope {
  error: { code: string; message: string; request_id: string; hint?: string }
}

// ---- identity (userView / orgView / mailboxViews in handlers_admin.go) ----

export interface UserView {
  id: string
  org_id: string
  email: string
  display_name: string
  role: 'owner' | 'admin' | 'member'
  status: string // active | suspended
  is_staff: boolean
  created_at: string
}

export interface OrgView {
  id: string
  name: string
  status: string // active | suspended | ...
  tier: string
  sending_enabled: boolean
  review_status: string
  created_at: string
}

export interface MailboxView {
  id: string
  kind: string // user | shared
  name: string
  send_status: string // sendable | paused | ...
  owner: boolean
}

export interface Me {
  user: UserView
  org: OrgView
  impersonated: boolean
  impersonated_by?: string | null
  mailboxes: MailboxView[]
}

export interface LoginResp {
  token: string
  user: UserView
  org: OrgView
}

export interface OrgDetail {
  org: OrgView
  subscription: unknown | null
}

// ---- folders (folderViews) ----

export interface Folder {
  id: string
  name: string
  special_use: string // '\\Inbox' | '\\Sent' | ... | ''
  uid_next: number
  uid_validity: number
  total: number
  unread: number
}

// ---- messages (itemView / messageView / itemViewList) ----

export interface Addr {
  name?: string
  email: string
}

export interface ItemView {
  id: string
  folder_id: string
  message_id: string
  uid: number
  flags: string[] // \Seen \Flagged \Answered ...
  created_at: string
}

export interface MessageView {
  id: string
  thread_id: string | null
  direction: 'inbound' | 'outbound' | 'internal'
  from: { name: string; email: string }
  to: Addr[]
  cc: Addr[]
  subject: string
  snippet: string
  has_attachments: boolean
  size_bytes: number
  internal_date: string
  spam_score: number
  tracking_enabled: boolean
  auth_results: Record<string, unknown> | null // spf/dkim/dmarc results map
}

export interface ItemWithMessage {
  item: ItemView
  message: MessageView
}

export interface FolderPage {
  folder: Folder
  items: ItemWithMessage[]
  limit: number
  has_more: boolean
  next_before_uid?: number
}

// GET /messages/{id} — body fields added server-side for the reading pane
// (handlers_mail.go handleGetMessage; webmail prerequisite §2).
export interface MessageDetail {
  message: MessageView
  item: ItemView
  folder: Folder
  body_text: string
  body_html: string
}

export interface SearchResp {
  results: ItemWithMessage[]
}

export interface TraceResp {
  message_id: string
  events: { state: string; occurred_at: string; detail: Record<string, unknown> | null }[]
}

// The three-label honesty model (§9.2): pixels/opens/links/clicks.
export interface TrackingResp {
  tracking_enabled: boolean
  pixels: number
  opens: number
  links: number
  clicks: number
}

export interface Storage {
  used_bytes: number
  included_bytes: number
  grace_bytes: number
  used_percent: number
  full: boolean
  beyond_grace: boolean
  never_autodelete: boolean
}

// ---- send (handleSend; attachments are base64 content fields — no multipart) ----

export interface SendAttachment {
  filename: string
  content_type: string
  /** base64-encoded bytes; Go json unmarshals into []byte */
  content: string
  is_inline?: boolean
  content_id?: string
}

export interface SendInput {
  from?: string
  to: Addr[]
  cc?: Addr[]
  bcc?: Addr[]
  subject: string
  text: string
  html?: string
  in_reply_to?: string
  track?: boolean
  newsletter?: boolean
  attachments?: SendAttachment[]
}

export interface SendResp {
  message_id: string
  recipients: unknown
}

// ---- attachments (handleListAttachments) ----

export interface AttachmentView {
  id: string
  filename: string
  content_type: string
  size_bytes: number
  is_inline: boolean
  content_id: string
  download_path: string // /api/v1/messages/{id}/attachments/{att_id}
}

// ---- addresses (alias/forward routing) ----

export interface AddressView {
  id: string
  email?: string
  local?: string
  domain?: string
  kind: 'alias' | 'forward' | 'user'
  mailbox_id?: string
  forward_target?: string
  created_at?: string
}

export interface CreateAddressInput {
  local: string
  domain_id: string
  mailbox_id?: string
  kind: 'alias' | 'forward' | 'user'
  forward_target?: string
}

// ---- domains (domainViews) ----

export interface DomainView {
  id: string
  name: string
  status: string // pending | active | ...
  is_platform: boolean
  dkim_selector: string
  verified_at: string | null
}

export interface DnsRecord {
  type?: string
  host?: string
  name?: string
  value?: string
  status?: string
  purpose?: string
  [k: string]: unknown
}

export interface DomainDNS {
  domain?: string
  records?: DnsRecord[]
  [k: string]: unknown
}

// ---- sessions (sessionView) ----

export interface SessionView {
  id: string
  kind: string // web | app-password | api-key | ...
  ip: string
  user_agent: string
  created_at: string
  expires_at: string
  current: boolean
}

// ---- org admin (users, quarantine, shared mailboxes) ----

export interface QuarantineRow {
  id: string
  reason: string
  score: number
  subject: string
  snippet: string
  expires_at: string
  created_at: string
}

export interface SharedMember {
  user_id: string
  email: string
  role: string
  created_at: string
}

// ---- webhooks / API keys / app passwords (§13) ----

export interface WebhookView {
  id: string
  url: string
  events: string[]
  active: boolean
  created_at?: string
}

export interface APIKeyView {
  id: string
  name: string
  prefix: string
  last_used_at: string | null
  expires_at: string | null
  revoked_at: string | null
  created_at: string
}

export interface APIKeySecret {
  id: string
  name: string
  prefix: string
  token: string // shown exactly once (§7.1)
}

export interface AppPasswordView {
  id: string
  name: string
  prefix: string
  created_at?: string
  expires_at?: string | null
  last_used_at?: string | null
}

export interface AppPasswordSecret {
  id: string
  name: string
  prefix: string
  secret: string // shown exactly once (§7.1)
  expires_at: string | null
}

export interface WebhookSecret {
  id: string
  url: string
  events: string[]
  secret: string // shown exactly once (§7.1)
}

// ---- analytics (§12/§13: deliverability, health, reliability) ----

export interface DomainFunnel {
  domain?: string
  sent?: number
  delivered?: number
  bounced?: number
  [k: string]: unknown
}

export interface DeliverabilityStats {
  sent: number
  delivered: number
  bounced: number
  soft_expired: number
  deferred: number
  suppressed: number
  delivery_rate: number
  by_domain: DomainFunnel[]
  window_days: number
}

export interface OrgHealth {
  score: number
  delivery_rate: number
  hard_bounce_rate: number
  sent: number
  suppressed: number
  window_days: number
}

export interface ReliabilityResp {
  days: { day: string; queued: number; delivered: number; bounced: number; deferred: number }[]
  window_days: number
}

// ---- features (§12: signatures, autoresponder, rules) ----

export interface Signature {
  id: string
  name: string
  html: string
  text: string
  is_default: boolean
  created_at: string
}

export interface Autoresponder {
  enabled: boolean
  subject: string
  body: string
  starts_at: string | null
  ends_at: string | null
  updated_at?: string
}

export interface Rule {
  id: string
  position: number
  enabled: boolean
  match_from: string
  match_subject: string
  action_folder_id: string | null
  action_mark_read: boolean
  created_at: string
}

export interface CreateRuleInput {
  match_from?: string
  match_subject?: string
  action_folder_id?: string
  action_mark_read?: boolean
  enabled?: boolean
}

// ---- suppression (POPIA opt-out register) ----

export interface SuppressionRow {
  email: string
  reason: string
  created_at: string
}

// ---- MFA (§ audit item 3) ----

export interface MFAEnrol {
  secret: string
  otpauth_url: string
  instructions: string
}
