// Aliases & forwards (routing addresses). Creation is an org-admin action
// server-side (audit item 11) and uses an Idempotency-Key — retried submits
// replay instead of double-creating (register §1.9).

import { useCallback, useEffect, useState } from 'react'
import { addressesApi, domainsApi } from '@/api/endpoints'
import { useAuth } from '@/stores/auth'
import { useUI, toastError } from '@/stores/ui'
import type { AddressView, DomainView } from '@/types/api'
import { PageHeader, Card } from '@/ui/data'
import { Badge, EmptyState, LoadingState } from '@/ui/feedback'
import { Button, Field, Input, Select } from '@/ui/primitives'
import { isValidEmail } from '@/lib/format'

export default function Aliases() {
  const me = useAuth((s) => s.me)
  const [addresses, setAddresses] = useState<AddressView[] | null>(null)
  const [domains, setDomains] = useState<DomainView[]>([])

  const load = useCallback(() => {
    addressesApi
      .list()
      .then((r) => setAddresses(r.addresses))
      .catch((e) => toastError(e, 'Could not load addresses'))
  }, [])

  useEffect(() => {
    load()
    domainsApi
      .list()
      .then((r) => setDomains(r.domains.filter((d) => d.status === 'active' && !d.is_platform)))
      .catch(() => undefined)
  }, [load])

  const remove = async (a: AddressView) => {
    if (!window.confirm(`Remove ${a.email ?? a.local}?`)) return
    try {
      await addressesApi.remove(a.id)
      useUI.getState().toast('success', 'Address removed')
      load()
    } catch (err) {
      toastError(err, 'Remove failed')
    }
  }

  const isAdmin = me?.user.role === 'owner' || me?.user.role === 'admin'
  const active = domains[0]

  if (!addresses) return <LoadingState />

  return (
    <div className="mx-auto max-w-3xl p-6">
      <PageHeader
        title="Aliases & forwards"
        subtitle="Routing addresses on your verified domains. Aliases deliver into a mailbox; forwards relay elsewhere."
      />

      {isAdmin && active && <CreateCard domains={domains} onCreated={load} />}

      <Card title={`Addresses (${addresses.length})`} className="mt-4">
        {addresses.length === 0 ? (
          <EmptyState title="No aliases yet" detail="Create one above to start routing mail." />
        ) : (
          <ul className="divide-y divide-slate-100">
            {addresses.map((a) => (
              <li key={a.id} className="flex items-center justify-between gap-3 py-2 text-sm">
                <div className="min-w-0">
                  <div className="truncate font-medium text-slate-800">
                    {a.email ?? `${a.local}@${a.domain ?? '?'}`}
                  </div>
                  <div className="text-xs text-slate-400">
                    {a.kind}
                    {a.forward_target ? ` → ${a.forward_target}` : ''}
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <Badge tone={a.kind === 'user' ? 'green' : a.kind === 'forward' ? 'amber' : 'brand'}>
                    {a.kind}
                  </Badge>
                  {a.kind !== 'user' && (
                    <Button variant="ghost" size="sm" onClick={() => void remove(a)}>
                      🗑️
                    </Button>
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  )
}

function CreateCard({ domains, onCreated }: { domains: DomainView[]; onCreated: () => void }) {
  const [local, setLocal] = useState('')
  const [domainId, setDomainId] = useState(domains[0]?.id ?? '')
  const [kind, setKind] = useState<'alias' | 'forward'>('alias')
  const [mailboxId, setMailboxId] = useState('')
  const [forwardTarget, setForwardTarget] = useState('')
  const [busy, setBusy] = useState(false)
  const me = useAuth.getState().me

  useEffect(() => {
    if (!mailboxId && me?.mailboxes?.length) setMailboxId(me.mailboxes[0].id)
  }, [me, mailboxId])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!local.trim()) return
    setBusy(true)
    try {
      await addressesApi.create(
        {
          local: local.trim().toLowerCase(),
          domain_id: domainId,
          mailbox_id: kind === 'alias' ? mailboxId : undefined,
          kind,
          forward_target: kind === 'forward' ? forwardTarget.trim() : undefined,
        },
        crypto.randomUUID()
      )
      useUI.getState().toast('success', 'Address created')
      setLocal('')
      setForwardTarget('')
      onCreated()
    } catch (err) {
      toastError(err, 'Create failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title="Create address">
      <form onSubmit={submit} className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field label="Local part" hint="the bit before @" required>
          <Input
            value={local}
            onChange={(e) => setLocal(e.target.value.replace(/[^a-z0-9._+-]/gi, ''))}
            placeholder="sales"
            required
          />
        </Field>
        <Field label="Domain" required>
          <Select value={domainId} onChange={(e) => setDomainId(e.target.value)}>
            {domains.map((d) => (
              <option key={d.id} value={d.id}>
                @{d.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Type">
          <Select value={kind} onChange={(e) => setKind(e.target.value as 'alias' | 'forward')}>
            <option value="alias">Alias → deliver to mailbox</option>
            <option value="forward">Forward → relay elsewhere</option>
          </Select>
        </Field>
        {kind === 'alias' ? (
          <Field label="Deliver to mailbox">
            <Select value={mailboxId} onChange={(e) => setMailboxId(e.target.value)}>
              {me?.mailboxes?.map((mb) => (
                <option key={mb.id} value={mb.id}>
                  {mb.name}
                </option>
              ))}
            </Select>
          </Field>
        ) : (
          <Field label="Forward to" hint="external address" required>
            <Input
              type="email"
              value={forwardTarget}
              onChange={(e) => setForwardTarget(e.target.value)}
              placeholder="ops@elsewhere.co.za"
              required
            />
          </Field>
        )}
        <div className="sm:col-span-2">
          <Button type="submit" loading={busy} disabled={kind === 'forward' && !isValidEmail(forwardTarget)}>
            Create address
          </Button>
        </div>
      </form>
    </Card>
  )
}
