import { useEffect, useState } from 'react';
import { useCreateTask, useUpdateTask } from '@/hooks/queries/tasks';
import { Button, Field, Input, Modal, Select, Textarea } from '@/design';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { formatDateTime } from '@/lib/time';
import type { Task, TaskList } from '@/lib/types';

interface Props {
  open: boolean;
  onClose: () => void;
  task?: Partial<Task>;
  defaultListId?: string;
  lists: TaskList[];
}

export function TaskModal({ open, onClose, task, defaultListId, lists }: Props) {
  const create = useCreateTask();
  const update = useUpdateTask();
  const [listId, setListId] = useState(task?.list_id ?? defaultListId ?? lists[0]?.id ?? '');
  const [title, setTitle] = useState(task?.title ?? '');
  const [notes, setNotes] = useState(task?.notes ?? '');
  const [dueAt, setDueAt] = useState(task?.due_at ? new Date(task.due_at).toISOString().slice(0, 16) : '');
  const [assignedTo, setAssignedTo] = useState(task?.assigned_to ?? '');
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setListId(task?.list_id ?? defaultListId ?? lists[0]?.id ?? '');
      setTitle(task?.title ?? '');
      setNotes(task?.notes ?? '');
      setDueAt(task?.due_at ? new Date(task.due_at).toISOString().slice(0, 16) : '');
      setAssignedTo(task?.assigned_to ?? '');
      setErr(null);
    }
  }, [open, task?.id, defaultListId, lists]);

  async function submit() {
    if (!title.trim()) { setErr('Title is required'); return; }
    if (!listId) { setErr('Pick a list'); return; }
    setErr(null);
    const due = dueAt ? new Date(dueAt).toISOString() : undefined;
    try {
      if (task) {
        await update.mutateAsync({
          id: task.id!,
          patch: { title: title.trim(), notes: notes.trim() || undefined, due_at: due, list_id: listId, assigned_to: assignedTo || undefined },
        });
        toast('success', 'Task updated');
      } else {
        await create.mutateAsync({
          list_id: listId,
          title: title.trim(),
          notes: notes.trim() || undefined,
          due_at: due,
          assigned_to: assignedTo || undefined,
        });
        toast('success', 'Task created');
      }
      onClose();
    } catch (e) {
      setErr(errorMessage(e));
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={task ? 'Edit task' : 'New task'}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button variant="primary" onClick={submit} loading={create.isPending || update.isPending}>
            {task ? 'Save' : 'Create'}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <Field label="Title" required>
          <Input value={title} onChange={(e) => setTitle(e.target.value)} autoFocus />
        </Field>
        <Field label="List">
          <Select value={listId} onChange={(e) => setListId(e.target.value)}>
            {lists.map((l) => <option key={l.id} value={l.id}>{l.name}</option>)}
          </Select>
        </Field>
        <Field label="Due date" hint={dueAt ? formatDateTime(new Date(dueAt).toISOString()) : undefined}>
          <Input type="datetime-local" value={dueAt} onChange={(e) => setDueAt(e.target.value)} />
        </Field>
        <Field label="Assign to" hint="Leave blank for yourself.">
          <Input type="email" value={assignedTo} onChange={(e) => setAssignedTo(e.target.value)} placeholder="colleague@example.com" />
        </Field>
        <Field label="Notes">
          <Textarea value={notes} onChange={(e) => setNotes(e.target.value)} rows={3} />
        </Field>
        {err && <p className="text-sm text-danger">{err}</p>}
      </div>
    </Modal>
  );
}
