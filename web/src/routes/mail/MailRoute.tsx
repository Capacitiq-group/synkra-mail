import { useState } from 'react';
import { Navigate, useParams } from 'react-router-dom';
import clsx from 'clsx';
import { useMailboxes, useFolders } from '@/hooks/queries/mail';
import { useMediaQuery } from '@/hooks';
import { Splash, ApiErrorView } from '@/components/system';
import { Drawer, EmptyState } from '@/design';
import { FolderSidebar } from './FolderSidebar';
import { MessageList } from './MessageList';
import { MessageView } from './MessageView';
import type { Folder, Mailbox } from '@/lib/types';

export function MailRoute() {
  const mailboxes = useMailboxes();
  const { mailboxId: urlMailboxId } = useParams();

  if (mailboxes.isLoading) return <Splash />;
  if (mailboxes.isError) return <ApiErrorView error={mailboxes.error} retry={() => mailboxes.refetch()} />;

  const boxes = mailboxes.data ?? [];
  if (boxes.length === 0) return <NoMailbox />;

  const mailbox = urlMailboxId ? boxes.find((b) => b.id === urlMailboxId) ?? boxes[0] : boxes[0];
  return <MailRouteInner key={mailbox.id} mailbox={mailbox} allMailboxes={boxes} />;
}

interface InnerProps { mailbox: Mailbox; allMailboxes: Mailbox[] }

function MailRouteInner({ mailbox, allMailboxes }: InnerProps) {
  const folders = useFolders(mailbox.id);
  const { mailboxId: urlMailboxId, folderId: urlFolderId, messageId } = useParams();

  if (folders.isLoading) return <Splash />;
  if (folders.isError) return <ApiErrorView error={folders.error} retry={() => folders.refetch()} />;

  const list = folders.data ?? [];
  if (list.length === 0) return <NoInbox />;

  const inbox = list.find((f) => f.role === 'inbox') ?? list[0];
  const folder: Folder = urlFolderId ? list.find((f) => f.id === urlFolderId) ?? inbox : inbox;

  // Canonicalise the URL: fill in missing segments and rewrite wrong ones.
  if (urlMailboxId !== mailbox.id) {
    const tail = messageId ? `/${messageId}` : '';
    return <Navigate to={`/mail/${mailbox.id}/${folder.id}${tail}`} replace />;
  }
  if (urlFolderId !== folder.id) {
    return <Navigate to={`/mail/${mailbox.id}/${folder.id}`} replace />;
  }

  return (
    <MailShell
      mailbox={mailbox}
      mailboxes={allMailboxes}
      folders={list}
      folder={folder}
      messageId={messageId}
    />
  );
}

interface ShellProps {
  mailbox: Mailbox;
  mailboxes: Mailbox[];
  folders: Folder[];
  folder: Folder;
  messageId?: string;
}

function MailShell({ mailbox, mailboxes, folders, folder, messageId }: ShellProps) {
  const isDesktop = useMediaQuery('(min-width: 1024px)');
  const [mobileFolderOpen, setMobileFolderOpen] = useState(false);

  // Mobile: message is focused, full-screen reading view with back button.
  if (!isDesktop && messageId) {
    return (
      <MessageView
        mailboxId={mailbox.id}
        folderId={folder.id}
        messageId={messageId}
        fullScreen
      />
    );
  }

  return (
    <div className="h-full flex min-h-0 bg-canvas">
      {isDesktop && (
        <aside className="w-60 shrink-0 border-r border-line bg-surface flex flex-col">
          <FolderSidebar
            mailbox={mailbox}
            mailboxes={mailboxes}
            folders={folders}
            activeFolderId={folder.id}
          />
        </aside>
      )}

      <div className={clsx('flex flex-1 min-w-0 min-h-0', isDesktop ? '' : 'w-full')}>
        <div
          className={clsx(
            isDesktop && messageId ? 'w-[420px] shrink-0' : 'flex-1',
            'min-w-0 min-h-0 border-r border-line bg-surface flex flex-col',
          )}
        >
          <MessageList
            mailbox={mailbox}
            folders={folders}
            folder={folder}
            currentMessageId={messageId}
            onOpenFolderDrawer={isDesktop ? undefined : () => setMobileFolderOpen(true)}
          />
        </div>
        {isDesktop && (
          <div className="flex-1 min-w-0 min-h-0">
            {messageId ? (
              <MessageView
                mailboxId={mailbox.id}
                folderId={folder.id}
                messageId={messageId}
              />
            ) : (
              <EmptyReadingPane />
            )}
          </div>
        )}
      </div>

      {!isDesktop && (
        <Drawer open={mobileFolderOpen} onClose={() => setMobileFolderOpen(false)} side="left" title="Folders">
          <FolderSidebar
            mailbox={mailbox}
            mailboxes={mailboxes}
            folders={folders}
            activeFolderId={folder.id}
            onNavigate={() => setMobileFolderOpen(false)}
          />
        </Drawer>
      )}
    </div>
  );
}

function EmptyReadingPane() {
  return (
    <div className="h-full flex items-center justify-center bg-canvas">
      <EmptyState
        title="Select a message"
        description="Choose a message from the list to read it here."
      />
    </div>
  );
}

function NoMailbox() {
  return (
    <div className="h-full flex items-center justify-center p-6">
      <EmptyState
        title="No mailbox yet"
        description="Your organisation has not created a mailbox for you. Contact your organisation administrator to be added."
      />
    </div>
  );
}

function NoInbox() {
  return (
    <div className="h-full flex items-center justify-center p-6">
      <EmptyState
        title="No folders yet"
        description="This mailbox has not been initialised. Try refreshing, or contact your administrator."
      />
    </div>
  );
}

/** Re-export for use by FolderSidebar and MessageList. */
export type { Folder, Mailbox };
