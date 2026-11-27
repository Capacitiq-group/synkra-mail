// Login — handles the full auth contract: wrong credentials, MFA (the API
// answers 401 mfa_required and expects totp_code on retry), account lockout
// (429 account_locked with a hint), suspended orgs (post-login banner comes
// from the shell).

import { useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { useAuth } from '@/stores/auth'
import { ApiError } from '@/api/client'
import { Button, Field, Input } from '@/ui/primitives'

export default function Login() {
  const { login, me, status } = useAuth()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [totp, setTotp] = useState('')
  const [mfaRequired, setMfaRequired] = useState(false)
  const [error, setError] = useState<{ title: string; detail?: string } | null>(null)
  const [busy, setBusy] = useState(false)

  if (status === 'ready' && me) return <Navigate to="/" replace />

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await login(email.trim(), password, mfaRequired ? totp.trim() : undefined)
      navigate('/', { replace: true })
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.code === 'mfa_required') {
          setMfaRequired(true)
          setError({
            title: 'Two-factor required',
            detail: err.hint || 'Enter the 6-digit code from your authenticator app.',
          })
        } else if (err.code === 'account_locked' || err.code === 'rate_limited') {
          setError({ title: err.code === 'account_locked' ? 'Account locked' : 'Too many attempts', detail: err.hint || err.message })
        } else {
          setError({ title: 'Sign-in failed', detail: err.message })
        }
      } else {
        setError({ title: 'Sign-in failed', detail: (err as Error).message })
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-gradient-to-br from-brand-900 via-slate-900 to-slate-800 p-4">
      <div className="w-full max-w-sm rounded-2xl bg-white p-8 shadow-2xl">
        <div className="mb-6 flex items-center gap-3">
          <img src="/icon.svg" alt="" className="h-10 w-10" />
          <div>
            <h1 className="text-lg font-semibold text-slate-900">Synkra Mail</h1>
            <p className="text-xs text-slate-500">South African business email</p>
          </div>
        </div>

        <form onSubmit={submit} className="flex flex-col gap-3">
          <Field label="Email address" required>
            <Input
              type="email"
              autoComplete="username"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@yourcompany.co.za"
              autoFocus={!mfaRequired}
            />
          </Field>
          <Field label="Password" required>
            <Input
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
            />
          </Field>
          {mfaRequired && (
            <Field label="Authenticator code" hint="6 digits from your TOTP app" required>
              <Input
                inputMode="numeric"
                autoComplete="one-time-code"
                value={totp}
                onChange={(e) => setTotp(e.target.value.replace(/\D/g, '').slice(0, 6))}
                placeholder="123456"
                autoFocus
                className="text-center font-mono text-lg tracking-widest"
              />
            </Field>
          )}

          {error && (
            <div
              role="alert"
              className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700"
            >
              <div className="font-semibold">{error.title}</div>
              {error.detail && <div className="mt-0.5 opacity-90">{error.detail}</div>}
            </div>
          )}

          <Button type="submit" loading={busy} className="mt-1 w-full">
            {busy ? 'Signing in…' : 'Sign in'}
          </Button>
        </form>

        <p className="mt-4 text-center text-[11px] text-slate-400">
          Sessions expire automatically. Failed attempts extend the lockout window.
        </p>
      </div>
    </div>
  )
}
