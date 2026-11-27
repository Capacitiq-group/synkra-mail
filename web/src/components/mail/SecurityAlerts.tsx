// Spoof & authentication badges + alert banners (reading pane).

import type { MessageView } from '@/types/api'
import { analyzeMessage, authResultsStatus, type SpoofAlert } from '@/lib/spoof'
import { Badge } from '@/ui/feedback'

export function AuthBadges({ message }: { message: MessageView }) {
  const ar = authResultsStatus(message.auth_results)
  const dot = (v: string) =>
    v === 'pass' ? 'green' : v === 'fail' || v === 'softfail' ? 'red' : 'neutral'
  return (
    <div className="flex flex-wrap items-center gap-1" title="SPF / DKIM / DMARC results from the ingest pipeline">
      <Badge tone={dot(ar.spf)}>SPF {ar.spf}</Badge>
      <Badge tone={dot(ar.dkim)}>DKIM {ar.dkim}</Badge>
      <Badge tone={dot(ar.dmarc)}>DMARC {ar.dmarc}</Badge>
      {message.tracking_enabled && <Badge tone="brand">tracked</Badge>}
    </div>
  )
}

const SEVERITY_TONE = {
  high: 'border-red-300 bg-red-50 text-red-800',
  medium: 'border-amber-300 bg-amber-50 text-amber-800',
  low: 'border-slate-300 bg-slate-50 text-slate-600',
} as const

const SEVERITY_ICON = { high: '🚨', medium: '⚠️', low: 'ℹ️' } as const

export function SecurityAlerts({ alerts }: { alerts: SpoofAlert[] }) {
  if (!alerts.length) return null
  return (
    <div className="flex flex-col gap-1.5">
      {alerts.map((a, i) => (
        <div
          key={i}
          role="alert"
          className={`flex items-start gap-2 rounded-lg border px-3 py-2 text-xs ${SEVERITY_TONE[a.severity]}`}
        >
          <span aria-hidden>{SEVERITY_ICON[a.severity]}</span>
          <div>
            <div className="font-semibold">{a.title}</div>
            <div className="mt-0.5 opacity-90">{a.detail}</div>
          </div>
        </div>
      ))}
    </div>
  )
}

export function alertsFor(message: MessageView): SpoofAlert[] {
  return analyzeMessage(message)
}
