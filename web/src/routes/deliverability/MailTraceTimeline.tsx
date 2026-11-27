import { Icon, type IconName } from '@/design/icons';
import { formatDateTime } from '@/lib/time';

const STAGE_META: Record<string, { label: string; icon: IconName; tone: 'neutral' | 'accent' | 'success' | 'warning' | 'danger' }> = {
  submitted: { label: 'Submitted', icon: 'mail', tone: 'neutral' },
  authenticated: { label: 'Authentication checked', icon: 'shield-check', tone: 'success' },
  queued: { label: 'Queued', icon: 'clock', tone: 'neutral' },
  remote_contacted: { label: 'Recipient server contacted', icon: 'external-link', tone: 'accent' },
  remote_response: { label: 'Recipient response received', icon: 'info', tone: 'neutral' },
  delivered: { label: 'Delivered', icon: 'check-circle', tone: 'success' },
  deferred: { label: 'Deferred', icon: 'clock', tone: 'warning' },
  bounced: { label: 'Bounced', icon: 'x-circle', tone: 'danger' },
};

export function MailTraceTimeline({ hops }: { hops: Array<{ stage: string; at: string; detail: string; duration_ms?: number }> }) {
  return (
    <ol className="p-4 space-y-0">
      {hops.map((hop, i) => {
        const meta = STAGE_META[hop.stage] ?? { label: hop.stage, icon: 'info' as IconName, tone: 'neutral' as const };
        const isLast = i === hops.length - 1;
        return (
          <li key={i} className="relative pl-8 pb-4">
            {!isLast && (
              <span
                aria-hidden
                className="absolute left-3 top-6 bottom-0 w-px bg-line"
                style={{ left: '11px' }}
              />
            )}
            <span
              className={`absolute left-0 top-0 w-6 h-6 rounded-full flex items-center justify-center ${
                meta.tone === 'success' ? 'bg-success-soft text-success'
                : meta.tone === 'danger' ? 'bg-danger-soft text-danger'
                : meta.tone === 'warning' ? 'bg-warning-soft text-warning'
                : meta.tone === 'accent' ? 'bg-accent-soft text-accent'
                : 'bg-canvas text-ink-soft'
              }`}
            >
              <Icon name={meta.icon} size={12} />
            </span>
            <div className="ml-1">
              <div className="flex items-baseline justify-between gap-2 flex-wrap">
                <p className="text-sm font-medium">{meta.label}</p>
                <p className="text-xs text-ink-mute">
                  {formatDateTime(hop.at)}
                  {typeof hop.duration_ms === 'number' && ` · ${formatDuration(hop.duration_ms)}`}
                </p>
              </div>
              <p className="text-xs text-ink-soft mt-0.5 break-words">{hop.detail}</p>
            </div>
          </li>
        );
      })}
    </ol>
  );
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60_000).toFixed(1)}m`;
}
