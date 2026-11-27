import { useState } from 'react';
import { Button, DateTimePicker, Field, Modal, Select } from '@/design';
import { addDays } from '@/lib/time';

interface Props {
  open: boolean;
  onClose: () => void;
  onSchedule: (iso: string) => void;
}

type Preset = 'in1h' | 'tomorrow9' | 'monday9' | 'custom';

function nextMonday9(): string {
  const d = new Date();
  const day = d.getDay();
  const delta = ((1 + 7 - day) % 7) || 7;
  d.setDate(d.getDate() + delta);
  d.setHours(9, 0, 0, 0);
  return d.toISOString();
}

function tomorrow9(): string {
  const d = new Date(addDays(new Date().toISOString(), 1));
  d.setHours(9, 0, 0, 0);
  return d.toISOString();
}

function inOneHour(): string {
  return new Date(Date.now() + 60 * 60_000).toISOString();
}

export function SendLaterDialog({ open, onClose, onSchedule }: Props) {
  const [preset, setPreset] = useState<Preset>('tomorrow9');
  const [custom, setCustom] = useState(tomorrow9());

  function submit() {
    const iso =
      preset === 'in1h' ? inOneHour()
      : preset === 'tomorrow9' ? tomorrow9()
      : preset === 'monday9' ? nextMonday9()
      : custom;
    if (!iso) return;
    onSchedule(iso);
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Schedule send"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button variant="primary" onClick={submit}>Schedule</Button>
        </>
      }
    >
      <div className="space-y-3">
        <Field label="When">
          <Select value={preset} onChange={(e) => setPreset(e.target.value as Preset)}>
            <option value="in1h">In one hour</option>
            <option value="tomorrow9">Tomorrow at 09:00</option>
            <option value="monday9">Next Monday at 09:00</option>
            <option value="custom">Pick a date and time…</option>
          </Select>
        </Field>
        {preset === 'custom' && (
          <DateTimePicker label="Send at" value={custom} onChange={setCustom} min={new Date().toISOString()} />
        )}
      </div>
    </Modal>
  );
}
