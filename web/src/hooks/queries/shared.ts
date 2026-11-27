import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { ClaimReplyResponse, ReplyOwner, SharedActivityEvent, SharedDraft, SharedMailbox, SharedStatus, ThreadAssignment } from '@/lib/types';

const listKey = () => ['shared-mailboxes'] as const;
const oneKey = (id: string) => ['shared-mailbox', id] as const;
const assignmentKey = (smid: string, tid: string) => ['shared-assignment', smid, tid] as const;
const ownerKey = (smid: string, tid: string) => ['shared-owner', smid, tid] as const;
const activityKey = (smid: string, tid?: string) => ['shared-activity', smid, tid ?? 'all'] as const;
const draftsKey = (smid: string) => ['shared-drafts', smid] as const;

export function useSharedMailboxes() {
  return useQuery({ queryKey: listKey(), queryFn: api.listSharedMailboxes, staleTime: 60_000 });
}

export function useSharedMailbox(id: string | undefined) {
  return useQuery({
    queryKey: oneKey(id ?? ''),
    queryFn: () => api.getSharedMailbox(id!),
    enabled: !!id,
    staleTime: 60_000,
  });
}

export function useThreadAssignment(sharedMailboxId: string, threadId: string | undefined) {
  return useQuery({
    queryKey: assignmentKey(sharedMailboxId, threadId ?? ''),
    queryFn: () => api.getThreadAssignment(sharedMailboxId, threadId!),
    enabled: !!sharedMailboxId && !!threadId,
    staleTime: 15_000,
    refetchInterval: 60_000,
  });
}

export function useReplyOwner(sharedMailboxId: string, threadId: string | undefined) {
  return useQuery({
    queryKey: ownerKey(sharedMailboxId, threadId ?? ''),
    queryFn: () => api.getReplyOwner(sharedMailboxId, threadId!),
    enabled: !!sharedMailboxId && !!threadId,
    staleTime: 15_000,
    refetchInterval: 30_000,
  });
}

export function useClaimReply(sharedMailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (threadId: string): Promise<ClaimReplyResponse> => api.claimReply(sharedMailboxId, threadId),
    onSuccess: (res, threadId) => {
      qc.setQueryData(ownerKey(sharedMailboxId, threadId), res.owner);
      qc.invalidateQueries({ queryKey: assignmentKey(sharedMailboxId, threadId) });
      qc.invalidateQueries({ queryKey: activityKey(sharedMailboxId) });
    },
  });
}

export function useReleaseReply(sharedMailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (threadId: string) => api.releaseReply(sharedMailboxId, threadId),
    onSuccess: (_d, threadId) => {
      qc.setQueryData(ownerKey(sharedMailboxId, threadId), null);
      qc.invalidateQueries({ queryKey: assignmentKey(sharedMailboxId, threadId) });
    },
  });
}

export function useUpdateAssignment(sharedMailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ threadId, patch }: { threadId: string; patch: { assigned_to?: string | null; status?: SharedStatus } }) =>
      api.updateThreadAssignment(sharedMailboxId, threadId, patch),
    onSuccess: (data, vars) => {
      qc.setQueryData(assignmentKey(sharedMailboxId, vars.threadId), data);
      qc.invalidateQueries({ queryKey: activityKey(sharedMailboxId) });
      qc.invalidateQueries({ queryKey: activityKey(sharedMailboxId, vars.threadId) });
    },
  });
}

export function useSharedActivity(sharedMailboxId: string, threadId?: string) {
  return useQuery({
    queryKey: activityKey(sharedMailboxId, threadId),
    queryFn: () => api.sharedActivity(sharedMailboxId, { thread_id: threadId, limit: 50 }),
    enabled: !!sharedMailboxId,
    staleTime: 15_000,
  });
}

export function useSharedDrafts(sharedMailboxId: string) {
  return useQuery({
    queryKey: draftsKey(sharedMailboxId),
    queryFn: () => api.sharedDrafts(sharedMailboxId),
    enabled: !!sharedMailboxId,
    staleTime: 15_000,
  });
}

export type { SharedMailbox, ThreadAssignment, ReplyOwner, SharedActivityEvent, SharedDraft };
