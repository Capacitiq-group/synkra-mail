import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { useLabels, useFolders } from '@/hooks/queries/mail';
import { Button, Input, Select } from '@/design';
import { Icon } from '@/design/icons';
import { ACTION_LABELS, NEW_ACTION_DEFAULTS } from './metadata';
import type { RuleAction } from '@/lib/types';

interface Props {
  actions: RuleAction[];
  onChange: (next: RuleAction[]) => void;
  mailboxId: string;
}

export function ActionEditor({ actions, onChange, mailboxId }: Props) {
  const labelsQ = useLabels();
  const foldersQ = useFolders(mailboxId);
  const taskListsQ = useQuery({ queryKey: ['task-lists'], queryFn: api.listTaskLists, staleTime: 60_000 });
  const templatesQ = useQuery({ queryKey: ['templates'], queryFn: api.templates, staleTime: 60_000 });
  const sharedQ = useQuery({ queryKey: ['shared-mailboxes'], queryFn: api.listSharedMailboxes, staleTime: 60_000 });

  function updateAt(index: number, next: RuleAction) {
    const copy = actions.slice();
    copy[index] = next;
    onChange(copy);
  }
  function removeAt(index: number) {
    onChange(actions.filter((_, i) => i !== index));
  }
  function addAction() {
    onChange([...actions, { ...NEW_ACTION_DEFAULTS.move }]);
  }
  function changeType(index: number, type: RuleAction['type']) {
    updateAt(index, { ...NEW_ACTION_DEFAULTS[type] });
  }

  return (
    <div className="space-y-2">
      {actions.length === 0 && <p className="text-sm text-ink-soft">No actions yet — add at least one.</p>}
      {actions.map((a, i) => (
        <div key={i} className="flex items-start gap-2 border border-line rounded p-2 bg-surface">
          <div className="flex-1 grid grid-cols-1 sm:grid-cols-[160px,1fr] gap-2">
            <Select value={a.type} onChange={(e) => changeType(i, e.target.value as RuleAction['type'])}>
              {Object.entries(ACTION_LABELS).map(([key, meta]) => (
                <option key={key} value={key}>{meta.label}</option>
              ))}
            </Select>
            {renderActionValue(a, (next) => updateAt(i, next), {
              labels: labelsQ.data ?? [],
              folders: foldersQ.data ?? [],
              taskLists: taskListsQ.data ?? [],
              templates: templatesQ.data ?? [],
              sharedMailboxes: sharedQ.data ?? [],
            })}
          </div>
          <button
            type="button"
            onClick={() => removeAt(i)}
            className="p-1 rounded text-ink-mute hover:text-danger"
            aria-label="Remove action"
          >
            <Icon name="close" size={14} />
          </button>
        </div>
      ))}
      <div className="pt-1">
        <Button size="sm" variant="secondary" onClick={addAction}>
          <Icon name="plus" size={12} /> Add action
        </Button>
      </div>
    </div>
  );
}

interface Resources {
  labels: { id: string; name: string; color: string }[];
  folders: { id: string; name: string }[];
  taskLists: { id: string; name: string }[];
  templates: { id: string; name: string }[];
  sharedMailboxes: { id: string; display_name: string }[];
}

function renderActionValue(
  a: RuleAction,
  setAction: (next: RuleAction) => void,
  resources: Resources,
): React.ReactNode {
  switch (a.type) {
    case 'forward':
      return (
        <div className="flex gap-1">
          <Input
            value={a.to.join(', ')}
            onChange={(e) =>
              setAction({
                type: 'forward',
                to: e.target.value.split(/[,;\s]+/).map((s) => s.trim()).filter(Boolean),
              })
            }
            placeholder="comma-separated addresses"
          />
        </div>
      );
    case 'redirect':
      return (
        <Input
          value={a.to}
          onChange={(e) => setAction({ type: 'redirect', to: e.target.value })}
          placeholder="destination@example.com"
        />
      );
    case 'apply_label':
      return (
        <Select value={a.label_id} onChange={(e) => setAction({ type: 'apply_label', label_id: e.target.value })}>
          <option value="">— select label —</option>
          {resources.labels.map((l) => <option key={l.id} value={l.id}>{l.name}</option>)}
        </Select>
      );
    case 'move':
      return (
        <Select value={a.folder_id} onChange={(e) => setAction({ type: 'move', folder_id: e.target.value })}>
          <option value="">— select folder —</option>
          {resources.folders.map((f) => <option key={f.id} value={f.id}>{f.name}</option>)}
        </Select>
      );
    case 'mark_important':
      return <span className="text-xs text-ink-soft self-center">Flag the message as important.</span>;
    case 'notify_colleague':
      return (
        <div className="space-y-1">
          <Input
            value={a.email}
            onChange={(e) => setAction({ ...a, email: e.target.value })}
            placeholder="colleague@example.com"
          />
          <Input
            value={a.message ?? ''}
            onChange={(e) => setAction({ ...a, message: e.target.value })}
            placeholder="Optional note (e.g. 'Heads up — this came in')"
          />
        </div>
      );
    case 'create_task':
      return (
        <div className="space-y-1">
          <Select value={a.list_id} onChange={(e) => setAction({ ...a, list_id: e.target.value })}>
            <option value="">— select task list —</option>
            {resources.taskLists.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
          </Select>
          <Input
            value={a.title_template}
            onChange={(e) => setAction({ ...a, title_template: e.target.value })}
            placeholder="Task title template"
          />
          <p className="text-xs text-ink-mute">Use {'{{subject}}'} for the message subject.</p>
        </div>
      );
    case 'auto_reply':
      return (
        <Select value={a.template_id} onChange={(e) => setAction({ type: 'auto_reply', template_id: e.target.value })}>
          <option value="">— select template —</option>
          {resources.templates.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
        </Select>
      );
    case 'add_to_shared_mailbox':
      return (
        <Select value={a.shared_mailbox_id} onChange={(e) => setAction({ type: 'add_to_shared_mailbox', shared_mailbox_id: e.target.value })}>
          <option value="">— select shared mailbox —</option>
          {resources.sharedMailboxes.map((m) => <option key={m.id} value={m.id}>{m.display_name}</option>)}
        </Select>
      );
  }
}
