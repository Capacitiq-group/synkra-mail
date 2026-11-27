import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { useTestRule } from '@/hooks/queries/rules';
import { useMessages } from '@/hooks/queries/mail';
import { Button, Modal, Select } from '@/design';
import { Icon } from '@/design/icons';
import { describeAction } from './metadata';
import { errorMessage } from '@/lib/errors';
import type { RuleAction } from '@/lib/types';

export function TestRuleModal({
  open, onClose, ruleId, mailboxId,
}: {
  open: boolean;
  onClose: () => void;
  ruleId: string;
  mailboxId: string;
}) {
  const foldersQ = useQuery({ queryKey: ['folders', mailboxId], queryFn: () => api.folders(mailboxId) });
  const inboxId = foldersQ.data?.find((f) => f.role === 'inbox')?.id ?? '';
  const messagesQ = useMessages(mailboxId, inboxId, { q: '', isUnread: false, hasAttachment: false });
  const test = useTestRule();
  const [messageId, setMessageId] = useState('');
  const [result, setResult] = useState<{ matched: boolean; actions: RuleAction[] } | null>(null);

  async function run() {
    if (!messageId) return;
    try {
      const r = await test.mutateAsync({ id: ruleId, messageId });
      setResult(r);
    } catch (e) {
      setResult(null);
      alert(errorMessage(e));
    }
  }

  const messages = messagesQ.data?.pages.flatMap((p) => p.items).slice(0, 50) ?? [];

  return (
    <Modal
      open={open}
      onClose={() => { setResult(null); onClose(); }}
      title="Test rule"
      description="Run this rule against a recent message to confirm it matches."
      footer={<Button variant="secondary" onClick={onClose}>Close</Button>}
    >
      <div className="space-y-3">
        <Select value={messageId} onChange={(e) => { setMessageId(e.target.value); setResult(null); }}>
          <option value="">— pick a message —</option>
          {messages.map((m) => (
            <option key={m.id} value={m.id}>
              {m.from.email} — {m.subject || '(no subject)'}
            </option>
          ))}
        </Select>
        <Button variant="primary" onClick={run} loading={test.isPending} disabled={!messageId}>
          Run test
        </Button>
        {result && (
          <div className={`p-3 border rounded ${result.matched ? 'border-success bg-success-soft' : 'border-line bg-canvas'}`}>
            <div className="flex items-center gap-2">
              <Icon
                name={result.matched ? 'check-circle' : 'info'}
                size={14}
                className={result.matched ? 'text-success' : 'text-ink-mute'}
              />
              <p className="text-sm font-medium">
                {result.matched ? 'Matched — the rule would fire.' : 'Did not match.'}
              </p>
            </div>
            {result.matched && result.actions.length > 0 && (
              <ul className="mt-2 text-xs space-y-1">
                {result.actions.map((a, i) => <li key={i}>· {describeAction(a)}</li>)}
              </ul>
            )}
          </div>
        )}
      </div>
    </Modal>
  );
}
