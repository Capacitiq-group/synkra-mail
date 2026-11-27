// Settings surface: aliases, signatures, OOO (single + multi-period), rules,
// newsletters, notes, security (TOTP / sessions / passkeys / devices /
// app-passwords) — mapped onto the real endpoints.

import { request, notAvailable, localId, nowIso,
  mapAlias, mapAppPassword, mapSession,
  type AddressRaw, type AutoresponderRaw, type DomainRaw, type RuleRaw, type SignatureRaw, type SessionRaw } from './apiCore'
import type {
  Alias, AliasCategory, AppPassword, Device, DigestPreferences, NewsletterSender,
  OutOfOffice, OutOfOfficePeriod, OutOfOfficeState, Passkey, RecoveryCodeSet, Rule,
  RuleAction, RuleCondition, Session, Signature, SignatureImportResult,
} from './types'

// ---- mapping helpers ----

function ruleFromRaw(r: RuleRaw, mailboxId: string): Rule {
  const conditions: RuleCondition[] = []
  if (r.match_from) conditions.push({ type: 'from_matches', value: r.match_from })
  if (r.match_subject) conditions.push({ type: 'subject_contains', value: r.match_subject })
  const actions: RuleAction[] = []
  if (r.action_folder_id) actions.push({ type: 'move', folder_id: r.action_folder_id })
  return {
    id: r.id,
    mailbox_id: mailboxId,
    name: r.match_from ? `From ${r.match_from}` : r.match_subject ? `Subject “${r.match_subject}”` : `Rule ${r.position}`,
    enabled: r.enabled,
    order: r.position,
    match: 'all',
    conditions,
    actions,
    created_at: r.created_at,
    updated_at: r.created_at,
  }
}

function ruleToRaw(rule: Pick<Rule, 'conditions' | 'actions'>) {
  const from = rule.conditions.find((c): c is Extract<RuleCondition, { type: 'from_matches' }> => c.type === 'from_matches')
  const subject = rule.conditions.find((c): c is Extract<RuleCondition, { type: 'subject_contains' }> => c.type === 'subject_contains')
  const move = rule.actions.find((a): a is Extract<RuleAction, { type: 'move' }> => a.type === 'move')
  return {
    match_from: from?.value,
    match_subject: subject?.value,
    action_folder_id: move?.folder_id,
    action_mark_read: false,
    enabled: true,
  }
}

const oooToRaw = (ooo: OutOfOffice) => ({
  enabled: ooo.enabled,
  subject: 'Out of office',
  body: ooo.body_internal || ooo.body_external || '',
  starts_at: ooo.starts_at || null,
  ends_at: ooo.ends_at || null,
})

function oooFromRaw(a: AutoresponderRaw): OutOfOffice {
  return {
    enabled: a.enabled,
    starts_at: a.starts_at ?? nowIso(),
    ends_at: a.ends_at ?? new Date(Date.now() + 7 * 86_400_000).toISOString(),
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'Africa/Johannesburg',
    scope: 'all',
    body_internal: a.body,
    body_external: a.body,
    reply_frequency_days: 1,
    reply_to_known_only: false,
  }
}

function periodFromRaw(a: AutoresponderRaw): OutOfOfficePeriod {
  const o = oooFromRaw(a)
  return {
    id: 'p-current',
    enabled: o.enabled,
    starts_at: o.starts_at,
    ends_at: o.ends_at,
    timezone: o.timezone,
    scope: o.scope,
    body_internal: o.body_internal,
    body_external: o.body_external,
    reply_frequency_days: o.reply_frequency_days,
    reply_to_known_only: o.reply_to_known_only,
  }
}

// ---- slice ----

// rule id → mailbox id index (the UI deletes by rule id alone)
const ruleMailbox = new Map<string, string>()

export const apiSettings = {
  // ===== Aliases (address routing) =====
  listAliases: async (mailboxId: string): Promise<Alias[]> => {
    const { addresses } = await request<{ addresses: AddressRaw[] }>('/addresses')
    return addresses.filter((a) => a.kind !== 'user').map((a) => mapAlias(a, mailboxId))
  },
  createAlias: async (mailboxId: string, input: { address?: string; label: string; category: AliasCategory; single_use?: boolean; expires_at?: string; forward_to?: string }): Promise<Alias> => {
    void input.single_use
    const address = input.address ?? `${input.label.replace(/[^a-z0-9-]/gi, '').toLowerCase()}-${Date.now().toString(36)}`
    const [local, domain] = address.split('@')
    const { domains } = await request<{ domains: DomainRaw[] }>('/domains')
    const d = domains.find((x) => x.name === domain) ?? domains[0]
    if (!d) notAvailable('Alias domains', 'domain registration (register 10.x)')
    const created = await request<{ address?: AddressRaw; id?: string }>('/addresses', {
      method: 'POST',
      body: JSON.stringify({
        local,
        domain_id: d.id,
        mailbox_id: input.forward_to ? undefined : mailboxId,
        kind: input.forward_to ? 'forward' : 'alias',
        forward_target: input.forward_to,
      }),
      headers: { 'Idempotency-Key': crypto.randomUUID() },
    })
    return {
      ...mapAlias({ id: created.id ?? localId('alias'), kind: input.forward_to ? 'forward' : 'alias', mailbox_id: mailboxId, forward_target: input.forward_to, address, created_at: nowIso() }, mailboxId),
      label: input.label,
      category: input.category,
      expires_at: input.expires_at,
    }
  },
  updateAlias: (_id: string, _patch: Partial<Pick<Alias, 'label' | 'category' | 'disabled' | 'expires_at' | 'forward_to' | 'notification_enabled' | 'blocked_senders'>>): Promise<Alias> => {
    void _id; void _patch
    notAvailable('Alias editing', 'address management API (register 2.x tail)')
  },
  deleteAlias: async (id: string): Promise<void> => {
    await request(`/addresses/${id}`, { method: 'DELETE' })
  },
  bulkCreateAliases: async (mailboxId: string, input: { count: number; label_prefix: string; category: AliasCategory }): Promise<Alias[]> => {
    const out: Alias[] = []
    for (let i = 1; i <= Math.min(input.count, 50); i++) {
      out.push(await apiSettings.createAlias(mailboxId, {
        address: undefined,
        label: `${input.label_prefix}-${i}`,
        category: input.category,
      }))
    }
    return out
  },

  // ===== Signatures =====
  listSignatures: async (_mailboxId: string): Promise<Signature[]> => {
    void _mailboxId
    const { signatures } = await request<{ signatures: SignatureRaw[] }>('/me/signatures')
    return signatures.map((s) => ({
      id: s.id,
      name: s.name,
      body_text: s.text,
      body_html: s.html,
      is_default: s.is_default,
      use_for_replies: false,
      locked: false,
    }))
  },
  createSignature: async (_mailboxId: string, input: Omit<Signature, 'id'>): Promise<Signature> => {
    void _mailboxId
    const { id } = await request<{ id: string }>('/me/signatures', {
      method: 'POST',
      body: JSON.stringify({ name: input.name, html: input.body_html, text: input.body_text, is_default: input.is_default }),
    })
    return { ...input, id }
  },
  updateSignature: (_id: string, _patch: Partial<Signature>): Promise<Signature> => {
    void _id; void _patch
    // No PATCH endpoint — the editor writes a new signature or deletes.
    notAvailable('Signature editing', 'signatures API (register 12c)')
  },
  deleteSignature: async (id: string): Promise<void> => {
    await request(`/me/signatures/${id}`, { method: 'DELETE' })
  },
  setDefaultSignature: (_id: string): Promise<void> => {
    void _id
    notAvailable('Default signature switch', 'signatures API (register 12c)')
  },
  importSignatureHtml: async (html: string): Promise<SignatureImportResult> => {
    // Local import: sanitize + lint client-side (no server endpoint).
    const { lintSignatureHtml } = await import('./lintSignature')
    return { body_html: html, issues: lintSignatureHtml(html) }
  },

  // ===== OOO — single (part 1 shape) and periods (part 5 shape) =====
  getOutOfOffice: async (mailboxId: string): Promise<OutOfOffice> => {
    const raw = await request<AutoresponderRaw>(`/mailboxes/${mailboxId}/autoresponder`)
    return oooFromRaw(raw)
  },
  setOutOfOffice: async (mailboxId: string, ooo: OutOfOffice): Promise<OutOfOffice> => {
    await request(`/mailboxes/${mailboxId}/autoresponder`, { method: 'PUT', body: JSON.stringify(oooToRaw(ooo)) })
    return ooo
  },
  disableOutOfOffice: async (mailboxId: string): Promise<void> => {
    await request(`/mailboxes/${mailboxId}/autoresponder`, {
      method: 'PUT',
      body: JSON.stringify({ enabled: false, subject: '', body: '', starts_at: null, ends_at: null }),
    })
  },

  getOutOfOfficePeriods: async (mailboxId: string): Promise<OutOfOfficeState> => {
    const raw = await request<AutoresponderRaw>(`/mailboxes/${mailboxId}/autoresponder`).catch(() => null)
    const period = raw && raw.enabled ? periodFromRaw(raw) : null
    return {
      globally_enabled: !!period,
      periods: period ? [period] : [],
      excluded_senders: [],
      loop_guard_acknowledged: true,
    }
  },
  setOutOfOfficePeriods: async (mailboxId: string, state: OutOfOfficeState): Promise<OutOfOfficeState> => {
    // The backend autoresponder is single-period: the first enabled period wins.
    const first = state.periods.find((p) => p.enabled)
    await request(`/mailboxes/${mailboxId}/autoresponder`, {
      method: 'PUT',
      body: JSON.stringify({
        enabled: state.globally_enabled && !!first,
        subject: 'Out of office',
        body: first ? (first.body_internal || first.body_external || '') : '',
        starts_at: first?.starts_at ?? null,
        ends_at: first?.ends_at ?? null,
      }),
    })
    return state
  },

  // ===== Rules =====
  listRules: async (mailboxId: string): Promise<Rule[]> => {
    const { rules } = await request<{ rules: RuleRaw[] }>(`/mailboxes/${mailboxId}/rules`)
    for (const r of rules) ruleMailbox.set(r.id, mailboxId)
    return rules.map((r) => ruleFromRaw(r, mailboxId))
  },
  createRule: async (mailboxId: string, rule: Omit<Rule, 'id' | 'created_at' | 'updated_at'>): Promise<Rule> => {
    const raw = ruleToRaw(rule)
    const created = await request<{ rule?: RuleRaw; id?: string }>(`/mailboxes/${mailboxId}/rules`, {
      method: 'POST', body: JSON.stringify(raw),
    })
    return {
      ...rule,
      id: created.id ?? created.rule?.id ?? localId('rule'),
      created_at: nowIso(),
      updated_at: nowIso(),
    }
  },
  updateRule: (_id: string, _patch: Partial<Rule>): Promise<Rule> => {
    void _id; void _patch
    // No PATCH endpoint — delete + recreate from the builder.
    notAvailable('Rule editing', 'rules API (register 12e)')
  },
  deleteRule: async (ruleId: string): Promise<void> => {
    const mailboxId = ruleMailbox.get(ruleId)
    if (!mailboxId) throw new Error('Unknown rule — refresh the rules list first')
    ruleMailbox.delete(ruleId)
    await request(`/mailboxes/${mailboxId}/rules/${ruleId}`, { method: 'DELETE' })
  },
  reorderRules: async (mailboxId: string, _order: string[]): Promise<void> => {
    void mailboxId; void _order
    // Applied in insertion order server-side; reordering is advisory only.
  },
  testRule: async (id: string, sampleMessageId: string): Promise<{ matched: boolean; actions: Rule['actions'] }> => {
    // Local evaluation against the sample message (no server dry-run endpoint).
    const msg = await request<{ message: { from: { email: string }; subject: string } }>(`/messages/${sampleMessageId}`)
    const rules = await apiSettings.listRules('')
    const rule = rules.find((r) => r.id === id)
    if (!rule) return { matched: false, actions: [] }
    const matched = rule.conditions.every((c) => {
      if (c.type === 'from_matches') return msg.message.from.email.toLowerCase().includes(c.value.toLowerCase())
      if (c.type === 'subject_contains') return msg.message.subject.toLowerCase().includes(c.value.toLowerCase())
      return false // conditions beyond the backend's two matchers can't fire
    })
    return { matched, actions: matched ? rule.actions : [] }
  },

  // ===== Newsletters =====
  unsubscribeOneClick: async (_messageId: string): Promise<void> => {
    void _messageId
    // The pipeline honours List-Unsubscribe on delivery; a manual unsubscribe
    // here would need a suppression POST the API does not expose yet.
    notAvailable('One-click unsubscribe', 'unsubscribe service (register 12d)')
  },
  listNewsletterSenders: async (_mailboxId: string): Promise<NewsletterSender[]> => {
    void _mailboxId
    return [] // the newsletters module aggregates on the backend (register 12d)
  },
  setNewsletterPref: async (_mailboxId: string, _sender: string, _pref: Partial<Pick<NewsletterSender, 'muted' | 'digest'>>): Promise<void> => { void _mailboxId; void _sender; void _pref /* preferences land with the newsletters module */ },
  getDigestPrefs: async (_mailboxId: string): Promise<DigestPreferences> => {
    void _mailboxId
    return { mode: 'off', time_of_day: '07:00', timezone: 'Africa/Johannesburg', exclude_senders: [] }
  },
  setDigestPrefs: async (_mailboxId: string, prefs: DigestPreferences): Promise<DigestPreferences> => {
    void _mailboxId
    return prefs
  },

  // ===== Notes (write-only backend: reads return what this client wrote) =====
  noteStore: new Map<string, import('./types').Note[]>(),
  listNotes: async (target: { messageId?: string; threadId?: string }): Promise<import('./types').Note[]> => {
    const key = target.messageId ?? target.threadId ?? ''
    return apiSettings.noteStore.get(key) ?? []
  },
  createNote: async (target: { messageId?: string; threadId?: string }, body: string, isPrivate: boolean): Promise<import('./types').Note> => {
    const key = target.messageId ?? target.threadId ?? ''
    if (target.threadId && !target.threadId.startsWith('t-')) {
      // Real backend thread id — persist the note server-side (write-only API).
      await request(`/threads/${target.threadId}/note`, { method: 'POST', body: JSON.stringify({ note: body }) }).catch(() => undefined)
    }
    const note: import('./types').Note = {
      id: localId('note'), message_id: target.messageId, thread_id: target.threadId,
      body, private: isPrivate, author: 'you', created_at: nowIso(),
    }
    apiSettings.noteStore.set(key, [...(apiSettings.noteStore.get(key) ?? []), note])
    return note
  },
  listThreadNotes: async (threadId: string): Promise<import('./types').Note[]> => {
    return apiSettings.noteStore.get(threadId) ?? []
  },
  createThreadNote: async (threadId: string, body: string, isPrivate: boolean): Promise<import('./types').Note> => {
    return apiSettings.createNote({ threadId }, body, isPrivate)
  },
  deleteNote: async (id: string): Promise<void> => {
    for (const [key, notes] of apiSettings.noteStore) {
      apiSettings.noteStore.set(key, notes.filter((n) => n.id !== id))
    }
  },

  // ===== Security: TOTP / sessions / passkeys / devices / app passwords =====
  totpSetup: async (): Promise<{ secret: string; otpauth_url: string; qr_svg: string }> => {
    const r = await request<{ secret: string; otpauth_url: string }>('/me/mfa/enrol')
    return { secret: r.secret, otpauth_url: r.otpauth_url, qr_svg: '' }
  },
  totpVerify: async (code: string): Promise<RecoveryCodeSet> => {
    await request('/me/mfa/activate', { method: 'POST', body: JSON.stringify({ code }) })
    return { codes: [], generated_at: nowIso() } // the API does not issue recovery codes yet
  },
  totpDisable: async (password: string): Promise<void> => {
    await request('/me/mfa/disable', { method: 'POST', body: JSON.stringify({ password }) })
  },
  regenerateRecoveryCodes: (): Promise<RecoveryCodeSet> => notAvailable('Recovery codes', 'MFA expansion (register 8.x)'),

  listPasskeys: async (): Promise<Passkey[]> => [],
  beginPasskeyRegister: (_label: string): Promise<{ challenge: string; rp: { id: string; name: string }; user: { id: string; name: string; displayName: string } }> => { void _label; return notAvailable('Passkeys', 'WebAuthn enrolment (register 8.x)') },
  finishPasskeyRegister: (_credential: unknown, _challenge: string, _label: string): Promise<Passkey> => { void _credential; void _challenge; void _label; return notAvailable('Passkeys', 'WebAuthn enrolment (register 8.x)') },
  deletePasskey: (_id: string): Promise<void> => { void _id; return notAvailable('Passkeys', 'WebAuthn enrolment (register 8.x)') },

  listSessions: async (): Promise<Session[]> => {
    void 0
    const { sessions } = await request<{ sessions: SessionRaw[] }>('/me/sessions')
    return sessions.map(mapSession)
  },
  revokeSession: async (id: string): Promise<void> => {
    await request(`/me/sessions/${id}/revoke`, { method: 'POST' })
  },
  revokeAllSessions: (): Promise<void> => notAvailable('Revoke-all sessions', 'session API (register 8.x)'),

  listDevices: async (): Promise<Device[]> => [],
  revokeDevice: (_id: string): Promise<void> => { void _id; return notAvailable('Devices', 'device registry (register 8.x)') },

  listAppPasswords: async (): Promise<AppPassword[]> => {
    const { app_passwords } = await request<{ app_passwords: { id: string; name: string; prefix: string; created_at?: string; last_used_at?: string | null }[] }>('/app-passwords')
    return app_passwords.map(mapAppPassword)
  },
  createAppPassword: async (label: string, _scopes: ('imap' | 'smtp')[]): Promise<{ app_password: AppPassword; secret: string }> => {
    void _scopes
    const r = await request<{ id: string; name: string; prefix: string; secret: string; expires_at: string | null }>('/app-passwords', {
      method: 'POST', body: JSON.stringify({ name: label }),
    })
    return { app_password: mapAppPassword(r), secret: r.secret }
  },
  revokeAppPassword: async (id: string): Promise<void> => {
    await request(`/app-passwords/${id}`, { method: 'DELETE' })
  },
}
