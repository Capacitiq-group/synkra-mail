// Quarantine (§2.11): messages held by the verdict engines; release on review.

import { useCallback, useEffect, useState } from 'react'
import { orgApi } from '@/api/endpoints'
import { useUI, toastError } from '@/stores/ui'
import { formatDateTime } from '@/lib/format'
import type { QuarantineRow } from '@/types/api'
import { PageHeader, Card } from '@/ui/data'
import { Badge, EmptyState, LoadingState } from '@/ui/feedback'
import { Button } from '@/ui/primitives'

export default function Quarantine() {
  const [rows, setRows] = useState<QuarantineRow[] | null>(null)

  const load = useCallback(() => {
    orgApi
      .quarantine()
      .then((r) => setRows(r.quarantine))
      .catch((e) => toastError(e, 'Could not load quarantine'))
  }, [])

  useEffect(load, [load])

  if (!rows) return <LoadingState />

  return (
    <div className="mx-auto max-w-4xl p-6">
      <PageHeader
        title="Quarantine"
        subtitle="Messages held before delivery. Releasing delivers the original message to its recipients."
        actions={
          <Button variant="secondary" size="sm" onClick={load}>
            Refresh
          </Button>
        }
      />
      <Card>
        {rows.length === 0 ? (
          <EmptyState title="Quarantine is empty" detail="Held messages appear here for review." />
        ) : (
          <ul className="divide-y divide-slate-100">
            {rows.map((r) => (
              <li key={r.id} className="flex items-start justify-between gap-3 py-3 text-sm">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-medium text-slate-800">{r.subject || '(no subject)'}</span>
                    <Badge tone={r.score >= 8 ? 'red' : 'amber'}>score {r.score.toFixed(1)}</Badge>
                    <Badge>{r.reason}</Badge>
                  </div>
                  <div className="mt-0.5 truncate text-xs text-slate-400">{r.snippet}</div>
                  <div className="mt-0.5 text-[11px] text-slate-400">
                    held {formatDateTime(r.created_at)} · expires {formatDateTime(r.expires_at)}
                  </div>
                </div>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => {
                    if (!window.confirm('Release this message to its recipients?')) return
                    orgApi
                      .releaseQuarantine(r.id)
                      .then(() => {
                        useUI.getState().toast('success', 'Message released')
                        load()
                      })
                      .catch((e) => toastError(e, 'Release failed'))
                  }}
                >
                  Release
                </Button>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  )
}
