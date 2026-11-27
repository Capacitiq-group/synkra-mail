import { useState } from 'react';
import { Button, Field, Input, Modal, Select } from '@/design';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { useBulkCreateAliases } from '@/hooks/queries/aliases';
import type { Alias } from '@/lib/types';

interface Props {
  mailboxId: string;
  open: boolean;
  onClose: () => void;
  onCreated: () => void;
}

export function BulkCreateModal({ mailboxId, open, onClose, onCreated }: Props) {
  const bulk = useBulkCreateAliases(mailboxId);
  const [count, setCount] = useState(5);
  const [prefix, setPrefix] = useState('alias');
  const [category, setCategory] = useState<Alias['category']>('other');

  async function submit() {
    try {
      const created = await bulk.mutateAsync({ count, label_prefix: prefix.trim() || 'alias', category });
      toast('success', `${created.length} aliases created`);
      onCreated();
      onClose();
    } catch (err) {
      toast('error', 'Could not create aliases', errorMessage(err));
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Bulk create aliases"
      description="Create several random aliases at once — useful when signing up for a batch of services."
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button variant="primary" onClick={submit} loading={bulk.isPending}>Create {count}</Button>
        </>
      }
    >
      <div className="space-y-3">
        <Field label="How many?">
          <Input
            type="number"
            min={1}
            max={50}
            value={count}
            onChange={(e) => setCount(Math.max(1, Math.min(50, parseInt(e.target.value, 10) || 1)))}
          />
        </Field>
        <Field label="Label prefix" hint="Each alias label becomes `${prefix} 1`, `${prefix} 2`, …">
          <Input value={prefix} onChange={(e) => setPrefix(e.target.value)} />
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
      </div>
    </Modal>
  );
}
