import { Link } from 'react-router-dom';
import { useSharedMailbox } from '@/hooks/queries/shared';
import { Avatar, Badge, Menu, MenuItem } from '@/design';
import { Icon } from '@/design/icons';
import { useNavigate } from 'react-router-dom';

export function SharedMailboxHeader({ sharedMailbox, compact }: { sharedMailbox: import('@/lib/types').SharedMailbox; compact?: boolean }) {
  const navigate = useNavigate();
  return (
    <header className="flex items-center gap-2 px-3 py-2 border-b border-line bg-surface shrink-0">
      <Link to="/shared" className="text-sm text-ink-soft hover:text-ink inline-flex items-center gap-1">
        <Icon name="chevron-left" size={14} /> Shared
      </Link>
      <div className="h-4 w-px bg-line mx-1" aria-hidden />
      <span className="w-8 h-8 rounded bg-accent-soft text-accent flex items-center justify-center">
        <Icon name="inbox" size={14} />
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium truncate">{sharedMailbox.display_name}</p>
        <p className="text-xs text-ink-soft truncate">{sharedMailbox.address}</p>
      </div>
      {!compact && (
        <>
          <div className="flex -space-x-2">
            {sharedMailbox.members.slice(0, 5).map((m) => (
              <span key={m.email} className="rounded-full border-2 border-surface" title={`${m.display_name} <${m.email}>`}>
                <Avatar name={m.display_name} email={m.email} size={22} />
              </span>
            ))}
          </div>
          {sharedMailbox.sla_minutes && (
            <Badge tone="neutral">
              SLA {sharedMailbox.sla_minutes < 60 ? `${sharedMailbox.sla_minutes}m` : `${sharedMailbox.sla_minutes / 60}h`}
            </Badge>
          )}
        </>
      )}
      <Menu trigger={
        <button className="p-1 rounded hover:bg-canvas" aria-label="Shared mailbox options">
          <Icon name="more" size={16} />
        </button>
      }>
        <MenuItem onClick={() => navigate(`/shared/${sharedMailbox.id}/activity`)}>View team activity</MenuItem>
        <MenuItem onClick={() => navigate(`/org/shared-mailboxes/${sharedMailbox.id}`)}>Manage members</MenuItem>
      </Menu>
    </header>
  );
}

// Suppress unused import.
void useSharedMailbox;
