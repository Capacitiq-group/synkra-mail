import { Navigate, useParams } from 'react-router-dom';
import clsx from 'clsx';
import { useMailbox, useFolders } from '@/hooks/queries/mail';
import { useSharedMailbox } from '@/hooks/queries/shared';
import { useMediaQuery } from '@/hooks';
import { Splash, ApiErrorView } from '@/components/system';
import { EmptyState } from '@/design';
import { FolderSidebar } from '@/routes/mail/FolderSidebar';
import { MessageList } from '@/routes/mail/MessageList';
import { SharedThreadView } from './SharedThreadView';
import { AssignmentBadge } from './AssignmentBadge';
import { SharedMailboxHeader } from './SharedMailboxHeader';
import type { SharedMailbox } from '@/lib/types';

export function SharedMailboxShellRoute() {
  const { sharedMailboxId } = useParams<{ sharedMailboxId: string }>();
  const sharedMailboxQ = useSharedMailbox(sharedMailboxId);
  const mailboxQ = useMailbox(sharedMailboxId ?? '');

  if (sharedMailboxQ.isLoading || mailboxQ.isLoading) return <Splash />;
  if (sharedMailboxQ.isError) return <ApiErrorView error={sharedMailboxQ.error} retry={() => sharedMailboxQ.refetch()} />;
  if (mailboxQ.isError) return <ApiErrorView error={mailboxQ.error} retry={() => mailboxQ.refetch()} />;
  if (!sharedMailboxQ.data || !mailboxQ.data) return <EmptyState title="Shared mailbox not found" />;

  return <SharedShell sharedMailbox={sharedMailboxQ.data} />;
}

function SharedShell({ sharedMailbox }: { sharedMailbox: SharedMailbox }) {
  const { folderId: urlFolderId, messageId } = useParams<{ folderId: string; messageId: string }>();
  const foldersQ = useFolders(sharedMailbox.id);
  const isDesktop = useMediaQuery('(min-width: 1024px)');

  if (foldersQ.isLoading) return <Splash />;
  if (foldersQ.isError) return <ApiErrorView error={foldersQ.error} retry={() => foldersQ.refetch()} />;
  const folders = foldersQ.data ?? [];
  if (folders.length === 0) return <EmptyState title="This mailbox has no folders" />;

  const inbox = folders.find((f) => f.role === 'inbox') ?? folders[0];
  const folder = urlFolderId ? folders.find((f) => f.id === urlFolderId) ?? inbox : inbox;

  if (urlFolderId !== folder.id) {
    const tail = messageId ? `/${messageId}` : '';
    return <Navigate to={`/shared/${sharedMailbox.id}/${folder.id}${tail}`} replace />;
  }

  if (!isDesktop && messageId) {
    return (
      <div className="h-full flex flex-col bg-surface">
        <SharedMailboxHeader sharedMailbox={sharedMailbox} compact />
        <div className="flex-1 min-h-0">
          <SharedThreadView
            sharedMailbox={sharedMailbox}
            folderId={folder.id}
            messageId={messageId}
            fullScreen
          />
        </div>
      </div>
    );
  }

  return (
    <div className="h-full flex flex-col bg-canvas min-h-0">
      <SharedMailboxHeader sharedMailbox={sharedMailbox} />
      <div className="flex-1 flex min-h-0">
        {isDesktop && (
          <aside className="w-60 shrink-0 border-r border-line bg-surface flex flex-col">
            <FolderSidebar
              mailbox={sharedMailboxAsMailbox(sharedMailbox)}
              mailboxes={[]}
              folders={folders}
              activeFolderId={folder.id}
            />
          </aside>
        )}
        <div className={clsx('flex flex-1 min-w-0 min-h-0', isDesktop ? '' : 'w-full')}>
          <div
            className={clsx(
              isDesktop && messageId ? 'w-[400px] shrink-0' : 'flex-1',
              'min-w-0 min-h-0 border-r border-line bg-surface flex flex-col',
            )}
          >
            <MessageList
              mailbox={sharedMailboxAsMailbox(sharedMailbox)}
              folders={folders}
              folder={folder}
              currentMessageId={messageId}
              rowDecorator={(m) => (
                <AssignmentBadge sharedMailboxId={sharedMailbox.id} threadId={m.thread_id} />
              )}
            />
          </div>
          {isDesktop && (
            <div className="flex-1 min-w-0 min-h-0">
              {messageId ? (
                <SharedThreadView
                  sharedMailbox={sharedMailbox}
                  folderId={folder.id}
                  messageId={messageId}
                />
              ) : (
                <div className="h-full flex items-center justify-center bg-canvas text-sm text-ink-soft p-6 text-center">
                  Select a message to view the thread, assign it, and see the activity log.
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

/** Adapts SharedMailbox metadata to the Mailbox shape the reused components expect. */
function sharedMailboxAsMailbox(sm: SharedMailbox) {
  return {
    id: sm.id,
    address: sm.address,
    display_name: sm.display_name,
    quota_bytes: 0,
    used_bytes: 0,
    suspended: false,
    bounce_breaker_paused: false,
    shared: true,
    kind: 'shared' as const,
  };
}
