// Org overview: organisation + subscription, the §13 tracking toggle
// (off by default — owner decision), and shared mailbox members.

import { useCallback, useEffect, useState } from 'react'
import { orgApi } from '@/api/endpoints'
import { useAuth } from '@/stores/auth'
import { useUI, toastError } from '@/stores/ui'
import { formatDate } from '@/lib/format'
import type { OrgDetail, SharedMember } from '@/types/api'
import { PageHeader, Card, KV } from '@/ui/data'
import { Badge, EmptyState, LoadingState } from '@/ui/feedback'
import { Button, Field, Select } from '@/ui/primitives'

export default function OrgHome() {
  const { me, refresh } = useAuth()
  const [detail, setDetail] = useState<OrgDetail | null>(null)
  const [trackingOn, setTrackingOn] = useState<boolean | null>(null)

  useEffect(() => {
    orgApi.org().then(setDetail).catch((e) => toastError(e, 'Could not load org'))
    // the tracking state arrives with a message's tracking_enabled; the PUT
    // response is the only toggle read — so we optimistically show "unknown"
    // until the first fetch: keep a local toggle without a GET (API has none).
  }, [])

  if (!me || !detail) return <LoadingState />
  const sub = detail.subscription as Record<string, unknown> | null

  const toggleTracking = (v: boolean) => {
    orgApi
      .setTracking(v)
      .then((r) => {
        setTrackingOn(r.tracking_enabled)
        useUI.getState().toast('success', `Open tracking ${r.tracking_enabled ? 'enabled' : 'disabled'}`)
        void refresh()
      })
      .catch((e) => toastError(e, 'Toggle failed'))
  }

  return (
    <div className="mx-auto max-w-3xl p-6">
      <PageHeader title={detail.org.name} subtitle="Organisation overview" />

      <Card title="Organisation">
        <KV label="Status">
          <Badge tone={detail.org.status === 'active' ? 'green' : 'red'}>{detail.org.status}</Badge>
        </KV>
        <KV label="Tier">{detail.org.tier}</KV>
        <KV label="Sending">
          <Badge tone={detail.org.sending_enabled ? 'green' : 'red'}>
            {detail.org.sending_enabled ? 'enabled' : 'disabled'}
          </Badge>
        </KV>
        <KV label="Review status">{detail.org.review_status}</KV>
        <KV label="Created">{formatDate(detail.org.created_at)}</KV>
        {sub && (
          <KV label="Plan / seats">
            {String((sub as { plan_name?: string; plan?: string }).plan_name ?? (sub as { plan?: string }).plan ?? 'plan')}
          </KV>
        )}
      </Card>

      <Card
        title="Open tracking (org-wide default)"
        className="mt-4"
        actions={
          <Button
            size="sm"
            variant={trackingOn ? 'danger' : 'primary'}
            onClick={() => toggleTracking(!trackingOn)}
            disabled={trackingOn === null}
          >
            {trackingOn ? 'Disable tracking' : 'Enable tracking'}
          </Button>
        }
      >
        <p className="text-sm text-slate-600">
          {trackingOn === null
            ? 'State unknown until the first toggle — the API exposes no read for this setting.'
            : trackingOn
              ? 'New outbound mail may include an open-tracking pixel. Recipients can see the "tracked" badge; per-message data stays in the honesty model (pixels/opens/links/clicks).'
              : 'Outbound mail carries no tracking pixels. Per-message overrides are possible at compose time.'}
        </p>
      </Card>

      <SharedMailboxesCard />
    </div>
  )
}

function SharedMailboxesCard() {
  const me = useAuth.getState().me
  const shared = me?.mailboxes?.filter((mb) => mb.kind === 'shared') ?? []
  const [openId, setOpenId] = useState<string | null>(null)
  const [members, setMembers] = useState<SharedMember[] | null>(null)
  const [users, setUsers] = useState<{ id: string; email: string }[]>([])

  const load = useCallback((id: string) => {
    orgApi
      .sharedMembers(id)
      .then((r) => setMembers(r.members))
      .catch((e) => toastError(e, 'Could not load members'))
  }, [])

  useEffect(() => {
    orgApi
      .users()
      .then((r) => setUsers(r.users.map((u) => ({ id: u.id, email: u.email }))))
      .catch(() => undefined)
  }, [])

  if (!shared.length)
    return (
      <Card title="Shared mailboxes" className="mt-4">
        <EmptyState title="No shared mailboxes" detail="Ask staff to create one (support@…) for your team." />
      </Card>
    )

  return (
    <Card title="Shared mailboxes" className="mt-4">
      <ul className="divide-y divide-slate-100">
        {shared.map((mb) => (
          <li key={mb.id} className="py-2">
            <div className="flex items-center justify-between text-sm">
              <span className="font-medium text-slate-700">
                {mb.name} <Badge tone={mb.send_status === 'sendable' ? 'green' : 'red'}>{mb.send_status}</Badge>
              </span>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  const next = openId === mb.id ? null : mb.id
                  setOpenId(next)
                  if (next) load(next)
                }}
              >
                {openId === mb.id ? 'Close' : `Members (${mb.owner ? 'admin' : 'view'})`}
              </Button>
            </div>
            {openId === mb.id && (
              <div className="mt-2">
                {!members ? (
                  <LoadingState label="Loading members…" />
                ) : (
                  <ul className="mb-2 text-xs text-slate-600">
                    {members.map((m) => (
                      <li key={m.user_id} className="flex items-center justify-between py-1">
                        <span>
                          {m.email} <Badge>{m.role}</Badge>
                        </span>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() =>
                            orgApi
                              .removeSharedMember(mb.id, m.user_id)
                              .then(() => load(mb.id))
                              .catch((e) => toastError(e, 'Remove failed'))
                          }
                        >
                          remove
                        </Button>
                      </li>
                    ))}
                  </ul>
                )}
                <AddMember
                  users={users.filter((u) => !members?.some((m) => m.user_id === u.id))}
                  onAdd={(userId) =>
                    orgApi
                      .addSharedMember(mb.id, userId, 'member')
                      .then(() => load(mb.id))
                      .catch((e) => toastError(e, 'Add failed'))
                  }
                />
              </div>
            )}
          </li>
        ))}
      </ul>
    </Card>
  )
}

function AddMember({
  users,
  onAdd,
}: {
  users: { id: string; email: string }[]
  onAdd: (userId: string) => void
}) {
  const [userId, setUserId] = useState('')
  if (!users.length) return <p className="text-xs text-slate-400">All org users are members.</p>
  return (
    <div className="flex items-end gap-2">
      <Field label="Add member">
        <Select value={userId} onChange={(e) => setUserId(e.target.value)}>
          <option value="">choose user…</option>
          {users.map((u) => (
            <option key={u.id} value={u.id}>
              {u.email}
            </option>
          ))}
        </Select>
      </Field>
      <Button size="sm" disabled={!userId} onClick={() => userId && onAdd(userId)} className="mb-0.5">
        Add
      </Button>
    </div>
  )
}
