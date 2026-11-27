// Typed fetch client for the Synkra Mail API.
//
// Contract behaviour baked in here (see ../README.md):
//  - every call carries `Authorization: Bearer <token>`;
//  - errors unwrap the {error:{code,message,request_id,hint?}} envelope into
//    ApiError so UI code can branch on `code` (mfa_required, account_locked,
//    mailbox_paused, limit_exceeded, ...) and show `hint` verbatim;
//  - a 401 outside the login route clears the session via the registered
//    unauthorized handler (the auth store installs it).

import type { ErrorEnvelope } from '@/types/api'

const BASE: string = (import.meta.env?.VITE_API_BASE as string | undefined) ?? '/api/v1'
const TOKEN_KEY = 'synkra.token'

let token: string | null = null
try {
  token = localStorage.getItem(TOKEN_KEY)
} catch {
  token = null // storage blocked (private mode): session lives for the tab only
}

let unauthorizedHandler: (() => void) | null = null

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId: string
  readonly hint: string

  constructor(status: number, code: string, message: string, hint = '', requestId = '') {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.hint = hint
    this.requestId = requestId
  }

  /** HTTP 4xx — client-side, expected control flow (not a crash). */
  get isClientError(): boolean {
    return this.status >= 400 && this.status < 500
  }
}

export function getAuthToken(): string | null {
  return token
}

export function setAuthToken(t: string | null): void {
  token = t
  try {
    if (t) localStorage.setItem(TOKEN_KEY, t)
    else localStorage.removeItem(TOKEN_KEY)
  } catch {
    /* storage unavailable — in-memory only */
  }
}

/** Installed once by the auth store: clears the session and routes to /login. */
export function onUnauthorized(fn: (() => void) | null): void {
  unauthorizedHandler = fn
}

export interface RequestOptions {
  body?: unknown
  idemKey?: string
  query?: Record<string, string | number | boolean | undefined>
  /** fetch a binary payload (attachment download, raw message) */
  blob?: boolean
}

function buildURL(path: string, query?: RequestOptions['query']): string {
  const url = new URL(BASE + path, window.location.origin)
  if (query) {
    for (const [k, v] of Object.entries(query)) {
      if (v !== undefined && v !== null && v !== '') url.searchParams.set(k, String(v))
    }
  }
  return url.pathname + url.search
}

async function toApiError(res: Response): Promise<ApiError> {
  let code = 'unknown_error'
  let message = res.statusText || `HTTP ${res.status}`
  let hint = ''
  let requestId = ''
  try {
    const env = (await res.json()) as ErrorEnvelope
    if (env?.error?.code) {
      code = env.error.code
      message = env.error.message
      hint = env.error.hint ?? ''
      requestId = env.error.request_id ?? ''
    }
  } catch {
    /* non-JSON error body — keep defaults */
  }
  return new ApiError(res.status, code, message, hint, requestId)
}

async function request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {}
  if (token) headers.Authorization = `Bearer ${token}`
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json'
  if (opts.idemKey) headers['Idempotency-Key'] = opts.idemKey

  let res: Response
  try {
    res = await fetch(buildURL(path, opts.query), {
      method,
      headers,
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      credentials: 'omit',
    })
  } catch {
    // fetch rejects on DNS/connection failure — a network error, not an API error
    throw new ApiError(0, 'network_error', 'The server could not be reached', 'check your connection and try again')
  }

  if (opts.blob) {
    if (!res.ok) throw await toApiError(res)
    return (await res.blob()) as unknown as T
  }

  const text = await res.text()
  let data: unknown
  try {
    data = text ? JSON.parse(text) : undefined
  } catch {
    if (!res.ok) throw await toApiError(res)
    throw new ApiError(res.status, 'bad_response', 'Server returned a malformed response')
  }

  if (!res.ok) {
    const err = await toApiError(res)
    // Expired/revoked session: wipe state and bounce to login — except on the
    // login route itself, where 401 is control flow (mfa_required, bad creds).
    if (res.status === 401 && code401IsSession(err) && !path.startsWith('/auth/login')) {
      unauthorizedHandler?.()
    }
    throw err
  }
  return data as T
}

function code401IsSession(err: ApiError): boolean {
  // mfa_required / bad-credentials on login are handled by the caller; only
  // genuinely dead sessions trip the global handler.
  return err.code === 'invalid_credentials' || err.code === 'session_expired'
}

export const api = {
  get: <T>(path: string, opts?: RequestOptions) => request<T>('GET', path, opts),
  post: <T>(path: string, opts?: RequestOptions) => request<T>('POST', path, opts),
  put: <T>(path: string, opts?: RequestOptions) => request<T>('PUT', path, opts),
  patch: <T>(path: string, opts?: RequestOptions) => request<T>('PATCH', path, opts),
  delete: <T>(path: string, opts?: RequestOptions) => request<T>('DELETE', path, opts),
  getBlob: (path: string, opts?: RequestOptions) => request<Blob>('GET', path, { ...opts, blob: true }),
}
