import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';

export function usePrivacySettings() {
  return useQuery({ queryKey: ['privacy-settings'], queryFn: api.privacySettings, staleTime: 300_000 });
}

export function useUpdatePrivacySettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.updatePrivacySettings,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['privacy-settings'] }),
  });
}

export function useProcessingRegister() {
  return useQuery({ queryKey: ['privacy-register'], queryFn: api.processingRegister, staleTime: 300_000 });
}

export function useSubProcessors() {
  return useQuery({ queryKey: ['privacy-sub-processors'], queryFn: api.subProcessors, staleTime: 300_000 });
}

export function useDsarRequests() {
  return useQuery({ queryKey: ['privacy-dsar'], queryFn: api.dsarRequests, staleTime: 30_000 });
}

export function useCreateDsar() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.createDsar,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['privacy-dsar'] }),
  });
}
