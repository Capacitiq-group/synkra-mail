import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { BimiState, DmarcPhase, DmarcRolloutState, MtaStsMode, MtaStsState, ReliabilityAlert, TlsRptState } from '@/lib/types';

export function useDeliverabilitySummary(domain: string, days = 30) {
  return useQuery({
    queryKey: ['deliverability', domain, days],
    queryFn: () => api.deliverability(domain, days),
    enabled: !!domain,
    staleTime: 30_000,
  });
}

export function useDeliverabilityTimeseries(domain: string, days = 30) {
  return useQuery({
    queryKey: ['deliverability-ts', domain, days],
    queryFn: () => api.deliverabilityTimeseries(domain, days),
    enabled: !!domain,
    staleTime: 60_000,
  });
}

export function useMailTrace(messageId: string | undefined) {
  return useQuery({
    queryKey: ['mail-trace', messageId ?? ''],
    queryFn: () => api.mailTrace(messageId!),
    enabled: !!messageId,
    retry: false,
    staleTime: 30_000,
  });
}

export function useHealthScore() {
  return useQuery({
    queryKey: ['health-score'],
    queryFn: api.healthScore,
    staleTime: 60_000,
  });
}

export function useReliabilityAlerts() {
  return useQuery({
    queryKey: ['reliability-alerts'],
    queryFn: api.reliabilityAlerts,
    staleTime: 30_000,
    refetchInterval: 60_000,
  });
}

export function useAcknowledgeAlert() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.acknowledgeAlert(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: ['reliability-alerts'] });
      const prev = qc.getQueryData<ReliabilityAlert[]>(['reliability-alerts']);
      if (prev) {
        qc.setQueryData<ReliabilityAlert[]>(
          ['reliability-alerts'],
          prev.map((a) => (a.id === id ? { ...a, acknowledged_at: new Date().toISOString() } : a)),
        );
      }
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(['reliability-alerts'], ctx.prev);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ['reliability-alerts'] }),
  });
}

export function useDmarcRollout(domain: string) {
  return useQuery({
    queryKey: ['dmarc-rollout', domain],
    queryFn: () => api.dmarcRollout(domain),
    enabled: !!domain,
    staleTime: 30_000,
  });
}

export function useSetDmarcPhase(domain: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (phase: DmarcPhase) => api.setDmarcPhase(domain, phase),
    onSuccess: (data) => {
      qc.setQueryData<DmarcRolloutState>(['dmarc-rollout', domain], data);
      qc.invalidateQueries({ queryKey: ['deliverability', domain] });
      qc.invalidateQueries({ queryKey: ['health-score'] });
    },
  });
}

export function useDmarcReports(domain: string) {
  return useQuery({
    queryKey: ['dmarc-reports', domain],
    queryFn: () => api.dmarcReports(domain),
    enabled: !!domain,
    staleTime: 120_000,
  });
}

export function useBimiState(domain: string) {
  return useQuery({
    queryKey: ['bimi', domain],
    queryFn: () => api.bimiState(domain),
    enabled: !!domain,
    staleTime: 60_000,
  });
}

export function useSetBimi(domain: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: { enabled: boolean; logo_url?: string }) => api.setBimi(domain, patch),
    onSuccess: (data) => {
      qc.setQueryData<BimiState>(['bimi', domain], data);
      qc.invalidateQueries({ queryKey: ['deliverability', domain] });
      qc.invalidateQueries({ queryKey: ['health-score'] });
    },
  });
}

export function useMtaStsState(domain: string) {
  return useQuery({
    queryKey: ['mta-sts', domain],
    queryFn: () => api.mtaStsState(domain),
    enabled: !!domain,
    staleTime: 60_000,
  });
}

export function useSetMtaSts(domain: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: { enabled: boolean; mode: MtaStsMode; max_age_seconds: number }) => api.setMtaSts(domain, patch),
    onSuccess: (data) => {
      qc.setQueryData<MtaStsState>(['mta-sts', domain], data);
      qc.invalidateQueries({ queryKey: ['deliverability', domain] });
      qc.invalidateQueries({ queryKey: ['health-score'] });
    },
  });
}

export function useTlsRptState(domain: string) {
  return useQuery({
    queryKey: ['tls-rpt', domain],
    queryFn: () => api.tlsRptState(domain),
    enabled: !!domain,
    staleTime: 60_000,
  });
}

export function useSetTlsRpt(domain: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: { enabled: boolean; rua_address?: string }) => api.setTlsRpt(domain, patch),
    onSuccess: (data) => {
      qc.setQueryData<TlsRptState>(['tls-rpt', domain], data);
      qc.invalidateQueries({ queryKey: ['deliverability', domain] });
      qc.invalidateQueries({ queryKey: ['health-score'] });
    },
  });
}

export type { ReliabilityAlert };
