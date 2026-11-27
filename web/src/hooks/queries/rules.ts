import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { Rule } from '@/lib/types';

const rulesKey = (mailboxId: string) => ['rules', mailboxId];

export function useRules(mailboxId: string) {
  return useQuery({
    queryKey: rulesKey(mailboxId),
    queryFn: () => api.listRules(mailboxId),
    enabled: !!mailboxId,
    staleTime: 30_000,
  });
}

export function useCreateRule(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rule: Parameters<typeof api.createRule>[1]) => api.createRule(mailboxId, rule),
    onSuccess: () => qc.invalidateQueries({ queryKey: rulesKey(mailboxId) }),
  });
}

export function useUpdateRule(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Partial<Rule> }) => api.updateRule(id, patch),
    onSuccess: () => qc.invalidateQueries({ queryKey: rulesKey(mailboxId) }),
  });
}

export function useDeleteRule(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteRule(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: rulesKey(mailboxId) }),
  });
}

export function useReorderRules(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (order: string[]) => api.reorderRules(mailboxId, order),
    onSuccess: () => qc.invalidateQueries({ queryKey: rulesKey(mailboxId) }),
  });
}

export function useTestRule() {
  return useMutation({
    mutationFn: ({ id, messageId }: { id: string; messageId: string }) => api.testRule(id, messageId),
  });
}
