import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { Signature } from '@/lib/types';

const signaturesKey = (mailboxId: string) => ['signatures', mailboxId];

export function useSignatures(mailboxId: string) {
  return useQuery({
    queryKey: signaturesKey(mailboxId),
    queryFn: () => api.listSignatures(mailboxId),
    enabled: !!mailboxId,
    staleTime: 60_000,
  });
}

export function useCreateSignature(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: Omit<Signature, 'id'>) => api.createSignature(mailboxId, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: signaturesKey(mailboxId) }),
  });
}

export function useUpdateSignature(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Partial<Signature> }) => api.updateSignature(id, patch),
    onSuccess: () => qc.invalidateQueries({ queryKey: signaturesKey(mailboxId) }),
  });
}

export function useDeleteSignature(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteSignature(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: signaturesKey(mailboxId) }),
  });
}

export function useSetDefaultSignature(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.setDefaultSignature(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: signaturesKey(mailboxId) }),
  });
}
