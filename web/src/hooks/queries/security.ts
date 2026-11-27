import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { Passkey } from '@/lib/types';

export function usePasskeys() {
  return useQuery({ queryKey: ['passkeys'], queryFn: api.listPasskeys, staleTime: 60_000 });
}

export function useDeletePasskey() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deletePasskey(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['passkeys'] });
      qc.invalidateQueries({ queryKey: ['me'] });
    },
  });
}

export function useSessions() {
  return useQuery({ queryKey: ['sessions'], queryFn: api.listSessions, staleTime: 15_000 });
}

export function useRevokeSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.revokeSession(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sessions'] }),
  });
}

export function useRevokeAllSessions() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.revokeAllSessions(),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sessions'] }),
  });
}

export function useDevices() {
  return useQuery({ queryKey: ['devices'], queryFn: api.listDevices, staleTime: 30_000 });
}

export function useRevokeDevice() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.revokeDevice(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['devices'] }),
  });
}

export function useAppPasswords() {
  return useQuery({ queryKey: ['app-passwords'], queryFn: api.listAppPasswords, staleTime: 30_000 });
}

export function useCreateAppPassword() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ label, scopes }: { label: string; scopes: ('imap' | 'smtp')[] }) =>
      api.createAppPassword(label, scopes),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['app-passwords'] }),
  });
}

export function useRevokeAppPassword() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.revokeAppPassword(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['app-passwords'] }),
  });
}

export function useIpRestrictions(enabled: boolean) {
  return useQuery({
    queryKey: ['ip-restrictions'],
    queryFn: api.org.ipRestrictions,
    enabled,
    staleTime: 60_000,
  });
}

export function useSetIpRestrictions() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (cidrs: string[]) => api.org.setIpRestrictions(cidrs),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['ip-restrictions'] }),
  });
}

export function useSsoConfig(enabled: boolean) {
  return useQuery({
    queryKey: ['sso-config'],
    queryFn: api.org.ssoConfig,
    enabled,
    staleTime: 60_000,
  });
}

export function useSetSsoConfig() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.org.setSsoConfig,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sso-config'] }),
  });
}

export type { Passkey };
