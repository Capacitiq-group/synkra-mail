import { useAuth } from '@/stores/auth';
import { useReplyOwner, useThreadAssignment, useUpdateAssignment } from '@/hooks/queries/shared';
import { Avatar, Badge, Menu, MenuItem } from '@/design';
import { Icon } from '@/design/icons';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import type { SharedMailboxMember, SharedStatus } from '@/lib/types';

const STATUS_META: Record<SharedStatus, { label: string; tone: 'neutral' | 'accent' | 'warning' | 'success' }> = {
  new: { label: 'New', tone: 'accent' },
  open: { label: 'Open', tone: 'neutral' },
  waiting: { label: 'Waiting', tone: 'warning' },
  closed: { label: 'Closed', tone: 'success' },
};

export function AssignmentBar({
  sharedMailboxId, threadId, members,
}: { sharedMailboxId: string; threadId: string; members: SharedMailboxMember[] }) {
  const me = useAuth((s) => s.me);
  const a = useThreadAssignment(sharedMailboxId, threadId);
  const owner = useReplyOwner(sharedMailboxId, threadId);
  const update = useUpdateAssignment(sharedMailboxId);

  async function assign(email: string | null) {
    try {
      await update.mutateAsync({ threadId, patch: { assigned_to: email } });
      toast('success', email ? 'Assigned' : 'Unassigned');
    } catch (e) {
      toast('error', 'Could not assign', errorMessage(e));
    }
  }

  async function setStatus(status: SharedStatus) {
    try {
      await update.mutateAsync({ threadId, patch: { status } });
      toast('success', `Status set to ${STATUS_META[status].label}`);
    } catch (e) {
      toast('error', 'Could not update status', errorMessage(e));
    }
  }

  const current = a.data;
  const status = current ? STATUS_META[current.status] : STATUS_META.open;
  const assigned = current?.assigned_to;
  const isMe = assigned?.email === me?.email;

  return (
    <div className="flex items-center gap-2 px-3 py-2 border-b border-line bg-surface flex-wrap">
      {/* Assignee */}
      <span className="text-xs text-ink-mute">Assigned</span>
      <Menu
        trigger={
          <button
            className="flex items-center gap-1 px-2 py-1 rounded border border-line hover:bg-canvas text-sm"
            aria-label="Assign thread"
          >
            {assigned ? (
              <>
                <Avatar name={assigned.name} email={assigned.email} size={16} />
                <span className="max-w-[140px] truncate">{isMe ? 'Me' : (assigned.name ?? assigned.email)}</span>
              </>
            ) : (
              <>
                <span className="w-[16px] h-[16px] rounded-full border border-dashed border-ink-mute" />
                <span className="text-ink-soft">Unassigned</span>
              </>
            )}
            <Icon name="chevron-down" size={10} />
          </button>
        }
      >
        <MenuItem onClick={() => me && assign(me.email)}>Assign to me</MenuItem>
        {assigned && <MenuItem onClick={() => assign(null)}>Unassign</MenuItem>}
        <div className="border-t border-line my-1" />
        {members
          .filter((m) => m.email !== me?.email && m.email !== assigned?.email)
          .map((m) => (
            <MenuItem key={m.email} onClick={() => assign(m.email)}>
              <div className="flex items-center gap-2">
                <Avatar name={m.display_name} email={m.email} size={18} />
                <span className="text-sm truncate">{m.display_name}</span>
              </div>
            </MenuItem>
          ))}
      </Menu>

      {/* Status */}
      <span className="text-xs text-ink-mute ml-2">Status</span>
      <Menu
        trigger={
          <button
            className="flex items-center gap-1 px-2 py-1 rounded border border-line hover:bg-canvas text-sm"
            aria-label="Change status"
          >
            <Badge tone={status.tone}>{status.label}</Badge>
            <Icon name="chevron-down" size={10} />
          </button>
        }
      >
        {(Object.keys(STATUS_META) as SharedStatus[]).map((s) => (
          <MenuItem key={s} onClick={() => setStatus(s)}>
            <Badge tone={STATUS_META[s].tone}>{STATUS_META[s].label}</Badge>
          </MenuItem>
        ))}
      </Menu>

      {/* Reply ownership indicator */}
      {owner.data && owner.data.user.email !== me?.email && (
        <span className="ml-2 inline-flex items-center gap-1 text-xs text-warning">
          <Icon name="alert-triangle" size={12} />
          {owner.data.user.name ?? owner.data.user.email} is replying
        </span>
      )}

      <div className="flex-1" />

      {current?.last_activity_at && (
        <span className="text-xs text-ink-mute">
          Last activity {new Date(current.last_activity_at).toLocaleString('en-ZA', { dateStyle: 'medium', timeStyle: 'short' })}
        </span>
      )}
    </div>
  );
}
