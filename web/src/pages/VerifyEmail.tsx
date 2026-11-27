// Email verification: POST /auth/verify-email with the token from the link.
// The token arrives as ?token=…; users can also paste it manually.

import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { authApi } from '@/api/endpoints'
import { Button, Field, Input } from '@/ui/primitives'

export default function VerifyEmail() {
  const [params] = useSearchParams()
  const [token, setToken] = useState(params.get('token') ?? '')
  const [state, setState] = useState<'idle' | 'busy' | 'done' | 'error'>('idle')
  const [message, setMessage] = useState('')

  useEffect(() => {
    const t = params.get('token')
    if (t) void submit(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const submit = async (tok: string) => {
    if (!tok.trim()) return
    setState('busy')
    try {
      await authApi.verifyEmail(tok.trim())
      setState('done')
    } catch (err) {
      setState('error')
      setMessage((err as Error).message)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-100 p-4">
      <div className="w-full max-w-sm rounded-2xl bg-white p-8 text-center shadow-lg">
        {state === 'done' ? (
          <>
            <div className="text-4xl" aria-hidden>✅</div>
            <h1 className="mt-3 text-lg font-semibold text-slate-900">Email verified</h1>
            <p className="mt-1 text-sm text-slate-500">Your address is confirmed. You can sign in now.</p>
            <Link to="/login" className="mt-4 inline-block text-sm font-medium text-brand-600 hover:underline">
              Go to sign in →
            </Link>
          </>
        ) : (
          <>
            <h1 className="text-lg font-semibold text-slate-900">Verify your email</h1>
            <p className="mt-1 text-xs text-slate-500">
              Paste the token from the verification email we sent you.
            </p>
            <form
              className="mt-4 flex flex-col gap-3 text-left"
              onSubmit={(e) => {
                e.preventDefault()
                void submit(token)
              }}
            >
              <Field label="Verification token" required>
                <Input value={token} onChange={(e) => setToken(e.target.value)} placeholder="uuid token" />
              </Field>
              {state === 'error' && <p className="text-xs text-red-600">{message}</p>}
              <Button type="submit" loading={state === 'busy'} className="w-full">
                Verify
              </Button>
            </form>
          </>
        )}
      </div>
    </div>
  )
}
