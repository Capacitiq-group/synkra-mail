// Core plumbing for the Synkra webmail API client.
//
// This file is the ADAPTATION LAYER between the part-1..9 frontend view-models
// (src/lib/types.ts — written against the design spec) and the REAL Go API
// (internal/api — bearer auth, flat routes, view-shaped JSON). Everything the
// UI expects but the backend does not yet expose resolves here into honest,
// inert data or a typed `notAvailable` error — never a guessed endpoint.
//
// Contract highlights (see repo web/README + BUILD_REGISTER §20):
//  - every session route requires `Authorization: Bearer <token>` (login mints
//    it; we hold it in localStorage and wipe it on 401);
//  - errors arrive as {error:{code,message,request_id,hint?}};
//  - /send is idempotent via the Idempotency-Key header;
//  - attachments ride inside the send JSON as base64 — there is no upload
//    endpoint, so uploaded files live in a client-side registry keyed by
//    attachment_id and are inlined at send time.

import { ApiError } from './errors'
import type {
  Alias, AppPassword, Attachment, AuthResults, DeliverabilitySummary, Domain,
  Folder, HealthScore, Me, MessageDetail, MessageFlag, MessageSummary, OrgSummary,
  QuarantineEntry, ReliabilityAlert, Session, StorageUsage, SuppressionEntry,
} from './types'

export { ApiError }

export const BASE = '/api/v1'
const TOKEN_KEY = 'synkra.token'

// ---- token ----

let token: string | null = null
try { token = localStorage.getItem(TOKEN_KEY) } catch { token = null }

export function getToken(): string | null { return token }
export function setToken(t: string | null): void {
  token = t
  try {
    if (t) localStorage.setItem(TOKEN_KEY, t)
    else localStorage.removeItem(TOKEN_KEY)
  } catch { /* in-memory only */ }
}

/** Login state kept across the two-step TOTP flow (password → code retry). */
export const pendingLogin: { email: string; password: string } = { email: '', password: '' }

// ---- request ----

export async function request<T>(path: string, init: RequestInit = {}, signal?: AbortSignal): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  if (token) headers.set('Authorization', `Bearer ${token}`)

  let res: Response
  try {
    res = await fetch(BASE + path, { ...init, headers, credentials: 'omit', signal })
  } catch {
    if (signal?.aborted) throw new DOMException('Aborted', 'AbortError')
    throw new ApiError(0, 'network_error', 'The server could not be reached', 'check your connection and try again')
  }

  const text = await res.text()
  let payload: unknown = null
  try { payload = text ? JSON.parse(text) : null } catch { payload = null }

  if (!res.ok) {
    const env = payload as { error?: { code?: string; message?: string; request_id?: string; hint?: string } } | null
    const code = env?.error?.code || `http_${res.status}`
    const err = new ApiError(res.status, code, env?.error?.message || res.statusText, env?.error?.hint)
    // Dead session outside the login route: wipe token; AppProviders' 401
    // watcher clears auth state and the router bounces to /login.
    if (res.status === 401 && code === 'invalid_credentials' && !path.startsWith('/auth/login')) {
      setToken(null)
    }
    throw err
  }
  return payload as T
}

export function qs(params: Record<string, string | number | boolean | undefined | null>): string {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') sp.set(k, String(v))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}

/** Modules the backend has not shipped yet (register 12/13/14 tail). */
export function notAvailable(what: string, where: string): never {
  throw new ApiError(
    501, 'module_unavailable',
    `${what} is not available on this deployment yet`,
    `ships with the backend module "${where}" — tracked in BUILD_REGISTER`,
  )
}

/** Local id for client-side stubs (drafts, notes reads, task seeds…). */
export function localId(prefix: string): string {
  return `${prefix}-${crypto.randomUUID().slice(0, 8)}`
}

export function nowIso(): string {
  return new Date().toISOString()
}

// ---- client-side attachment registry (base64 inlined at send) ----

const attachmentRegistry = new Map<string, { filename: string; contentType: string; size: number; base64: string }>()

export function registerAttachment(file: File, base64: string): string {
  const id = localId('att')
  attachmentRegistry.set(id, { filename: file.name, contentType: file.type || 'application/octet-stream', size: file.size, base64 })
  return id
}

export function takeAttachments(ids: string[] | undefined): { filename: string; content_type: string; content: string }[] {
  if (!ids?.length) return []
  const out: { filename: string; content_type: string; content: string }[] = []
  for (const id of ids) {
    const a = attachmentRegistry.get(id)
    if (a) {
      out.push({ filename: a.filename, content_type: a.contentType, content: a.base64 })
      attachmentRegistry.delete(id) // sent — do not resend on a retry of a NEW send
    }
  }
  return out
}

// ---- backend raw shapes (mirrors internal/api views) ----

export interface AddrRaw { name?: string; email: string }
export interface ItemRaw { id: string; folder_id: string; message_id: string; uid: number; flags: string[]; created_at: string }
export interface MessageRaw {
  id: string; thread_id: string | null; direction: 'inbound' | 'outbound' | 'internal';
  from: { name: string; email: string }; to: AddrRaw[]; cc: AddrRaw[];
  subject: string; snippet: string; has_attachments: boolean; size_bytes: number;
  internal_date: string; spam_score: number; tracking_enabled: boolean;
  auth_results: Record<string, unknown> | null;
}
export interface ItemWithMessageRaw { item: ItemRaw; message: MessageRaw }
export interface FolderPageRaw { folder: FolderRaw; items: ItemWithMessageRaw[]; limit: number; has_more: boolean; next_before_uid?: number }
export interface FolderRaw { id: string; name: string; special_use: string; uid_next: number; uid_validity: number; total: number; unread: number }
export interface MailboxRaw { id: string; kind: string; name: string; send_status: string; owner: boolean }
export interface AttachmentRaw { id: string; filename: string; content_type: string; size_bytes: number; is_inline: boolean; content_id: string; download_path: string }
export interface MeRaw { user: UserViewRaw; org: OrgRaw; impersonated: boolean; impersonated_by?: string | null; mailboxes: MailboxRaw[] }
export interface UserViewRaw { id: string; org_id: string; email: string; display_name: string; role: string; status: string; is_staff: boolean; created_at: string }
export interface OrgRaw { id: string; name: string; status: string; tier: string; sending_enabled: boolean; review_status: string; created_at: string }
export interface SessionRaw { id: string; kind: string; ip: string; user_agent: string; created_at: string; expires_at: string; current: boolean }
export interface AddressRaw { id: string; address: string; mailbox_id?: string; kind: string; is_primary?: boolean; allow_send_as?: boolean; forward_target?: string; created_at?: string }
export interface DomainRaw { id: string; name: string; status: string; is_platform: boolean; dkim_selector: string; verified_at: string | null }
export interface SignatureRaw { id: string; name: string; html: string; text: string; is_default: boolean; created_at: string }
export interface AutoresponderRaw { enabled: boolean; subject: string; body: string; starts_at: string | null; ends_at: string | null; updated_at?: string }
export interface RuleRaw { id: string; position: number; enabled: boolean; match_from: string; match_subject: string; action_folder_id: string | null; action_mark_read: boolean; created_at: string }
export interface QuarantineRaw { id: string; reason: string; score: number; subject: string; snippet: string; expires_at: string; created_at: string }
export interface SuppressionRaw { email: string; reason: string; created_at: string }
export interface DeliverabilityRaw {
  sent: number; delivered: number; bounced: number; soft_expired: number; deferred: number;
  suppressed: number; delivery_rate: number;
  by_domain: { domain?: string; sent?: number; delivered?: number; bounced?: number }[];
  window_days: number;
}
export interface HealthRaw { score: number; delivery_rate: number; hard_bounce_rate: number; sent: number; suppressed: number; window_days: number }
export interface ReliabilityRaw { days: { day: string; queued: number; delivered: number; bounced: number; deferred: number }[]; window_days: number }
export interface TraceRaw { message_id: string; events: { state: string; occurred_at: string; detail: Record<string, unknown> | null }[] }
export interface StorageRaw { used_bytes: number; included_bytes: number; grace_bytes: number; used_percent: number; full: boolean; beyond_grace: boolean; never_autodelete: boolean }
export interface SendRespRaw { message_id: string; recipients: unknown }

// ---- mappers: backend raw → frontend view-models ----

function specialToRole(specialUse: string): Folder['role'] {
  const su = specialUse.replace(/\\/g, '').toLowerCase()
  if (su.includes('inbox')) return 'inbox'
  if (su.includes('sent')) return 'sent'
  if (su.includes('draft')) return 'drafts'
  if (su.includes('trash')) return 'trash'
  if (su.includes('spam') || su.includes('junk')) return 'spam'
  if (su.includes('archive')) return 'archive'
  return null
}

export function mapMailbox(m: MailboxRaw): import('./types').Mailbox {
  return {
    id: m.id,
    address: m.name,
    display_name: m.name,
    quota_bytes: 0,
    used_bytes: 0,
    suspended: m.send_status !== 'active',
    bounce_breaker_paused: false,
    shared: m.kind === 'shared',
    kind: m.kind === 'shared' ? 'shared' : m.kind === 'role' ? 'role' : 'personal',
  }
}

export function mapFolder(f: FolderRaw, index: number, mailboxId: string): Folder {
  return {
    id: f.id,
    mailbox_id: mailboxId,
    name: f.name,
    role: specialToRole(f.special_use),
    unread: f.unread,
    total: f.total,
    sort_order: index,
  }
}

function flagsFromItem(item: ItemRaw): MessageFlag[] {
  const out: MessageFlag[] = []
  for (const f of item.flags ?? []) {
    const l = f.toLowerCase()
    if (l.includes('seen')) out.push('seen')
    else if (l.includes('flag')) out.push('flagged')
    else if (l.includes('answer')) out.push('answered')
    else if (l.includes('forward')) out.push('forwarded')
    else if (l.includes('draft')) out.push('draft')
  }
  return out
}

function authResultsFrom(raw: Record<string, unknown> | null): AuthResults {
  const pick = (k: string): string => {
    const v = raw?.[k]
    if (typeof v === 'string') return v.toLowerCase()
    if (v && typeof v === 'object') {
      const r = (v as Record<string, unknown>).result ?? (v as Record<string, unknown>).status
      if (typeof r === 'string') return r.toLowerCase()
    }
    return 'none'
  }
  const allowed = ['pass', 'fail', 'softfail', 'neutral', 'none', 'temperror', 'permerror'] as const
  const norm = (v: string): AuthResults['spf'] =>
    (allowed as readonly string[]).includes(v) ? (v as AuthResults['spf']) : 'none'
  const tlsRaw = raw?.tls
  return {
    spf: norm(pick('spf')),
    dkim: norm(pick('dkim')) as AuthResults['dkim'],
    dmarc: norm(pick('dmarc')) as AuthResults['dmarc'],
    tls: tlsRaw === undefined ? true : Boolean(tlsRaw),
  }
}

export function mapMessageSummary(r: ItemWithMessageRaw, mailboxId: string): MessageSummary {
  const { item, message } = r
  return {
    id: message.id,
    mailbox_id: mailboxId,
    thread_id: message.thread_id ?? `t-${message.id}`,
    folder_id: item.folder_id,
    from: { name: message.from.name || undefined, email: message.from.email },
    to: (message.to ?? []).map((a) => ({ name: a.name || undefined, email: a.email })),
    subject: message.subject,
    snippet: message.snippet,
    received_at: item.created_at,
    is_read: (item.flags ?? []).some((f) => f.toLowerCase().includes('seen')),
    has_attachments: message.has_attachments,
    flags: flagsFromItem(item),
    label_ids: [],
    spam_score: message.spam_score,
    external_sender: message.direction === 'inbound',
    auth_results: authResultsFrom(message.auth_results),
  }
}

export interface MessageDetailRaw { message: MessageRaw; item: ItemRaw; folder: FolderRaw; body_text: string; body_html: string }

export function mapMessageDetail(d: MessageDetailRaw, mailboxId: string): MessageDetail {
  const summary = mapMessageSummary({ item: d.item, message: d.message }, mailboxId)
  return {
    ...summary,
    cc: (d.message.cc ?? []).map((a) => ({ name: a.name || undefined, email: a.email })),
    bcc: [],
    body_text: d.body_text,
    body_html: d.body_html,
    attachments: [], // filled by getMessage() from /attachments
    headers: {},
    in_reply_to: undefined,
    references: undefined,
    received_at: d.message.internal_date,
  }
}

export function mapAttachment(a: AttachmentRaw): Attachment {
  return {
    id: a.id,
    filename: a.filename,
    content_type: a.content_type,
    size: a.size_bytes,
    inline: a.is_inline,
    content_id: a.content_id || undefined,
    scan_status: 'clean',
  }
}

export function mapMe(m: MeRaw): Me {
  const roleMap: Record<string, Me['role']> = { owner: 'owner', admin: 'admin', member: 'employee', staff: 'staff' }
  const statusMap: Record<string, Me['org_status']> = { active: 'approved', suspended: 'suspended', pending: 'pending' }
  return {
    user_id: m.user.id,
    org_id: m.user.org_id,
    email: m.user.email,
    display_name: m.user.display_name,
    role: roleMap[m.user.role] ?? 'employee',
    org_status: statusMap[m.org.status] ?? 'pending',
    totp_enabled: false, // the API does not expose enrolment state on /me yet
    passkeys: 0,
    timezone: 'Africa/Johannesburg',
    locale: 'en-ZA',
    org_name: m.org.name,
  }
}

export function mapSession(s: SessionRaw): Session {
  return {
    id: s.id,
    device_name: s.kind,
    ip: s.ip,
    user_agent: s.user_agent,
    created_at: s.created_at,
    last_seen_at: s.created_at,
    current: s.current,
  }
}

export function mapAlias(a: AddressRaw, mailboxId: string): Alias {
  return {
    id: a.id,
    address: a.address,
    target_mailbox_id: a.mailbox_id ?? mailboxId,
    label: a.kind,
    category: 'other',
    created_at: a.created_at ?? nowIso(),
    message_count: 0,
    disabled: false,
    single_use: false,
    blocked_senders: [],
    forward_to: a.forward_target || undefined,
    notification_enabled: false,
    reply_from_enabled: a.kind === 'alias',
  }
}

export function mapDeliverability(d: DeliverabilityRaw, domain: string): DeliverabilitySummary {
  const rate = d.delivery_rate
  const reputation = rate >= 0.97 ? 'excellent' : rate >= 0.9 ? 'good' : rate >= 0.8 ? 'fair' : 'poor'
  const sent = Math.max(1, d.sent)
  return {
    domain,
    period_days: d.window_days,
    sent: d.sent,
    delivered: d.delivered,
    deferred: d.deferred,
    bounced: d.bounced,
    complained: 0,
    rejected: d.soft_expired,
    success_rate: rate,
    bounce_rate: d.bounced / sent,
    complaint_rate: 0,
    reputation,
    auth: { spf: true, dkim: true, dmarc: false, mta_sts: false, tls_rpt: false, bimi: false },
    blacklists: [],
    sending_limit: { current: d.sent, limit: Math.max(d.sent, 1), resets_at: nowIso() },
  }
}

export function mapHealth(h: HealthRaw): HealthScore {
  const cat = (key: HealthScore['categories'][number]['key'], label: string, score: number, improvements: { title: string; detail: string }[] = []): HealthScore['categories'][number] => ({
    key, label, score: Math.round(score), status: score >= 90 ? 'excellent' : score >= 75 ? 'good' : score >= 50 ? 'fair' : 'poor',
    improvements,
  })
  return {
    score: h.score,
    categories: [
      cat('deliverability', 'Deliverability', h.delivery_rate * 100,
        h.delivery_rate < 0.95 ? [{ title: 'Delivery rate below 95%', detail: 'Review bounces and deferrals in the deliverability funnel.' }] : []),
      cat('reputation', 'Reputation', Math.max(0, (1 - h.hard_bounce_rate) * 100),
        h.hard_bounce_rate > 0.05 ? [{ title: 'Hard bounce rate above 5%', detail: 'Keep the suppression register — bounces from stale lists tank the score.' }] : []),
      cat('domain_auth', 'Domain auth', 60, [{ title: 'DMARC policy not enforced yet', detail: 'Publish a DMARC record and move through none → quarantine → reject.' }]),
    ],
  }
}

export function mapReliabilityAlerts(r: ReliabilityRaw): ReliabilityAlert[] {
  const alerts: ReliabilityAlert[] = []
  for (const d of r.days.slice(-7)) {
    if (d.bounced > 0) {
      alerts.push({
        id: `bnc-${d.day}`,
        kind: 'rejected_message',
        severity: d.bounced > 5 ? 'critical' : 'warning',
        created_at: `${d.day}T00:00:00Z`,
        message: `${d.bounced} bounce${d.bounced > 1 ? 's' : ''} on ${d.day} — check the delivery funnel by recipient domain.`,
      })
    }
    if (d.queued > 0 && d.deferred / Math.max(1, d.queued) > 0.3) {
      alerts.push({
        id: `dfr-${d.day}`,
        kind: 'queue_growing',
        severity: 'warning',
        created_at: `${d.day}T00:00:00Z`,
        message: `Heavy deferral on ${d.day}: ${d.deferred} of ${d.queued} queued jobs were still in flight.`,
      })
    }
  }
  return alerts
}

export function mapStorage(s: StorageRaw): StorageUsage {
  return {
    used_bytes: s.used_bytes,
    quota_bytes: s.included_bytes + s.grace_bytes,
    by_folder: [],
    largest_messages: [],
  }
}

export function mapDomain(d: DomainRaw): Domain {
  return {
    id: d.id,
    name: d.name,
    status: d.status === 'active' ? 'verified' : d.status === 'pending' ? 'pending' : d.status === 'failed' ? 'failed' : 'verifying',
    ownership_token: '',
    registered_through_us: false,
    nameservers: [],
    registrar: 'external',
    auto_renew: false,
    transfer_lock: false,
    whois_privacy: false,
    managed: 'partial',
    dns_zone_ok: d.status === 'active',
    verification_emails: [],
  }
}

export function mapOrgSummary(o: OrgRaw & { mailbox_count?: number }): OrgSummary {
  return {
    id: o.id,
    name: o.name,
    primary_domain: '',
    status: o.status === 'active' ? 'approved' : (o.status as OrgSummary['status']),
    created_at: o.created_at,
    mailbox_count: o.mailbox_count ?? 0,
  }
}

export function mapQuarantine(q: QuarantineRaw): QuarantineEntry {
  return {
    id: q.id,
    mailbox_id: '',
    from: { email: q.snippet.slice(0, 0) || 'unknown@unknown' },
    subject: q.subject,
    received_at: q.created_at,
    reason: q.reason,
    score: q.score,
  }
}

export function mapSuppression(s: SuppressionRaw): SuppressionEntry {
  return { address: s.email, reason: s.reason, source: 'auto', created_at: s.created_at }
}

export function mapAppPassword(p: { id: string; name: string; prefix: string; created_at?: string; last_used_at?: string | null }): AppPassword {
  return {
    id: p.id,
    label: p.name,
    scopes: ['imap', 'smtp'],
    created_at: p.created_at ?? nowIso(),
    last_used_at: p.last_used_at ?? undefined,
    revoked: false,
  }
}
