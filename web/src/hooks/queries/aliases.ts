import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { Alias } from '@/lib/types';

const aliasesKey = (mailboxId: string) => ['aliases', mailboxId];

export function useAliases(mailboxId: string) {
  return useQuery({
    queryKey: aliasesKey(mailboxId),
    queryFn: () => api.listAliases(mailboxId),
    enabled: !!mailboxId,
    staleTime: 30_000,
  });
}

export function useCreateAlias(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: Parameters<typeof api.createAlias>[1]) => api.createAlias(mailboxId, input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: aliasesKey(mailboxId) });
      qc.invalidateQueries({ queryKey: ['identities', mailboxId] });
    },
  });
}

export function useUpdateAlias(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Partial<Alias> }) => api.updateAlias(id, patch),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: aliasesKey(mailboxId) });
      qc.invalidateQueries({ queryKey: ['identities', mailboxId] });
    },
  });
}

export function useDeleteAlias(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteAlias(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: aliasesKey(mailboxId) });
      qc.invalidateQueries({ queryKey: ['identities', mailboxId] });
    },
  });
}

export function useBulkCreateAliases(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: Parameters<typeof api.bulkCreateAliases>[1]) => api.bulkCreateAliases(mailboxId, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: aliasesKey(mailboxId) }),
  });
}
