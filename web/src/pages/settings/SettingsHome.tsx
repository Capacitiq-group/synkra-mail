// Settings overview: storage quota + password change + account summary.
// Password change POSTs /me/password — the backend revokes all other sessions.

import { useEffect, useState } from 'react'
import { authApi, mailApi } from '@/api/endpoints'
import { useAuth } from '@/stores/auth'
import { useUI, toastError } from '@/stores/ui'
import { formatBytes, formatDate } from '@/lib/format'
import type { Storage } from '@/types/api'
import { PageHeader, Card, KV } from '@/ui/data'
import { Badge, ProgressBar } from '@/ui/feedback'
import { Button, Field, Input } from '@/ui/primitives'

export default function SettingsHome() {
  const { me, refresh } = useAuth()
  const [storage, setStorage] = useState<Storage | null>(null)

  useEffect(() => {
    mailApi.storage().then(setStorage).catch(() => undefined)
  }, [])

  if (!me) return null

  return (
    <div className="mx-auto max-w-3xl p-6">
      <PageHeader
        title="Settings"
        subtitle="Your account, quota and credentials"
      />

      <Card title="Account">
        <KV label="Email">{me.user.email}</KV>
        <KV label="Display name">{me.user.display_name || '—'}</KV>
        <KV label="Role">
          <Badge tone={me.user.role === 'owner' ? 'brand' : 'neutral'}>{me.user.role}</Badge>
        </KV>
        <KV label="Member since">{formatDate(me.user.created_at)}</KV>
        <KV label="Organisation">
          {me.org.name} <Badge tone={me.org.status === 'active' ? 'green' : 'red'}>{me.org.status}</Badge>
        </KV>
        <KV label="Tier">{me.org.tier}</KV>
        <KV label="Sending">
          <Badge tone={me.org.sending_enabled ? 'green' : 'red'}>
            {me.org.sending_enabled ? 'enabled' : 'disabled'}
          </Badge>
        </KV>
      </Card>

      {storage && (
        <Card title="Storage" className="mt-4">
          <div className="mb-2 flex items-baseline justify-between text-sm">
            <span className="text-slate-600">
              {formatBytes(storage.used_bytes)} of {formatBytes(storage.included_bytes)}
              {storage.grace_bytes > 0 && ` (+${formatBytes(storage.grace_bytes)} grace)`}
            </span>
            <span className="font-semibold">{storage.used_percent}%</span>
          </div>
          <ProgressBar
            percent={storage.used_percent}
            tone={storage.used_percent > 90 ? 'red' : storage.used_percent > 75 ? 'amber' : 'brand'}
          />
          {storage.full && (
            <p className="mt-2 text-xs text-red-600" role="alert">
              Your mailbox is full — sending is paused. Delete messages or upgrade the plan.
            </p>
          )}
          <p className="mt-2 text-[11px] text-slate-400">
            Synkra never auto-deletes mail (POPIA §: data is retained only while your account exists).
          </p>
        </Card>
      )}

      <PasswordCard onChanged={refresh} />
    </div>
  )
}

function PasswordCard({ onChanged }: { onChanged: () => Promise<void> }) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const r = await authApi.changePassword(current, next)
      useUI.getState().toast(
        'success',
        'Password changed',
        r.other_sessions_revoked ? 'All other sessions were signed out' : undefined
      )
      setCurrent('')
      setNext('')
      await onChanged()
    } catch (err) {
      toastError(err, 'Password change failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title="Change password" className="mt-4">
      <form onSubmit={submit} className="flex max-w-sm flex-col gap-3">
        <Field label="Current password" required>
          <Input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
        </Field>
        <Field label="New password" hint="revokes every other session" required>
          <Input type="password" value={next} onChange={(e) => setNext(e.target.value)} required minLength={8} />
        </Field>
        <Button type="submit" loading={busy} className="w-fit">
          Update password
        </Button>
      </form>
    </Card>
  )
}
