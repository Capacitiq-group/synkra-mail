// The api object — same surface the parts 1-9 UI was written against, now
// implemented against the real Go backend. Slices:
//   apiCore.ts     — request plumbing, mappers, honest stubs
//   apiMail.ts     — mailboxes, folders, messages, attachments, compose
//   apiSettings.ts — aliases, signatures, OOO, rules, newsletters, notes, security
//   apiOrg.ts      — shared mailboxes, deliverability, domains, org + staff admin
// Auth lives here (bearer token flow with the two-step TOTP retry: the API
// answers 401 mfa_required, and loginTotp re-POSTs with totp_code attached).

import { request, setToken, pendingLogin, mapMe, type MeRaw, type OrgRaw, type UserViewRaw } from './apiCore'
import { apiMail } from './apiMail'
import { apiSettings } from './apiSettings'
import { apiOrg, orgApi, adminApi } from './apiOrg'
import { ApiError } from './errors'
import type { LoginResponse, Me } from './types'

export { ApiError }

export const eventsUrl = '/api/v1/events' // reserved: no SSE channel yet (useSSE polls)

const auth = {
  login: async (email: string, password: string): Promise<LoginResponse> => {
    try {
      const resp = await request<{ token: string; user: UserViewRaw; org: OrgRaw }>('/auth/login', {
        method: 'POST',
        body: JSON.stringify({ email, password }),
      })
      setToken(resp.token)
      pendingLogin.email = ''
      pendingLogin.password = ''
      const me = mapMe({ user: resp.user, org: resp.org, impersonated: false, mailboxes: [] })
      return { me }
    } catch (err) {
      const e = err as { code?: string }
      if (e.code === 'mfa_required') {
        // The real API retries the SAME login with totp_code attached.
        pendingLogin.email = email
        pendingLogin.password = password
        return { requires_totp: true, totp_challenge: 'retry' }
      }
      throw err
    }
  },

  loginTotp: async (challenge: string, code: string): Promise<LoginResponse> => {
    void challenge
    const resp = await request<{ token: string; user: UserViewRaw; org: OrgRaw }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({
        email: pendingLogin.email,
        password: pendingLogin.password,
        totp_code: code,
      }),
    })
    setToken(resp.token)
    pendingLogin.email = ''
    pendingLogin.password = ''
    const me = mapMe({ user: resp.user, org: resp.org, impersonated: false, mailboxes: [] })
    return { me }
  },

  // Passkeys: not on this deployment — LoginRoute shows its own message when
  // beginPasskeyLogin yields no challenge.
  beginPasskeyLogin: async (_email: string): Promise<LoginResponse> => { void _email; return {} },
  finishPasskeyLogin: async (_credential: unknown, _challenge: string): Promise<LoginResponse> => { void _credential; void _challenge; return {} },

  logout: async () => {
    try {
      await request('/auth/logout', { method: 'POST' })
    } catch { /* local session dies regardless */ }
    setToken(null)
  },

  me: (signal?: AbortSignal): Promise<Me> =>
    request<MeRaw>('/me', {}, signal).then(mapMe),

  forgotPassword: async (_email: string) => {
    void _email
    throw new ApiError(501, 'module_unavailable',
      'Password reset by email ships with the onboarding module (register 6c)',
      'ask an administrator to reset it in the interim')
  },
  resetPassword: async (_token: string, _newPassword: string) => {
    void _token; void _newPassword
    throw new ApiError(501, 'module_unavailable',
      'Password reset by email ships with the onboarding module (register 6c)')
  },

  changePassword: async (currentPassword: string, newPassword: string) => {
    await request('/me/password', {
      method: 'POST',
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    })
  },
}

export const api = {
  ...auth,
  ...apiMail,
  ...apiSettings,
  ...apiOrg,
  org: orgApi,
  admin: adminApi,
}

export type Api = typeof api
