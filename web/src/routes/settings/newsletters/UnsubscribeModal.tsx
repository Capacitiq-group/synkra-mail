import { useState } from 'react';
import { useUnsubscribeOneClick } from '@/hooks/queries/newsletters';
import { Button, Modal } from '@/design';
import { Icon } from '@/design/icons';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import type { NewsletterSender } from '@/lib/types';

interface Props {
  open: boolean;
  onClose: () => void;
  sender: NewsletterSender | null;
  messageId?: string;
  onDone?: () => void;
}

export function UnsubscribeModal({ open, onClose, sender, messageId, onDone }: Props) {
  const unsub = useUnsubscribeOneClick();
  const [done, setDone] = useState(false);

  if (!sender) return null;

  async function oneClick() {
    if (!messageId) {
      toast('error', 'Open a message from this sender first');
      return;
    }
    try {
      await unsub.mutateAsync(messageId);
      setDone(true);
      toast('success', 'Unsubscribe request sent');
      onDone?.();
    } catch (e) {
      toast('error', 'Could not unsubscribe', errorMessage(e));
    }
  }

  const mechanism = sender.unsubscribe;

  return (
    <Modal
      open={open}
      onClose={() => { setDone(false); onClose(); }}
      title={`Unsubscribe from ${sender.sender.name || sender.sender.email}`}
      footer={
        done ? (
          <Button variant="primary" onClick={() => { setDone(false); onClose(); }}>Done</Button>
        ) : (
          <>
            <Button variant="secondary" onClick={onClose}>Cancel</Button>
            {mechanism === 'one_click' && (
              <Button variant="primary" onClick={oneClick} loading={unsub.isPending}>
                Unsubscribe
              </Button>
            )}
          </>
        )
      }
    >
      {done ? (
        <div className="flex items-start gap-2 text-sm">
          <Icon name="check-circle" size={16} className="text-success mt-0.5" />
          <p>The unsubscribe request was sent. It may take a few days for the sender's system to process.</p>
        </div>
      ) : (
        <div className="space-y-3 text-sm">
          {mechanism === 'one_click' && (
            <>
              <p>This sender supports one-click unsubscribe. Clicking Unsubscribe sends an HTTP request to their server — nothing else.</p>
              <div className="border border-line rounded p-2 bg-canvas text-xs">
                <strong>Safety:</strong> the request is sent from our servers with a short timeout.
                We only call the sender's own endpoint, and the result is logged for your audit trail.
              </div>
            </>
          )}
          {mechanism === 'website' && (
            <p>
              This sender does not support one-click unsubscribe. Open their most recent message and follow the
              link in the body — we cannot send an automated request on your behalf.
            </p>
          )}
          {mechanism === 'link_in_body' && (
            <p>
              Unsubscribe is available only through a link in the message body. Open the most recent message and
              click the unsubscribe link at the bottom.
            </p>
          )}
          {mechanism === 'none' && (
            <p>
              We could not find any unsubscribe mechanism in this sender's emails. You can mute the sender instead —
              future messages go to a folder of your choice without notifying the sender.
            </p>
          )}
        </div>
      )}
    </Modal>
  );
}
