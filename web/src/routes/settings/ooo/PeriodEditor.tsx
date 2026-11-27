import { useState } from 'react';
import { Button, Checkbox, DateTimePicker, Field, Input, Modal, Select, Switch, Textarea } from '@/design';
import { toast } from '@/stores/toast';
import { useAuth } from '@/stores/auth';
import { toLocalTimezone } from '@/lib/time';
import type { OutOfOfficePeriod } from '@/lib/types';

interface Props {
  open: boolean;
  onClose: () => void;
  period: OutOfOfficePeriod | null;
  onSave: (p: OutOfOfficePeriod) => void;
}

export function PeriodEditor({ open, onClose, period, onSave }: Props) {
  const me = useAuth((s) => s.me);
  const initial: OutOfOfficePeriod = period ?? {
    id: `p${Date.now()}`,
    enabled: true,
    starts_at: new Date().toISOString(),
    ends_at: new Date(Date.now() + 7 * 24 * 3600 * 1000).toISOString(),
    timezone: me?.timezone ?? toLocalTimezone(),
    scope: 'all',
    body_internal: '',
    body_external: '',
    reply_frequency_days: 1,
    reply_to_known_only: false,
  };
  const [draft, setDraft] = useState<OutOfOfficePeriod>(initial);
  const set = (p: Partial<OutOfOfficePeriod>) => setDraft((d) => ({ ...d, ...p }));

  function save() {
    if (!draft.starts_at || !draft.ends_at) { toast('error', 'Start and end are required'); return; }
    if (new Date(draft.ends_at) <= new Date(draft.starts_at)) { toast('error', 'End must be after start'); return; }
    if (draft.scope !== 'internal' && !draft.body_external?.trim()) {
      toast('error', 'External reply message is required');
      return;
    }
    if (draft.scope !== 'external' && !draft.body_internal?.trim()) {
      toast('error', 'Internal reply message is required');
      return;
    }
    onSave(draft);
    onClose();
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={period ? 'Edit out-of-office period' : 'Add out-of-office period'}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button variant="primary" onClick={save}>Save period</Button>
        </>
      }
    >
      <div className="space-y-3">
        <Checkbox
          label="Enabled"
          checked={draft.enabled}
          onChange={(e) => set({ enabled: e.target.checked })}
        />

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <DateTimePicker label="Starts at" value={draft.starts_at} onChange={(v) => set({ starts_at: v })} />
          <DateTimePicker label="Ends at" value={draft.ends_at} onChange={(v) => set({ ends_at: v })} />
        </div>

        <Field label="Timezone">
          <Select value={draft.timezone} onChange={(e) => set({ timezone: e.target.value })}>
            <option value="Africa/Johannesburg">Africa/Johannesburg</option>
            <option value="UTC">UTC</option>
          </Select>
        </Field>

        <Field label="Who gets a reply">
          <Select value={draft.scope} onChange={(e) => set({ scope: e.target.value as OutOfOfficePeriod['scope'] })}>
            <option value="all">Everyone (internal and external)</option>
            <option value="internal">Internal senders only</option>
            <option value="external">External senders only</option>
          </Select>
        </Field>

        {(draft.scope === 'all' || draft.scope === 'internal') && (
          <Field label="Internal reply message">
            <Textarea
              value={draft.body_internal ?? ''}
              onChange={(e) => set({ body_internal: e.target.value })}
              rows={4}
              placeholder="I'm away until Monday. For urgent matters contact sales@company.co.za."
            />
          </Field>
        )}
        {(draft.scope === 'all' || draft.scope === 'external') && (
          <Field label="External reply message">
            <Textarea
              value={draft.body_external ?? ''}
              onChange={(e) => set({ body_external: e.target.value })}
              rows={4}
              placeholder="Thank you for your email. I'm away and will return on 4 December. For urgent matters please contact accounts@company.co.za."
            />
          </Field>
        )}

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Field label="Reply frequency" hint="Once per sender per N days.">
            <Input
              type="number" min={1} max={30}
              value={draft.reply_frequency_days}
              onChange={(e) => set({ reply_frequency_days: Math.max(1, Math.min(30, parseInt(e.target.value, 10) || 1)) })}
            />
          </Field>
          <div className="pt-5">
            <Switch
              checked={draft.reply_to_known_only}
              onChange={(v) => set({ reply_to_known_only: v })}
              label="Only to known contacts"
            />
          </div>
        </div>

        <div className="border border-line rounded p-3 bg-canvas text-xs text-ink-soft">
          <strong>Loop prevention is on.</strong> We never send auto-replies to automated senders, mailing lists,
          or messages with a null return-path. This cannot be disabled.
        </div>
      </div>
    </Modal>
  );
}
