import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { Note } from '@/lib/types';

const notesKey = (scope: { messageId?: string; threadId?: string }) =>
  ['notes', scope.messageId ?? '', scope.threadId ?? ''] as const;

export function useNotes(scope: { messageId?: string; threadId?: string }) {
  return useQuery({
    queryKey: notesKey(scope),
    queryFn: () =>
      scope.messageId
        ? api.listNotes({ messageId: scope.messageId })
        : scope.threadId
        ? api.listThreadNotes(scope.threadId)
        : Promise.resolve([] as Note[]),
    enabled: !!(scope.messageId || scope.threadId),
    staleTime: 10_000,
  });
}

export function useCreateNote(scope: { messageId?: string; threadId?: string }) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ body, isPrivate }: { body: string; isPrivate: boolean }) =>
      scope.messageId
        ? api.createNote({ messageId: scope.messageId }, body, isPrivate)
        : api.createThreadNote(scope.threadId!, body, isPrivate),
    onSuccess: () => qc.invalidateQueries({ queryKey: notesKey(scope) }),
  });
}

export function useDeleteNote(scope: { messageId?: string; threadId?: string }) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteNote(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: notesKey(scope) }),
  });
}
