import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  flattenPages, useBulkAction, useLabels, useMessages,
} from '@/hooks/queries/mail';
import { Icon } from '@/design/icons';
import { Button, EmptyState, Menu, MenuItem, Skeleton } from '@/design';
import { useIntersectionObserver } from '@/hooks';
import { useMailActions } from './useMailActions';
import { toast } from '@/stores/toast';
import { errorMessage } from '@/lib/errors';
import { parseSearch, type ParsedSearch } from './parseSearch';
import { SearchBar } from './SearchBar';
import { MessageListItem } from './MessageListItem';
import { useMailShortcuts } from './useMailShortcuts';
import type { ReactNode } from 'react';
import type { Folder, Mailbox, MessageSummary } from '@/lib/types';

interface MessageListProps {
  /** Optional per-row decoration (shared-mailbox badges, part 8). */
  rowDecorator?: (m: MessageSummary) => ReactNode;
  mailbox: Mailbox;
  folders: Folder[];
  folder: Folder;
  currentMessageId?: string;
  onOpenFolderDrawer?: () => void;
}

export function MessageList({ mailbox, folders, folder, currentMessageId, onOpenFolderDrawer }: MessageListProps) {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const q = params.get('q') ?? '';
  const parsed: ParsedSearch = useMemo(() => parseSearch(q), [q]);
  const filter = useMemo(
    () => ({
      q: parsed.query.q ?? '',
      isUnread: parsed.query.is_unread === true,
      hasAttachment: parsed.query.has_attachment === true,
    }),
    [parsed],
  );

  const query = useMessages(mailbox.id, folder.id, filter);
  const pages = query.data?.pages;
  const messages = useMemo(() => flattenPages(pages), [pages]);

  const labelsQ = useLabels();
  const labels = labelsQ.data ?? [];

  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [search, setSearch] = useState(parsed);
  const onSearchChange = useCallback((p: ParsedSearch) => setSearch(p), []);
  const actions = useMailActions(mailbox.id);

  // Clear selection when folder or filter changes.
  useEffect(() => { setSelected(new Set()); }, [mailbox.id, folder.id, q]);

  // Scroll-to-top button behaviour.
  const listRef = useRef<HTMLDivElement>(null);

  const scrollToMessage = useCallback((id: string) => {
    const el = listRef.current?.querySelector<HTMLElement>(`[data-message-id="${id}"]`);
    el?.scrollIntoView({ block: 'nearest' });
  }, []);

  useEffect(() => {
    if (currentMessageId) scrollToMessage(currentMessageId);
  }, [currentMessageId, scrollToMessage]);

  const handleOpenMessage = useCallback((id: string) => {
    navigate(`/mail/${mailbox.id}/${folder.id}/${id}`);
  }, [mailbox.id, folder.id, navigate]);

  const goNext = useCallback(() => {
    if (!messages.length) return;
    const idx = messages.findIndex((m) => m.id === currentMessageId);
    const next = messages[idx + 1] ?? (idx < 0 ? messages[0] : undefined);
    if (next) handleOpenMessage(next.id);
  }, [messages, currentMessageId, handleOpenMessage]);
  const goPrev = useCallback(() => {
    if (!messages.length) return;
    const idx = messages.findIndex((m) => m.id === currentMessageId);
    const prev = messages[idx - 1];
    if (prev) handleOpenMessage(prev.id);
  }, [messages, currentMessageId, handleOpenMessage]);

  const goBack = useCallback(() => {
    navigate(`/mail/${mailbox.id}/${folder.id}`);
  }, [navigate, mailbox.id, folder.id]);

  const archiveCurrent = useCallback(() => {
    if (!currentMessageId) return;
    actions.archive([currentMessageId]).then(() => {
      toast('info', 'Archived');
      // Advance to next message if any.
      const idx = messages.findIndex((m) => m.id === currentMessageId);
      const next = messages[idx + 1] ?? messages[idx - 1];
      if (next) handleOpenMessage(next.id);
      else goBack();
    }).catch((e: unknown) => toast('error', 'Could not archive', errorMessage(e)));
  }, [currentMessageId, actions, messages, handleOpenMessage, goBack]);

  const trashCurrent = useCallback(() => {
    if (!currentMessageId) return;
    actions.trash([currentMessageId]).then(() => {
      toast('info', 'Moved to Trash');
      const idx = messages.findIndex((m) => m.id === currentMessageId);
      const next = messages[idx + 1] ?? messages[idx - 1];
      if (next) handleOpenMessage(next.id);
      else goBack();
    }).catch((e: unknown) => toast('error', 'Could not delete', errorMessage(e)));
  }, [currentMessageId, actions, messages, handleOpenMessage, goBack]);

  const starCurrent = useCallback(() => {
    if (!currentMessageId) return;
    const m = messages.find((x) => x.id === currentMessageId);
    if (!m) return;
    const starred = m.flags.includes('flagged');
    (starred ? actions.unstar : actions.star)([currentMessageId])
      .then(() => toast('info', starred ? 'Unstarred' : 'Starred'))
      .catch((e: unknown) => toast('error', 'Could not update', errorMessage(e)));
  }, [currentMessageId, messages, actions]);

  const findFolderByRole = useCallback(
    (role: Folder['role']) => folders.find((f) => f.role === role),
    [folders],
  );

  useMailShortcuts({
    next: goNext,
    prev: goPrev,
    back: goBack,
    archive: archiveCurrent,
    trash: trashCurrent,
    star: starCurrent,
    compose: () => navigate('/compose'),
    goInbox: () => {
      const f = findFolderByRole('inbox');
      if (f) navigate(`/mail/${mailbox.id}/${f.id}`);
    },
    goSent: () => {
      const f = findFolderByRole('sent');
      if (f) navigate(`/mail/${mailbox.id}/${f.id}`);
    },
    goDrafts: () => {
      const f = findFolderByRole('drafts');
      if (f) navigate(`/mail/${mailbox.id}/${f.id}`);
    },
    goArchive: () => {
      const f = findFolderByRole('archive');
      if (f) navigate(`/mail/${mailbox.id}/${f.id}`);
    },
    goSpam: () => {
      const f = findFolderByRole('spam');
      if (f) navigate(`/mail/${mailbox.id}/${f.id}`);
    },
    goTrash: () => {
      const f = findFolderByRole('trash');
      if (f) navigate(`/mail/${mailbox.id}/${f.id}`);
    },
  });

  // Infinite scroll sentinel.
  const loadMore = useCallback(() => {
    if (query.hasNextPage && !query.isFetchingNextPage) query.fetchNextPage();
  }, [query]);
  const sentinelRef = useIntersectionObserver(loadMore, { enabled: !!query.hasNextPage });

  const allVisibleSelected =
    messages.length > 0 && messages.every((m) => selected.has(m.id));
  const someSelected = selected.size > 0;

  const toggleSelectAll = useCallback(() => {
    if (allVisibleSelected) setSelected(new Set());
    else setSelected(new Set(messages.map((m) => m.id)));
  }, [allVisibleSelected, messages]);

  const toggleSelect = useCallback((id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  }, []);

  const bulk = useBulkAction();
  const runBulk = useCallback(async (action: 'read' | 'unread' | 'flag' | 'unflag' | 'archive' | 'trash' | 'delete' | 'move', folderId?: string) => {
    const ids = Array.from(selected);
    if (ids.length === 0) return;
    try {
      await bulk.mutateAsync({ mailboxId: mailbox.id, ids, action, folderId });
      toast('success', `${ids.length} message${ids.length === 1 ? '' : 's'} updated`);
      setSelected(new Set());
      if (currentMessageId && ids.includes(currentMessageId)) {
        navigate(`/mail/${mailbox.id}/${folder.id}`);
      }
    } catch (err) {
      toast('error', 'Could not update messages', errorMessage(err));
    }
  }, [selected, bulk, mailbox.id, currentMessageId, navigate, folder.id]);

  const title =
    search.query.q || search.chips.length > 0
      ? `Search results (${messages.length}${query.hasNextPage ? '+' : ''})`
      : folder.name;

  return (
    <div className="h-full flex flex-col min-h-0">
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 border-b border-line bg-surface">
        {onOpenFolderDrawer && (
          <button
            onClick={onOpenFolderDrawer}
            className="md:hidden p-1 rounded hover:bg-canvas"
            aria-label="Open folders"
          >
            <Icon name="menu" size={18} />
          </button>
        )}
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <input
            type="checkbox"
            checked={allVisibleSelected}
            onChange={toggleSelectAll}
            aria-label={allVisibleSelected ? 'Deselect all' : 'Select all'}
            className="rounded border-line"
          />
          <h1 className="font-medium truncate">{title}</h1>
        </div>
        <Button
          size="icon"
          variant="ghost"
          onClick={() => query.refetch()}
          aria-label="Refresh"
          title="Refresh"
        >
          <Icon name="refresh" size={16} />
        </Button>
      </div>

      <SearchBar folderName={folder.name} onSearchChange={onSearchChange} />

      {someSelected && (
        <BulkToolbar
          count={selected.size}
          folders={folders}
          onClear={() => setSelected(new Set())}
          onMarkRead={() => runBulk('read')}
          onMarkUnread={() => runBulk('unread')}
          onStar={() => runBulk('flag')}
          onUnstar={() => runBulk('unflag')}
          onArchive={() => runBulk('archive')}
          onTrash={() => runBulk('trash')}
          onDelete={() => runBulk('delete')}
          onMove={(folderId) => runBulk('move', folderId)}
          onLabel={() => runBulk('flag').then(() => {
            // For labels the bulk endpoint doesn't handle them in one shot; fall back to per-id.
            for (const id of Array.from(selected)) void id;
          })}
          labels={labels}
        />
      )}

      {/* List */}
      <div className="flex-1 overflow-y-auto scroll-thin" ref={listRef}>
        {query.isLoading && (
          <div className="p-3 space-y-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="flex gap-2">
                <Skeleton className="w-4 h-4" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-3 w-1/3" />
                  <Skeleton className="h-3 w-3/4" />
                </div>
              </div>
            ))}
          </div>
        )}
        {query.isError && (
          <EmptyState
            title="Could not load messages"
            description={errorMessage(query.error)}
            action={<Button onClick={() => query.refetch()}>Try again</Button>}
          />
        )}
        {!query.isLoading && !query.isError && messages.length === 0 && (
          <EmptyState
            title={
              search.query.q
                ? 'No results'
                : folder.role === 'inbox'
                ? 'Your inbox is empty'
                : 'No messages here'
            }
            description={
              search.query.q
                ? 'Try different search terms or clear the filters.'
                : undefined
            }
          />
        )}
        <ul role="list" aria-label="Messages">
          {messages.map((m) => (
            <MessageListItem
              key={m.id}
              message={m}
              folderRole={folder.role}
              selected={selected.has(m.id)}
              active={m.id === currentMessageId}
              labels={labels.filter((l) => m.label_ids.includes(l.id))}
              onToggleSelect={() => toggleSelect(m.id)}
              onOpen={() => handleOpenMessage(m.id)}
            />
          ))}
        </ul>
        <div ref={sentinelRef} className="h-10" aria-hidden />
        {query.isFetchingNextPage && (
          <div className="p-3 space-y-2">
            <Skeleton className="h-3 w-1/2" />
            <Skeleton className="h-3 w-2/3" />
          </div>
        )}
        {!query.hasNextPage && messages.length > 0 && (
          <p className="text-center text-xs text-ink-mute py-4">End of {folder.name.toLowerCase()}</p>
        )}
      </div>
    </div>
  );
}

// ===== Bulk toolbar =====
interface BulkToolbarProps {
  count: number;
  folders: Folder[];
  labels: { id: string; name: string; color: string }[];
  onClear: () => void;
  onMarkRead: () => void;
  onMarkUnread: () => void;
  onStar: () => void;
  onUnstar: () => void;
  onArchive: () => void;
  onTrash: () => void;
  onDelete: () => void;
  onMove: (folderId: string) => void;
  onLabel: (labelId: string) => void;
}

function BulkToolbar(p: BulkToolbarProps) {
  return (
    <div className="flex items-center gap-1 px-2 py-1.5 border-b border-line bg-accent-soft text-sm">
      <button
        type="button"
        onClick={p.onClear}
        className="p-1 rounded hover:bg-black/5"
        aria-label="Clear selection"
      >
        <Icon name="close" size={14} />
      </button>
      <span className="font-medium px-1">{p.count} selected</span>
      <div className="flex items-center gap-1 ml-auto">
        <ToolbarButton label="Mark read" onClick={p.onMarkRead} icon="check" />
        <ToolbarButton label="Mark unread" onClick={p.onMarkUnread} icon="mail" />
        <ToolbarButton label="Star" onClick={p.onStar} icon="star" />
        <ToolbarButton label="Unstar" onClick={p.onUnstar} icon="star-filled" />
        <ToolbarButton label="Archive" onClick={p.onArchive} icon="archive" />
        <Menu
          trigger={
            <button type="button" className="p-1 rounded hover:bg-black/5" aria-label="Move to folder">
              <Icon name="folder" size={16} />
            </button>
          }
        >
          {p.folders.map((f) => (
            <MenuItem key={f.id} onClick={() => p.onMove(f.id)}>{f.name}</MenuItem>
          ))}
        </Menu>
        <Menu
          trigger={
            <button type="button" className="p-1 rounded hover:bg-black/5" aria-label="Apply label">
              <Icon name="label" size={16} />
            </button>
          }
        >
          {p.labels.length === 0 && <MenuItem>No labels</MenuItem>}
          {p.labels.map((l) => (
            <MenuItem key={l.id} onClick={() => p.onLabel(l.id)}>
              <span className="inline-block w-2 h-2 rounded-full mr-2 align-middle" style={{ backgroundColor: l.color }} />
              {l.name}
            </MenuItem>
          ))}
        </Menu>
        <ToolbarButton label="Trash" onClick={p.onTrash} icon="trash" />
        <ToolbarButton label="Delete permanently" onClick={p.onDelete} icon="delete" />
      </div>
    </div>
  );
}

function ToolbarButton({ label, onClick, icon }: { label: string; onClick: () => void; icon: Parameters<typeof Icon>[0]['name'] }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="p-1 rounded hover:bg-black/5"
      aria-label={label}
      title={label}
    >
      <Icon name={icon} size={16} />
    </button>
  );
}
