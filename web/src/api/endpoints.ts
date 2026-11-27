// Every API surface the SPA uses, one function per route — nothing else
// hand-builds URLs. Grouped by domain; paths mirror internal/api/server.go.

import { api } from './client'
import type {
  AddressView,
  APIKeySecret,
  APIKeyView,
  AppPasswordSecret,
  AppPasswordView,
  AttachmentView,
  Autoresponder,
  CreateAddressInput,
  CreateRuleInput,
  DeliverabilityStats,
  DomainDNS,
  DomainView,
  Folder,
  FolderPage,
  LoginResp,
  MFAEnrol,
  Me,
  MessageDetail,
  OrgDetail,
  QuarantineRow,
  ReliabilityResp,
  Rule,
  SearchResp,
  SendInput,
  SendResp,
  SessionView,
  SharedMember,
  Signature,
  Storage,
  SuppressionRow,
  TrackingResp,
  TraceResp,
  UserView,
  WebhookSecret,
  WebhookView,
} from '@/types/api'

// ---- auth + account ----

export const authApi = {
  login: (email: string, password: string, totpCode?: string) =>
    api.post<LoginResp>('/auth/login', {
      body: { email, password, totp_code: totpCode || undefined },
    }),
  logout: () => api.post<{ ok: boolean }>('/auth/logout'),
  me: () => api.get<Me>('/me'),
  requestEmailVerification: () => api.post<{ ok?: boolean; token?: string }>('/me/verify-email'),
  verifyEmail: (token: string) => api.post<{ ok: boolean; user_id: string }>('/auth/verify-email', { body: { token } }),
  changePassword: (currentPassword: string, newPassword: string) =>
    api.post<{ ok: boolean; other_sessions_revoked: boolean }>('/me/password', {
      body: { current_password: currentPassword, new_password: newPassword },
    }),
}

export const mfaApi = {
  enrol: () => api.post<MFAEnrol>('/me/mfa/enrol'),
  activate: (code: string) => api.post<{ mfa_enabled: boolean }>('/me/mfa/activate', { body: { code } }),
  disable: (password: string) => api.post<{ mfa_enabled: boolean }>('/me/mfa/disable', { body: { password } }),
}

export const sessionsApi = {
  list: () => api.get<{ sessions: SessionView[] }>('/me/sessions'),
  revoke: (id: string) => api.post<{ ok: boolean }>(`/me/sessions/${id}/revoke`),
}

// ---- mailboxes / folders / messages ----

export const mailApi = {
  mailboxes: () => api.get<{ mailboxes: { id: string; kind: string; name: string; send_status: string; owner: boolean }[] }>('/mailboxes'),
  folders: () => api.get<{ folders: Folder[] }>('/folders'),
  createFolder: (mailboxId: string, name: string) =>
    api.post<{ folder: Folder }>('/folders', { body: { mailbox_id: mailboxId, name } }),
  deleteFolder: (id: string) => api.delete<{ ok: boolean }>(`/folders/${id}`),

  messages: (folderId: string, opts: { limit?: number; beforeUid?: number } = {}) =>
    api.get<FolderPage>('/messages', {
      query: { folder_id: folderId, limit: opts.limit, before_uid: opts.beforeUid },
    }),
  message: (id: string) => api.get<MessageDetail>(`/messages/${id}`),
  messageRaw: (id: string) => api.getBlob(`/messages/${id}/raw`),
  setFlags: (id: string, flags: string[]) =>
    api.patch<{ flags: string[] }>(`/messages/${id}/flags`, { body: { flags } }),
  move: (id: string, folderId: string) =>
    api.post<{ folder_id: string; uid: number }>(`/messages/${id}/move`, { body: { folder_id: folderId } }),
  deleteMessage: (id: string) => api.delete<{ ok: boolean }>(`/messages/${id}`),

  attachments: (id: string) => api.get<{ attachments: AttachmentView[] }>(`/messages/${id}/attachments`),
  downloadAttachment: (downloadPath: string) => api.getBlob(downloadPath),

  trace: (id: string) => api.get<TraceResp>(`/messages/${id}/trace`),
  tracking: (id: string) => api.get<TrackingResp>(`/messages/${id}/tracking`),
  search: (q: string) => api.get<SearchResp>('/search', { query: { q } }),
  storage: () => api.get<Storage>('/storage'),
}

export const sendApi = {
  send: (input: SendInput, idemKey: string) => api.post<SendResp>('/send', { body: input, idemKey }),
}

// threads: note (POST only — there is no read endpoint yet, see README)
export const threadApi = {
  addNote: (threadId: string, note: string) =>
    api.post<{ ok: boolean }>(`/threads/${threadId}/note`, { body: { note } }),
  assign: (threadId: string, userId: string) =>
    api.post<{ ok: boolean }>(`/threads/${threadId}/assign`, { body: { user_id: userId } }),
}

// ---- addresses (alias/forward routing) + domains ----

export const addressesApi = {
  list: () => api.get<{ addresses: AddressView[] }>('/addresses'),
  create: (input: CreateAddressInput, idemKey: string) =>
    api.post<{ address?: AddressView; id?: string; email?: string }>('/addresses', { body: input, idemKey }),
  remove: (id: string) => api.delete<{ ok: boolean }>(`/addresses/${id}`),
}

export const domainsApi = {
  list: () => api.get<{ domains: DomainView[] }>('/domains'),
  create: (name: string, idemKey: string) =>
    api.post<{ domain?: DomainView; id?: string }>('/domains', { body: { name }, idemKey }),
  verify: (id: string) => api.post<Record<string, unknown>>(`/domains/${id}/verify`),
  dns: (id: string) => api.get<DomainDNS>(`/domains/${id}/dns`),
  rotateDKIM: (id: string) => api.post<Record<string, unknown>>(`/domains/${id}/rotate-dkim`),
  activateDKIM: (id: string) => api.post<Record<string, unknown>>(`/domains/${id}/activate-dkim`),
}

// ---- org console ----

export const orgApi = {
  org: () => api.get<OrgDetail>('/org'),
  users: () => api.get<{ users: UserView[] }>('/org/users'),
  invite: (email: string, displayName: string, password: string) =>
    api.post<{ user: UserView; mailbox_id: string }>('/users/invite', {
      body: { email, display_name: displayName, password },
    }),
  suspendUser: (id: string) => api.post<{ ok: boolean }>(`/org/users/${id}/suspend`),
  resumeUser: (id: string) => api.post<{ ok: boolean }>(`/org/users/${id}/resume`),
  revokeUserSessions: (id: string) => api.post<{ ok: boolean }>(`/org/users/${id}/revoke-sessions`),

  quarantine: () => api.get<{ quarantine: QuarantineRow[] }>('/org/quarantine'),
  releaseQuarantine: (id: string) => api.post<{ ok: boolean }>(`/org/quarantine/${id}/release`),

  sharedMembers: (mailboxId: string) =>
    api.get<{ members: SharedMember[] }>(`/org/shared-mailboxes/${mailboxId}/members`),
  addSharedMember: (mailboxId: string, userId: string, role: string) =>
    api.post<{ ok: boolean }>(`/org/shared-mailboxes/${mailboxId}/members`, {
      body: { user_id: userId, role },
    }),
  removeSharedMember: (mailboxId: string, userId: string) =>
    api.delete<{ ok: boolean }>(`/org/shared-mailboxes/${mailboxId}/members`, {
      body: { user_id: userId },
    }),

  webhooks: () => api.get<{ webhooks: WebhookView[] }>('/org/webhooks'),
  createWebhook: (url: string, events: string[], idemKey: string) =>
    api.post<WebhookSecret>('/org/webhooks', { body: { url, events }, idemKey }),
  testWebhook: (id: string) => api.post<Record<string, unknown>>(`/org/webhooks/${id}/test`),
  deleteWebhook: (id: string) => api.delete<{ ok: boolean }>(`/org/webhooks/${id}`),

  apiKeys: () => api.get<{ api_keys: APIKeyView[] }>('/org/api-keys'),
  createAPIKey: (name: string, expiresInDays?: number) =>
    api.post<APIKeySecret>('/org/api-keys', {
      body: { name, expires_in_days: expiresInDays ? String(expiresInDays) : undefined },
    }),
  revokeAPIKey: (id: string) => api.delete<{ ok: boolean }>(`/org/api-keys/${id}`),

  deliverability: () => api.get<DeliverabilityStats>('/org/deliverability'),
  health: () =>
    api.get<{ score: number; delivery_rate: number; hard_bounce_rate: number; sent: number; suppressed: number; window_days: number }>('/org/health'),
  reliability: () => api.get<ReliabilityResp>('/org/reliability'),
  setTracking: (enabled: boolean) =>
    api.put<{ tracking_enabled: boolean }>('/org/tracking', { body: { enabled } }),
}

// ---- personal features (§12) ----

export const featuresApi = {
  signatures: () => api.get<{ signatures: Signature[] }>('/me/signatures'),
  createSignature: (name: string, html: string, text: string, isDefault: boolean) =>
    api.post<{ id: string }>('/me/signatures', {
      body: { name, html, text, is_default: isDefault },
    }),
  deleteSignature: (id: string) => api.delete<{ ok: boolean }>(`/me/signatures/${id}`),

  autoresponder: (mailboxId: string) => api.get<Autoresponder>(`/mailboxes/${mailboxId}/autoresponder`),
  setAutoresponder: (mailboxId: string, ar: Autoresponder) =>
    api.put<{ ok: boolean; enabled: boolean }>(`/mailboxes/${mailboxId}/autoresponder`, {
      body: {
        enabled: ar.enabled,
        subject: ar.subject,
        body: ar.body,
        starts_at: ar.starts_at,
        ends_at: ar.ends_at,
      },
    }),

  rules: (mailboxId: string) => api.get<{ rules: Rule[] }>(`/mailboxes/${mailboxId}/rules`),
  createRule: (mailboxId: string, input: CreateRuleInput) =>
    api.post<{ rule?: Rule; id?: string }>(`/mailboxes/${mailboxId}/rules`, { body: input }),
  deleteRule: (mailboxId: string, ruleId: string) =>
    api.delete<{ ok: boolean }>(`/mailboxes/${mailboxId}/rules/${ruleId}`),
}

// ---- app passwords (SMTP/IMAP credentials) ----

export const appPasswordsApi = {
  list: () => api.get<{ app_passwords: AppPasswordView[] }>('/app-passwords'),
  create: (name: string) => api.post<AppPasswordSecret>('/app-passwords', { body: { name } }),
  revoke: (id: string) => api.delete<{ ok: boolean }>(`/app-passwords/${id}`),
}

// ---- suppression (POPIA opt-out register) ----

export const suppressionApi = {
  list: () => api.get<{ suppression: SuppressionRow[] }>('/suppression'),
  remove: (email: string) => api.delete<{ ok: boolean }>(`/suppression/${encodeURIComponent(email)}`),
}
