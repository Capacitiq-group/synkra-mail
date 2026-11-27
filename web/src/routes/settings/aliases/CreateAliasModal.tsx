import { useState } from 'react';
import { Button, Checkbox, DateTimePicker, Field, Input, Modal, Select } from '@/design';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { useCreateAlias } from '@/hooks/queries/aliases';
import type { Alias } from '@/lib/types';

interface Props {
  mailboxId: string;
  domain: string;
  open: boolean;
  onClose: () => void;
  onCreated: (alias: Alias) => void;
}

type Mode = 'random' | 'custom';

export function CreateAliasModal({ mailboxId, domain, open, onClose, onCreated }: Props) {
  const create = useCreateAlias(mailboxId);
  const [mode, setMode] = useState<Mode>('random');
  const [local, setLocal] = useState('');
  const [label, setLabel] = useState('');
  const [category, setCategory] = useState<Alias['category']>('other');
  const [singleUse, setSingleUse] = useState(false);
  const [expires, setExpires] = useState('');
  const [forwardTo, setForwardTo] = useState('');

  function reset() {
    setMode('random');
    setLocal('');
    setLabel('');
    setCategory('other');
    setSingleUse(false);
    setExpires('');
    setForwardTo('');
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    try {
      const created = await create.mutateAsync({
        address: mode === 'custom' ? `${local}@${domain}` : undefined,
        label: label.trim() || 'New alias',
        category,
        single_use: singleUse || undefined,
        expires_at: expires || undefined,
        forward_to: forwardTo.trim() || undefined,
      });
      toast('success', 'Alias created', created.address);
      onCreated(created);
      reset();
      onClose();
    } catch (err) {
      toast('error', 'Could not create alias', errorMessage(err));
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="New alias"
      description="Forward mail sent to this alias to your mailbox. You can disable or delete it at any time."
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            onClick={submit}
            loading={create.isPending}
            disabled={mode === 'custom' && !local.trim()}
          >Create alias</Button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3">
        <div role="radiogroup" aria-label="Address type" className="flex gap-2">
          <label className={`flex-1 p-2 border rounded cursor-pointer ${mode === 'random' ? 'border-accent bg-accent-soft' : 'border-line'}`}>
            <input type="radio" className="sr-only" checked={mode === 'random'} onChange={() => setMode('random')} />
            <div className="text-sm font-medium">Generate</div>
            <div className="text-xs text-ink-soft">A random address like <span className="font-mono">quiet-forest-482@…</span></div>
          </label>
          <label className={`flex-1 p-2 border rounded cursor-pointer ${mode === 'custom' ? 'border-accent bg-accent-soft' : 'border-line'}`}>
            <input type="radio" className="sr-only" checked={mode === 'custom'} onChange={() => setMode('custom')} />
            <div className="text-sm font-medium">Custom</div>
            <div className="text-xs text-ink-soft">Choose the local part yourself</div>
          </label>
        </div>

        {mode === 'custom' && (
          <Field label="Address" hint="Lowercase letters, numbers, dots, dashes and underscores.">
            <div className="flex items-stretch">
              <Input
                value={local}
                onChange={(e) => setLocal(e.target.value.toLowerCase().replace(/[^a-z0-9._-]/g, ''))}
                placeholder="amazon"
                className="rounded-r-none"
              />
              <span className="inline-flex items-center px-3 border border-l-0 border-line rounded-r bg-canvas text-sm text-ink-soft font-mono">
                @{domain}
              </span>
            </div>
          </Field>
        )}

        <Field label="Label" required>
          <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="e.g. Amazon shopping" />
        </Field>

        <Field label="Category">
          <Select value={category} onChange={(e) => setCategory(e.target.value as Alias['category'])}>
            <option value="shopping">Shopping</option>
            <option value="newsletters">Newsletters</option>
            <option value="suppliers">Suppliers</option>
            <option value="personal">Personal</option>
            <option value="other">Other</option>
          </Select>
        </Field>

        <Field label="Forward to (optional)" hint="Leave blank to forward to your own mailbox.">
          <Input type="email" value={forwardTo} onChange={(e) => setForwardTo(e.target.value)} placeholder="someone@example.com" />
        </Field>

        <Checkbox label="Single-use (deactivates after the first received message)" checked={singleUse} onChange={(e) => setSingleUse(e.target.checked)} />

        <DateTimePicker label="Expires at (optional)" value={expires} onChange={setExpires} min={new Date().toISOString()} />
      </form>
    </Modal>
  );
}
