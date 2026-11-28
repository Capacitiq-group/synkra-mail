// Shared banner + panel primitives (the zip keeps a local Banner inside
// MessageView; the phase-11 pages share these).
import type { ReactNode } from 'react';

const TONES = {
  success: { box: 'border-success/30 bg-success/5', title: 'text-success', icon: '✓' },
  info: { box: 'border-info/30 bg-info/5', title: 'text-info', icon: 'ℹ' },
  warning: { box: 'border-warning/30 bg-warning/5', title: 'text-warning', icon: '⚠' },
  danger: { box: 'border-danger/30 bg-danger/5', title: 'text-danger', icon: '✕' },
} as const;

export interface BannerProps {
  tone?: keyof typeof TONES;
  icon?: string;
  title: string;
  body?: string;
  children?: ReactNode;
}

export function Banner({ tone = 'info', title, body }: BannerProps) {
  const t = TONES[tone];
  return (
    <div className={`border rounded p-3 flex items-start gap-3 ${t.box}`}>
      <span className={`text-sm font-bold ${t.title}`}>{t.icon}</span>
      <div>
        <p className={`text-sm font-medium ${t.title}`}>{title}</p>
        {body && <p className="text-sm text-ink-soft mt-0.5">{body}</p>}
      </div>
    </div>
  );
}

export function Panel({ title, action, children }: { title?: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="border border-line rounded bg-surface p-4 space-y-3">
      {(title || action) && (
        <div className="flex items-center justify-between">
          {title && <h2 className="font-medium">{title}</h2>}
          {action}
        </div>
      )}
      {children}
    </section>
  );
}
