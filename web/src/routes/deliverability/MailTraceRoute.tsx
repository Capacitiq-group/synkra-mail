import { useNavigate, useParams } from 'react-router-dom';
import { useMailTrace } from '@/hooks/queries/deliverability';
import { Button, EmptyState, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { usePageTitle } from '@/hooks';
import { MailTraceTimeline } from './MailTraceTimeline';
import { errorSurface } from '@/lib/errors';

export function MailTraceRoute() {
  usePageTitle('Delivery trace');
  const navigate = useNavigate();
  const { messageId } = useParams<{ messageId: string }>();
  const q = useMailTrace(messageId);

  return (
    <div className="h-full overflow-y-auto scroll-thin bg-canvas">
      <div className="max-w-2xl mx-auto p-4 md:p-6">
        <header className="flex items-center gap-2 mb-4">
          <Button size="icon" variant="ghost" onClick={() => navigate(-1 as never)} aria-label="Back">
            <Icon name="chevron-left" size={18} />
          </Button>
          <h1 className="text-lg font-semibold flex-1">Delivery trace</h1>
        </header>

        {q.isLoading && <Skeleton className="h-60 w-full" />}

        {q.isError && (
          <EmptyState
            title="Could not load trace"
            description={errorSurface(q.error).message}
            action={<Button onClick={() => q.refetch()}>Try again</Button>}
          />
        )}

        {q.data && (
          <div className="bg-surface border border-line rounded-lg">
            <div className="p-4 border-b border-line">
              <p className="text-xs uppercase text-ink-mute">Recipient</p>
              <p className="text-sm font-medium">{q.data.recipient}</p>
              <p className="text-xs text-ink-mute mt-1">Message {q.data.message_id}</p>
            </div>
            <MailTraceTimeline hops={q.data.hops} />
          </div>
        )}
      </div>
    </div>
  );
}
