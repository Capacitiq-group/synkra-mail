// Insights: the §13 differentiators — deliverability funnel (30 days),
// health score, reliability monitor (14 days). Three tabs, one page.

import { useEffect, useState } from 'react'
import { orgApi } from '@/api/endpoints'
import { toastError } from '@/stores/ui'
import { formatPercent } from '@/lib/format'
import type { DeliverabilityStats, ReliabilityResp } from '@/types/api'
import { PageHeader, Card, StatBar, KV } from '@/ui/data'
import { Badge, EmptyState, LoadingState } from '@/ui/feedback'

type Tab = 'deliverability' | 'health' | 'reliability'

export default function Insights() {
  const [tab, setTab] = useState<Tab>('deliverability')
  const [stats, setStats] = useState<DeliverabilityStats | null>(null)
  const [health, setHealth] = useState<{ score: number; delivery_rate: number; hard_bounce_rate: number; sent: number; suppressed: number; window_days: number } | null>(null)
  const [rel, setRel] = useState<ReliabilityResp | null>(null)

  useEffect(() => {
    if (tab === 'deliverability' && !stats)
      orgApi.deliverability().then(setStats).catch((e) => toastError(e, 'Could not load deliverability'))
    if (tab === 'health' && !health)
      orgApi.health().then(setHealth).catch((e) => toastError(e, 'Could not load health'))
    if (tab === 'reliability' && !rel)
      orgApi.reliability().then(setRel).catch((e) => toastError(e, 'Could not load reliability'))
  }, [tab, stats, health, rel])

  return (
    <div className="mx-auto max-w-4xl p-6">
      <PageHeader title="Deliverability insights" subtitle="Where your mail actually lands — and why" />
      <div className="mb-4 flex gap-1 rounded-lg border border-slate-200 bg-white p-1 text-sm">
        {(['deliverability', 'health', 'reliability'] as Tab[]).map((t) => (
          <button
            key={t}
            className={`flex-1 rounded-md px-3 py-1.5 capitalize ${
              tab === t ? 'bg-brand-50 font-medium text-brand-700' : 'text-slate-500 hover:bg-slate-50'
            }`}
            onClick={() => setTab(t)}
          >
            {t}
          </button>
        ))}
      </div>

      {tab === 'deliverability' &&
        (!stats ? (
          <LoadingState />
        ) : (
          <div className="flex flex-col gap-4">
            <Card title={`Delivery funnel — last ${stats.window_days} days`}>
              <StatBar
                segments={[
                  { label: 'delivered', value: stats.delivered, color: 'bg-emerald-500' },
                  { label: 'deferred (in flight)', value: stats.deferred, color: 'bg-amber-400' },
                  { label: 'soft expired', value: stats.soft_expired, color: 'bg-orange-400' },
                  { label: 'bounced (hard)', value: stats.bounced, color: 'bg-red-500' },
                  { label: 'suppressed', value: stats.suppressed, color: 'bg-slate-400' },
                ]}
              />
              <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
                <Metric label="sent" value={stats.sent.toLocaleString()} />
                <Metric label="delivery rate" value={formatPercent(stats.delivery_rate)} tone={stats.delivery_rate >= 0.95 ? 'green' : 'amber'} />
                <Metric label="bounced" value={stats.bounced.toLocaleString()} />
                <Metric label="suppressed" value={stats.suppressed.toLocaleString()} />
              </div>
            </Card>
            {stats.by_domain.length > 0 && (
              <Card title="By recipient domain">
                <ul className="divide-y divide-slate-100 text-sm">
                  {stats.by_domain.map((d, i) => (
                    <li key={i} className="flex items-center justify-between py-2">
                      <span className="font-medium text-slate-700">{d.domain}</span>
                      <span className="text-xs text-slate-500">
                        sent {d.sent ?? '—'} · delivered {d.delivered ?? '—'} · bounced {d.bounced ?? '—'}
                      </span>
                    </li>
                  ))}
                </ul>
              </Card>
            )}
          </div>
        ))}

      {tab === 'health' &&
        (!health ? (
          <LoadingState />
        ) : (
          <Card title="Sender health score">
            <div className="flex items-center gap-6">
              <ScoreRing score={health.score} />
              <div className="min-w-0 flex-1">
                <KV label="Delivery rate">{formatPercent(health.delivery_rate)}</KV>
                <KV label="Hard bounce rate">
                  <span className={health.hard_bounce_rate > 0.05 ? 'text-red-600' : ''}>
                    {formatPercent(health.hard_bounce_rate)}
                  </span>
                </KV>
                <KV label="Sent (window)">{health.sent.toLocaleString()}</KV>
                <KV label="Suppressed">{health.suppressed.toLocaleString()}</KV>
                <KV label="Window">{health.window_days} days</KV>
              </div>
            </div>
            <p className="mt-3 text-xs text-slate-500">
              The score weighs delivery rate and hard bounces. Above 90 is healthy; below 70 review
              your lists (suppression is your friend — bounces from bought lists tank it).
            </p>
          </Card>
        ))}

      {tab === 'reliability' &&
        (!rel ? (
          <LoadingState />
        ) : rel.days.length === 0 ? (
          <EmptyState title="No delivery activity" />
        ) : (
          <Card title={`Reliability — last ${rel.window_days} days`}>
            <ul className="flex flex-col gap-2 text-xs">
              {rel.days.map((d) => {
                const total = Math.max(1, d.queued)
                return (
                  <li key={d.day} className="flex items-center gap-3">
                    <span className="w-24 shrink-0 text-slate-400">{d.day}</span>
                    <div className="h-4 min-w-0 flex-1 overflow-hidden rounded bg-slate-100">
                      <div className="h-full bg-emerald-500" style={{ width: `${(d.delivered / total) * 100}%` }} />
                      <div className="h-full bg-amber-400" style={{ width: `${(d.deferred / total) * 100}%`, marginTop: '-100%' }} />
                      <div className="h-full bg-red-500" style={{ width: `${(d.bounced / total) * 100}%`, marginTop: '-100%' }} />
                    </div>
                    <span className="w-40 shrink-0 text-right text-slate-500">
                      {d.delivered}/{d.queued} ok{d.bounced > 0 && `, ${d.bounced} bounced`}
                    </span>
                  </li>
                )
              })}
            </ul>
            <p className="mt-2 text-[11px] text-slate-400">green delivered · amber deferred · red bounced</p>
          </Card>
        ))}
    </div>
  )
}

function Metric({ label, value, tone }: { label: string; value: string; tone?: 'green' | 'amber' }) {
  return (
    <div className="rounded-lg bg-slate-50 p-3 text-center">
      <div
        className={`text-lg font-semibold ${
          tone === 'green' ? 'text-emerald-600' : tone === 'amber' ? 'text-amber-600' : 'text-slate-800'
        }`}
      >
        {value}
      </div>
      <div className="text-[10px] uppercase tracking-wide text-slate-400">{label}</div>
    </div>
  )
}

function ScoreRing({ score }: { score: number }) {
  const tone = score >= 90 ? 'text-emerald-500' : score >= 70 ? 'text-amber-500' : 'text-red-500'
  return (
    <div className="relative h-24 w-24 shrink-0">
      <svg viewBox="0 0 36 36" className="h-full w-full -rotate-90">
        <circle cx="18" cy="18" r="15.9" fill="none" className="stroke-slate-200" strokeWidth="3" />
        <circle
          cx="18"
          cy="18"
          r="15.9"
          fill="none"
          className={`${tone} stroke-current`}
          strokeWidth="3"
          strokeDasharray={`${score} 100`}
          strokeLinecap="round"
        />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        <span className={`text-2xl font-bold ${tone}`}>{score}</span>
        <span className="text-[9px] uppercase tracking-wide text-slate-400">/ 100</span>
      </div>
      <Badge tone={score >= 90 ? 'green' : score >= 70 ? 'amber' : 'red'} className="absolute -bottom-1 left-1/2 -translate-x-1/2">
        health
      </Badge>
    </div>
  )
}
