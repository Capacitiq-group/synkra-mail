// Feedback primitives: badges, empty states, toasts, progress.

import { useEffect, type ReactNode } from 'react'
import { useUI } from '@/stores/ui'
import { Spinner } from './primitives'

export function Badge({
  tone = 'neutral',
  children,
  className = '',
}: {
  tone?: 'neutral' | 'green' | 'red' | 'amber' | 'brand'
  children: ReactNode
  className?: string
}) {
  const TONE = {
    neutral: 'bg-slate-100 text-slate-600',
    green: 'bg-emerald-100 text-emerald-700',
    red: 'bg-red-100 text-red-700',
    amber: 'bg-amber-100 text-amber-700',
    brand: 'bg-brand-100 text-brand-700',
  } as const
  return (
    <span
      className={`inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium ${TONE[tone]} ${className}`}
    >
      {children}
    </span>
  )
}

export function EmptyState({
  icon = '✉️',
  title,
  detail,
  action,
}: {
  icon?: string
  title: string
  detail?: string
  action?: ReactNode
}) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 p-10 text-center">
      <div className="text-4xl" aria-hidden>
        {icon}
      </div>
      <div className="text-sm font-medium text-slate-700">{title}</div>
      {detail && <div className="max-w-sm text-xs text-slate-500">{detail}</div>}
      {action}
    </div>
  )
}

export function ErrorState({ message, hint, onRetry }: { message: string; hint?: string; onRetry?: () => void }) {
  return (
    <EmptyState
      icon="⚠️"
      title={message}
      detail={hint}
      action={
        onRetry && (
          <button className="text-xs font-medium text-brand-600 hover:underline" onClick={onRetry}>
            Retry
          </button>
        )
      }
    />
  )
}

export function LoadingState({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="flex flex-1 items-center justify-center gap-2 p-10 text-sm text-slate-500">
      <Spinner className="h-4 w-4" /> {label}
    </div>
  )
}

export function ProgressBar({ percent, tone = 'brand' }: { percent: number; tone?: 'brand' | 'red' | 'amber' }) {
  const COLOR = { brand: 'bg-brand-500', red: 'bg-red-500', amber: 'bg-amber-500' } as const
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-200" role="progressbar" aria-valuenow={Math.round(percent)}>
      <div
        className={`h-full rounded-full transition-all ${COLOR[tone]}`}
        style={{ width: `${Math.min(100, Math.max(2, percent))}%` }}
      />
    </div>
  )
}

export function Toaster() {
  const { toasts, dismiss } = useUI()
  useEffect(() => {
    if (!toasts.length) return
    const timers = toasts.map((t) => setTimeout(() => dismiss(t.id), t.kind === 'error' ? 8000 : 4000))
    return () => timers.forEach(clearTimeout)
  }, [toasts, dismiss])

  const TONE = {
    info: 'border-slate-300 bg-white',
    success: 'border-emerald-300 bg-emerald-50',
    error: 'border-red-300 bg-red-50',
  } as const

  return (
    <div className="pointer-events-none fixed bottom-4 right-4 z-50 flex w-80 flex-col gap-2" role="status" aria-live="polite">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`pointer-events-auto rounded-lg border p-3 text-sm shadow-lg ${TONE[t.kind]}`}
          onClick={() => dismiss(t.id)}
        >
          <div className="font-medium text-slate-800">{t.title}</div>
          {t.detail && <div className="mt-0.5 text-xs text-slate-500">{t.detail}</div>}
        </div>
      ))}
    </div>
  )
}
