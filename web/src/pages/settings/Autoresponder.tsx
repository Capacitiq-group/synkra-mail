// Autoresponder (§12): enabled/subject/body + starts_at/ends_at — the
// multi-period window (catalog Part 5) maps straight onto the API's
// nullable timestamps. PUT /mailboxes/{id}/autoresponder.

import { useEffect, useState } from 'react'
import { featuresApi } from '@/api/endpoints'
import { useAuth } from '@/stores/auth'
import { useUI, toastError } from '@/stores/ui'
import { formatDateTime } from '@/lib/format'
import type { Autoresponder } from '@/types/api'
import { PageHeader, Card } from '@/ui/data'
import { Badge } from '@/ui/feedback'
import { Button, Field, Input, Textarea } from '@/ui/primitives'

function toLocalInput(iso: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export default function AutoresponderPage() {
  const me = useAuth((s) => s.me)
  const mailbox = me?.mailboxes?.[0]
  const [ar, setAr] = useState<Autoresponder | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!mailbox) return
    featuresApi
      .autoresponder(mailbox.id)
      .then(setAr)
      .catch((e) => toastError(e, 'Could not load autoresponder'))
  }, [mailbox])

  if (!mailbox || !ar) return null

  const activeNow =
    ar.enabled &&
    (!ar.starts_at || new Date(ar.starts_at) <= new Date()) &&
    (!ar.ends_at || new Date(ar.ends_at) >= new Date())

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (ar.enabled && (!ar.subject.trim() || !ar.body.trim())) {
      toastError({ message: 'Enabled autoresponders need subject and body' })
      return
    }
    setBusy(true)
    try {
      await featuresApi.setAutoresponder(mailbox.id, ar)
      useUI.getState().toast('success', 'Autoresponder saved')
    } catch (err) {
      toastError(err, 'Save failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mx-auto max-w-3xl p-6">
      <PageHeader
        title="Autoresponder"
        subtitle="Automatic replies with an optional schedule window"
        actions={
          ar.enabled ? (
            <Badge tone={activeNow ? 'green' : 'amber'}>{activeNow ? 'active now' : 'scheduled'}</Badge>
          ) : (
            <Badge>off</Badge>
          )
        }
      />

      <Card>
        <form onSubmit={save} className="flex flex-col gap-3">
          <label className="flex items-center gap-2 text-sm font-medium text-slate-700">
            <input
              type="checkbox"
              checked={ar.enabled}
              onChange={(e) => setAr({ ...ar, enabled: e.target.checked })}
            />
            Enabled
          </label>

          <Field label="Subject" required={ar.enabled}>
            <Input
              value={ar.subject}
              onChange={(e) => setAr({ ...ar, subject: e.target.value })}
              placeholder="Out of office"
              disabled={!ar.enabled}
            />
          </Field>
          <Field label="Body" hint="plain text reply sent once per sender" required={ar.enabled}>
            <Textarea
              value={ar.body}
              onChange={(e) => setAr({ ...ar, body: e.target.value })}
              rows={5}
              placeholder="I'm away until Monday with limited email access. For urgent matters call …"
              disabled={!ar.enabled}
            />
          </Field>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Starts at" hint="empty = immediately">
              <Input
                type="datetime-local"
                value={toLocalInput(ar.starts_at)}
                onChange={(e) => setAr({ ...ar, starts_at: e.target.value ? new Date(e.target.value).toISOString() : null })}
                disabled={!ar.enabled}
              />
            </Field>
            <Field label="Ends at" hint="empty = until switched off">
              <Input
                type="datetime-local"
                value={toLocalInput(ar.ends_at)}
                onChange={(e) => setAr({ ...ar, ends_at: e.target.value ? new Date(e.target.value).toISOString() : null })}
                disabled={!ar.enabled}
              />
            </Field>
          </div>

          {ar.updated_at && (
            <p className="text-[11px] text-slate-400">last updated {formatDateTime(ar.updated_at)}</p>
          )}

          <Button type="submit" loading={busy} className="w-fit">
            Save autoresponder
          </Button>
        </form>
      </Card>
    </div>
  )
}
