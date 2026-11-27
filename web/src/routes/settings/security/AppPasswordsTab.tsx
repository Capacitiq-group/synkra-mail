import { useState } from 'react';
import { useAppPasswords, useCreateAppPassword, useRevokeAppPassword } from '@/hooks/queries/security';
import { Button, Checkbox, ConfirmDialog, EmptyState, Field, Input, Modal, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { formatDateTime, relativeTime } from '@/lib/time';
import { CopyButton } from '@/components/CopyButton';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import type { AppPassword } from '@/lib/types';

export function AppPasswordsTab() {
  const q = useAppPasswords();
  const revoke = useRevokeAppPassword();
  const [createOpen, setCreateOpen] = useState(false);
  const [revokeTarget, setRevokeTarget] = useState<AppPassword | null>(null);

  return (
    <div className="max-w-2xl space-y-4">
      <p className="text-sm text-ink-soft">
        App passwords let third-party mail clients (Thunderbird, Apple Mail, Outlook) sign in to your mailbox without
        using your main password. Each password can be limited to receiving, sending, or both.
      </p>

      <Button variant="primary" onClick={() => setCreateOpen(true)}>
        <Icon name="plus" size={14} /> Create app password
      </Button>

      {q.isLoading && <Skeleton className="h-20 w-full" />}
      {q.data && q.data.length === 0 && (
        <EmptyState
          icon={<Icon name="lock" size={28} />}
          title="No app passwords"
          description="Create one for each device or application you connect."
        />
      )}
      {q.data && q.data.length > 0 && (
        <ul className="space-y-2">
          {q.data.filter((p) => !p.revoked).map((p) => (
            <li key={p.id} className="flex items-start gap-3 p-3 border border-line rounded">
              <Icon name="lock" size={16} className="text-ink-mute" />
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium">{p.label}</p>
                <p className="text-xs text-ink-soft">Scopes: {p.scopes.join(', ')}</p>
                <p className="text-xs text-ink-mute">
                  Created {formatDateTime(p.created_at)}
                  {p.last_used_at ? ` · last used ${relativeTime(p.last_used_at)}` : ' · never used'}
                  {p.last_used_ip ? ` from ${p.last_used_ip}` : ''}
                </p>
              </div>
              <Button size="sm" variant="ghost" onClick={() => setRevokeTarget(p)}>Revoke</Button>
            </li>
          ))}
        </ul>
      )}

      <CreateAppPasswordModal open={createOpen} onClose={() => setCreateOpen(false)} />

      <ConfirmDialog
        open={!!revokeTarget}
        onCancel={() => setRevokeTarget(null)}
        onConfirm={async () => {
          if (!revokeTarget) return;
          try {
            await revoke.mutateAsync(revokeTarget.id);
            toast('success', 'App password revoked');
            setRevokeTarget(null);
          } catch (e) {
            toast('error', 'Could not revoke', errorMessage(e));
          }
        }}
        title={`Revoke "${revokeTarget?.label}"?`}
        description="Any application using this password will lose access immediately."
        confirmLabel="Revoke"
        danger
        busy={revoke.isPending}
      />
    </div>
  );
}

function CreateAppPasswordModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const create = useCreateAppPassword();
  const [label, setLabel] = useState('');
  const [imap, setImap] = useState(true);
  const [smtp, setSmtp] = useState(true);
  const [secret, setSecret] = useState<string | null>(null);

  function close() {
    setLabel(''); setImap(true); setSmtp(true); setSecret(null);
    onClose();
  }

  async function submit() {
    if (!label.trim() || (!imap && !smtp)) return;
    const scopes: ('imap' | 'smtp')[] = [];
    if (imap) scopes.push('imap');
    if (smtp) scopes.push('smtp');
    try {
      const res = await create.mutateAsync({ label: label.trim(), scopes });
      setSecret(res.secret);
    } catch (e) {
      toast('error', 'Could not create app password', errorMessage(e));
    }
  }

  return (
    <Modal
      open={open}
      onClose={close}
      title={secret ? 'Save this app password' : 'Create app password'}
      description={secret ? 'This is the only time you will see this password. Copy it now into the app you are configuring.' : undefined}
      footer={
        secret ? (
          <Button variant="primary" onClick={close}>Done</Button>
        ) : (
          <>
            <Button variant="secondary" onClick={close}>Cancel</Button>
            <Button variant="primary" onClick={submit} loading={create.isPending} disabled={!label.trim() || (!imap && !smtp)}>Create</Button>
          </>
        )
      }
    >
      {secret ? (
        <div className="space-y-3">
          <div className="font-mono text-sm border border-line rounded p-3 bg-canvas break-all">{secret}</div>
          <CopyButton value={secret} label="Copy password" />
        </div>
      ) : (
        <div className="space-y-3">
          <Field label="Label" htmlFor="app-pw-label">
            <Input id="app-pw-label" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="e.g. Thunderbird on MacBook" autoFocus />
          </Field>
          <div>
            <p className="text-xs font-medium text-ink-soft mb-1">Allowed protocols</p>
            <div className="space-y-1">
              <Checkbox label="IMAP (receive mail)" checked={imap} onChange={(e) => setImap(e.target.checked)} />
              <Checkbox label="SMTP (send mail)" checked={smtp} onChange={(e) => setSmtp(e.target.checked)} />
            </div>
          </div>
        </div>
      )}
    </Modal>
  );
}
