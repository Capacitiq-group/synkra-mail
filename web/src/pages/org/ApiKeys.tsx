// API keys (§13f — "the API is the product"): machine tokens that
// authenticate through the same session middleware. Token shown exactly once.

import { useCallback, useEffect, useState } from 'react'
import { orgApi } from '@/api/endpoints'
import { useUI, toastError } from '@/stores/ui'
import { formatDate } from '@/lib/format'
import type { APIKeyView } from '@/types/api'
import { PageHeader, Card } from '@/ui/data'
import { Badge, EmptyState, LoadingState } from '@/ui/feedback'
import { Button, Field, Input, Select } from '@/ui/primitives'

export default function ApiKeys() {
  const [keys, setKeys] = useState<APIKeyView[] | null>(null)
  const [secret, setSecret] = useState<{ name: string; prefix: string; token: string } | null>(null)

  const load = useCallback(() => {
    orgApi
      .apiKeys()
      .then((r) => setKeys(r.api_keys))
      .catch((e) => toastError(e, 'Could not load API keys'))
  }, [])

  useEffect(load, [load])

  if (!keys) return <LoadingState />

  return (
    <div className="mx-auto max-w-3xl p-6">
      <PageHeader
        title="API keys"
        subtitle="Machine access to /api/v1 — Authorization: Bearer sk_… on every call. Scoped to this organisation; impersonation rules don't apply to them."
      />
      <CreateCard
        onCreated={(s) => {
          setSecret(s)
          load()
        }}
      />

      {secret && (
        <div className="mt-4 rounded-lg border border-emerald-300 bg-emerald-50 p-3 text-sm" role="alert">
          <div className="font-medium text-emerald-800">
            Token for {secret.name} ({secret.prefix}…) — copy it now, it is never shown again:
          </div>
          <code className="mt-1 block break-all rounded bg-white px-2 py-1.5 font-mono text-xs">{secret.token}</code>
          <button
            className="mt-1.5 text-xs font-medium text-emerald-700 hover:underline"
            onClick={() => {
              void navigator.clipboard?.writeText(secret.token)
              useUI.getState().toast('success', 'Copied')
            }}
          >
            Copy
          </button>
        </div>
      )}

      <Card title={`Keys (${keys.length})`} className="mt-4">
        {keys.length === 0 ? (
          <EmptyState title="No API keys" detail="Create one for scripts, integrations and the ops tooling." />
        ) : (
          <ul className="divide-y divide-slate-100">
            {keys.map((k) => (
              <li key={k.id} className="flex items-center justify-between gap-3 py-2.5 text-sm">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-slate-800">{k.name}</span>
                    <span className="font-mono text-xs text-slate-400">{k.prefix}…</span>
                    {k.revoked_at && <Badge tone="red">revoked</Badge>}
                  </div>
                  <div className="text-xs text-slate-400">
                    created {formatDate(k.created_at)}
                    {k.last_used_at ? ` · last used ${formatDate(k.last_used_at)}` : ' · never used'}
                    {k.expires_at ? ` · expires ${formatDate(k.expires_at)}` : ''}
                  </div>
                </div>
                {!k.revoked_at && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      if (!window.confirm(`Revoke "${k.name}"? Clients using it will get 401s.`)) return
                      orgApi
                        .revokeAPIKey(k.id)
                        .then(() => {
                          useUI.getState().toast('success', 'Key revoked')
                          load()
                        })
                        .catch((e) => toastError(e, 'Revoke failed'))
                    }}
                  >
                    Revoke
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  )
}

function CreateCard({ onCreated }: { onCreated: (s: { name: string; prefix: string; token: string }) => void }) {
  const [name, setName] = useState('')
  const [days, setDays] = useState('90')
  const [busy, setBusy] = useState(false)

  return (
    <Card title="Create key">
      <form
        className="flex max-w-md flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          setBusy(true)
          orgApi
            .createAPIKey(name.trim(), days ? Number(days) : undefined)
            .then((r) => {
              onCreated({ name: r.name, prefix: r.prefix, token: r.token })
              setName('')
            })
            .catch((e2) => toastError(e2, 'Create failed'))
            .finally(() => setBusy(false))
        }}
      >
        <div className="grid grid-cols-2 gap-3">
          <Field label="Name" hint="what uses it" required>
            <Input value={name} onChange={(e) => setName(e.target.value)} required placeholder="billing sync" />
          </Field>
          <Field label="Expires">
            <Select value={days} onChange={(e) => setDays(e.target.value)}>
              <option value="30">30 days</option>
              <option value="90">90 days</option>
              <option value="365">1 year</option>
              <option value="">never</option>
            </Select>
          </Field>
        </div>
        <Button type="submit" loading={busy} className="w-fit">
          Create key
        </Button>
      </form>
    </Card>
  )
}
