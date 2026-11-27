import { memo } from 'react';
import clsx from 'clsx';
import { Icon } from '@/design/icons';
import { Avatar } from '@/design';
import { truncate } from '@/lib/format';
import type { Folder, Label, MessageSummary } from '@/lib/types';

// NOTE: listTime is in @/lib/time — importing from correct path:
// eslint-disable-next-line import/order
import { listTime } from '@/lib/time';

interface Props {
  message: MessageSummary;
  folderRole: Folder['role'];
  selected: boolean;
  active: boolean;
  labels: Label[];
  onToggleSelect: () => void;
  onOpen: () => void;
}

export const MessageListItem = memo(function MessageListItem({
  message, folderRole, selected, active, labels, onToggleSelect, onOpen,
}: Props) {
  const showTo = folderRole === 'sent' || folderRole === 'drafts';
  const display = showTo
    ? message.to[0]?.name || message.to[0]?.email || '(no recipient)'
    : message.from.name || message.from.email;
  const starred = message.flags.includes('flagged');
  const unread = !message.is_read;

  return (
    <li data-message-id={message.id}>
      <div
        role="button"
        tabIndex={0}
        onClick={onOpen}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onOpen(); }
        }}
        aria-current={active ? 'true' : undefined}
        className={clsx(
          'group flex items-start gap-2 px-3 py-2.5 border-b border-line cursor-pointer',
          active ? 'bg-accent-soft' : 'hover:bg-canvas',
          unread && !active && 'bg-surface',
          selected && 'bg-canvas',
        )}
      >
        <span className="pt-1" onClick={(e) => e.stopPropagation()}>
          <input
            type="checkbox"
            checked={selected}
            onChange={onToggleSelect}
            aria-label={`Select message from ${message.from.email}`}
            className="rounded border-line opacity-0 group-hover:opacity-100 focus:opacity-100 data-[checked=true]:opacity-100"
            data-checked={selected ? 'true' : 'false'}
          />
        </span>

        <span className="pt-0.5">
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              // Direct star toggle via bulk action to keep list responsive.
              // Called from parent through a callback would be cleaner,
              // but we keep this local for snappy feedback.
              // MessageList wraps with optimistic update via mutation.
              // To stay functional without an optimistic mutation wrapper,
              // the caller passes onToggleSelect only; use bulk star here.
              // (See MessageList for the API call path.)
              // We route through window for simplicity, avoiding duplicate hooks.
              const evt = new CustomEvent('synkra:star', { detail: { id: message.id } });
              window.dispatchEvent(evt);
            }}
            className="p-0.5 rounded hover:bg-black/5"
            aria-label={starred ? 'Unstar message' : 'Star message'}
            aria-pressed={starred}
          >
            <Icon
              name={starred ? 'star-filled' : 'star'}
              size={16}
              className={starred ? 'text-warning' : 'text-ink-mute'}
            />
          </button>
        </span>

        <span className="w-8 shrink-0 hidden sm:flex justify-center pt-0.5">
          <Avatar name={message.from.name} email={message.from.email} size={24} />
        </span>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className={clsx('text-sm truncate', unread ? 'font-semibold' : 'text-ink-soft')}>
              {display}
            </span>
            {message.has_attachments && (
              <Icon name="attachment" size={12} className="text-ink-mute shrink-0" aria-label="Has attachments" />
            )}
            {message.external_sender && (
              <Icon name="alert-triangle" size={12} className="text-warning shrink-0" aria-label="External sender" />
            )}
          </div>
          <div className={clsx('text-sm truncate', unread ? 'font-medium' : 'text-ink-soft')}>
            {truncate(message.subject || '(no subject)', 120)}
          </div>
          <div className="text-xs text-ink-mute truncate">
            {truncate(message.snippet, 140)}
          </div>
          {labels.length > 0 && (
            <div className="mt-1 flex flex-wrap gap-1">
              {labels.map((l) => (
                <span
                  key={l.id}
                  className="inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium"
                  style={{ backgroundColor: l.color + '22', color: l.color }}
                >
                  {l.name}
                </span>
              ))}
            </div>
          )}
        </div>

        <span className="w-16 shrink-0 text-right text-xs text-ink-mute pt-0.5">
          {listTime(message.received_at)}
        </span>
      </div>
    </li>
  );
});
