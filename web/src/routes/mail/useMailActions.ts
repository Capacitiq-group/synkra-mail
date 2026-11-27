import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useCallback } from 'react';
import { api } from '@/lib/api';
import { qk } from '@/lib/queryKeys';

export interface MailActionHelpers {
  markRead: (ids: string[]) => Promise<void>;
  markUnread: (ids: string[]) => Promise<void>;
  star: (ids: string[]) => Promise<void>;
  unstar: (ids: string[]) => Promise<void>;
  archive: (ids: string[]) => Promise<void>;
  trash: (ids: string[]) => Promise<void>;
  move: (ids: string[], folderId: string) => Promise<void>;
  destroy: (ids: string[]) => Promise<void>;
  applyLabel: (ids: string[], labelId: string) => Promise<void>;
  removeLabel: (ids: string[], labelId: string) => Promise<void>;
}

export function useMailActions(mailboxId: string): MailActionHelpers {
  const qc = useQueryClient();

  const invalidate = useCallback(() => {
    qc.invalidateQueries({ queryKey: qk.folders(mailboxId) });
    qc.invalidateQueries({ queryKey: qk.messagesPrefix(mailboxId) });
  }, [mailboxId, qc]);

  const bulk = useMutation({
    mutationFn: (input: Parameters<typeof api.bulkAction>) =>
      api.bulkAction(input[0], input[1], input[2]),
  });

  const act = useCallback(
    async (
      action: 'read' | 'unread' | 'flag' | 'unflag' | 'archive' | 'trash' | 'delete' | 'move',
      ids: string[],
      folderId?: string,
    ) => {
      if (ids.length === 0) return;
      await bulk.mutateAsync([ids, action, folderId]);
      invalidate();
    },
    [bulk, invalidate],
  );

  const labelMutation = useMutation({
    mutationFn: async ({ ids, labelId, remove }: { ids: string[]; labelId: string; remove?: boolean }) => {
      for (const id of ids) {
        if (remove) await api.removeLabelFromMessage(id, labelId);
        else await api.addLabelToMessage(id, labelId);
      }
    },
    onSuccess: () => {
      invalidate();
      qc.invalidateQueries({ queryKey: ['messages'] });
    },
  });

  return {
    markRead: (ids) => act('read', ids),
    markUnread: (ids) => act('unread', ids),
    star: (ids) => act('flag', ids),
    unstar: (ids) => act('unflag', ids),
    archive: (ids) => act('archive', ids),
    trash: (ids) => act('trash', ids),
    move: (ids, folderId) => act('move', ids, folderId),
    destroy: (ids) => act('delete', ids),
    applyLabel: async (ids, labelId) => { await labelMutation.mutateAsync({ ids, labelId }); },
    removeLabel: async (ids, labelId) => { await labelMutation.mutateAsync({ ids, labelId, remove: true }); },
  };
}
