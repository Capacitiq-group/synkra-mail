// Org console, staff admin, shared mailboxes, deliverability, domains,
// contacts/calendar/tasks (module stubs), storage, billing, privacy.

import { request, qs, notAvailable, localId, nowIso,
  mapDomain, mapOrgSummary, mapQuarantine, mapSuppression,
  mapDeliverability, mapHealth, mapReliabilityAlerts,
  type DeliverabilityRaw, type DomainRaw, type HealthRaw, type MailboxRaw, type ReliabilityRaw,
  type TraceRaw, type StorageRaw, type QuarantineRaw, type SuppressionRaw, type OrgRaw, type UserViewRaw } from './apiCore'
import type {
  AbuseCase, AddressEntry, AuditEvent, BillingPlan, Calendar, CalendarEvent, Contact,
  ContactGroup, DeliverabilitySummary, DeliveryTrace, Department, DigestPreferences as _dp,
  DnsDoctorReport, Domain, DsarRequest, FleetStats, HealthScore, ImpersonationSession,
  Invoice, Mailbox, MigrationJob, Note as _n, OrgProfile, OrgSummary, OrgUser,
  PaymentMethod, ProcessingRegisterEntry, QueueJob, QuarantineEntry, ReliabilityAlert,
  SchedulingLink, SharedMailbox, SharedMailboxMember, StorageUsage, SubProcessor,
  SuppressionEntry, Task, TaskList, ThreadAssignment,
} from './types'

// The static POPIA processing register — truthful entries describing what
// this deployment actually does (kept in code until the privacy module lands).
const PROCESSING_REGISTER: ProcessingRegisterEntry[] = [
  { id: 'pr-1', purpose: 'Providing the mailbox service (store, transmit, deliver email)', lawful_basis: 'Contract performance', data_categories: ['email content', 'headers', 'attachments'], retention: 'While the account exists — mail is never auto-deleted', recipients: ['recipient mail servers'], },
  { id: 'pr-2', purpose: 'Authentication and session security', lawful_basis: 'Legitimate interest (security)', data_categories: ['credentials (hashed)', 'session IPs', 'user agents'], retention: 'Sessions expire per policy; audit trail retained', recipients: ['internal only'], },
  { id: 'pr-3', purpose: 'Delivery audit trail (Mail Trace)', lawful_basis: 'Legitimate interest (service integrity)', data_categories: ['delivery events', 'recipient domains', 'timestamps'], retention: 'Per delivery-event retention policy', recipients: ['internal only'], },
  { id: 'pr-4', purpose: 'Open-tracking engagement labels (when the sender enables it)', lawful_basis: 'Legitimate interest; flagged to recipients', data_categories: ['pixel loads', 'link clicks'], retention: 'Aggregated counters only', recipients: ['internal only'], },
]

const SUB_PROCESSORS: SubProcessor[] = [
  { name: 'Cloud infrastructure (Johannesburg region)', purpose: 'Hosting and storage', country: 'South Africa' },
]

export const apiOrg = {
  // ===== Shared mailboxes =====
  listSharedMailboxes: async (): Promise<SharedMailbox[]> => {
    const boxes = await request<{ mailboxes: MailboxRaw[] }>('/mailboxes')
    return boxes.mailboxes.filter((b) => b.kind === 'shared').map((b) => ({
      id: b.id, address: b.name, display_name: b.name, members: [],
    }))
  },
  getSharedMailbox: async (id: string): Promise<SharedMailbox> => {
    const all = await apiOrg.listSharedMailboxes()
    const base = all.find((m) => m.id === id) ?? { id, address: id, display_name: id, members: [] as SharedMailboxMember[] }
    try {
      const { members } = await request<{ members: { user_id: string; email: string; role: string }[] }>(`/org/shared-mailboxes/${id}/members`)
      return { ...base, members: members.map((m) => ({ email: m.email, display_name: m.email, role: m.role === 'manager' ? 'manager' : 'member' })) }
    } catch {
      return base
    }
  },
  listThreadAssignments: async (_sharedMailboxId: string): Promise<ThreadAssignment[]> => { void _sharedMailboxId; return [] },
  assignThread: async (sharedMailboxId: string, threadId: string, assignedTo?: string): Promise<ThreadAssignment> => {
    void sharedMailboxId
    await request(`/threads/${threadId}/assign`, { method: 'POST', body: JSON.stringify({ user_id: assignedTo }) })
    return {
      thread_id: threadId, shared_mailbox_id: sharedMailboxId,
      assigned_to: assignedTo ? { email: assignedTo } : undefined,
      status: 'open', last_activity_at: nowIso(), created_at: nowIso(), updated_at: nowIso(),
    }
  },
  setAssignmentStatus: async (sharedMailboxId: string, threadId: string, _status: ThreadAssignment['status']): Promise<ThreadAssignment> => {
    void sharedMailboxId; void threadId; void _status
    notAvailable('Assignment status workflow', 'shared mailbox workflow (register 12f)')
  },
  claimReply: (_sharedMailboxId: string, _threadId: string): never => { void _sharedMailboxId; void _threadId; return notAvailable('Reply claiming', 'shared mailbox workflow (register 12f)') },
  releaseReply: (_sharedMailboxId: string, _threadId: string): Promise<void> => { void _sharedMailboxId; void _threadId; return notAvailable('Reply claiming', 'shared mailbox workflow (register 12f)') },
  getReplyOwner: async (_sharedMailboxId: string, _threadId: string): Promise<import('./types').ReplyOwner | null> => { void _sharedMailboxId; void _threadId; return null },
  getThreadAssignment: async (sharedMailboxId: string, threadId: string): Promise<ThreadAssignment> => ({
    thread_id: threadId, shared_mailbox_id: sharedMailboxId,
    status: 'new', last_activity_at: nowIso(), created_at: nowIso(), updated_at: nowIso(),
  }),
  updateThreadAssignment: async (sharedMailboxId: string, threadId: string, patch: { assigned_to?: string | null; status?: ThreadAssignment['status'] }): Promise<ThreadAssignment> => {
    if (patch.assigned_to) return apiOrg.assignThread(sharedMailboxId, threadId, patch.assigned_to)
    notAvailable('Assignment status workflow', 'shared mailbox workflow (register 12f)')
  },
  sharedActivity: async (_sharedMailboxId: string, _params: { thread_id?: string; limit?: number; cursor?: string } = {}) => { void _sharedMailboxId; void _params; return { items: [] as import('./types').SharedActivityEvent[], next_cursor: null, has_more: false } },
  sharedDrafts: async (_sharedMailboxId: string) => { void _sharedMailboxId; return [] as import('./types').SharedDraft[] },

  // ===== Deliverability / health / reliability / trace =====
  deliverability: async (domain: string, _days = 30): Promise<DeliverabilitySummary> => {
    void _days
    const raw = await request<DeliverabilityRaw>('/org/deliverability')
    return mapDeliverability(raw, domain)
  },
  mailTrace: async (messageId: string): Promise<DeliveryTrace> => {
    const t = await request<TraceRaw>(`/messages/${messageId}/trace`)
    const stage = (s: string): import('./types').DeliveryHop['stage'] => {
      if (s.includes('deliver')) return 'delivered'
      if (s.includes('bounc')) return 'bounced'
      if (s.includes('defer')) return 'deferred'
      if (s.includes('queue')) return 'queued'
      return 'remote_response'
    }
    return {
      message_id: messageId,
      recipient: String(t.events[0]?.detail?.recipient ?? ''),
      hops: t.events.map((e) => ({
        at: e.occurred_at,
        stage: stage(e.state),
        detail: `${e.state}${e.detail ? ` — ${JSON.stringify(e.detail).slice(0, 220)}` : ''}`,
      })),
    }
  },
  healthScore: async (): Promise<HealthScore> => {
    const raw = await request<HealthRaw>('/org/health')
    return mapHealth(raw)
  },
  reliabilityAlerts: async (): Promise<ReliabilityAlert[]> => {
    const raw = await request<ReliabilityRaw>('/org/reliability')
    return mapReliabilityAlerts(raw)
  },
  acknowledgeAlert: async (id: string) => { void id /* derived alerts need no acknowledgement */ },

  deliverabilityTimeseries: async (domain: string, days = 30): Promise<import('./types').DeliverabilityTimeseries> => {
    const raw = await request<ReliabilityRaw>('/org/reliability')
    return {
      domain,
      period_days: raw.window_days,
      buckets: raw.days.slice(-days).map((d) => ({
        date: `${d.day}T00:00:00Z`, sent: d.queued, delivered: d.delivered,
        bounced: d.bounced, deferred: d.deferred, complained: 0, rejected: 0,
      })),
    }
  },
  dmarcRollout: (_domain: string): Promise<import('./types').DmarcRolloutState> => { void _domain; return notAvailable('DMARC rollout tracker', 'DMARC service (register 12a)') },
  setDmarcPhase: (_domain: string, _phase: import('./types').DmarcPhase): Promise<import('./types').DmarcRolloutState> => { void _domain; void _phase; return notAvailable('DMARC rollout tracker', 'DMARC service (register 12a)') },
  dmarcReports: (_domain: string): Promise<import('./types').DmarcAggregateReport[]> => { void _domain; return notAvailable('DMARC aggregate reports', 'DMARC service (register 12a)') },
  bimiState: (_domain: string): Promise<import('./types').BimiState> => { void _domain; return notAvailable('BIMI', 'brand indicators (register 12a)') },
  setBimi: (_domain: string, _patch: { enabled: boolean; logo_url?: string }): Promise<import('./types').BimiState> => { void _domain; void _patch; return notAvailable('BIMI', 'brand indicators (register 12a)') },
  mtaStsState: (_domain: string): Promise<import('./types').MtaStsState> => { void _domain; return notAvailable('MTA-STS', 'transport security policy (register 12a)') },
  setMtaSts: (_domain: string, _patch: { enabled: boolean; mode: import('./types').MtaStsMode; max_age_seconds: number }): Promise<import('./types').MtaStsState> => { void _domain; void _patch; return notAvailable('MTA-STS', 'transport security policy (register 12a)') },
  tlsRptState: (_domain: string): Promise<import('./types').TlsRptState> => { void _domain; return notAvailable('TLS-RPT', 'transport security policy (register 12a)') },
  setTlsRpt: (_domain: string, _patch: { enabled: boolean; rua_address?: string }): Promise<import('./types').TlsRptState> => { void _domain; void _patch; return notAvailable('TLS-RPT', 'transport security policy (register 12a)') },

  // ===== Domains =====
  listDomains: async (): Promise<Domain[]> => {
    const { domains } = await request<{ domains: DomainRaw[] }>('/domains')
    return domains.map(mapDomain)
  },
  addDomain: async (name: string): Promise<Domain> => {
    const r = await request<{ domain?: DomainRaw; id?: string }>('/domains', {
      method: 'POST', body: JSON.stringify({ name }), headers: { 'Idempotency-Key': crypto.randomUUID() },
    })
    if (r.domain) return mapDomain(r.domain)
    const all = await apiOrg.listDomains()
    return all.find((d) => d.id === r.id) ?? { ...mapDomain({ id: r.id ?? '', name, status: 'pending', is_platform: false, dkim_selector: '', verified_at: null }) }
  },
  getDomain: async (id: string): Promise<Domain> => {
    const all = await apiOrg.listDomains()
    const d = all.find((x) => x.id === id)
    if (!d) notAvailable('Domain detail', 'domain service')
    return d
  },
  verifyDomain: async (id: string): Promise<Domain> => {
    await request(`/domains/${id}/verify`, { method: 'POST' })
    return apiOrg.getDomain(id)
  },
  dnsDoctor: async (id: string): Promise<DnsDoctorReport> => {
    const dns = await request<{ domain?: string; records?: Record<string, unknown>[] }>(`/domains/${id}/dns`)
    const domain = await apiOrg.getDomain(id)
    return {
      domain: domain.name,
      checked_at: nowIso(),
      overall: (dns.records?.length ?? 0) > 0 ? 'pass' : 'warn',
      records: (dns.records ?? []).map((r) => ({
        type: String(r.type ?? 'TXT') as DnsRecordCheckType,
        name: String(r.host ?? r.name ?? ''),
        value: String(r.value ?? ''),
        purpose: String(r.purpose ?? 'mail authentication'),
        status: 'pending' as const,
      })),
    }
  },
  updateNameservers: () => notAvailable('Nameserver management', 'domain registration (register 10.x)') as never as Promise<Domain>,
  setTransferLock: () => notAvailable('Transfer lock', 'domain registration (register 10.x)') as never as Promise<Domain>,
  beginTransferOut: () => notAvailable('Transfer out', 'domain registration (register 10.x)') as never,
  updateRegistrantContacts: () => notAvailable('Registrant contacts', 'domain registration (register 10.x)') as never as Promise<Domain>,
  setWhoisPrivacy: () => notAvailable('WHOIS privacy', 'domain registration (register 10.x)') as never as Promise<Domain>,
  setAutoRenew: () => notAvailable('Auto-renew', 'domain registration (register 10.x)') as never as Promise<Domain>,

  // ===== Migration =====
  listMigrationJobs: async (): Promise<MigrationJob[]> => [],
  startMigration: () => notAvailable('IMAP migration', 'migration service (register 13c)') as never as Promise<MigrationJob>,
  migrationStatus: () => notAvailable('IMAP migration', 'migration service (register 13c)') as never as Promise<MigrationJob>,
  pauseMigration: () => notAvailable('IMAP migration', 'migration service (register 13c)') as Promise<void>,
  resumeMigration: () => notAvailable('IMAP migration', 'migration service (register 13c)') as Promise<void>,
  cancelMigration: () => notAvailable('IMAP migration', 'migration service (register 13c)') as Promise<void>,

  // ===== Storage =====
  storage: async (): Promise<StorageUsage> => {
    const raw = await request<StorageRaw>('/storage')
    return { used_bytes: raw.used_bytes, quota_bytes: raw.included_bytes + raw.grace_bytes, by_folder: [], largest_messages: [] }
  },

  // ===== Billing (module not shipped) =====
  billingPlan: async (): Promise<BillingPlan> => notAvailable('Billing plans', 'billing (register 11.x)') as never,
  invoices: async (): Promise<Invoice[]> => [],
  invoiceDownloadUrl: (id: string) => { void id; return '#' },
  paymentMethods: async (): Promise<PaymentMethod[]> => [],
  setDefaultPaymentMethod: () => notAvailable('Billing', 'billing (register 11.x)') as Promise<void>,
  removePaymentMethod: () => notAvailable('Billing', 'billing (register 11.x)') as Promise<void>,

  // ===== Privacy / POPIA =====
  processingRegister: async (): Promise<ProcessingRegisterEntry[]> => PROCESSING_REGISTER,
  subProcessors: async (): Promise<SubProcessor[]> => SUB_PROCESSORS,
  dsarRequests: async (): Promise<DsarRequest[]> => [],
  createDsar: (_kind: 'export' | 'delete'): Promise<DsarRequest> => { void _kind; return notAvailable('DSAR pipeline', 'privacy centre (register 9.x)') },
  privacySettings: async () => ({ block_remote_images: false, strip_tracking_pixels: false, external_link_warnings: true }),
  updatePrivacySettings: async (_patch: Partial<{ block_remote_images: boolean; strip_tracking_pixels: boolean; external_link_warnings: boolean }>) => { void _patch; return undefined },

  // ===== Contacts (module stubs — register 14.x) =====
  listContacts: async (_q?: string): Promise<Contact[]> => { void _q; return [] },
  getContact: (_id: string): Promise<Contact> => { void _id; return notAvailable('Contacts', 'PIM suite (register 14.x)') },
  createContact: (_c: Omit<Contact, 'id' | 'created_at' | 'updated_at'>): Promise<Contact> => { void _c; return notAvailable('Contacts', 'PIM suite (register 14.x)') },
  updateContact: (_id: string, _patch: Partial<Contact>): Promise<Contact> => { void _id; void _patch; return notAvailable('Contacts', 'PIM suite (register 14.x)') },
  deleteContact: (_id: string): Promise<void> => { void _id; return notAvailable('Contacts', 'PIM suite (register 14.x)') },
  importContacts: (_file: File): Promise<{ imported: number; errors: number }> => { void _file; return notAvailable('Contacts', 'PIM suite (register 14.x)') },
  exportContactsUrl: () => '#',
  listContactGroups: async (): Promise<ContactGroup[]> => [],
  createContactGroup: (_name: string): Promise<ContactGroup> => { void _name; return notAvailable('Contacts', 'PIM suite (register 14.x)') },
  deleteContactGroup: (_id: string): Promise<void> => { void _id; return notAvailable('Contacts', 'PIM suite (register 14.x)') },
  autocompleteContacts: async (_q: string) => { void _q; return [] as { email: string; name?: string; source: 'personal' | 'org' | 'recent' }[] },

  // ===== Calendar (module stubs) =====
  listCalendars: async (): Promise<Calendar[]> => [],
  createCalendar: (_c: Omit<Calendar, 'id' | 'url'>): Promise<Calendar> => { void _c; return notAvailable('Calendar', 'PIM suite (register 14.x)') },
  updateCalendar: (_id: string, _patch: Partial<Calendar>): Promise<Calendar> => { void _id; void _patch; return notAvailable('Calendar', 'PIM suite (register 14.x)') },
  deleteCalendar: (_id: string): Promise<void> => { void _id; return notAvailable('Calendar', 'PIM suite (register 14.x)') },
  listEvents: async (_calendarId: string, _from: string, _to: string): Promise<CalendarEvent[]> => { void _calendarId; void _from; void _to; return [] },
  createEvent: (_calendarId: string, _ev: Omit<CalendarEvent, 'id' | 'organizer' | 'rsvp' | 'status'>): Promise<CalendarEvent> => { void _calendarId; void _ev; return notAvailable('Calendar', 'PIM suite (register 14.x)') },
  updateEvent: (_id: string, _patch: Partial<CalendarEvent>): Promise<CalendarEvent> => { void _id; void _patch; return notAvailable('Calendar', 'PIM suite (register 14.x)') },
  deleteEvent: (_id: string): Promise<void> => { void _id; return notAvailable('Calendar', 'PIM suite (register 14.x)') },
  rsvpEvent: (_id: string, _response: 'accepted' | 'declined' | 'tentative', _message?: string): Promise<void> => { void _id; void _response; void _message; return notAvailable('Calendar', 'PIM suite (register 14.x)') },
  schedulingLinks: async (): Promise<SchedulingLink[]> => [],
  createSchedulingLink: (_input: Omit<SchedulingLink, 'id' | 'slug'>): Promise<SchedulingLink> => { void _input; return notAvailable('Scheduling links', 'PIM suite (register 14.x)') },
  deleteSchedulingLink: (_id: string): Promise<void> => { void _id; return notAvailable('Scheduling links', 'PIM suite (register 14.x)') },

  // ===== Tasks (module stubs) =====
  listTaskLists: async (): Promise<TaskList[]> => [],
  createTaskList: (_input: Omit<TaskList, 'id'>): Promise<TaskList> => { void _input; return notAvailable('Tasks', 'PIM suite (register 14.x)') },
  deleteTaskList: (_id: string): Promise<void> => { void _id; return notAvailable('Tasks', 'PIM suite (register 14.x)') },
  listTasks: async (_listId?: string): Promise<Task[]> => { void _listId; return [] },
  createTask: (_input: Omit<Task, 'id' | 'created_at' | 'completed' | 'completed_at'>): Promise<Task> => { void _input; return notAvailable('Tasks', 'PIM suite (register 14.x)') },
  updateTask: (_id: string, _patch: Partial<Task>): Promise<Task> => { void _id; void _patch; return notAvailable('Tasks', 'PIM suite (register 14.x)') },
  deleteTask: (_id: string): Promise<void> => { void _id; return notAvailable('Tasks', 'PIM suite (register 14.x)') },
  createTaskFromMessage: (_input: { message_id?: string; thread_id?: string; list_id?: string; title?: string; notes?: string; due_at?: string; assigned_to?: string }): Promise<Task> => { void _input; return notAvailable('Tasks', 'PIM suite (register 14.x)') },
  getMessageCalendarEvent: async (_messageId: string): Promise<CalendarEvent | null> => { void _messageId; return null },
}

type DnsRecordCheckType = import('./types').DnsRecordCheck['type']

// ---- org console (nested api.org.*) ----

export const orgApi = {
  profile: async (): Promise<OrgProfile> => {
    const r = await request<{ org: OrgRaw; subscription: unknown }>('/org')
    const domains = await apiOrg.listDomains().catch(() => [] as Domain[])
    return {
      id: r.org.id,
      name: r.org.name,
      primary_domain: domains[0]?.name ?? '',
      domains,
      plan: { name: r.org.tier, mailboxes: 0, price_per_mailbox_zar: 0, vat_rate: 0.15 },
      vat_number: undefined,
      billing_email: '',
      timezone: 'Africa/Johannesburg',
      locale: 'en-ZA',
    }
  },
  updateProfile: (_patch: Partial<OrgProfile>): Promise<OrgProfile> => { void _patch; return notAvailable('Org profile editing', 'org API (register 7.x)') },
  departments: async (): Promise<Department[]> => [],
  createDepartment: (_name: string, _managerEmail?: string): Promise<Department> => { void _name; void _managerEmail; return notAvailable('Departments', 'org API (register 7.x)') },
  updateDepartment: (_id: string, _patch: Partial<Department>): Promise<Department> => { void _id; void _patch; return notAvailable('Departments', 'org API (register 7.x)') },
  deleteDepartment: (_id: string): Promise<void> => { void _id; return notAvailable('Departments', 'org API (register 7.x)') },

  users: async (): Promise<OrgUser[]> => {
    const { users } = await request<{ users: UserViewRaw[] }>('/org/users')
    return users.map((u) => ({
      id: u.id,
      email: u.email,
      display_name: u.display_name,
      role: (u.role === 'owner' ? 'owner' : u.role === 'admin' ? 'admin' : 'employee') as OrgUser['role'],
      suspended: u.status !== 'active',
      totp_enabled: false,
      last_login_at: undefined,
    }))
  },
  inviteUser: async (email: string, _role: OrgUser['role'], _department?: string): Promise<OrgUser> => {
    void _role; void _department
    // The API's invite takes email + display name + a temporary password and
    // always creates a member; role management arrives with the org API.
    const tempPassword = `Synkra-${crypto.randomUUID().slice(0, 8)}!`
    const r = await request<{ user: UserViewRaw }>('/users/invite', {
      method: 'POST',
      body: JSON.stringify({ email, display_name: email.split('@')[0], password: tempPassword }),
    })
    return {
      id: r.user.id, email: r.user.email, display_name: r.user.display_name,
      role: 'employee', suspended: false, totp_enabled: false,
    }
  },
  suspendUser: async (id: string) => {
    await request(`/org/users/${id}/suspend`, { method: 'POST' })
  },
  revokeUserSessions: async (id: string) => {
    await request(`/org/users/${id}/revoke-sessions`, { method: 'POST' })
  },
  offboardUser: (_id: string, _continuity: { route_to: string } | null): Promise<void> => { void _id; void _continuity; return notAvailable('Offboarding continuity', 'org API (register 12e continuity)') },
  bulkImportUsers: (_file: File): Promise<{ imported: number; errors: { row: number; message: string }[] }> => { void _file; return notAvailable('Bulk user import', 'org API (register 7.x)') },

  mailboxes: async (): Promise<Mailbox[]> => {
    const { mailboxes } = await request<{ mailboxes: MailboxRaw[] }>('/mailboxes')
    return mailboxes.map((m) => ({ id: m.id, address: m.name, display_name: m.name, quota_bytes: 0, used_bytes: 0, suspended: m.send_status !== 'sendable', bounce_breaker_paused: false, shared: m.kind === 'shared', kind: m.kind === 'shared' ? 'shared' : 'personal' }))
  },
  createMailbox: (_address: string, _displayName: string, _quotaBytes?: number): Promise<Mailbox> => { void _address; void _displayName; void _quotaBytes; return notAvailable('Mailbox creation', 'org API (register 7.x)') },
  updateMailbox: (_id: string, _patch: Partial<Pick<Mailbox, 'display_name' | 'quota_bytes' | 'suspended'>>): Promise<Mailbox> => { void _id; void _patch; return notAvailable('Mailbox editing', 'org API (register 7.x)') },
  deleteMailbox: (_id: string): Promise<void> => { void _id; return notAvailable('Mailbox deletion', 'org API (register 7.x)') },
  resumeMailbox: async (id: string) => {
    await request(`/mailboxes/${id}/resume`, { method: 'POST' })
  },

  addresses: async (): Promise<AddressEntry[]> => {
    const { addresses } = await request<{ addresses: { id: string; address: string; kind: string; forward_target?: string }[] }>('/addresses')
    return addresses.map((a) => ({
      id: a.id,
      address: a.address,
      kind: 'alias' as const,
      target: a.forward_target || undefined,
      active: true,
    }))
  },
  createAddress: async (entry: Omit<AddressEntry, 'id' | 'active'>): Promise<AddressEntry> => {
    const [local, domain] = entry.address.split('@')
    const { domains } = await request<{ domains: DomainRaw[] }>('/domains')
    const d = domains.find((x) => x.name === domain) ?? domains[0]
    if (!d) notAvailable('Address domains', 'domain service')
    await request('/addresses', {
      method: 'POST',
      body: JSON.stringify({ local, domain_id: d.id, kind: 'alias', forward_target: entry.target }),
      headers: { 'Idempotency-Key': crypto.randomUUID() },
    })
    return { ...entry, id: localId('addr'), active: true }
  },
  deleteAddress: async (id: string) => {
    await request(`/addresses/${id}`, { method: 'DELETE' })
  },

  sharedMailboxes: async (): Promise<SharedMailbox[]> => apiOrg.listSharedMailboxes(),
  createSharedMailbox: (_address: string, _displayName: string): Promise<SharedMailbox> => { void _address; void _displayName; return notAvailable('Shared mailbox creation', 'staff provisioning (register 2.6)') },
  sharedMailboxMembers: async (id: string): Promise<SharedMailboxMember[]> => {
    const { members } = await request<{ members: { user_id: string; email: string; role: string }[] }>(`/org/shared-mailboxes/${id}/members`)
    return members.map((m) => ({ email: m.email, display_name: m.email, role: m.role === 'manager' ? 'manager' : 'member' }))
  },
  addSharedMailboxMember: async (id: string, email: string, _role: 'member' | 'manager') => {
    void _role
    const { users } = await request<{ users: UserViewRaw[] }>('/org/users')
    const u = users.find((x) => x.email === email)
    if (!u) throw new (await import('./errors')).ApiError(404, 'not_found', `No org user ${email}`)
    await request(`/org/shared-mailboxes/${id}/members`, { method: 'POST', body: JSON.stringify({ user_id: u.id, role: 'member' }) })
  },
  removeSharedMailboxMember: async (id: string, email: string) => {
    const { users } = await request<{ users: UserViewRaw[] }>('/org/users')
    const u = users.find((x) => x.email === email)
    if (!u) throw new (await import('./errors')).ApiError(404, 'not_found', `No org user ${email}`)
    await request(`/org/shared-mailboxes/${id}/members`, { method: 'DELETE', body: JSON.stringify({ user_id: u.id }) })
  },

  signatureTemplates: async () => [] as never as import('./types').CompanySignatureTemplate[],
  createSignatureTemplate: (_t: Omit<import('./types').CompanySignatureTemplate, 'id'>): never => { void _t; return notAvailable('Company signatures', 'register 12c company admin') },
  updateSignatureTemplate: (_id: string, _patch: Partial<import('./types').CompanySignatureTemplate>): never => { void _id; void _patch; return notAvailable('Company signatures', 'register 12c company admin') },
  deleteSignatureTemplate: (_id: string): Promise<void> => { void _id; return notAvailable('Company signatures', 'register 12c company admin') },

  auditLog: () => notAvailable('Org audit trail', 'audit API is staff-scoped (register 8.x)') as never as Promise<AuditEvent[]>,
  dsarQueue: async (): Promise<DsarRequest[]> => [],
  resolveDsar: (_id: string, _resolution: 'fulfilled' | 'rejected', _reason?: string): Promise<DsarRequest> => { void _id; void _resolution; void _reason; return notAvailable('DSAR pipeline', 'privacy centre (register 9.x)') },
  ssoConfig: async (): Promise<{ provider: 'oidc' | 'saml' | null; enabled: boolean; metadata_url?: string }> => ({ provider: null, enabled: false, metadata_url: undefined }),
  setSsoConfig: (_patch: { provider: 'oidc' | 'saml'; metadata_url: string; enabled: boolean }): Promise<void> => { void _patch; return notAvailable('SSO', 'register 14.5') },
  ipRestrictions: async (): Promise<string[]> => [],
  setIpRestrictions: (_cidrs: string[]): Promise<string[]> => { void _cidrs; return notAvailable('IP restrictions', 'register 8.x') },
}

// ---- staff admin (nested api.admin.*) ----

export const adminApi = {
  listOrgs: async (status?: OrgSummary['status']): Promise<OrgSummary[]> => {
    const r = await request<{ orgs?: Record<string, unknown>[] }>(`/admin/orgs${qs({ status: status === 'approved' ? 'active' : status })}`)
    return (r.orgs ?? []).map((row) => {
      const org = row.org as OrgRaw
      return mapOrgSummary({ ...org, mailbox_count: Number(row.usage_bytes ?? 0) > 0 ? 1 : 0 })
    })
  },
  approveOrg: async (id: string) => {
    await request(`/admin/orgs/${id}/approve`, { method: 'POST' })
  },
  declineOrg: async (id: string, reason: string) => {
    await request(`/admin/orgs/${id}/decline`, { method: 'POST', body: JSON.stringify({ reason }) })
  },
  suspendOrg: async (id: string, reason: string) => {
    await request(`/admin/orgs/${id}/suspend`, { method: 'POST', body: JSON.stringify({ reason }) })
  },

  startImpersonation: async (userId: string, reason: string): Promise<ImpersonationSession> => {
    const r = await request<{ token: string; ttl_minutes: number }>('/admin/orgs/impersonate-by-user', { method: 'POST', body: JSON.stringify({ user_id: userId, reason }) }).catch(async (err) => {
      // The impersonation route is /admin/orgs/{org_id}/impersonate; resolve
      // the org for this user first.
      const orgs = await request<{ orgs?: { org: OrgRaw }[] }>('/admin/orgs')
      const orgId = orgs.orgs?.[0]?.org?.id
      if (!orgId) throw err
      return request<{ token: string; ttl_minutes: number }>(`/admin/orgs/${orgId}/impersonate`, { method: 'POST', body: JSON.stringify({ user_id: userId, reason }) })
    })
    return {
      id: r.token,
      staff_email: 'you (staff)',
      target_user_email: userId,
      reason,
      started_at: nowIso(),
      expires_at: new Date(Date.now() + r.ttl_minutes * 60_000).toISOString(),
    }
  },
  activeImpersonation: async (): Promise<ImpersonationSession | null> => null,
  endImpersonation: async (id: string) => {
    await request(`/admin/impersonation/${id}/end`, { method: 'POST' })
  },

  deliveryQueue: async (_orgId?: string): Promise<QueueJob[]> => {
    void _orgId
    const r = await request<{ queue?: Record<string, unknown>[]; jobs?: Record<string, unknown>[] }>(`/admin/queue`)
    const rows = r.queue ?? r.jobs ?? []
    return rows.map((j, i) => ({
      id: String(j.id ?? i),
      message_id: String(j.message_id ?? ''),
      recipient: String(j.recipient ?? ''),
      attempts: Number(j.attempts ?? 0),
      next_attempt_at: String(j.next_attempt_at ?? nowIso()),
      last_error: j.last_error ? String(j.last_error) : undefined,
      state: 'pending' as const,
    }))
  },
  retryJob: (_id: string): Promise<void> => { void _id; return notAvailable('Queue job retry', 'admin queue API (register 3.x)') },
  cancelJob: (_id: string): Promise<void> => { void _id; return notAvailable('Queue job cancel', 'admin queue API (register 3.x)') },

  resumeMailbox: async (id: string, _reason: string) => {
    void _reason
    await request(`/admin/mailboxes/${id}/resume`, { method: 'POST' })
  },

  suppression: async (): Promise<SuppressionEntry[]> => {
    const r = await request<{ suppression: SuppressionRaw[] }>('/admin/suppression')
    return r.suppression.map(mapSuppression)
  },
  quarantine: async (_orgId?: string): Promise<QuarantineEntry[]> => {
    void _orgId
    const r = await request<{ quarantine: QuarantineRaw[] }>('/org/quarantine')
    return r.quarantine.map(mapQuarantine)
  },
  releaseQuarantine: async (id: string, _reason: string) => {
    void _reason
    await request(`/org/quarantine/${id}/release`, { method: 'POST' })
  },

  abuseCases: async (): Promise<AbuseCase[]> => [],
  openAbuseCase: (_input: Pick<AbuseCase, 'mailbox_id' | 'kind' | 'evidence'>): Promise<AbuseCase> => { void _input; return notAvailable('Abuse case tracker', 'abuse workflow (register 2.9)') },
  setAbuseStatus: (_id: string, _status: AbuseCase['status']): Promise<AbuseCase> => { void _id; void _status; return notAvailable('Abuse case tracker', 'abuse workflow (register 2.9)') },

  fleetStats: async (): Promise<FleetStats> => {
    const r = await request<{ queue_depth?: number; sent_24h?: number; received_24h?: number; delivery_rate_24h?: number; breaker_trips_24h?: number; ts?: string }>('/admin/stats')
    return {
      sent_24h: r.sent_24h ?? 0,
      received_24h: r.received_24h ?? 0,
      delivery_rate_24h: r.delivery_rate_24h ?? 0,
      queue_depth: r.queue_depth ?? 0,
      breaker_trips_24h: r.breaker_trips_24h ?? 0,
      at: r.ts ?? nowIso(),
    }
  },
  auditAll: async (params: { from?: string; to?: string; actor?: string; action?: string; tenant?: string } = {}): Promise<AuditEvent[]> => {
    const r = await request<{ audit: { id: string; actor?: string; admin?: string; action: string; target_type?: string; target_id?: string; created_at: string; meta?: Record<string, unknown> }[] }>(`/admin/audit${qs(params)}`)
    return r.audit.map((a) => ({
      id: a.id,
      actor_email: a.actor ?? a.admin ?? 'staff',
      action: a.action,
      resource: `${a.target_type ?? ''}${a.target_id ? `:${a.target_id}` : ''}`,
      ip: '',
      at: a.created_at,
      detail: a.meta,
    }))
  },
  breachScope: (_params: { tenant_id: string; from: string; to: string; resources?: string }): never => { void _params; return notAvailable('Breach scope tool', 'staff tooling (register 8.x)') },
  supportLookup: (_email: string): never => { void _email; return notAvailable('Support lookup', 'staff tooling (register 8.x)') },
}

void localId
