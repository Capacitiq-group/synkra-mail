import { useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { errorMessage } from '@/lib/errors';
import { toast, useToast } from '@/stores/toast';
import { useComposeStore } from './useComposeStore';
import { clearComposePersistence } from './useDraftAutosave';
import { qk } from '@/lib/queryKeys';
import { formatDateTime } from '@/lib/time';
import type { SendRequest } from '@/lib/types';

export function useSendCompose() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const removeToast = useToast((s) => s.remove);

  const send = useCallback(async () => {
    const s = useComposeStore.getState();
    if (s.sending) return;

    // Validation
    if (!s.mailboxId) { s.setSendError('No mailbox selected'); return; }
    if (s.to.length === 0 && s.cc.length === 0 && s.bcc.length === 0) {
      s.setSendError('Add at least one recipient');
      return;
    }
    if (!s.identityId) { s.setSendError('Choose a sender identity'); return; }

    const identity = s.identities.find((i) => i.id === s.identityId);
    if (!identity) { s.setSendError('Sender identity is invalid'); return; }

    s.setSendError(null);
    s.setSending(true);

    const snapshot = s.toSnapshot();

    const req: SendRequest = {
      mailbox_id: s.mailboxId,
      from: { email: identity.address, name: identity.display_name },
      to: s.to,
      cc: s.cc,
      bcc: s.bcc,
      subject: s.subject,
      body_text: s.bodyText,
      body_html: s.bodyHtml || undefined,
      in_reply_to: s.inReplyTo,
      references: s.references,
      attachment_ids: s.attachmentIds.length > 0 ? s.attachmentIds : undefined,
      schedule_at: s.scheduledFor,
      undo_send_seconds: !s.scheduledFor && s.undoSeconds > 0 ? s.undoSeconds : undefined,
    };

    try {
      const res = await api.send(req);

      if (res.suppressed.length > 0) {
        toast(
          'warning',
          `${res.suppressed.length} recipient${res.suppressed.length === 1 ? '' : 's'} suppressed`,
          `Skipped: ${res.suppressed.join(', ')}`,
        );
      }

      clearComposePersistence(s.mailboxId, s.draftId);
      s.reset();
      qc.invalidateQueries({ queryKey: qk.folders(s.mailboxId) });
      qc.invalidateQueries({ queryKey: qk.messagesPrefix(s.mailboxId) });

      // Navigate to the Sent folder if we can resolve it.
      const folders = qc.getQueryData<{ id: string; role: string | null }[]>(qk.folders(s.mailboxId)) ?? [];
      const sentFolder = folders.find((f) => f.role === 'sent');

      // Undo-send: show a toast with a countdown and a cancel action.
      const queuedUntil = res.queued_until ? new Date(res.queued_until).getTime() : 0;
      const now = Date.now();
      const undoWindowMs = Math.max(0, queuedUntil - now);

      if (!s.scheduledFor && undoWindowMs > 0 && snapshot) {
        const toastId = toast('success', 'Message sent', `Undo within ${Math.round(undoWindowMs / 1000)}s`, {
          label: 'Undo',
          onClick: async () => {
            removeToast(toastId);
            try {
              await api.cancelScheduledSend(res.message_id);
              useComposeStore.setState({ ...snapshot, initialized: true, sending: false, sendError: null, sessionId: `u${Date.now()}` });
              toast('info', 'Message cancelled — back in the composer');
              navigate('/compose');
            } catch (e) {
              toast('error', 'Could not cancel', errorMessage(e));
            }
          },
        });
        qc.invalidateQueries({ queryKey: qk.folders(s.mailboxId) });

        if (sentFolder) navigate(`/mail/${s.mailboxId}/${sentFolder.id}`);
        else navigate(`/mail/${s.mailboxId}`);
        return;
      }

      if (res.scheduled_for) {
        toast('success', `Scheduled for ${formatDateTime(res.scheduled_for)}`);
      } else {
        toast('success', 'Message sent');
      }

      if (sentFolder) navigate(`/mail/${s.mailboxId}/${sentFolder.id}`);
      else navigate(`/mail/${s.mailboxId}`);
    } catch (e) {
      s.setSendError(errorMessage(e));
      s.setSending(false);
      return;
    }

    s.setSending(false);
  }, [navigate, qc, removeToast]);

  return { send };
}
