import { useCallback, useMemo } from 'react';
import { useKeyboardShortcuts, type Shortcut } from '@/hooks';
import { useSequenceShortcut } from './useSequenceShortcut';

export interface MailShortcutHandlers {
  next?: () => void;
  prev?: () => void;
  back?: () => void;
  archive?: () => void;
  trash?: () => void;
  star?: () => void;
  markRead?: () => void;
  markUnread?: () => void;
  compose?: () => void;
  search?: () => void;
  help?: () => void;
  reply?: () => void;
  replyAll?: () => void;
  forward?: () => void;
  escape?: () => void;
  goInbox?: () => void;
  goSent?: () => void;
  goDrafts?: () => void;
  goArchive?: () => void;
  goSpam?: () => void;
  goTrash?: () => void;
}

export function useMailShortcuts(h: MailShortcutHandlers, enabled = true) {
  const shortcuts = useMemo<Shortcut[]>(() => {
    const out: Shortcut[] = [];
    const add = (keys: string, handler?: () => void, description?: string) => {
      if (handler) out.push({ keys, handler, description });
    };
    add('j', h.next, 'Next message');
    add('k', h.prev, 'Previous message');
    add('u', h.back, 'Back to list');
    add('e', h.archive, 'Archive');
    add('#', h.trash, 'Delete');
    add('s', h.star, 'Star / unstar');
    add('shift+u', h.markUnread, 'Mark unread');
    add('shift+i', h.markRead, 'Mark read');
    add('c', h.compose, 'Compose');
    add('r', h.reply, 'Reply');
    add('a', h.replyAll, 'Reply all');
    add('f', h.forward, 'Forward');
    add('/', h.search, 'Search');
    add('?', h.help, 'Keyboard shortcuts');
    add('escape', h.escape, 'Close');
    return out;
  }, [h]);

  useKeyboardShortcuts(shortcuts, { enabled });

  // Sequence shortcuts (g then X) — declared separately so they don't collide
  // with the single-key bindings above.
  const goInbox = useCallback(() => h.goInbox?.(), [h.goInbox]);
  const goSent = useCallback(() => h.goSent?.(), [h.goSent]);
  const goDrafts = useCallback(() => h.goDrafts?.(), [h.goDrafts]);
  const goArchive = useCallback(() => h.goArchive?.(), [h.goArchive]);
  const goSpam = useCallback(() => h.goSpam?.(), [h.goSpam]);
  const goTrash = useCallback(() => h.goTrash?.(), [h.goTrash]);
  useSequenceShortcut(['g', 'i'], goInbox, enabled && !!h.goInbox);
  useSequenceShortcut(['g', 's'], goSent, enabled && !!h.goSent);
  useSequenceShortcut(['g', 'd'], goDrafts, enabled && !!h.goDrafts);
  useSequenceShortcut(['g', 'a'], goArchive, enabled && !!h.goArchive);
  useSequenceShortcut(['g', '!'], goSpam, enabled && !!h.goSpam);
  useSequenceShortcut(['g', 't'], goTrash, enabled && !!h.goTrash);
}
