import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import clsx from 'clsx';
import {
  useMarkMessageRead, useMessage, usePrivacySettings, useThread,
} from '@/hooks/queries/mail';
import { useMailActions } from './useMailActions';
import { useMailShortcuts } from './useMailShortcuts';
import { Icon } from '@/design/icons';
import { Button, Menu, MenuItem, Skeleton } from '@/design';
import { Avatar } from '@/design';
import { renderEmail, wrapForIframe } from '@/lib/sanitize';
import { analyzeMessage } from '@/lib/spoof';
import { formatDateTime } from '@/lib/time';
import { toast } from '@/stores/toast';
import { errorMessage } from '@/lib/errors';
import { getItem, setItem, StorageKeys } from '@/lib/storage';
import type { MessageDetail } from '@/lib/types';
import { AttachmentChip } from './AttachmentChip';

interface Props {
  mailboxId: string;
  folderId: string;
  messageId: string;
  fullScreen?: boolean;
  /** Present when composing/reading in a shared mailbox (part 8). */
  sharedContext?: { sharedMailboxId: string; threadId: string };
}

export function MessageView({ mailboxId, folderId, messageId, fullScreen, sharedContext }: Props) {
  void sharedContext
  const navigate = useNavigate();
  const messageQ = useMessage(messageId);
  const threadQ = useThread(messageQ.data?.thread_id);
  const privacyQ = usePrivacySettings();
  const markRead = useMarkMessageRead();
  const actions = useMailActions(mailboxId);

  // Auto-mark-read once.
  useEffect(() => {
    if (!messageQ.data || messageQ.data.is_read) return;
    markRead.mutate(messageQ.data.id);
  }, [messageQ.data, markRead]);

  const goBack = () => navigate(`/mail/${mailboxId}/${folderId}`);
  const composeUrl = (mode: 'reply' | 'reply_all' | 'forward', id: string) =>
    `/compose?mode=${mode}&source=${id}&mailbox=${mailboxId}`;

  useMailShortcuts({
    back: goBack,
    reply: () => messageQ.data && navigate(composeUrl('reply', messageQ.data.id)),
    replyAll: () => messageQ.data && navigate(composeUrl('reply_all', messageQ.data.id)),
    forward: () => messageQ.data && navigate(composeUrl('forward', messageQ.data.id)),
    archive: () => {
      if (!messageQ.data) return;
      actions.archive([messageQ.data.id]).then(() => { toast('info', 'Archived'); goBack(); })
        .catch((e) => toast('error', 'Could not archive', errorMessage(e)));
    },
    trash: () => {
      if (!messageQ.data) return;
      actions.trash([messageQ.data.id]).then(() => { toast('info', 'Moved to Trash'); goBack(); })
        .catch((e) => toast('error', 'Could not delete', errorMessage(e)));
    },
    star: () => {
      if (!messageQ.data) return;
      const starred = messageQ.data.flags.includes('flagged');
      (starred ? actions.unstar : actions.star)([messageQ.data.id])
        .then(() => toast('info', starred ? 'Unstarred' : 'Starred'))
        .catch((e) => toast('error', 'Could not update', errorMessage(e)));
    },
    escape: goBack,
  });

  if (messageQ.isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-6 w-2/3" />
        <Skeleton className="h-4 w-1/3" />
        <Skeleton className="h-3 w-full" />
        <Skeleton className="h-3 w-full" />
        <Skeleton className="h-3 w-2/3" />
      </div>
    );
  }
  if (messageQ.isError || !messageQ.data) {
    return (
      <div className="p-6">
        <p className="text-sm">Could not load this message.</p>
        <Button className="mt-3" onClick={goBack}>Back to list</Button>
      </div>
    );
  }

  const message = messageQ.data;
  const thread = threadQ.data?.messages && threadQ.data.messages.length > 1 ? threadQ.data.messages : [message];

  return (
    <div className="h-full flex flex-col bg-surface">
      <Toolbar
        message={message}
        mailboxId={mailboxId}
        folderId={folderId}
        fullScreen={!!fullScreen}
        onBack={goBack}
        onReply={() => navigate(composeUrl('reply', message.id))}
        onReplyAll={() => navigate(composeUrl('reply_all', message.id))}
        onForward={() => navigate(composeUrl('forward', message.id))}
      />
      <div className="flex-1 overflow-y-auto scroll-thin">
        <article className="max-w-3xl mx-auto p-4 md:p-6">
          <h1 className="text-lg md:text-xl font-semibold mb-3">
            {message.subject || '(no subject)'}
          </h1>
          {thread.map((msg, idx) => (
            <ThreadMessage
              key={msg.id}
              message={msg}
              expanded={msg.id === messageId || (idx === thread.length - 1 && messageId === msg.id)}
              showButtons={idx === thread.length - 1}
              onReply={() => navigate(composeUrl('reply', msg.id))}
              onReplyAll={() => navigate(composeUrl('reply_all', msg.id))}
              onForward={() => navigate(composeUrl('forward', msg.id))}
              allowRemoteImagesDefault={privacyQ.data?.block_remote_images === false}
            />
          ))}
        </article>
      </div>
    </div>
  );
}

// ===== Toolbar =====
function Toolbar(props: {
  message: MessageDetail;
  mailboxId: string;
  folderId: string;
  fullScreen: boolean;
  onBack: () => void;
  onReply: () => void;
  onReplyAll: () => void;
  onForward: () => void;
}) {
  const { message } = props;
  const actions = useMailActions(props.mailboxId);
  const starred = message.flags.includes('flagged');
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);

  async function run(fn: () => Promise<unknown>, successMsg: string) {
    setBusy(true);
    try { await fn(); toast('success', successMsg); } catch (e) { toast('error', errorMessage(e)); }
    finally { setBusy(false); }
  }

  return (
    <div className="flex items-center gap-1 px-2 py-1.5 border-b border-line bg-surface">
      {props.fullScreen && (
        <Button size="icon" variant="ghost" onClick={props.onBack} aria-label="Back to list">
          <Icon name="chevron-left" size={18} />
        </Button>
      )}
      <Button size="icon" variant="ghost" onClick={props.onBack} aria-label="Back">
        <Icon name="chevron-left" size={18} />
      </Button>
      <div className="flex-1" />
      <Button size="icon" variant="ghost" onClick={props.onReply} aria-label="Reply" title="Reply">
        <Icon name="reply" size={18} />
      </Button>
      <Button size="icon" variant="ghost" onClick={props.onReplyAll} aria-label="Reply all" title="Reply all">
        <Icon name="reply-all" size={18} />
      </Button>
      <Button size="icon" variant="ghost" onClick={props.onForward} aria-label="Forward" title="Forward">
        <Icon name="forward" size={18} />
      </Button>
      <div className="w-px h-5 bg-line mx-1" aria-hidden />
      <Button
        size="icon" variant="ghost"
        aria-label={starred ? 'Unstar' : 'Star'} title={starred ? 'Unstar' : 'Star'}
        onClick={() => run(() => (starred ? actions.unstar : actions.star)([message.id]), starred ? 'Unstarred' : 'Starred')}
      >
        <Icon name={starred ? 'star-filled' : 'star'} size={18} className={starred ? 'text-warning' : ''} />
      </Button>
      <Button
        size="icon" variant="ghost"
        aria-label="Mark unread" title="Mark unread"
        onClick={() => run(() => actions.markUnread([message.id]), 'Marked unread').then(() => navigate(`/mail/${props.mailboxId}/${props.folderId}`))}
      >
        <Icon name="mail" size={18} />
      </Button>
      <Button
        size="icon" variant="ghost"
        aria-label="Archive" title="Archive"
        disabled={busy}
        onClick={() => run(() => actions.archive([message.id]), 'Archived').then(() => navigate(`/mail/${props.mailboxId}/${props.folderId}`))}
      >
        <Icon name="archive" size={18} />
      </Button>
      <Button
        size="icon" variant="ghost"
        aria-label="Trash" title="Trash"
        onClick={() => run(() => actions.trash([message.id]), 'Moved to Trash').then(() => navigate(`/mail/${props.mailboxId}/${props.folderId}`))}
      >
        <Icon name="trash" size={18} />
      </Button>
      <Menu
        trigger={
          <Button size="icon" variant="ghost" aria-label="More actions" title="More">
            <Icon name="more" size={18} />
          </Button>
        }
      >
        <MenuItem onClick={() => window.print()}>Print</MenuItem>
        <MenuItem
          danger
          onClick={() => run(() => actions.destroy([message.id]), 'Deleted').then(() => navigate(`/mail/${props.mailboxId}/${props.folderId}`))}
        >
          Delete permanently
        </MenuItem>
      </Menu>
    </div>
  );
}

// ===== Thread message =====
function ThreadMessage({
  message, expanded: initialExpanded, showButtons,
  onReply, onReplyAll, onForward,
  allowRemoteImagesDefault,
}: {
  message: MessageDetail;
  expanded: boolean;
  showButtons: boolean;
  onReply: () => void;
  onReplyAll: () => void;
  onForward: () => void;
  allowRemoteImagesDefault: boolean;
}) {
  const [expanded, setExpanded] = useState(initialExpanded);
  const trusted = getItem<string[]>(StorageKeys.readImages, []);
  const isTrusted = trusted.includes(message.from.email.toLowerCase());
  const [allowImages, setAllowImages] = useState(allowRemoteImagesDefault || isTrusted);

  useEffect(() => { setExpanded(initialExpanded); }, [initialExpanded]);

  const rendered = useMemo(() => {
    const html = message.body_html ?? escapeHtml(message.body_text ?? '');
    return renderEmail(html, { allowRemoteImages: allowImages });
  }, [message.body_html, message.body_text, allowImages]);

  function trustSender() {
    const next = Array.from(new Set([...trusted, message.from.email.toLowerCase()]));
    setItem(StorageKeys.readImages, next);
    setAllowImages(true);
  }

  const authFailed =
    (message.auth_results &&
      (message.auth_results.dmarc === 'fail' ||
        message.auth_results.dkim === 'fail' ||
        message.auth_results.spf === 'fail')) ||
    false;

  // Spoof heuristics (lookalike domains, name/address mismatch, spam score)
  // — advisory banners from lib/spoof.ts, in addition to auth_results.
  const spoofAlerts = analyzeMessage(message);

  if (!expanded) {
    return (
      <div className="border border-line rounded mb-2">
        <button
          type="button"
          onClick={() => setExpanded(true)}
          className="w-full text-left p-3 flex items-start gap-3 hover:bg-canvas"
        >
          <Avatar name={message.from.name} email={message.from.email} size={28} />
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2">
              <span className="text-sm font-medium truncate">{message.from.name || message.from.email}</span>
              <span className="text-xs text-ink-mute">{formatDateTime(message.received_at)}</span>
            </div>
            <div className="text-xs text-ink-soft truncate">{message.snippet}</div>
          </div>
        </button>
      </div>
    );
  }

  return (
    <div className="border border-line rounded mb-2 overflow-hidden">
      <header className="p-3 border-b border-line bg-canvas">
        <div className="flex items-start gap-3">
          <Avatar name={message.from.name} email={message.from.email} size={32} />
          <div className="flex-1 min-w-0">
            <div className="flex items-baseline gap-2 flex-wrap">
              <span className="text-sm font-medium">{message.from.name || message.from.email}</span>
              <span className="text-xs text-ink-mute">&lt;{message.from.email}&gt;</span>
            </div>
            <div className="text-xs text-ink-mute mt-0.5">
              To: {message.to.map((a) => a.email).join(', ')}
              {message.cc.length > 0 && ` · Cc: ${message.cc.map((a) => a.email).join(', ')}`}
            </div>
            <div className="text-xs text-ink-mute">{formatDateTime(message.received_at)}</div>
          </div>
          {initialExpanded && (
            <button
              type="button"
              onClick={() => setExpanded(false)}
              className="text-xs text-ink-mute hover:text-ink"
            >
              Collapse
            </button>
          )}
        </div>
      </header>

      {message.external_sender && (
        <Banner
          tone="warning"
          icon="alert-triangle"
          title="External sender"
          body="This message was sent from outside your organisation. Take care with links and attachments."
        />
      )}
      {authFailed && (
        <Banner
          tone="danger"
          icon="shield-check"
          title="This message may be spoofed"
          body={`Email authentication failed (SPF: ${message.auth_results?.spf}, DKIM: ${message.auth_results?.dkim}, DMARC: ${message.auth_results?.dmarc}). Treat links and attachments with caution.`}
        />
      )}
      {spoofAlerts
        .filter((a) => !authFailed || a.title !== 'Sender authentication failed')
        .map((a) => (
          <Banner
            key={a.title}
            tone={a.severity === 'high' ? 'danger' : 'warning'}
            icon="alert-triangle"
            title={a.title}
            body={a.detail}
          />
        ))}

      {rendered.hadRemoteImages && !allowImages && (
        <Banner
          tone="info"
          icon="image"
          title={`${rendered.blockedImages.length} remote image${rendered.blockedImages.length === 1 ? '' : 's'} blocked`}
          body="Images from remote servers can notify the sender when you read this message."
          actions={
            <div className="flex gap-2 mt-2">
              <Button size="sm" onClick={() => setAllowImages(true)}>Show images once</Button>
              <Button size="sm" variant="secondary" onClick={trustSender}>Always for this sender</Button>
            </div>
          }
        />
      )}

      {rendered.externalLinks.length > 0 && (
        <Banner
          tone="info"
          icon="external-link"
          title={`${rendered.externalLinks.length} external link${rendered.externalLinks.length === 1 ? '' : 's'}`}
          body="Links open in a new tab. Hover to preview before clicking."
        />
      )}

      <RenderedBody html={rendered.html} allowRemoteImages={allowImages} />

      {message.attachments.length > 0 && (
        <div className="border-t border-line p-3">
          <p className="text-xs uppercase text-ink-mute mb-2">Attachments</p>
          <div className="flex flex-wrap gap-2">
            {message.attachments.map((a) => (
              <AttachmentChip key={a.id} attachment={a} messageId={message.id} />
            ))}
          </div>
        </div>
      )}

      {showButtons && (
        <div className="border-t border-line p-3 flex gap-2">
          <Button size="sm" onClick={onReply}><Icon name="reply" size={14} /> Reply</Button>
          <Button size="sm" variant="secondary" onClick={onReplyAll}><Icon name="reply-all" size={14} /> Reply all</Button>
          <Button size="sm" variant="secondary" onClick={onForward}><Icon name="forward" size={14} /> Forward</Button>
        </div>
      )}
    </div>
  );
}

// ===== Rendered body (iframe) =====
function RenderedBody({ html, allowRemoteImages }: { html: string; allowRemoteImages: boolean }) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const srcDoc = wrapForIframe(html, { allowRemoteImages });

  function resize() {
    const el = iframeRef.current;
    if (!el) return;
    const doc = el.contentDocument;
    if (!doc) return;
    const h = Math.max(doc.documentElement.scrollHeight, doc.body?.scrollHeight ?? 0, 200);
    el.style.height = `${h + 8}px`;
  }

  return (
    <iframe
      ref={iframeRef}
      title="Message body"
      sandbox="allow-popups allow-popups-to-escape-sandbox"
      srcDoc={srcDoc}
      className="w-full border-0 bg-white"
      style={{ minHeight: 200 }}
      onLoad={resize}
    />
  );
}

// ===== Banner =====
function Banner({
  tone, icon, title, body, actions,
}: {
  tone: 'info' | 'warning' | 'danger';
  icon: Parameters<typeof Icon>[0]['name'];
  title: string;
  body: string;
  actions?: React.ReactNode;
}) {
  const tones = {
    info: 'bg-canvas border-line',
    warning: 'bg-warning-soft border-warning/30',
    danger: 'bg-danger-soft border-danger/30',
  } as const;
  return (
    <div className={clsx('border-b px-3 py-2 text-sm', tones[tone])}>
      <div className="flex items-start gap-2">
        <Icon name={icon} size={14} className="mt-0.5 shrink-0" />
        <div>
          <p className="font-medium">{title}</p>
          <p className="text-xs text-ink-soft mt-0.5">{body}</p>
          {actions}
        </div>
      </div>
    </div>
  );
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
    .replace(/\n/g, '<br>');
}
