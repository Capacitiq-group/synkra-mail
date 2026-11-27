// Domains & DKIM: register, verify, DNS record inspector, staged DKIM
// rotation (the DNS daemon serves the old + new selectors during staging —
// §4b). Creation is admin-only and idempotent (§1.9).

import { useCallback, useEffect, useState } from 'react'
import { domainsApi } from '@/api/endpoints'
import { useUI, toastError } from '@/stores/ui'
import { formatDate } from '@/lib/format'
import type { DomainDNS, DomainView } from '@/types/api'
import { PageHeader, Card, KV } from '@/ui/data'
import { Badge, EmptyState, LoadingState } from '@/ui/feedback'
import { Button, Field, Input } from '@/ui/primitives'

export default function Domains() {
  const [domains, setDomains] = useState<DomainView[] | null>(null)

  const load = useCallback(() => {
    domainsApi
      .list()
      .then((r) => setDomains(r.domains))
      .catch((e) => toastError(e, 'Could not load domains'))
  }, [])

  useEffect(load, [load])

  if (!domains) return <LoadingState />

  return (
    <div className="mx-auto max-w-3xl p-6">
      <PageHeader
        title="Domains & DKIM"
        subtitle="Verified sending domains, DNS records and key rotation"
      />
      <CreateCard onCreated={load} />

      {domains.length === 0 ? (
        <Card title="Domains" className="mt-4">
          <EmptyState title="No domains yet" detail="Register your domain to start sending from it." />
        </Card>
      ) : (
        domains.map((d) => (
          <DomainCard key={d.id} domain={d} onChanged={load} />
        ))
      )}
    </div>
  )
}

function CreateCard({ onCreated }: { onCreated: () => void }) {
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)

  return (
    <Card title="Register domain">
      <form
        className="flex max-w-md items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          setBusy(true)
          domainsApi
            .create(name.trim().toLowerCase(), crypto.randomUUID())
            .then(() => {
              useUI.getState().toast('success', 'Domain registered — add the DNS records, then verify')
              setName('')
              onCreated()
            })
            .catch((e2) => toastError(e2, 'Registration failed'))
            .finally(() => setBusy(false))
        }}
      >
        <Field label="Domain name" hint="e.g. acme.co.za" required>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value.replace(/[^a-z0-9.-]/gi, ''))}
            placeholder="acme.co.za"
            required
          />
        </Field>
        <Button type="submit" loading={busy} className="mb-0.5">
          Register
        </Button>
      </form>
    </Card>
  )
}

function DomainCard({ domain, onChanged }: { domain: DomainView; onChanged: () => void }) {
  const [open, setOpen] = useState(false)
  const [dns, setDns] = useState<DomainDNS | null>(null)
  const [dnsErr, setDnsErr] = useState<string | null>(null)
  const [rotating, setRotating] = useState(false)

  const verify = () => {
    domainsApi
      .verify(domain.id)
      .then((r) => {
        const ok = Object.values(r).some((v) => v === true) ?? false
        useUI.getState().toast(ok ? 'success' : 'error', ok ? 'Domain verified' : 'Verification failed', ok ? undefined : 'Check the DNS records below — propagation can take a few minutes')
        onChanged()
      })
      .catch((e) => toastError(e, 'Verify failed'))
  }

  const loadDns = () => {
    setDnsErr(null)
    domainsApi
      .dns(domain.id)
      .then(setDns)
      .catch((e) => setDnsErr((e as Error).message))
  }

  return (
    <Card
      title={domain.name}
      className="mt-4"
      actions={
        <div className="flex items-center gap-2">
          <Badge tone={domain.status === 'active' ? 'green' : domain.status === 'pending' ? 'amber' : 'neutral'}>
            {domain.status}
          </Badge>
          <Button size="sm" variant="secondary" onClick={verify}>
            Verify DNS
          </Button>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => {
              setOpen((v) => !v)
              if (!open) loadDns()
            }}
          >
            {open ? 'Hide records' : 'DNS records'}
          </Button>
        </div>
      }
    >
      <KV label="DKIM selector">
        <code className="font-mono text-xs">{domain.dkim_selector || '—'}</code>
      </KV>
      <KV label="Verified">{domain.verified_at ? formatDate(domain.verified_at) : 'not yet'}</KV>
      <KV label="Platform domain">{domain.is_platform ? 'yes' : 'no'}</KV>

      {open && (
        <div className="mt-3">
          {dnsErr && <p className="text-xs text-red-600">Could not load records: {dnsErr}</p>}
          {!dns && !dnsErr && <LoadingState label="Loading DNS…" />}
          {dns && (
            <>
              {dns.records?.length ? (
                <div className="overflow-x-auto">
                  <table className="w-full text-left text-xs">
                    <thead>
                      <tr className="border-b border-slate-200 text-slate-400">
                        <th className="py-1 pr-3">Type</th>
                        <th className="py-1 pr-3">Host / name</th>
                        <th className="py-1 pr-3">Value</th>
                        <th className="py-1">Purpose</th>
                      </tr>
                    </thead>
                    <tbody>
                      {dns.records.map((r, i) => (
                        <tr key={i} className="border-b border-slate-50 align-top">
                          <td className="py-1.5 pr-3 font-mono">{r.type}</td>
                          <td className="py-1.5 pr-3 font-mono">{r.host ?? r.name}</td>
                          <td className="max-w-96 break-all py-1.5 pr-3 font-mono text-slate-600">{r.value}</td>
                          <td className="py-1.5 text-slate-400">{r.purpose}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <EmptyState title="No records returned" />
              )}

              <div className="mt-3 flex flex-wrap gap-2">
                <Button
                  size="sm"
                  variant="secondary"
                  loading={rotating}
                  onClick={() => {
                    setRotating(true)
                    domainsApi
                      .rotateDKIM(domain.id)
                      .then(() => {
                        useUI.getState().toast('success', 'DKIM rotation staged', 'Both selectors now serve; publish the new DNS record, then activate')
                        loadDns()
                      })
                      .catch((e) => toastError(e, 'Rotation failed'))
                      .finally(() => setRotating(false))
                  }}
                >
                  Stage DKIM rotation
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() =>
                    domainsApi
                      .activateDKIM(domain.id)
                      .then(() => {
                        useUI.getState().toast('success', 'New DKIM key activated')
                        onChanged()
                        loadDns()
                      })
                      .catch((e) => toastError(e, 'Activation failed'))
                  }
                >
                  Activate rotated key
                </Button>
                <Button size="sm" variant="ghost" onClick={loadDns}>
                  Refresh
                </Button>
              </div>
              <p className="mt-2 text-[11px] text-slate-400">
                Staging serves old + new selectors in parallel (§4b): sign with the new key while
                receivers still validate the old — flip only after the new DNS TXT record is visible.
              </p>
            </>
          )}
        </div>
      )}
    </Card>
  )
}
