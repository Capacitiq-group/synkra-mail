// Webhooks (§13f): signed event delivery to your systems. The signing
// secret is shown exactly once (§7.1). Creation is guarded (no
// impersonated/API-key sessions) and idempotent.

import { useCallback, useEffect, useState } from 'react'
import { orgApi } from '@/api/endpoints'
import { useUI, toastError } from '@/stores/ui'
import type { WebhookView } from '@/types/api'
import { PageHeader, Card } from '@/ui/data'
import { Badge, EmptyState, LoadingState } from '@/ui/feedback'
import { Button, Field, Input } from '@/ui/primitives'

const EVENT_OPTIONS = [
  'delivery.delivered',
  'delivery.bounced',
  'delivery.deferred',
  'tracking.open',
  'tracking.click',
  'quarantine.held',
]

export default function Webhooks() {
  const [webhooks, setWebhooks] = useState<WebhookView[] | null>(null)
  const [secret, setSecret] = useState<{ url: string; events: string[]; secret: string } | null>(null)

  const load = useCallback(() => {
    orgApi
      .webhooks()
      .then((r) => setWebhooks(r.webhooks))
      .catch((e) => toastError(e, 'Could not load webhooks'))
  }, [])

  useEffect(load, [load])

  if (!webhooks) return <LoadingState />

  return (
    <div className="mx-auto max-w-3xl p-6">
      <PageHeader
        title="Webhooks"
        subtitle="Signed HTTP POSTs for the events you pick. Verify the HMAC signature with the secret before trusting a delivery."
      />
      <CreateCard
        onCreated={(s) => {
          setSecret(s)
          load()
        }}
      />

      {secret && (
        <div className="mt-4 rounded-lg border border-emerald-300 bg-emerald-50 p-3 text-sm" role="alert">
          <div className="font-medium text-emerald-800">Signing secret for {secret.url} — shown once:</div>
          <code className="mt-1 block break-all rounded bg-white px-2 py-1.5 font-mono text-xs">{secret.secret}</code>
          <button
            className="mt-1.5 text-xs font-medium text-emerald-700 hover:underline"
            onClick={() => {
              void navigator.clipboard?.writeText(secret.secret)
              useUI.getState().toast('success', 'Copied')
            }}
          >
            Copy
          </button>
        </div>
      )}

      <Card title={`Endpoints (${webhooks.length})`} className="mt-4">
        {webhooks.length === 0 ? (
          <EmptyState title="No webhooks" detail="Register an endpoint to receive signed events." />
        ) : (
          <ul className="divide-y divide-slate-100">
            {webhooks.map((w) => (
              <li key={w.id} className="py-3 text-sm">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate font-medium text-slate-800">{w.url}</div>
                    <div className="mt-1 flex flex-wrap gap-1">
                      {w.events.map((e) => (
                        <Badge key={e}>{e}</Badge>
                      ))}
                    </div>
                  </div>
                  <div className="flex shrink-0 gap-1">
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() =>
                        orgApi
                          .testWebhook(w.id)
                          .then(() => useUI.getState().toast('success', 'Test event sent'))
                          .catch((e) => toastError(e, 'Test failed'))
                      }
                    >
                      Test
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        if (!window.confirm(`Delete webhook ${w.url}?`)) return
                        orgApi
                          .deleteWebhook(w.id)
                          .then(() => {
                            useUI.getState().toast('success', 'Webhook deleted')
                            load()
                          })
                          .catch((e) => toastError(e, 'Delete failed'))
                      }}
                    >
                      🗑️
                    </Button>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  )
}

function CreateCard({ onCreated }: { onCreated: (s: { url: string; events: string[]; secret: string }) => void }) {
  const [url, setUrl] = useState('')
  const [events, setEvents] = useState<string[]>(['delivery.delivered', 'delivery.bounced'])
  const [busy, setBusy] = useState(false)

  return (
    <Card title="Register endpoint">
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          setBusy(true)
          orgApi
            .createWebhook(url.trim(), events, crypto.randomUUID())
            .then((r) => {
              onCreated({ url: r.url, events: r.events, secret: r.secret })
              setUrl('')
            })
            .catch((e2) => toastError(e2, 'Create failed'))
            .finally(() => setBusy(false))
        }}
      >
        <Field label="HTTPS endpoint" hint="we POST signed JSON" required>
          <Input type="url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://ops.acme.co.za/hooks/synkra" required />
        </Field>
        <Field label="Events">
          <div className="flex flex-wrap gap-3 pt-1">
            {EVENT_OPTIONS.map((ev) => (
              <label key={ev} className="flex items-center gap-1.5 text-xs text-slate-600">
                <input
                  type="checkbox"
                  checked={events.includes(ev)}
                  onChange={(e) => setEvents(e.target.checked ? [...events, ev] : events.filter((x) => x !== ev))}
                />
                <code className="font-mono">{ev}</code>
              </label>
            ))}
          </div>
        </Field>
        <Button type="submit" loading={busy} disabled={!events.length} className="w-fit">
          Create webhook
        </Button>
      </form>
    </Card>
  )
}
