import { useRevokeAllSessions, useRevokeSession, useSessions } from '@/hooks/queries/security';
import { Button, ConfirmDialog, EmptyState, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { formatDateTime, relativeTime } from '@/lib/time';
import { useState } from 'react';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import type { Session } from '@/lib/types';

export function SessionsTab() {
  const q = useSessions();
  const revoke = useRevokeSession();
  const revokeAll = useRevokeAllSessions();
  const [target, setTarget] = useState<Session | null>(null);
  const [allOpen, setAllOpen] = useState(false);

  return (
    <div className="max-w-3xl space-y-4">
      <div className="flex items-start justify-between gap-3">
        <p className="text-sm text-ink-soft">
          Everywhere your account is currently signed in. Revoking a session logs that device out immediately.
        </p>
        <Button variant="secondary" onClick={() => setAllOpen(true)}>Revoke all others</Button>
      </div>

      {q.isLoading && <Skeleton className="h-20 w-full" />}
      {q.data && q.data.length === 0 && <EmptyState title="No active sessions" />}
      {q.data && q.data.length > 0 && (
        <ul className="space-y-2">
          {q.data.map((s) => (
            <li key={s.id} className="flex items-start gap-3 p-3 border border-line rounded">
              <Icon name={s.current ? 'check-circle' : 'mail'} size={16} className={s.current ? 'text-success' : 'text-ink-mute'} />
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium">{s.device_name || 'Unknown device'} {s.current && <span className="text-xs px-1.5 py-0.5 rounded bg-success-soft text-success ml-1">This device</span>}</p>
                <p className="text-xs text-ink-soft">
                  {s.location ? `${s.location} · ` : ''}{s.ip}
                </p>
                <p className="text-xs text-ink-mute">
                  Started {formatDateTime(s.created_at)} · last seen {relativeTime(s.last_seen_at)}
                </p>
              </div>
              {!s.current && (
                <Button size="sm" variant="ghost" onClick={() => setTarget(s)}>Revoke</Button>
              )}
            </li>
          ))}
        </ul>
      )}

      <ConfirmDialog
        open={!!target}
        onCancel={() => setTarget(null)}
        onConfirm={async () => {
          if (!target) return;
          try { await revoke.mutateAsync(target.id); toast('success', 'Session revoked'); setTarget(null); }
          catch (e) { toast('error', 'Could not revoke', errorMessage(e)); }
        }}
        title="Revoke this session?"
        description="The device will be signed out immediately and must sign in again."
        confirmLabel="Revoke"
        danger
        busy={revoke.isPending}
      />

      <ConfirmDialog
        open={allOpen}
        onCancel={() => setAllOpen(false)}
        onConfirm={async () => {
          try { await revokeAll.mutateAsync(); toast('success', 'All other sessions revoked'); setAllOpen(false); }
          catch (e) { toast('error', 'Could not revoke', errorMessage(e)); }
        }}
        title="Revoke all other sessions?"
        description="Every device except this one will be signed out."
        confirmLabel="Revoke others"
        danger
        busy={revokeAll.isPending}
      />
    </div>
  );
}
