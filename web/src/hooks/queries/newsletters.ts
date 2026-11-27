import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { DigestPreferences, NewsletterSender } from '@/lib/types';

const newslettersKey = (mailboxId: string) => ['newsletters', mailboxId];
const digestKey = (mailboxId: string) => ['digest', mailboxId];

export function useNewsletterSenders(mailboxId: string) {
  return useQuery({
    queryKey: newslettersKey(mailboxId),
    queryFn: () => api.listNewsletterSenders(mailboxId),
    enabled: !!mailboxId,
    staleTime: 60_000,
  });
}

export function useSetNewsletterPref(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ sender, pref }: { sender: string; pref: Partial<Pick<NewsletterSender, 'muted' | 'digest'>> }) =>
      api.setNewsletterPref(mailboxId, sender, pref),
    onSuccess: () => qc.invalidateQueries({ queryKey: newslettersKey(mailboxId) }),
  });
}

export function useUnsubscribeOneClick() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (messageId: string) => api.unsubscribeOneClick(messageId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['newsletters'] }),
  });
}

export function useDigestPrefs(mailboxId: string) {
  return useQuery({
    queryKey: digestKey(mailboxId),
    queryFn: () => api.getDigestPrefs(mailboxId),
    enabled: !!mailboxId,
    staleTime: 60_000,
  });
}

export function useSetDigestPrefs(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (prefs: DigestPreferences) => api.setDigestPrefs(mailboxId, prefs),
    onSuccess: (data) => qc.setQueryData(digestKey(mailboxId), data),
  });
}
