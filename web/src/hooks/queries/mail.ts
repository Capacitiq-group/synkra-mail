import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { qk } from '@/lib/queryKeys';
import type { CursorPage, Folder, Label, Mailbox, MessageDetail, MessageSummary, Thread } from '@/lib/types';

const PAGE_SIZE = 50;

// ===== Read queries =====
export function useMailboxes() {
  return useQuery({ queryKey: qk.mailboxes(), queryFn: api.mailboxes, staleTime: 30_000 });
}

export function useMailbox(id: string) {
  return useQuery({ queryKey: qk.mailbox(id), queryFn: () => api.mailbox(id), enabled: !!id });
}

export function useFolders(mailboxId: string) {
  return useQuery({
    queryKey: qk.folders(mailboxId),
    queryFn: () => api.folders(mailboxId),
    enabled: !!mailboxId,
    staleTime: 15_000,
  });
}

export function useLabels() {
  return useQuery({ queryKey: qk.labels(), queryFn: api.labels, staleTime: 60_000 });
}

export interface MessageListFilter { q: string; isUnread: boolean; hasAttachment: boolean }

export function useMessages(
  mailboxId: string,
  folderId: string,
  filter: MessageListFilter,
) {
  const useSearch = !!filter.q || filter.isUnread || filter.hasAttachment;
  return useInfiniteQuery({
    queryKey: qk.messages(mailboxId, folderId, filter),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => {
      if (useSearch) {
        return api.searchMessages(
          mailboxId,
          {
            folder: folderId,
            q: filter.q || undefined,
            is_unread: filter.isUnread || undefined,
            has_attachment: filter.hasAttachment || undefined,
          },
          pageParam,
        );
      }
      return api.listMessages(mailboxId, {
        folder: folderId,
        cursor: pageParam,
        limit: PAGE_SIZE,
      });
    },
    getNextPageParam: (last: CursorPage<MessageSummary>) =>
      last.has_more ? last.next_cursor ?? undefined : undefined,
    enabled: !!mailboxId && !!folderId,
  });
}

export function useMessage(id: string | undefined) {
  return useQuery({
    queryKey: qk.message(id ?? ''),
    queryFn: ({ signal }) => api.getMessage(id!, signal),
    enabled: !!id,
    staleTime: 10_000,
  });
}

export function useThread(threadId: string | undefined) {
  return useQuery({
    queryKey: qk.thread(threadId ?? ''),
    queryFn: () => api.getThread(threadId!),
    enabled: !!threadId,
    staleTime: 10_000,
  });
}

export function usePrivacySettings() {
  return useQuery({
    queryKey: qk.privacySettings(),
    queryFn: api.privacySettings,
    staleTime: 5 * 60_000,
  });
}

// ===== Mutations =====
export function useMarkMessageRead() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.setFlags(id, { is_read: true }),
    onSuccess: (_d, id) => {
      qc.invalidateQueries({ queryKey: qk.message(id) });
      qc.invalidateQueries({ queryKey: ['messages'] });
      qc.invalidateQueries({ queryKey: ['folders'] });
    },
  });
}

export interface BulkInput {
  mailboxId: string;
  ids: string[];
  action: 'read' | 'unread' | 'flag' | 'unflag' | 'archive' | 'trash' | 'delete' | 'move';
  folderId?: string;
}

export function useBulkAction() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: BulkInput) => api.bulkAction(input.ids, input.action, input.folderId),
    onSuccess: (_d, input) => {
      qc.invalidateQueries({ queryKey: qk.messagesPrefix(input.mailboxId) });
      qc.invalidateQueries({ queryKey: qk.folders(input.mailboxId) });
      for (const id of input.ids) qc.removeQueries({ queryKey: qk.message(id) });
    },
  });
}

// ===== Folder mutations =====
export function useCreateFolder(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ name, parentId }: { name: string; parentId?: string }) =>
      api.createFolder(mailboxId, name, parentId),
    onSuccess: (f: Folder) => {
      qc.invalidateQueries({ queryKey: qk.folders(mailboxId) });
      return f;
    },
  });
}

export function useRenameFolder(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ folderId, name }: { folderId: string; name: string }) =>
      api.renameFolder(mailboxId, folderId, name),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.folders(mailboxId) }),
  });
}

export function useDeleteFolder(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (folderId: string) => api.deleteFolder(mailboxId, folderId),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.folders(mailboxId) }),
  });
}

// ===== Label mutations =====
export function useCreateLabel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ name, color }: { name: string; color: string }) => api.createLabel(name, color),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.labels() }),
  });
}

export function useDeleteLabel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteLabel(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.labels() });
      qc.invalidateQueries({ queryKey: ['messages'] });
    },
  });
}

export function useApplyLabel(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ ids, labelId, remove }: { ids: string[]; labelId: string; remove?: boolean }) => {
      for (const id of ids) {
        if (remove) await api.removeLabelFromMessage(id, labelId);
        else await api.addLabelToMessage(id, labelId);
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.messagesPrefix(mailboxId) });
      qc.invalidateQueries({ queryKey: ['folders'] });
    },
  });
}

// ===== Helper: flatten all loaded pages =====
export function flattenPages(pages: CursorPage<MessageSummary>[] | undefined): MessageSummary[] {
  if (!pages) return [];
  return pages.flatMap((p) => p.items);
}

// ===== Helper: keep mailboxes list fresh after moving between folders =====
export function useInvalidateMailboxView(mailboxId: string) {
  const qc = useQueryClient();
  return () => {
    qc.invalidateQueries({ queryKey: qk.folders(mailboxId) });
    qc.invalidateQueries({ queryKey: qk.messagesPrefix(mailboxId) });
  };
}

// Re-export types for convenience.
export type { Folder, Label, Mailbox, MessageDetail, MessageSummary, Thread };
