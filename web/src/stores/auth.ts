import { create } from 'zustand'
import { authApi } from '@/api/endpoints'
import { onUnauthorized, setAuthToken, getAuthToken } from '@/api/client'
import type { Me } from '@/types/api'

interface AuthState {
  me: Me | null
  /** loading = bootstrapping with a stored token; anon = logged out */
  status: 'loading' | 'anon' | 'ready'
  init: () => Promise<void>
  login: (email: string, password: string, totpCode?: string) => Promise<void>
  logout: () => Promise<void>
  clear: () => void
  refresh: () => Promise<void>
}

export const useAuth = create<AuthState>((set, get) => {
  async function bootstrap(): Promise<void> {
    if (!getAuthToken()) {
      set({ me: null, status: 'anon' })
      return
    }
    try {
      const me = await authApi.me()
      set({ me, status: 'ready' })
    } catch {
      // invalid token (401 handler already cleared it) or network down
      if (getAuthToken() === null) set({ me: null, status: 'anon' })
      else set({ me: null, status: 'anon' })
      setAuthToken(null)
    }
  }

  // Global handler: any 401 outside /auth/login wipes the session and
  // bounces to the login route.
  onUnauthorized(() => {
    setAuthToken(null)
    set({ me: null, status: 'anon' })
    if (!window.location.pathname.startsWith('/login')) {
      window.location.assign('/login?expired=1')
    }
  })

  return {
    me: null,
    status: 'loading',
    init: bootstrap,
    login: async (email, password, totpCode) => {
      const resp = await authApi.login(email, password, totpCode)
      setAuthToken(resp.token)
      set({ me: { user: resp.user, org: resp.org, impersonated: false, mailboxes: [] }, status: 'ready' })
      await get().refresh() // hydrate mailboxes + impersonation flags
    },
    logout: async () => {
      try {
        await authApi.logout()
      } catch {
        /* the local session dies regardless */
      }
      setAuthToken(null)
      set({ me: null, status: 'anon' })
    },
    clear: () => {
      setAuthToken(null)
      set({ me: null, status: 'anon' })
    },
    refresh: async () => {
      const me = await authApi.me()
      set({ me })
    },
  }
})
