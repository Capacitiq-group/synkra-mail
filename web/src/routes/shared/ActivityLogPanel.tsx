import { useSharedActivity } from '@/hooks/queries/shared';
import { EmptyState, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { relativeTime } from '@/lib/time';
import type { SharedActivityEvent } from '@/lib/types';

export function ActivityLogPanel({
  sharedMailboxId, threadId,
}: { sharedMailboxId: string; threadId?: string }) {
  const q = useSharedActivity(sharedMailboxId, threadId);

  if (q.isLoading) {
    return (
      <div className="p-3 space-y-2">
        {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-10 w-full" />)}
      </div>
    );
  }
  if (q.isError) {
    return <p className="p-3 text-xs text-danger">Could not load activity.</p>;
  }
  if (!q.data || q.data.items.length === 0) {
    return <EmptyState title="No activity yet" />;
  }

  return (
    <div className="p-3">
      <h3 className="text-xs uppercase text-ink-mute mb-2">
        {threadId ? 'Thread activity' : 'Recent team activity'}
      </h3>
      <ol className="space-y-2">
        {q.data.items.map((e) => (
          <ActivityRow key={e.id} event={e} showThread={!threadId} />
        ))}
      </ol>
    </div>
  );
}

function ActivityRow({ event, showThread }: { event: SharedActivityEvent; showThread: boolean }) {
  const { icon, text } = describe(event);
  return (
    <li className="flex items-start gap-2 text-xs">
      <span className="mt-0.5 shrink-0 text-ink-mute"><Icon name={icon} size={12} /></span>
      <div className="flex-1 min-w-0">
        <p className="text-ink">
          <strong className="font-medium">{event.actor.name ?? event.actor.email}</strong> {text}
        </p>
        {showThread && event.detail?.subject && (
          <p className="text-ink-soft truncate">{event.detail.subject}</p>
        )}
        <p className="text-ink-mute">{relativeTime(event.at)}</p>
      </div>
    </li>
  );
}

function describe(e: SharedActivityEvent): { icon: Parameters<typeof Icon>[0]['name']; text: string } {
  switch (e.kind) {
    case 'assigned':
      return { icon: 'check', text: `assigned to ${e.detail?.to_assignee?.name ?? e.detail?.to_assignee?.email ?? 'someone'}` };
    case 'unassigned':
      return { icon: 'check', text: 'unassigned the thread' };
    case 'status_changed':
      return {
        icon: 'sparkles',
        text: `changed status from ${e.detail?.from_status ?? '?'} to ${e.detail?.to_status ?? '?'}`,
      };
    case 'reply_started':
      return { icon: 'reply', text: 'opened a reply' };
    case 'reply_sent':
      return { icon: 'sent', text: 'sent a reply' };
    case 'note_added':
      return { icon: 'info', text: 'added a note' };
    case 'thread_created':
      return { icon: 'inbox', text: 'created the thread' };
    case 'thread_closed':
      return { icon: 'check-circle', text: 'closed the thread' };
    case 'thread_reopened':
      return { icon: 'refresh', text: 'reopened the thread' };
  }
}
