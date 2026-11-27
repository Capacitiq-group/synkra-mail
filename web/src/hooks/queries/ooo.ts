import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { OutOfOfficeState } from '@/lib/types';

const oooKey = (mailboxId: string) => ['ooo', mailboxId];

export function useOoo(mailboxId: string) {
  return useQuery({
    queryKey: oooKey(mailboxId),
    queryFn: () => api.getOutOfOfficePeriods(mailboxId),
    enabled: !!mailboxId,
    staleTime: 15_000,
  });
}

export function useSetOoo(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (state: OutOfOfficeState) => api.setOutOfOfficePeriods(mailboxId, state),
    onSuccess: (data) => qc.setQueryData(oooKey(mailboxId), data),
  });
}
