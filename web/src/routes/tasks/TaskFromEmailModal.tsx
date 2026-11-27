import { useState } from 'react';
import { useCreateTaskFromMessage, useTaskLists } from '@/hooks/queries/tasks';
import { Button, Field, Input, Modal, Select, Textarea } from '@/design';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { truncate } from '@/lib/format';
import { formatDateTime } from '@/lib/time';
import type { MessageDetail } from '@/lib/types';

interface Props {
  open: boolean;
  onClose: () => void;
  message: MessageDetail;
}

export function TaskFromEmailModal({ open, onClose, message }: Props) {
  const listsQ = useTaskLists();
  const create = useCreateTaskFromMessage();
  const [listId, setListId] = useState(listsQ.data?.[0]?.id ?? '');
  const [title, setTitle] = useState(message.subject || `Re: ${truncate(message.snippet, 40)}`);
  const [dueAt, setDueAt] = useState('');
  const [err, setErr] = useState<string | null>(null);

  async function submit() {
    if (!title.trim()) { setErr('Title is required'); return; }
    if (!listId) { setErr('Pick a task list'); return; }
    setErr(null);
    try {
      await create.mutateAsync({
        message_id: message.id,
        list_id: listId,
        title: title.trim(),
        due_at: dueAt ? new Date(dueAt).toISOString() : undefined,
      });
      toast('success', 'Task created from message');
      onClose();
    } catch (e) {
      setErr(errorMessage(e));
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Create task from message"
      description={`From: ${message.from.email} · ${formatDateTime(message.received_at)}`}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button variant="primary" onClick={submit} loading={create.isPending}>Create task</Button>
        </>
      }
    >
      <div className="space-y-3">
        <Field label="Title" required>
          <Input value={title} onChange={(e) => setTitle(e.target.value)} autoFocus />
        </Field>
        <Field label="List">
          <Select value={listId} onChange={(e) => setListId(e.target.value)}>
            {listsQ.data?.map((l) => <option key={l.id} value={l.id}>{l.name}</option>)}
          </Select>
        </Field>
        <Field label="Due date">
          <Input type="datetime-local" value={dueAt} onChange={(e) => setDueAt(e.target.value)} />
        </Field>
        <div className="border border-line rounded p-2 text-xs text-ink-soft">
          <p className="font-medium text-ink mb-1">{message.subject || '(no subject)'}</p>
          <p className="line-clamp-3">{truncate(message.snippet, 240)}</p>
          <p className="mt-1 text-ink-mute">A link back to this message will be attached to the task.</p>
        </div>
        {err && <p className="text-sm text-danger">{err}</p>}
      </div>
    </Modal>
  );
}

// Suppress unused import.
void Textarea;
