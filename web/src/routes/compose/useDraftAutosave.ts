import { useEffect, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { useComposeStore } from './useComposeStore';
import { clearLocalAutosave, writeLocalAutosave } from './autosave';
import { qk } from '@/lib/queryKeys';

const DEBOUNCE_MS = 1500;

/**
 * Persists compose state to the server as a draft (debounced) and mirrors every
 * change to a local autosave so a browser crash loses nothing.
 * Skipped when: nothing to save yet, or currently sending.
 */
export function useDraftAutosave() {
  const qc = useQueryClient();
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastSavedRef = useRef<string>('');

  useEffect(() => {
    const unsub = useComposeStore.subscribe((state) => {
      if (!state.initialized) return;
      if (state.sending) return;

      const snap = state.toSnapshot();
      if (!snap) return;

      // Mirror to local autosave immediately (cheap, no round-trip).
      writeLocalAutosave(state.sessionId, snap);

      // Skip server save if nothing meaningful has changed.
      const meaningful =
        snap.to.length > 0 ||
        snap.cc.length > 0 ||
        snap.bcc.length > 0 ||
        snap.subject.trim().length > 0 ||
        (snap.bodyText ?? '').trim().length > 0 ||
        snap.attachmentIds.length > 0;
      if (!meaningful) return;

      const fingerprint = JSON.stringify({
        to: snap.to, cc: snap.cc, bcc: snap.bcc, subject: snap.subject,
        bodyHtml: snap.bodyHtml, bodyText: snap.bodyText, attachmentIds: snap.attachmentIds,
        inReplyTo: snap.inReplyTo,
      });
      if (fingerprint === lastSavedRef.current) return;

      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(async () => {
        try {
          const identity = state.identities.find((i) => i.id === snap.identityId);
          const draft = await api.saveDraft({
            id: snap.draftId,
            mailbox_id: snap.mailboxId,
            from: identity ? { email: identity.address, name: identity.display_name } : undefined,
            to: snap.to,
            cc: snap.cc,
            bcc: snap.bcc,
            subject: snap.subject,
            body_text: snap.bodyText,
            body_html: snap.bodyHtml || undefined,
            in_reply_to: snap.inReplyTo,
            attachment_ids: snap.attachmentIds,
          });
          if (!snap.draftId && draft.id) useComposeStore.getState().setDraftId(draft.id);
          lastSavedRef.current = fingerprint;
          qc.invalidateQueries({ queryKey: ['drafts', snap.mailboxId] });
        } catch {
          // Draft persistence failures are non-fatal — the next keystroke retries.
        }
      }, DEBOUNCE_MS);
    });

    return () => {
      unsub();
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [qc]);

  // Best-effort save on page unload — the local autosave is already written,
  // so a refresh loses nothing even if this fails.
  useEffect(() => {
    const onUnload = () => {
      const state = useComposeStore.getState();
      const snap = state.toSnapshot();
      if (snap) writeLocalAutosave(state.sessionId, snap);
    };
    window.addEventListener('beforeunload', onUnload);
    return () => window.removeEventListener('beforeunload', onUnload);
  }, []);
}

/** Call when the compose is intentionally finished (sent or discarded). */
export function clearComposePersistence(mailboxId?: string, draftId?: string) {
  clearLocalAutosave();
  if (mailboxId) {
    import('@/lib/queryClient').then(({ queryClient }) => {
      queryClient.invalidateQueries({ queryKey: ['drafts', mailboxId] });
      if (draftId) queryClient.removeQueries({ queryKey: ['draft', draftId] });
      queryClient.invalidateQueries({ queryKey: qk.folders(mailboxId) });
    });
  }
}
