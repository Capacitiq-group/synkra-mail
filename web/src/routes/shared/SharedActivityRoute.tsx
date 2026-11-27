import { useNavigate, useParams } from 'react-router-dom';
import { useSharedMailbox } from '@/hooks/queries/shared';
import { ActivityLogPanel } from './ActivityLogPanel';
import { Button, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { usePageTitle } from '@/hooks';

export function SharedActivityRoute() {
  const { sharedMailboxId } = useParams<{ sharedMailboxId: string }>();
  const navigate = useNavigate();
  const sm = useSharedMailbox(sharedMailboxId);
  usePageTitle(sm.data ? `${sm.data.display_name} · Activity` : 'Activity');

  if (sm.isLoading) return <div className="p-6"><Skeleton className="h-20 w-full" /></div>;
  if (!sm.data) return <div className="p-6 text-sm text-danger">Shared mailbox not found.</div>;

  return (
    <div className="h-full overflow-y-auto scroll-thin bg-surface">
      <header className="px-4 py-3 border-b border-line flex items-center gap-2">
        <Button size="icon" variant="ghost" onClick={() => navigate(`/shared/${sm.data.id}`)} aria-label="Back">
          <Icon name="chevron-left" size={18} />
        </Button>
        <h1 className="text-lg font-semibold flex-1">Team activity · {sm.data.display_name}</h1>
      </header>
      <div className="max-w-2xl mx-auto p-4">
        <ActivityLogPanel sharedMailboxId={sm.data.id} />
      </div>
    </div>
  );
}
