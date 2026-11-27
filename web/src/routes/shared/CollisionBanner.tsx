import { useReplyOwner } from '@/hooks/queries/shared';
import { useAuth } from '@/stores/auth';
import { relativeTime } from '@/lib/time';
import { Icon } from '@/design/icons';

export function CollisionBanner({
  sharedMailboxId, threadId, messageId,
}: { sharedMailboxId: string; threadId: string; messageId?: string }) {
  const me = useAuth((s) => s.me);
  const q = useReplyOwner(sharedMailboxId, threadId);

  if (!q.data) return null;
  if (q.data.user.email === me?.email) {
    // You're the owner — subtle confirmation, not a warning.
    return (
      <div className="px-3 py-1.5 border-b border-line bg-success-soft text-xs text-success flex items-center gap-2">
        <Icon name="check-circle" size={12} />
        You are replying to this thread. If a teammate opens a reply, they will be notified.
      </div>
    );
  }

  return (
    <div className="px-3 py-2 border-b border-warning/40 bg-warning-soft text-sm flex items-start gap-2">
      <Icon name="alert-triangle" size={14} className="text-warning mt-0.5 shrink-0" />
      <div className="flex-1">
        <p className="font-medium text-warning">
          {q.data.user.name ?? q.data.user.email} is currently replying to this thread.
        </p>
        <p className="text-xs text-ink-soft mt-0.5">
          Started {relativeTime(q.data.claimed_at)}.
          If you reply anyway, they will be notified when you send.
          {messageId && ' Both drafts remain visible to the team.'}
        </p>
      </div>
    </div>
  );
}
