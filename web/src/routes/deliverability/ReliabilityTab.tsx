import { useAcknowledgeAlert, useReliabilityAlerts } from '@/hooks/queries/deliverability';
import { Badge, Button, EmptyState, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { relativeTime } from '@/lib/time';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import type { ReliabilityAlert } from '@/lib/types';

const KIND_LABEL: Record<ReliabilityAlert['kind'], string> = {
  mx_down: 'MX unreachable',
  dkim_failing: 'DKIM failing',
  queue_growing: 'Queue growing',
  storage_high: 'Storage high',
  reputation_drop: 'Reputation drop',
  rejected_message: 'Rejected message',
  domain_expiring: 'Domain expiring',
};

const SEVERITY_META: Record<ReliabilityAlert['severity'], { tone: 'success' | 'warning' | 'danger'; icon: Parameters<typeof Icon>[0]['name'] }> = {
  info: { tone: 'success', icon: 'info' },
  warning: { tone: 'warning', icon: 'alert-triangle' },
  critical: { tone: 'danger', icon: 'x-circle' },
};

export function ReliabilityTab() {
  const q = useReliabilityAlerts();
  const ack = useAcknowledgeAlert();

  if (q.isLoading) return <Skeleton className="h-40 w-full" />;
  if (q.isError) return <p className="text-sm text-danger">Could not load reliability alerts.</p>;

  const active = (q.data ?? []).filter((a) => !a.acknowledged_at);
  const acknowledged = (q.data ?? []).filter((a) => a.acknowledged_at);

  return (
    <div className="space-y-6">
      <section>
        <h2 className="font-medium mb-2">Active alerts</h2>
        {active.length === 0 ? (
          <div className="border border-line rounded-lg p-6 bg-surface flex items-center gap-3">
            <span className="w-9 h-9 rounded-full bg-success-soft text-success flex items-center justify-center">
              <Icon name="shield-check" size={16} />
            </span>
            <div>
              <p className="text-sm font-medium">All clear</p>
              <p className="text-xs text-ink-soft">No active reliability issues in the last 24 hours.</p>
            </div>
          </div>
        ) : (
          <ul className="space-y-2">
            {active.map((a) => (
              <li key={a.id}>
                <AlertRow alert={a} onAck={() => {
                  ack.mutate(a.id, {
                    onSuccess: () => toast('success', 'Alert acknowledged'),
                    onError: (e) => toast('error', 'Could not acknowledge', errorMessage(e)),
                  });
                }} />
              </li>
            ))}
          </ul>
        )}
      </section>

      {acknowledged.length > 0 && (
        <section>
          <h2 className="font-medium mb-2 text-ink-soft">Acknowledged</h2>
          <ul className="space-y-2 opacity-70">
            {acknowledged.map((a) => (
              <li key={a.id}><AlertRow alert={a} /></li>
            ))}
          </ul>
        </section>
      )}

      <div className="text-xs text-ink-soft">
        <EmptyState title="About the monitor" description="We check every domain every minute. Alerts are pushed via email and the notification bell." />
      </div>
    </div>
  );
}

function AlertRow({ alert, onAck }: { alert: ReliabilityAlert; onAck?: () => void }) {
  const meta = SEVERITY_META[alert.severity];
  return (
    <div className={`border border-line rounded-lg p-3 bg-surface flex items-start gap-3 ${alert.acknowledged_at ? 'opacity-60' : ''}`}>
      <span className={`w-8 h-8 rounded-full flex items-center justify-center shrink-0 ${
        meta.tone === 'danger' ? 'bg-danger-soft text-danger' :
        meta.tone === 'warning' ? 'bg-warning-soft text-warning' :
        'bg-success-soft text-success'
      }`}>
        <Icon name={meta.icon} size={14} />
      </span>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <p className="text-sm font-medium">{KIND_LABEL[alert.kind]}</p>
          <Badge tone={meta.tone}>{alert.severity}</Badge>
          {alert.domain && <span className="text-xs text-ink-mute">{alert.domain}</span>}
          {alert.mailbox && <span className="text-xs text-ink-mute">{alert.mailbox}</span>}
        </div>
        <p className="text-sm mt-0.5">{alert.message}</p>
        <p className="text-xs text-ink-mute mt-0.5">
          {relativeTime(alert.created_at)}
          {alert.acknowledged_at && ` · acknowledged ${relativeTime(alert.acknowledged_at)}`}
        </p>
      </div>
      {onAck && (
        <Button size="sm" variant="ghost" onClick={onAck}>Acknowledge</Button>
      )}
    </div>
  );
}
