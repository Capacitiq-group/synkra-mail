import { useState } from 'react';
import { useParams } from 'react-router-dom';
import { useMessage } from '@/hooks/queries/mail';
import { useReplyOwner, useThreadAssignment } from '@/hooks/queries/shared';
import { useAuth } from '@/stores/auth';
import { MessageView } from '@/routes/mail/MessageView';
import { NotesPanel } from '@/components/NotesPanel';
import { AssignmentBar } from './AssignmentBar';
import { CollisionBanner } from './CollisionBanner';
import { ActivityLogPanel } from './ActivityLogPanel';
import { Button, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import type { SharedMailbox } from '@/lib/types';

interface Props {
  sharedMailbox: SharedMailbox;
  folderId: string;
  messageId: string;
  fullScreen?: boolean;
}

export function SharedThreadView({ sharedMailbox, folderId, messageId, fullScreen }: Props) {
  const me = useAuth((s) => s.me);
  const { mailboxId } = useParams<{ mailboxId: string }>();
  const messageQ = useMessage(messageId);
  const [showActivity, setShowActivity] = useState(false);

  if (messageQ.isLoading || !messageQ.data) {
    return (
      <div className="p-4 space-y-3">
        <Skeleton className="h-6 w-2/3" />
        <Skeleton className="h-4 w-1/3" />
        <Skeleton className="h-3 w-full" />
        <Skeleton className="h-3 w-full" />
      </div>
    );
  }

  const threadId = messageQ.data.thread_id;

  return (
    <div className="h-full flex flex-col bg-surface min-h-0">
      <SharedTopBar
        sharedMailboxId={sharedMailbox.id}
        threadId={threadId}
        onToggleActivity={() => setShowActivity((v) => !v)}
        activityOpen={showActivity}
        me_email={me?.email}
      />

      <div className="border-b border-line">
        <CollisionBanner sharedMailboxId={sharedMailbox.id} threadId={threadId} messageId={messageId} />
        <AssignmentBar sharedMailboxId={sharedMailbox.id} threadId={threadId} members={sharedMailbox.members} />
      </div>

      <div className="flex-1 min-h-0 flex">
        <div className="flex-1 min-w-0">
          <MessageView
            mailboxId={mailboxId ?? sharedMailbox.id}
            folderId={folderId}
            messageId={messageId}
            fullScreen={fullScreen}
            sharedContext={{
              sharedMailboxId: sharedMailbox.id,
              threadId,
            }}
          />
        </div>
        {showActivity && (
          <aside className="w-80 shrink-0 border-l border-line overflow-y-auto scroll-thin bg-canvas">
            <ActivityLogPanel sharedMailboxId={sharedMailbox.id} threadId={threadId} />
            <div className="p-3 border-t border-line">
              <NotesPanel scope={{ threadId }} title="Team notes" />
            </div>
          </aside>
        )}
      </div>
    </div>
  );
}

function SharedTopBar({
  sharedMailboxId, threadId, onToggleActivity, activityOpen, me_email,
}: {
  sharedMailboxId: string;
  threadId: string;
  onToggleActivity: () => void;
  activityOpen: boolean;
  me_email?: string;
}) {
  const a = useThreadAssignment(sharedMailboxId, threadId);
  const owner = useReplyOwner(sharedMailboxId, threadId);
  const someoneReplying = owner.data && owner.data.user.email !== me_email;

  return (
    <div className="flex items-center gap-2 px-3 py-1.5 border-b border-line bg-canvas text-xs text-ink-soft">
      <span>Team thread</span>
      {a.data?.assigned_to && (
        <span>· assigned to {a.data.assigned_to.name ?? a.data.assigned_to.email}</span>
      )}
      {someoneReplying && owner.data && (
        <span className="text-warning font-medium">
          · {owner.data.user.name ?? owner.data.user.email} replying
        </span>
      )}
      <div className="flex-1" />
      <Button
        size="sm"
        variant={activityOpen ? 'secondary' : 'ghost'}
        onClick={onToggleActivity}
        aria-pressed={activityOpen}
      >
        <Icon name="sparkles" size={12} /> Activity
      </Button>
    </div>
  );
}
