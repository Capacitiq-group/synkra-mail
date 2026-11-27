import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMailboxes } from '@/hooks/queries/mail';
import { useDeleteRule, useReorderRules, useRules, useUpdateRule } from '@/hooks/queries/rules';
import { Button, ConfirmDialog, EmptyState, Menu, MenuItem, Skeleton, Switch } from '@/design';
import { Icon } from '@/design/icons';
import { SettingsContent, SettingsHeader, SettingsPage } from '../SettingsLayout';
import { describeAction, describeCondition } from './metadata';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import type { Rule } from '@/lib/types';

export function RulesRoute() {
  const navigate = useNavigate();
  const mailboxesQ = useMailboxes();
  const mailboxId = mailboxesQ.data?.[0]?.id ?? '';
  const rulesQ = useRules(mailboxId);
  const update = useUpdateRule(mailboxId);
  const del = useDeleteRule(mailboxId);
  const reorder = useReorderRules(mailboxId);
  const [deleteTarget, setDeleteTarget] = useState<Rule | null>(null);

  const rules = rulesQ.data ?? [];

  async function move(index: number, dir: -1 | 1) {
    const next = index + dir;
    if (next < 0 || next >= rules.length) return;
    const order = rules.map((r) => r.id);
    [order[index], order[next]] = [order[next], order[index]];
    try {
      await reorder.mutateAsync(order);
    } catch (e) {
      toast('error', 'Could not reorder', errorMessage(e));
    }
  }

  return (
    <SettingsPage maxWidth="xl">
      <SettingsHeader
        title="Rules"
        description="Automatically process incoming mail. Rules are evaluated top to bottom; the first match wins for each action."
        back={{ to: '/settings', label: 'Settings' }}
      />
      <SettingsContent>
        <div className="flex justify-end mb-4">
          <Button variant="primary" onClick={() => navigate('/settings/rules/new')}>
            <Icon name="plus" size={14} /> New rule
          </Button>
        </div>

        {rulesQ.isLoading && (
          <div className="space-y-2">
            {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-24 w-full" />)}
          </div>
        )}

        {!rulesQ.isLoading && rules.length === 0 && (
          <EmptyState
            icon={<Icon name="sparkles" size={28} />}
            title="No rules yet"
            description="Create a rule to auto-label invoices, forward urgent messages, or route customer enquiries."
            action={<Button variant="primary" onClick={() => navigate('/settings/rules/new')}>New rule</Button>}
          />
        )}

        <ol className="space-y-2">
          {rules.map((r, i) => (
            <li key={r.id} className="flex items-start gap-3 p-3 border border-line rounded-lg">
              <div className="flex flex-col items-center pt-1 gap-1">
                <button
                  onClick={() => move(i, -1)}
                  disabled={i === 0 || reorder.isPending}
                  className="text-ink-mute hover:text-ink disabled:opacity-30"
                  aria-label="Move up"
                ><Icon name="chevron-left" size={12} className="rotate-90" /></button>
                <span className="text-xs text-ink-mute">{i + 1}</span>
                <button
                  onClick={() => move(i, 1)}
                  disabled={i === rules.length - 1 || reorder.isPending}
                  className="text-ink-mute hover:text-ink disabled:opacity-30"
                  aria-label="Move down"
                ><Icon name="chevron-right" size={12} className="rotate-90" /></button>
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <p className="font-medium truncate">{r.name}</p>
                  {!r.enabled && <span className="text-xs px-1.5 py-0.5 rounded bg-canvas border border-line text-ink-soft">Disabled</span>}
                </div>
                <div className="text-xs text-ink-soft mt-1 space-y-0.5">
                  <p>When {r.match === 'all' ? 'all' : 'any'} of:
                    {' '}{r.conditions.map(describeCondition).join(r.match === 'all' ? ' AND ' : ' OR ')}
                  </p>
                  <p>Then: {r.actions.map(describeAction).join('; ')}</p>
                </div>
              </div>
              <div className="flex items-center gap-1">
                <Switch
                  checked={r.enabled}
                  onChange={(enabled) => void update.mutateAsync({ id: r.id, patch: { enabled } })}
                />
                <Menu trigger={
                  <Button size="icon" variant="ghost" aria-label="Rule actions">
                    <Icon name="more" size={16} />
                  </Button>
                }>
                  <MenuItem onClick={() => navigate(`/settings/rules/${r.id}`)}>Edit</MenuItem>
                  <MenuItem onClick={() => navigate(`/settings/rules/${r.id}/duplicate`)}>Duplicate</MenuItem>
                  <MenuItem danger onClick={() => setDeleteTarget(r)}>Delete</MenuItem>
                </Menu>
              </div>
            </li>
          ))}
        </ol>
      </SettingsContent>

      <ConfirmDialog
        open={!!deleteTarget}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={async () => {
          if (!deleteTarget) return;
          try {
            await del.mutateAsync(deleteTarget.id);
            toast('success', 'Rule deleted');
            setDeleteTarget(null);
          } catch (e) {
            toast('error', 'Could not delete', errorMessage(e));
          }
        }}
        title={`Delete "${deleteTarget?.name}"?`}
        description="Messages will stop being processed by this rule."
        confirmLabel="Delete rule"
        danger
        busy={del.isPending}
      />

      <p className="text-xs text-ink-mute mt-6">
        Need the raw form? <Link to="/settings/rules/advanced" className="text-accent underline">Advanced (Sieve) view</Link> — coming when ManageSieve is exposed.
      </p>
    </SettingsPage>
  );
}
