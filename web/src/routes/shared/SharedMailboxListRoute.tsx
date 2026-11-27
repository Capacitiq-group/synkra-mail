import { Link } from 'react-router-dom';
import { useSharedMailboxes } from '@/hooks/queries/shared';
import { useAuth } from '@/stores/auth';
import { Avatar, Badge, Button, EmptyState, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { usePageTitle } from '@/hooks';

export function SharedMailboxListRoute() {
  usePageTitle('Shared mailboxes');
  const me = useAuth((s) => s.me);
  const q = useSharedMailboxes();

  return (
    <div className="h-full overflow-y-auto scroll-thin bg-canvas">
      <div className="max-w-4xl mx-auto p-4 md:p-8">
        <header className="mb-6">
          <h1 className="text-xl font-semibold">Shared mailboxes</h1>
          <p className="text-sm text-ink-soft mt-1">
            Team inboxes — assign threads, track reply ownership, and see every action in the activity log.
          </p>
        </header>

        {q.isLoading && (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-40 w-full rounded-lg" />)}
          </div>
        )}

        {!q.isLoading && (q.data?.length ?? 0) === 0 && (
          <EmptyState
            icon={<Icon name="inbox" size={28} />}
            title="No shared mailboxes yet"
            description="Your organisation administrator can create shared mailboxes from the admin console."
            action={
              me && ['owner', 'admin'].includes(me.role)
                ? <Link to="/org/shared-mailboxes"><Button variant="primary">Manage shared mailboxes</Button></Link>
                : undefined
            }
          />
        )}

        {!q.isLoading && q.data && q.data.length > 0 && (
          <ul className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {q.data.map((sm) => (
              <li key={sm.id}>
                <Link
                  to={`/shared/${sm.id}`}
                  className="block p-4 rounded-lg border border-line bg-surface hover:border-accent transition-colors"
                >
                  <div className="flex items-start gap-3">
                    <span className="w-10 h-10 rounded bg-accent-soft text-accent flex items-center justify-center shrink-0">
                      <Icon name="inbox" size={18} />
                    </span>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <p className="font-medium truncate">{sm.display_name}</p>
                        {typeof sm.unread_count === 'number' && sm.unread_count > 0 && (
                          <Badge tone="accent">{sm.unread_count} unread</Badge>
                        )}
                      </div>
                      <p className="text-sm text-ink-soft truncate">{sm.address}</p>
                      {sm.description && (
                        <p className="text-xs text-ink-mute mt-1 line-clamp-2">{sm.description}</p>
                      )}
                    </div>
                  </div>

                  <div className="mt-3 flex items-center gap-2">
                    <div className="flex -space-x-2">
                      {sm.members.slice(0, 4).map((m) => (
                        <span key={m.email} title={`${m.display_name} <${m.email}>`} className="rounded-full border-2 border-surface">
                          <Avatar name={m.display_name} email={m.email} size={24} />
                        </span>
                      ))}
                      {sm.members.length > 4 && (
                        <span className="w-6 h-6 rounded-full bg-canvas border-2 border-surface flex items-center justify-center text-[10px] font-medium text-ink-soft">
                          +{sm.members.length - 4}
                        </span>
                      )}
                    </div>
                    <span className="text-xs text-ink-mute">
                      {sm.members.length} member{sm.members.length === 1 ? '' : 's'}
                    </span>
                    {sm.sla_minutes && (
                      <span className="text-xs text-ink-mute ml-auto">
                        SLA {sm.sla_minutes < 60 ? `${sm.sla_minutes}m` : `${sm.sla_minutes / 60}h`}
                      </span>
                    )}
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
