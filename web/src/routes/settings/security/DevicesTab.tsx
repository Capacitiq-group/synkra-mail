import { useDevices, useRevokeDevice } from '@/hooks/queries/security';
import { Button, EmptyState, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { formatDateTime, relativeTime } from '@/lib/time';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';

export function DevicesTab() {
  const q = useDevices();
  const revoke = useRevokeDevice();

  return (
    <div className="max-w-3xl space-y-4">
      <p className="text-sm text-ink-soft">
        Devices that have signed in to your account. Revoking a device forgets its trusted state and forces re-authentication next time.
      </p>

      {q.isLoading && <Skeleton className="h-20 w-full" />}
      {q.data && q.data.length === 0 && <EmptyState title="No known devices" />}
      {q.data && q.data.length > 0 && (
        <ul className="space-y-2">
          {q.data.map((d) => (
            <li key={d.id} className="flex items-start gap-3 p-3 border border-line rounded">
              <Icon name={d.kind === 'mobile' ? 'mail' : d.kind === 'desktop' ? 'inbox' : 'inbox'} size={16} className="text-ink-mute" />
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium">{d.name} {d.current && <span className="text-xs px-1.5 py-0.5 rounded bg-success-soft text-success ml-1">This device</span>}</p>
                <p className="text-xs text-ink-soft">{d.kind} · {d.ip}</p>
                <p className="text-xs text-ink-mute">
                  First seen {formatDateTime(d.first_seen_at)} · last seen {relativeTime(d.last_seen_at)}
                </p>
              </div>
              {!d.current && (
                <Button size="sm" variant="ghost" onClick={async () => {
                  try { await revoke.mutateAsync(d.id); toast('success', 'Device revoked'); }
                  catch (e) { toast('error', 'Could not revoke', errorMessage(e)); }
                }}>Forget</Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
