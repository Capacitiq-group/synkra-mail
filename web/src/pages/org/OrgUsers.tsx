// Org users: list, invite (temp password set here — the backend returns the
// created user + mailbox), suspend/resume, revoke sessions.

import { useCallback, useEffect, useState } from 'react'
import { orgApi } from '@/api/endpoints'
import { useUI, toastError } from '@/stores/ui'
import { formatDate } from '@/lib/format'
import type { UserView } from '@/types/api'
import { PageHeader, Card } from '@/ui/data'
import { Badge, EmptyState, LoadingState } from '@/ui/feedback'
import { Button, Field, Input } from '@/ui/primitives'

export default function OrgUsers() {
  const [users, setUsers] = useState<UserView[] | null>(null)

  const load = useCallback(() => {
    orgApi
      .users()
      .then((r) => setUsers(r.users))
      .catch((e) => toastError(e, 'Could not load users'))
  }, [])

  useEffect(load, [load])

  if (!users) return <LoadingState />

  return (
    <div className="mx-auto max-w-3xl p-6">
      <PageHeader title="Users" subtitle="Members of your organisation" />
      <InviteCard onCreated={load} />
      <Card title={`Users (${users.length})`} className="mt-4">
        {users.length === 0 ? (
          <EmptyState title="No users" />
        ) : (
          <ul className="divide-y divide-slate-100">
            {users.map((u) => (
              <li key={u.id} className="flex items-center justify-between gap-3 py-2.5 text-sm">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-slate-800">{u.display_name || u.email}</span>
                    <Badge tone={u.role === 'owner' ? 'brand' : 'neutral'}>{u.role}</Badge>
                    <Badge tone={u.status === 'active' ? 'green' : 'red'}>{u.status}</Badge>
                  </div>
                  <div className="text-xs text-slate-400">
                    {u.email} · joined {formatDate(u.created_at)}
                  </div>
                </div>
                <div className="flex shrink-0 gap-1">
                  {u.status === 'active' ? (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() =>
                        orgApi
                          .suspendUser(u.id)
                          .then(() => {
                            useUI.getState().toast('success', 'User suspended')
                            load()
                          })
                          .catch((e) => toastError(e, 'Suspend failed'))
                      }
                    >
                      Suspend
                    </Button>
                  ) : (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() =>
                        orgApi
                          .resumeUser(u.id)
                          .then(() => {
                            useUI.getState().toast('success', 'User resumed')
                            load()
                          })
                          .catch((e) => toastError(e, 'Resume failed'))
                      }
                    >
                      Resume
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() =>
                      orgApi
                        .revokeUserSessions(u.id)
                        .then(() => useUI.getState().toast('success', 'Sessions revoked'))
                        .catch((e) => toastError(e, 'Revoke failed'))
                    }
                    title="Force sign-out on all devices"
                  >
                    Kill sessions
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  )
}

function InviteCard({ onCreated }: { onCreated: () => void }) {
  const [email, setEmail] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const r = await orgApi.invite(email.trim(), displayName.trim(), password)
      useUI.getState().toast(
        'success',
        `${r.user.email} invited`,
        'Share the temporary password securely — they should change it at first sign-in'
      )
      setEmail('')
      setDisplayName('')
      setPassword('')
      onCreated()
    } catch (err) {
      toastError(err, 'Invite failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title="Invite user">
      <form onSubmit={submit} className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <Field label="Email" required>
          <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </Field>
        <Field label="Display name">
          <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </Field>
        <Field label="Temporary password" hint="share securely" required>
          <Input
            type="text"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            minLength={8}
            placeholder="min 8 chars"
          />
        </Field>
        <div className="sm:col-span-3">
          <Button type="submit" loading={busy}>
            Create user
          </Button>
        </div>
      </form>
    </Card>
  )
}
