import { useReplyOwner, useThreadAssignment } from '@/hooks/queries/shared';
import { Avatar, Badge } from '@/design';
import { Icon } from '@/design/icons';

const STATUS_META: Record<string, { label: string; tone: 'neutral' | 'accent' | 'warning' | 'success' }> = {
  new: { label: 'New', tone: 'accent' },
  open: { label: 'Open', tone: 'neutral' },
  waiting: { label: 'Waiting', tone: 'warning' },
  closed: { label: 'Closed', tone: 'success' },
};

/** Row-level badge for the shared inbox message list. */
export function AssignmentBadge({ sharedMailboxId, threadId }: { sharedMailboxId: string; threadId: string }) {
  const a = useThreadAssignment(sharedMailboxId, threadId);
  const owner = useReplyOwner(sharedMailboxId, threadId);

  if (!a.data) return null;
  const status = STATUS_META[a.data.status] ?? STATUS_META.open;
  const activeReply = owner.data;

  return (
    <span className="inline-flex items-center gap-1">
      {activeReply && (
        <span title={`${activeReply.user.name ?? activeReply.user.email} is replying`} className="flex items-center">
          <Icon name="sparkles" size={12} className="text-accent" />
        </span>
      )}
      {a.data.assigned_to ? (
        <span title={`Assigned to ${a.data.assigned_to.name ?? a.data.assigned_to.email}`}>
          <Avatar name={a.data.assigned_to.name} email={a.data.assigned_to.email} size={18} />
        </span>
      ) : (
        <span className="w-[18px] h-[18px] rounded-full border border-dashed border-ink-mute" title="Unassigned" />
      )}
      <Badge tone={status.tone}>{status.label}</Badge>
    </span>
  );
}
