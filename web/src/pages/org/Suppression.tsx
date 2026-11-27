// Suppression — the POPIA opt-out register view. The API exposes list +
// delete (entries are added automatically on bounce/complaint/unsubscribe;
// there is no manual-add endpoint, which is deliberate: suppression is
// evidence-driven, see README).

import { useCallback, useEffect, useState } from 'react'
import { suppressionApi } from '@/api/endpoints'
import { useUI, toastError } from '@/stores/ui'
import { formatDate } from '@/lib/format'
import type { SuppressionRow } from '@/types/api'
import { PageHeader, Card } from '@/ui/data'
import { Badge, EmptyState, LoadingState } from '@/ui/feedback'
import { Button } from '@/ui/primitives'

const REASON_TONE: Record<string, 'red' | 'amber' | 'neutral'> = {
  bounce: 'red',
  complaint: 'red',
  unsubscribe: 'amber',
  manual: 'neutral',
}

export default function Suppression() {
  const [rows, setRows] = useState<SuppressionRow[] | null>(null)

  const load = useCallback(() => {
    suppressionApi
      .list()
      .then((r) => setRows(r.suppression))
      .catch((e) => toastError(e, 'Could not load suppression list'))
  }, [])

  useEffect(load, [load])

  if (!rows) return <LoadingState />

  return (
    <div className="mx-auto max-w-3xl p-6">
      <PageHeader
        title="Suppression register"
        subtitle="Addresses we never email again — bounces, complaints and unsubscribes (POPIA). Removing an entry re-allows mail to it; do this only when the underlying problem is fixed."
      />
      <Card>
        {rows.length === 0 ? (
          <EmptyState
            title="Register is empty"
            detail="Entries appear automatically when a recipient bounces, complains or unsubscribes."
          />
        ) : (
          <ul className="divide-y divide-slate-100">
            {rows.map((r) => (
              <li key={r.email} className="flex items-center justify-between gap-3 py-2 text-sm">
                <div className="min-w-0">
                  <div className="truncate font-medium text-slate-800">{r.email}</div>
                  <div className="text-xs text-slate-400">added {formatDate(r.created_at)}</div>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <Badge tone={REASON_TONE[r.reason.toLowerCase()] ?? 'neutral'}>{r.reason}</Badge>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      if (
                        !window.confirm(
                          `Remove ${r.email} from suppression? Only do this once the bounce/complaint cause is fixed.`
                        )
                      )
                        return
                      suppressionApi
                        .remove(r.email)
                        .then(() => {
                          useUI.getState().toast('success', `${r.email} removed from suppression`)
                          load()
                        })
                        .catch((e) => toastError(e, 'Remove failed'))
                    }}
                  >
                    Remove
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
