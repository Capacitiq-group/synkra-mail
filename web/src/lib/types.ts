export type ISODateString = string;

// ===== Common =====
export interface Address { name?: string; email: string }
export interface Envelope<T> { data: T; error?: { code: string; message: string; fix?: string } }

export interface CursorPage<T> {
  items: T[];
  next_cursor: string | null;
  has_more: boolean;
}

// ===== Auth =====
export type Role = 'owner' | 'admin' | 'billing' | 'mailbox_admin' | 'employee' | 'auditor' | 'staff';

export interface Me {
  user_id: string;
  org_id: string;
  email: string;
  display_name: string;
  role: Role;
  org_status: 'pending' | 'approved' | 'suspended';
  totp_enabled: boolean;
  passkeys: number;
  avatar_url?: string;
  department?: string;
  job_title?: string;
  phone?: string;
  timezone: string;
  locale: string;
  org_name?: string;
  org_has_sso?: boolean;
  org_has_ip_restrictions?: boolean;
}

export interface LoginResponse {
  me?: Me;
  requires_totp?: boolean;
  totp_challenge?: string;
  requires_passkey?: boolean;
  passkey_challenge?: { challenge: string; rp_id: string; allow_credentials: { id: string; type: 'public-key' }[] };
  csrf_token?: string;
}

export interface Session {
  id: string;
  device_name: string;
  ip: string;
  user_agent: string;
  created_at: ISODateString;
  last_seen_at: ISODateString;
  current: boolean;
  location?: string;
}

export interface Device {
  id: string;
  name: string;
  kind: 'web' | 'mobile' | 'desktop';
  first_seen_at: ISODateString;
  last_seen_at: ISODateString;
  ip: string;
  current: boolean;
}

export interface AppPassword {
  id: string;
  label: string;
  scopes: ('imap' | 'smtp')[];
  created_at: ISODateString;
  last_used_at?: ISODateString;
  last_used_ip?: string;
  revoked: boolean;
}

export interface Passkey {
  id: string;
  label: string;
  created_at: ISODateString;
  last_used_at?: ISODateString;
  transports?: ('usb' | 'nfc' | 'ble' | 'internal' | 'hybrid')[];
}

export interface RecoveryCodeSet { codes: string[]; generated_at: ISODateString }

// ===== Mailbox / folders / messages =====
export interface Mailbox {
  id: string;
  address: string;
  display_name: string;
  quota_bytes: number;
  used_bytes: number;
  suspended: boolean;
  bounce_breaker_paused: boolean;
  shared: boolean;
  kind: 'personal' | 'shared' | 'role';
}

export type FolderRole = 'inbox' | 'sent' | 'drafts' | 'archive' | 'spam' | 'trash' | null;

export interface Folder {
  id: string;
  mailbox_id: string;
  name: string;
  role: FolderRole;
  unread: number;
  total: number;
  parent_id?: string;
  sort_order: number;
}

export type MessageFlag = 'seen' | 'flagged' | 'answered' | 'forwarded' | 'draft' | 'pinned' | 'snoozed' | 'deleted';

export interface MessageSummary {
  id: string;
  mailbox_id: string;
  thread_id: string;
  folder_id: string;
  from: Address;
  to: Address[];
  subject: string;
  snippet: string;
  received_at: ISODateString;
  is_read: boolean;
  has_attachments: boolean;
  flags: MessageFlag[];
  label_ids: string[];
  spam_score?: number;
  external_sender?: boolean;
  auth_results?: AuthResults;
}

export interface AuthResults {
  spf: 'pass' | 'fail' | 'softfail' | 'neutral' | 'none' | 'temperror' | 'permerror';
  dkim: 'pass' | 'fail' | 'none' | 'temperror' | 'permerror';
  dmarc: 'pass' | 'fail' | 'none' | 'temperror' | 'permerror';
  tls: boolean;
}

export interface Attachment {
  id: string;
  filename: string;
  content_type: string;
  size: number;
  inline: boolean;
  content_id?: string;
  scan_status: 'pending' | 'clean' | 'infected' | 'failed';
}

export interface MessageDetail extends MessageSummary {
  cc: Address[];
  bcc: Address[];
  reply_to?: Address[];
  body_text?: string;
  body_html?: string;
  attachments: Attachment[];
  headers: Record<string, string[]>;
  in_reply_to?: string;
  references?: string[];
  notes?: Note[];
}

export interface Thread {
  id: string;
  messages: MessageDetail[];
}

export interface Label {
  id: string;
  name: string;
  color: string;
}

export interface SearchQuery {
  folder?: string;
  q?: string;
  from?: string;
  to?: string;
  subject?: string;
  has_attachment?: boolean;
  is_unread?: boolean;
  after?: ISODateString;
  before?: ISODateString;
  label?: string;
}

// ===== Compose =====
export interface SendRequest {
  mailbox_id: string;
  from?: Address;
  to: Address[];
  cc: Address[];
  bcc: Address[];
  subject: string;
  body_text: string;
  body_html?: string;
  in_reply_to?: string;
  references?: string[];
  attachment_ids?: string[];
  schedule_at?: ISODateString;
  undo_send_seconds?: number;
  tracking?: boolean;
}

export interface SendResponse {
  message_id: string;
  accepted: string[];
  suppressed: string[];
  scheduled_for?: ISODateString;
  queued_until?: ISODateString;
}

export interface Draft {
  id: string;
  mailbox_id: string;
  from?: Address;
  to: Address[];
  cc: Address[];
  bcc: Address[];
  subject: string;
  body_text: string;
  body_html?: string;
  in_reply_to?: string;
  attachment_ids: string[];
  updated_at: ISODateString;
}

export interface EmailTemplate {
  id: string;
  name: string;
  body_text: string;
  body_html?: string;
  subject?: string;
  shared: boolean;
}

export interface Identity {
  id: string;
  mailbox_id: string;
  address: string;
  display_name: string;
  is_default: boolean;
  kind: 'primary' | 'alias';
}

// ===== Aliases =====
export type AliasCategory = 'shopping' | 'newsletters' | 'suppliers' | 'personal' | 'other';

export interface Alias {
  id: string;
  address: string;
  target_mailbox_id: string;
  label: string;
  category: AliasCategory;
  created_at: ISODateString;
  last_used_at?: ISODateString;
  message_count: number;
  disabled: boolean;
  single_use: boolean;
  expires_at?: ISODateString;
  blocked_senders: string[];
  forward_to?: string;
  notification_enabled: boolean;
  /** When true the alias is exposed as a sender identity in the composer.
   *  Only valid if the platform manages DKIM/SPF for the alias domain. */
  reply_from_enabled?: boolean;
}

// ===== Signatures =====
export interface Signature {
  id: string;
  name: string;
  body_text: string;
  body_html: string;
  is_default: boolean;
  use_for_replies: boolean;
  locked: boolean;
  locked_html?: string;
}

export interface SignatureLintIssue {
  rule: string;
  severity: 'error' | 'warning' | 'info';
  message: string;
  fix?: string;
}

export interface SignatureImportResult {
  body_html: string;
  issues: SignatureLintIssue[];
}

// ===== OOO / rules =====
export interface OutOfOffice {
  id?: string;
  enabled: boolean;
  starts_at: ISODateString;
  ends_at: ISODateString;
  timezone: string;
  scope: 'internal' | 'external' | 'all';
  body_internal?: string;
  body_external?: string;
  reply_frequency_days: number;
  reply_to_known_only: boolean;
}

export type RuleCondition =
  | { type: 'from_matches'; value: string }
  | { type: 'to_matches'; value: string }
  | { type: 'subject_contains'; value: string }
  | { type: 'body_contains'; value: string }
  | { type: 'has_attachment'; value: boolean }
  | { type: 'is_external'; value: boolean }
  | { type: 'address_matches'; value: string }
  | { type: 'alias_id'; value: string };

export type RuleAction =
  | { type: 'forward'; to: string[] }
  | { type: 'redirect'; to: string }
  | { type: 'apply_label'; label_id: string }
  | { type: 'move'; folder_id: string }
  | { type: 'mark_important' }
  | { type: 'notify_colleague'; email: string; message?: string }
  | { type: 'create_task'; list_id: string; title_template: string }
  | { type: 'auto_reply'; template_id: string }
  | { type: 'add_to_shared_mailbox'; shared_mailbox_id: string };

export interface Rule {
  id: string;
  mailbox_id: string;
  name: string;
  enabled: boolean;
  order: number;
  match: 'all' | 'any';
  conditions: RuleCondition[];
  actions: RuleAction[];
  created_at: ISODateString;
  updated_at: ISODateString;
}

// ===== Newsletters =====
export interface NewsletterSender {
  sender: Address;
  frequency: 'daily' | 'weekly' | 'monthly' | 'irregular';
  message_count: number;
  unread: number;
  last_received_at: ISODateString;
  unsubscribe: 'one_click' | 'website' | 'link_in_body' | 'none';
  muted: boolean;
  digest: 'on' | 'off' | 'inherit';
}

export interface DigestPreferences {
  mode: 'daily' | 'weekly' | 'off';
  time_of_day: string;
  timezone: string;
  exclude_senders: string[];
}

// ===== Contacts =====
export interface Contact {
  id: string;
  display_name: string;
  emails: Address[];
  phones: string[];
  company?: string;
  job_title?: string;
  notes?: string;
  groups: string[];
  org_visible: boolean;
  avatar_url?: string;
  created_at: ISODateString;
  updated_at: ISODateString;
}

export interface ContactGroup {
  id: string;
  name: string;
  member_count: number;
}

// ===== Calendar =====
export interface Calendar {
  id: string;
  name: string;
  color: string;
  kind: 'personal' | 'shared';
  owner_email: string;
  read_only: boolean;
  url: string;
}

export interface CalendarEvent {
  id: string;
  calendar_id: string;
  title: string;
  description?: string;
  location?: string;
  starts_at: ISODateString;
  ends_at: ISODateString;
  all_day: boolean;
  timezone: string;
  rrule?: string;
  attendees: Address[];
  reminders_minutes: number[];
  organizer: Address;
  rsvp: 'accepted' | 'declined' | 'tentative' | 'needs_action';
  status: 'confirmed' | 'tentative' | 'cancelled';
  url?: string;
}

export interface SchedulingLink {
  id: string;
  slug: string;
  duration_minutes: number;
  availability: Record<string, { start: string; end: string }[]>;
  timezone: string;
  calendar_id: string;
  active: boolean;
}

// ===== Tasks =====
export interface TaskList {
  id: string;
  name: string;
  color: string;
  shared: boolean;
}

export interface Task {
  id: string;
  list_id: string;
  title: string;
  notes?: string;
  due_at?: ISODateString;
  completed: boolean;
  completed_at?: ISODateString;
  assigned_to?: string;
  source_message_id?: string;
  source_thread_id?: string;
  created_at: ISODateString;
}

// ===== Notes =====
export interface Note {
  id: string;
  message_id?: string;
  thread_id?: string;
  body: string;
  private: boolean;
  author: string;
  created_at: ISODateString;
}

// ===== Shared mailboxes =====
export type SharedStatus = 'new' | 'open' | 'waiting' | 'closed';

// Part 8 replaced the part-1 minimal SharedMailbox with this superset —
// optional where the server may not yet return the field.
export interface SharedMailboxMember {
  email: string;
  display_name: string;
  role: 'member' | 'manager';
}

export interface SharedMailbox {
  id: string;
  address: string;
  display_name: string;
  description?: string;
  members: SharedMailboxMember[];
  sla_minutes?: number;
  default_assignee?: Address;
  unread_count?: number;
}

export interface ReplyOwner {
  user: Address;
  claimed_at: ISODateString;
  expires_at: ISODateString;
  message_id?: string;
}

export interface ThreadAssignment {
  thread_id: string;
  shared_mailbox_id: string;
  assigned_to?: Address;
  status: SharedStatus;
  last_activity_at: ISODateString;
  reply_owner?: ReplyOwner | null;
  created_at: ISODateString;
  updated_at: ISODateString;
}

export interface ClaimReplyResponse {
  claimed: boolean;
  /** The current owner (yours if claimed). */
  owner: ReplyOwner;
  /** Set only when claimed === false — the owner you conflicted with. */
  competing_owner?: ReplyOwner;
}

export type SharedActivityKind =
  | 'assigned'
  | 'unassigned'
  | 'status_changed'
  | 'reply_started'
  | 'reply_sent'
  | 'note_added'
  | 'thread_created'
  | 'thread_reopened'
  | 'thread_closed';

export interface SharedActivityEvent {
  id: string;
  thread_id?: string;
  actor: Address;
  kind: SharedActivityKind;
  at: ISODateString;
  detail?: {
    from_status?: SharedStatus;
    to_status?: SharedStatus;
    from_assignee?: Address;
    to_assignee?: Address;
    note_id?: string;
    message_id?: string;
    subject?: string;
  };
}

export interface SharedDraft {
  id: string;
  thread_id: string;
  shared_mailbox_id: string;
  from: Address;
  subject: string;
  snippet: string;
  updated_at: ISODateString;
  updated_by: Address;
}

// ===== Part 5/6 — multiple scheduled OOO periods (supersedes single-period) =====
export interface OutOfOfficePeriod {
  id: string;
  enabled: boolean;
  starts_at: ISODateString;
  ends_at: ISODateString;
  timezone: string;
  scope: 'internal' | 'external' | 'all';
  body_internal?: string;
  body_external?: string;
  reply_frequency_days: number;
  reply_to_known_only: boolean;
}

export interface OutOfOfficeState {
  globally_enabled: boolean;
  periods: OutOfOfficePeriod[];
  /** Senders excluded from every auto-reply — typically distribution lists. */
  excluded_senders: string[];
  /** Whether the loop-prevention guard is acknowledged (must be true). */
  loop_guard_acknowledged: boolean;
}

// ===== Deliverability / health / trace =====
export interface DeliverabilitySummary {
  domain: string;
  period_days: number;
  sent: number;
  delivered: number;
  deferred: number;
  bounced: number;
  complained: number;
  rejected: number;
  success_rate: number;
  bounce_rate: number;
  complaint_rate: number;
  reputation: 'excellent' | 'good' | 'fair' | 'poor';
  auth: { spf: boolean; dkim: boolean; dmarc: boolean; mta_sts: boolean; tls_rpt: boolean; bimi: boolean };
  blacklists: { name: string; listed: boolean; delist_url?: string }[];
  sending_limit: { current: number; limit: number; resets_at: ISODateString };
}

export interface DeliveryTrace {
  message_id: string;
  recipient: string;
  hops: DeliveryHop[];
}

export interface DeliveryHop {
  at: ISODateString;
  stage: 'submitted' | 'authenticated' | 'queued' | 'remote_contacted' | 'remote_response' | 'delivered' | 'deferred' | 'bounced';
  detail: string;
  duration_ms?: number;
}

export interface HealthScore {
  score: number;
  categories: {
    key: 'domain_auth' | 'deliverability' | 'security' | 'storage' | 'dns' | 'reputation';
    label: string;
    score: number;
    status: 'excellent' | 'good' | 'fair' | 'poor';
    improvements: { title: string; detail: string; link?: string }[];
  }[];
}

export interface ReliabilityAlert {
  id: string;
  kind: 'mx_down' | 'dkim_failing' | 'queue_growing' | 'storage_high' | 'reputation_drop' | 'rejected_message' | 'domain_expiring';
  severity: 'info' | 'warning' | 'critical';
  domain?: string;
  mailbox?: string;
  created_at: ISODateString;
  acknowledged_at?: ISODateString;
  message: string;
}

// ===== Domains =====
export interface Domain {
  id: string;
  name: string;
  status: 'pending' | 'verifying' | 'verified' | 'failed';
  ownership_token: string;
  registered_through_us: boolean;
  nameservers: string[];
  registrar: 'oner' | 'external';
  registrar_id?: string;
  expires_at?: ISODateString;
  auto_renew: boolean;
  transfer_lock: boolean;
  whois_privacy: boolean;
  managed: 'fully' | 'partial';
  dns_zone_ok: boolean;
  verification_emails: { address: string; verified: boolean; sent_at: ISODateString }[];
}

export type DnsRecordCheck = {
  type: 'MX' | 'TXT' | 'CNAME' | 'A' | 'PTR';
  name: string;
  value: string;
  purpose: string;
  status: 'pass' | 'fail' | 'warn' | 'pending';
  observed?: string;
  fix_instructions?: { provider: string; steps: string[] }[];
};

export interface DnsDoctorReport {
  domain: string;
  checked_at: ISODateString;
  overall: 'pass' | 'fail' | 'warn';
  records: DnsRecordCheck[];
}

// ===== Migration =====
export interface MigrationJob {
  id: string;
  source: { host: string; port: number; username: string; use_tls: boolean };
  target_mailbox_id: string;
  status: 'queued' | 'running' | 'paused' | 'complete' | 'failed' | 'cancelled';
  total_messages: number;
  migrated_messages: number;
  failed_messages: number;
  attention: { message_id?: string; reason: string }[];
  started_at?: ISODateString;
  completed_at?: ISODateString;
}

// ===== Storage =====
export interface StorageUsage {
  used_bytes: number;
  quota_bytes: number;
  by_folder: { folder_id: string; name: string; bytes: number }[];
  largest_messages: { id: string; subject: string; size: number; received_at: ISODateString }[];
}

// ===== Billing =====
export interface BillingPlan {
  name: string;
  mailboxes: number;
  price_per_mailbox_zar: number;
  vat_rate: number;
}

export interface Invoice {
  id: string;
  number: string;
  issued_at: ISODateString;
  paid_at?: ISODateString;
  status: 'paid' | 'due' | 'overdue' | 'void';
  total_zar: number;
  vat_zar: number;
  lines: { description: string; quantity: number; unit_zar: number; total_zar: number }[];
  download_url: string;
}

export interface PaymentMethod {
  id: string;
  provider: 'payfast' | 'yoco' | 'ozow' | 'pay_shap' | 'stitch' | 'peach';
  kind: 'card' | 'eft' | 'instant_eft';
  masked: string;
  expires_at?: ISODateString;
  is_default: boolean;
}

// ===== Privacy / POPIA =====
export interface ProcessingRegisterEntry {
  id: string;
  purpose: string;
  lawful_basis: string;
  data_categories: string[];
  retention: string;
  recipients: string[];
}

export interface SubProcessor {
  name: string;
  purpose: string;
  country: string;
  transfer_safeguard?: string;
}

export interface DsarRequest {
  id: string;
  kind: 'export' | 'delete';
  status: 'queued' | 'processing' | 'ready' | 'completed' | 'rejected';
  created_at: ISODateString;
  download_url?: string;
}

// ===== Org admin =====
export interface OrgProfile {
  id: string;
  name: string;
  primary_domain: string;
  domains: Domain[];
  plan: BillingPlan;
  vat_number?: string;
  billing_email: string;
  support_email?: string;
  timezone: string;
  locale: string;
}

export interface Department {
  id: string;
  name: string;
  manager_email?: string;
  member_count: number;
}

export interface OrgUser {
  id: string;
  email: string;
  display_name: string;
  role: Role;
  department?: string;
  suspended: boolean;
  totp_enabled: boolean;
  last_login_at?: ISODateString;
}

export interface AddressEntry {
  id: string;
  address: string;
  kind: 'alias' | 'group' | 'distribution' | 'catch_all' | 'role';
  members?: string[];
  target?: string;
  active: boolean;
}

export interface CompanySignatureTemplate {
  id: string;
  name: string;
  body_html: string;
  variables: string[];
  locked_nodes: string[];
  assigned_to: { kind: 'everyone' | 'department' | 'user'; value?: string }[];
  policy: 'required' | 'editable_sections';
}

export interface AuditEvent {
  id: string;
  actor_email: string;
  action: string;
  resource: string;
  ip: string;
  at: ISODateString;
  detail?: Record<string, unknown>;
}

// ===== Staff admin =====
export interface OrgSummary {
  id: string;
  name: string;
  primary_domain: string;
  status: 'pending' | 'approved' | 'suspended';
  created_at: ISODateString;
  mailbox_count: number;
  declined_at?: ISODateString;
}

export interface SuppressionEntry {
  address: string;
  reason: string;
  source: string;
  created_at: ISODateString;
}

export interface QuarantineEntry {
  id: string;
  mailbox_id: string;
  from: Address;
  subject: string;
  received_at: ISODateString;
  reason: string;
  score: number;
}

export interface ImpersonationSession {
  id: string;
  staff_email: string;
  target_user_email: string;
  reason: string;
  started_at: ISODateString;
  expires_at: ISODateString;
  ended_at?: ISODateString;
}

export interface AbuseCase {
  id: string;
  mailbox_id: string;
  kind: 'compromise' | 'spam' | 'phishing' | 'bulk_abuse' | 'other';
  evidence: string;
  opened_at: ISODateString;
  status: 'investigating' | 'throttled' | 'suspended' | 'restored' | 'closed';
}

export interface QueueJob {
  id: string;
  message_id: string;
  recipient: string;
  attempts: number;
  next_attempt_at: ISODateString;
  last_error?: string;
  state: 'pending' | 'leased' | 'deferred' | 'failed';
}

export interface FleetStats {
  sent_24h: number;
  received_24h: number;
  delivery_rate_24h: number;
  queue_depth: number;
  breaker_trips_24h: number;
  at: ISODateString;
}

// ===== SSE events =====
export type SseEvent =
  | { type: 'message.new'; mailbox_id: string; folder_id: string }
  | { type: 'message.updated'; mailbox_id: string; folder_id: string; message_id: string }
  | { type: 'folder.updated'; mailbox_id: string }
  | { type: 'mailbox.updated'; mailbox_id: string }
  | { type: 'org.updated' }
  | { type: 'queue.updated' }
  | { type: 'health.updated' };

// ===== Part 9 — deliverability timeseries & DMARC/BIMI/MTA-STS/TLS-RPT =====
export interface DeliverabilityBucket {
  date: ISODateString;
  sent: number;
  delivered: number;
  bounced: number;
  deferred: number;
  complained: number;
  rejected: number;
}

export interface DeliverabilityTimeseries {
  domain: string;
  period_days: number;
  buckets: DeliverabilityBucket[];
}

export type DmarcPhase = 'none' | 'quarantine' | 'reject';

export interface DmarcRolloutState {
  domain: string;
  current_phase: DmarcPhase;
  rua_address: string;
  last_change_at?: ISODateString;
  next_recommended_phase?: DmarcPhase;
  next_phase_reason?: string;
  pass_rate_7d: number;
  message_volume_7d: number;
  record_value: string;
}

export interface DmarcAggregateReport {
  id: string;
  reporter: string;
  domain: string;
  date_range_start: ISODateString;
  date_range_end: ISODateString;
  total_messages: number;
  pass_count: number;
  fail_count: number;
  disposition: { none: number; quarantine: number; reject: number };
}

export interface BimiState {
  domain: string;
  enabled: boolean;
  logo_url?: string;
  logo_verified: boolean;
  vmc_status: 'none' | 'pending' | 'verified' | 'rejected';
  vmc_expires_at?: ISODateString;
  ready: boolean;
  blockers: string[];
  record_name: string;
  record_value: string;
  dmarc_phase: DmarcPhase;
}

export type MtaStsMode = 'none' | 'testing' | 'enforce';

export interface MtaStsState {
  domain: string;
  enabled: boolean;
  mode: MtaStsMode;
  max_age_seconds: number;
  mx_hosts: string[];
  policy_id?: string;
  last_published_at?: ISODateString;
  policy_url: string;
  dns_record_name: string;
  dns_record_value: string;
}

export interface TlsRptState {
  domain: string;
  enabled: boolean;
  rua_address?: string;
  last_report_at?: ISODateString;
  success_rate_7d?: number;
  dns_record_name: string;
  dns_record_value: string;
}

// ===== Part 4 type extensions (reply-from aliases, org hints) =====
declare module './types' {}
