import { useAliases } from '@/hooks/queries/aliases';
import { Button, Input, Select, Switch } from '@/design';
import { Icon } from '@/design/icons';
import { CONDITION_LABELS, NEW_CONDITION_DEFAULTS } from './metadata';
import type { RuleCondition } from '@/lib/types';

interface Props {
  conditions: RuleCondition[];
  onChange: (next: RuleCondition[]) => void;
  mailboxId: string;
}

export function ConditionEditor({ conditions, onChange, mailboxId }: Props) {
  const aliasesQ = useAliases(mailboxId);

  function updateAt(index: number, next: RuleCondition) {
    const copy = conditions.slice();
    copy[index] = next;
    onChange(copy);
  }

  function removeAt(index: number) {
    onChange(conditions.filter((_, i) => i !== index));
  }

  function addCondition() {
    onChange([...conditions, { ...NEW_CONDITION_DEFAULTS.from_matches }]);
  }

  function changeType(index: number, type: RuleCondition['type']) {
    updateAt(index, { ...NEW_CONDITION_DEFAULTS[type] });
  }

  return (
    <div className="space-y-2">
      {conditions.length === 0 && (
        <p className="text-sm text-ink-soft">No conditions yet — add at least one.</p>
      )}
      {conditions.map((c, i) => (
        <div key={i} className="flex items-start gap-2 border border-line rounded p-2 bg-surface">
          <div className="flex-1 grid grid-cols-1 sm:grid-cols-[160px,1fr] gap-2">
            <Select value={c.type} onChange={(e) => changeType(i, e.target.value as RuleCondition['type'])}>
              {Object.entries(CONDITION_LABELS).map(([key, meta]) => (
                <option key={key} value={key}>{meta.label}</option>
              ))}
            </Select>
            {renderValue(c, (value) => updateAt(i, { ...c, value } as RuleCondition), aliasesQ.data ?? [])}
          </div>
          <button
            type="button"
            onClick={() => removeAt(i)}
            className="p-1 rounded text-ink-mute hover:text-danger"
            aria-label="Remove condition"
          >
            <Icon name="close" size={14} />
          </button>
        </div>
      ))}
      <div className="pt-1">
        <Button size="sm" variant="secondary" onClick={addCondition}>
          <Icon name="plus" size={12} /> Add condition
        </Button>
      </div>
      {conditions.length > 0 && (
        <p className="text-xs text-ink-mute">
          {conditions.length === 1
            ? 'A message must match this condition.'
            : 'Conditions combine according to the match mode above.'}
        </p>
      )}
    </div>
  );
}

function renderValue(
  c: RuleCondition,
  setValue: (v: string | boolean) => void,
  aliases: { id: string; address: string }[],
): React.ReactNode {
  if (c.type === 'has_attachment') {
    return (
      <div className="flex items-center gap-2">
        <Switch checked={!!c.value} onChange={setValue} label={c.value ? 'Has attachment' : 'No attachment'} />
      </div>
    );
  }
  if (c.type === 'is_external') {
    return (
      <div className="flex items-center gap-2">
        <Switch checked={!!c.value} onChange={setValue} label={c.value ? 'External sender' : 'Internal sender'} />
      </div>
    );
  }
  if (c.type === 'alias_id') {
    return (
      <Select value={String(c.value)} onChange={(e) => setValue(e.target.value)}>
        <option value="">— select an alias —</option>
        {aliases.map((a) => (
          <option key={a.id} value={a.id}>{a.address}</option>
        ))}
      </Select>
    );
  }
  return (
    <Input
      value={String(c.value)}
      onChange={(e) => setValue(e.target.value)}
      placeholder={CONDITION_LABELS[c.type].placeholder}
    />
  );
}
