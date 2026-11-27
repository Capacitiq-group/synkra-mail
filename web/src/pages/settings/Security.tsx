// Security & sessions: MFA (TOTP enrol → activate, disable), active session
// list with revoke, and app passwords (SMTP/IMAP credentials — the secret is
// shown exactly once, §7.1). WebAuthn passkeys need backend enrolment
// endpoints that don't exist yet — tracked in web/README.md.

import { useCallback, useEffect, useState } from 'react'
import { appPasswordsApi, mfaApi, sessionsApi } from '@/api/endpoints'
import { useAuth } from '@/stores/auth'
import { useUI, toastError } from '@/stores/ui'
import { formatDateTime } from '@/lib/format'
import type { AppPasswordView, MFAEnrol, SessionView } from '@/types/api'
import { PageHeader, Card } from '@/ui/data'
import { Badge, EmptyState } from '@/ui/feedback'
import { Button, Field, Input } from '@/ui/primitives'
import { ConfirmDialog } from '@/ui/overlays'

export default function Security() {
  const { me, refresh } = useAuth()
  if (!me) return null
  return (
    <div className="mx-auto max-w-3xl p-6">
      <PageHeader title="Security & sessions" subtitle="Two-factor auth, devices and app passwords" />
      <MFACard enabled={me.user.status === 'active'} onChanged={refresh} />
      <SessionsCard />
      <AppPasswordsCard />
    </div>
  )
}

// ---- TOTP ----

function MFACard({ enabled, onChanged }: { enabled: boolean; onChanged: () => Promise<void> }) {
  const [enrol, setEnrol] = useState<MFAEnrol | null>(null)
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirmDisable, setConfirmDisable] = useState(false)

  const startEnrol = () => {
    mfaApi
      .enrol()
      .then(setEnrol)
      .catch((e) => toastError(e, 'Enrolment failed'))
  }

  const activate = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await mfaApi.activate(code)
      useUI.getState().toast('success', 'Two-factor enabled', 'You will need your authenticator at next sign-in')
      setEnrol(null)
      setCode('')
      await onChanged()
    } catch (err) {
      toastError(err, 'Activation failed')
    } finally {
      setBusy(false)
    }
  }

  const disable = async () => {
    setBusy(true)
    try {
      await mfaApi.disable(password)
      useUI.getState().toast('success', 'Two-factor disabled')
      setConfirmDisable(false)
      setPassword('')
      await onChanged()
    } catch (err) {
      toastError(err, 'Disable failed')
    } finally {
      setBusy(false)
      setConfirmDisable(false)
    }
  }

  return (
    <Card title="Two-factor authentication (TOTP)" className="mt-4">
      {enabled ? (
        <div className="flex items-center justify-between gap-3">
          <p className="text-sm text-slate-600">
            <Badge tone="green">enabled</Badge> Your account requires an authenticator code at sign-in.
          </p>
          <Button
            variant="danger"
            size="sm"
            onClick={() => {
              setPassword('')
              setConfirmDisable(true)
            }}
          >
            Disable
          </Button>
        </div>
      ) : (
        <div>
          <p className="text-sm text-slate-600">
            Add a time-based one-time password (Google Authenticator, 1Password, Aegis…) as your
            second factor.
          </p>
          {!enrol ? (
            <Button className="mt-3" onClick={startEnrol}>
              Start enrolment
            </Button>
          ) : (
            <form onSubmit={activate} className="mt-3 flex max-w-md flex-col gap-3">
              <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs">
                <div className="mb-1 font-medium text-slate-600">1. Add this secret to your app:</div>
                <code className="block break-all rounded bg-white px-2 py-1 font-mono text-brand-700">
                  {enrol.secret}
                </code>
                <div className="mt-2 mb-1 font-medium text-slate-600">2. Or open the otpauth link:</div>
                <a href={enrol.otpauth_url} className="break-all text-brand-600 hover:underline">
                  {enrol.otpauth_url}
                </a>
              </div>
              <Field label="3. Enter the current 6-digit code" required>
                <Input
                  inputMode="numeric"
                  value={code}
                  onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                  placeholder="123456"
                  className="text-center font-mono tracking-widest"
                  required
                />
              </Field>
              <div className="flex gap-2">
                <Button type="submit" loading={busy} disabled={code.length !== 6}>
                  Activate
                </Button>
                <Button type="button" variant="secondary" onClick={() => setEnrol(null)}>
                  Cancel
                </Button>
              </div>
            </form>
          )}
        </div>
      )}

      <ConfirmDialog
        open={confirmDisable}
        title="Disable two-factor?"
        body="Enter your password to confirm. Disabling MFA weakens account security."
        confirmLabel="Disable MFA"
        danger
        onConfirm={() => void disable()}
        onCancel={() => setConfirmDisable(false)}
      />
      {confirmDisable && (
        <div className="mt-3 max-w-sm">
          <Field label="Password" required>
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <Button variant="danger" size="sm" className="mt-2" loading={busy} disabled={!password} onClick={() => void disable()}>
            Confirm disable
          </Button>
        </div>
      )}
    </Card>
  )
}

// ---- sessions ----

function SessionsCard() {
  const [sessions, setSessions] = useState<SessionView[] | null>(null)

  const load = useCallback(() => {
    sessionsApi
      .list()
      .then((r) => setSessions(r.sessions))
      .catch((e) => toastError(e, 'Could not load sessions'))
  }, [])

  useEffect(load, [load])

  return (
    <Card title="Active sessions" className="mt-4">
      {!sessions ? (
        <EmptyState title="Loading…" />
      ) : (
        <ul className="divide-y divide-slate-100">
          {sessions.map((s) => (
            <li key={s.id} className="flex items-center justify-between gap-3 py-2 text-sm">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-slate-700">{s.kind}</span>
                  {s.current && <Badge tone="brand">this device</Badge>}
                </div>
                <div className="truncate text-xs text-slate-400">
                  {s.ip} — {s.user_agent || 'unknown client'} · started {formatDateTime(s.created_at)}
                </div>
              </div>
              {!s.current && (
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() =>
                    sessionsApi
                      .revoke(s.id)
                      .then(() => {
                        useUI.getState().toast('success', 'Session revoked')
                        load()
                      })
                      .catch((e) => toastError(e, 'Revoke failed'))
                  }
                >
                  Revoke
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

// ---- app passwords ----

function AppPasswordsCard() {
  const [list, setList] = useState<AppPasswordView[] | null>(null)
  const [name, setName] = useState('')
  const [secret, setSecret] = useState<{ name: string; prefix: string; secret: string } | null>(null)

  const load = useCallback(() => {
    appPasswordsApi
      .list()
      .then((r) => setList(r.app_passwords))
      .catch((e) => toastError(e, 'Could not load app passwords'))
  }, [])

  useEffect(load, [load])

  return (
    <Card title="App passwords — SMTP/IMAP credentials for mail clients" className="mt-4">
      <p className="mb-3 text-xs text-slate-500">
        For desktop/phone mail clients that don't do OAuth. The secret is shown <strong>once</strong> —
        store it in your password manager.
      </p>
      <form
        className="flex max-w-md items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          appPasswordsApi
            .create(name.trim())
            .then((r) => {
              setSecret({ name: r.name, prefix: r.prefix, secret: r.secret })
              setName('')
              load()
            })
            .catch((e) => toastError(e, 'Create failed'))
        }}
      >
        <Field label="Name" hint="e.g. Thunderbird laptop">
          <Input value={name} onChange={(e) => setName(e.target.value)} required placeholder="client name" />
        </Field>
        <Button type="submit" className="mb-0.5">
          Create
        </Button>
      </form>

      {secret && (
        <div className="mt-3 rounded-lg border border-emerald-300 bg-emerald-50 p-3 text-sm" role="alert">
          <div className="font-medium text-emerald-800">Copy it now — shown once: {secret.name} ({secret.prefix}…)</div>
          <code className="mt-1 block break-all rounded bg-white px-2 py-1.5 font-mono text-xs">{secret.secret}</code>
          <button
            className="mt-1.5 text-xs font-medium text-emerald-700 hover:underline"
            onClick={() => {
              void navigator.clipboard?.writeText(secret.secret)
              useUI.getState().toast('success', 'Copied to clipboard')
            }}
          >
            Copy
          </button>
        </div>
      )}

      {list && list.length > 0 && (
        <ul className="mt-3 divide-y divide-slate-100">
          {list.map((p) => (
            <li key={p.id} className="flex items-center justify-between gap-3 py-2 text-sm">
              <div>
                <span className="font-medium text-slate-700">{p.name}</span>
                <span className="ml-2 font-mono text-xs text-slate-400">{p.prefix}…</span>
              </div>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  if (!window.confirm(`Revoke "${p.name}"? Clients using it will stop working.`)) return
                  appPasswordsApi
                    .revoke(p.id)
                    .then(() => {
                      useUI.getState().toast('success', 'Revoked')
                      load()
                    })
                    .catch((e) => toastError(e, 'Revoke failed'))
                }}
              >
                Revoke
              </Button>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}
