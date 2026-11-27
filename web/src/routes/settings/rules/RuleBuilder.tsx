import { useEffect, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { useMailboxes } from '@/hooks/queries/mail';
import { useCreateRule, useRules, useUpdateRule } from '@/hooks/queries/rules';
import { Button, Field, Input, Select, Skeleton, Switch } from '@/design';
import { Icon } from '@/design/icons';
import { SettingsContent, SettingsHeader, SettingsPage } from '../SettingsLayout';
import { ConditionEditor } from './ConditionEditor';
import { ActionEditor } from './ActionEditor';
import { TestRuleModal } from './TestRuleModal';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import type { Rule, RuleAction, RuleCondition } from '@/lib/types';

export function RuleBuilder() {
  const { id } = useParams<{ id: string }>();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const mailboxesQ = useMailboxes();
  const mailboxId = mailboxesQ.data?.[0]?.id ?? '';
  const rulesQ = useRules(mailboxId);
  const create = useCreateRule(mailboxId);
  const update = useUpdateRule(mailboxId);

  const existing = id && id !== 'new' ? rulesQ.data?.find((r) => r.id === id) : undefined;

  const [name, setName] = useState('');
  const [enabled, setEnabled] = useState(true);
  const [match, setMatch] = useState<'all' | 'any'>('all');
  const [conditions, setConditions] = useState<RuleCondition[]>([]);
  const [actions, setActions] = useState<RuleAction[]>([]);
  const [testOpen, setTestOpen] = useState(false);

  // Seed from existing OR from URL pre-fill (from message viewer "Forward this sender")
  useEffect(() => {
    if (existing) {
      setName(existing.name);
      setEnabled(existing.enabled);
      setMatch(existing.match);
      setConditions(existing.conditions);
      setActions(existing.actions);
      return;
    }
    // Pre-fill from query params
    const preFrom = params.get('from');
    const preAction = params.get('action');
    if (preFrom && !name) {
      setName(`From ${preFrom}`);
      setConditions([{ type: 'from_matches', value: preFrom }]);
    }
    if (preAction === 'forward' && actions.length === 0) {
      setActions([{ type: 'forward', to: [] }]);
    } else if (preAction === 'label' && actions.length === 0) {
      setActions([{ type: 'apply_label', label_id: '' }]);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [existing?.id]);

  async function save() {
    if (!name.trim()) { toast('error', 'Give the rule a name'); return; }
    if (conditions.length === 0) { toast('error', 'Add at least one condition'); return; }
    if (actions.length === 0) { toast('error', 'Add at least one action'); return; }

    const next: Omit<Rule, 'id' | 'created_at' | 'updated_at'> = {
      mailbox_id: mailboxId,
      name: name.trim(),
      enabled,
      order: existing?.order ?? rulesQ.data?.length ?? 0,
      match,
      conditions,
      actions,
    };
    try {
      if (existing) {
        await update.mutateAsync({ id: existing.id, patch: next });
      } else {
        await create.mutateAsync(next);
      }
      toast('success', 'Rule saved');
      navigate('/settings/rules');
    } catch (e) {
      toast('error', 'Could not save rule', errorMessage(e));
    }
  }

  if (rulesQ.isLoading && existing !== undefined) {
    return (
      <SettingsPage maxWidth="lg">
        <SettingsHeader title="Rule" back={{ to: '/settings/rules', label: 'Rules' }} />
        <SettingsContent><Skeleton className="h-40 w-full" /></SettingsContent>
      </SettingsPage>
    );
  }

  return (
    <SettingsPage maxWidth="lg">
      <SettingsHeader
        title={existing ? 'Edit rule' : 'New rule'}
        description="Automatically process incoming mail that matches your conditions."
        back={{ to: '/settings/rules', label: 'Rules' }}
      />
      <SettingsContent>
        <div className="space-y-5 max-w-2xl">
          <div className="flex items-center gap-3">
            <Field label="Rule name" required>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Invoices to Accounts folder" />
            </Field>
            <div className="pt-5">
              <Switch checked={enabled} onChange={setEnabled} label="Enabled" />
            </div>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-[160px,1fr] gap-2 items-center">
            <label className="text-sm text-ink-soft">When a message arrives</label>
            <Select value={match} onChange={(e) => setMatch(e.target.value as 'all' | 'any')}>
              <option value="all">and matches all of the following</option>
              <option value="any">and matches any of the following</option>
            </Select>
          </div>

          <div>
            <p className="text-xs uppercase text-ink-mute mb-2">Conditions</p>
            <ConditionEditor conditions={conditions} onChange={setConditions} mailboxId={mailboxId} />
          </div>

          <div>
            <p className="text-xs uppercase text-ink-mute mb-2">Then do</p>
            <ActionEditor actions={actions} onChange={setActions} mailboxId={mailboxId} />
          </div>

          {existing && (
            <div className="pt-1">
              <Button size="sm" variant="ghost" onClick={() => setTestOpen(true)}>
                <Icon name="sparkles" size={12} /> Test against a message
              </Button>
            </div>
          )}

          <div className="flex gap-2 pt-3 border-t border-line">
            <Button variant="primary" onClick={save} loading={create.isPending || update.isPending}>
              Save rule
            </Button>
            <Button variant="secondary" onClick={() => navigate('/settings/rules')}>Cancel</Button>
          </div>

          <p className="text-xs text-ink-mute">
            Rules apply to incoming mail in delivery order. Messages already in your mailbox are not affected.
          </p>
        </div>
      </SettingsContent>

      {existing && (
        <TestRuleModal
          open={testOpen}
          onClose={() => setTestOpen(false)}
          ruleId={existing.id}
          mailboxId={mailboxId}
        />
      )}
    </SettingsPage>
  );
}
