import { useEffect, useState } from 'react';
import { useMailboxes } from '@/hooks/queries/mail';
import { useOoo, useSetOoo } from '@/hooks/queries/ooo';
import { Button, ConfirmDialog, EmptyState, Skeleton, Switch } from '@/design';
import { Icon } from '@/design/icons';
import { SettingsContent, SettingsHeader, SettingsPage } from '../SettingsLayout';
import { PeriodEditor } from './PeriodEditor';
import { formatDateTime } from '@/lib/time';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import type { OutOfOfficePeriod, OutOfOfficeState } from '@/lib/types';

export function OooRoute() {
  const mailboxesQ = useMailboxes();
  const mailboxId = mailboxesQ.data?.[0]?.id ?? '';
  const q = useOoo(mailboxId);
  const save = useSetOoo(mailboxId);

  const [state, setState] = useState<OutOfOfficeState | null>(null);
  const [editing, setEditing] = useState<OutOfOfficePeriod | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<OutOfOfficePeriod | null>(null);

  useEffect(() => { if (q.data) setState(q.data); }, [q.data]);

  async function persist(next: OutOfOfficeState) {
    setState(next);
    try {
      await save.mutateAsync(next);
    } catch (e) {
      toast('error', 'Could not save', errorMessage(e));
    }
  }

  function openNew() { setEditing(null); setEditorOpen(true); }
  function openEdit(p: OutOfOfficePeriod) { setEditing(p); setEditorOpen(true); }

  function onSavePeriod(p: OutOfOfficePeriod) {
    if (!state) return;
    const exists = state.periods.some((x) => x.id === p.id);
    const periods = exists
      ? state.periods.map((x) => (x.id === p.id ? p : x))
      : [...state.periods, p];
    void persist({ ...state, periods });
  }

  if (q.isLoading || !state) {
    return (
      <SettingsPage maxWidth="lg">
        <SettingsHeader title="Out of office" back={{ to: '/settings', label: 'Settings' }} />
        <SettingsContent><Skeleton className="h-40 w-full" /></SettingsContent>
      </SettingsPage>
    );
  }

  return (
    <SettingsPage maxWidth="lg">
      <SettingsHeader
        title="Out of office"
        description="Automatically reply to incoming mail while you are away. Loop prevention is enforced."
        back={{ to: '/settings', label: 'Settings' }}
      />
      <SettingsContent>
        <div className="flex items-center justify-between p-3 border border-line rounded-lg mb-4">
          <div>
            <p className="font-medium text-sm">Out-of-office is {state.globally_enabled ? 'enabled' : 'disabled'}</p>
            <p className="text-xs text-ink-soft mt-0.5">
              {state.globally_enabled
                ? 'Active periods below will run automatically.'
                : 'No replies will be sent, even for scheduled periods.'}
            </p>
          </div>
          <Switch
            checked={state.globally_enabled}
            onChange={(v) => persist({ ...state, globally_enabled: v })}
          />
        </div>

        <div className="flex justify-end mb-3">
          <Button variant="primary" onClick={openNew}>
            <Icon name="plus" size={14} /> Add period
          </Button>
        </div>

        {state.periods.length === 0 ? (
          <EmptyState
            icon={<Icon name="clock" size={28} />}
            title="No scheduled periods"
            description="Add a period to auto-reply during a specific range. You can add several, e.g. one for this week and one for the December holidays."
            action={<Button variant="primary" onClick={openNew}>Add period</Button>}
          />
        ) : (
          <ul className="space-y-2">
            {state.periods.map((p) => (
              <li key={p.id} className="flex items-start gap-3 p-3 border border-line rounded-lg">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <p className="text-sm font-medium">
                      {formatDateTime(p.starts_at)} → {formatDateTime(p.ends_at)}
                    </p>
                    {!p.enabled && <span className="text-xs px-1.5 py-0.5 rounded bg-canvas border border-line text-ink-soft">Disabled</span>}
                    <span className="text-xs text-ink-soft">{p.scope === 'all' ? 'All senders' : p.scope === 'internal' ? 'Internal only' : 'External only'}</span>
                  </div>
                  <p className="text-xs text-ink-soft mt-1 truncate">
                    {p.body_external?.trim() || p.body_internal?.trim() || '(no message)'}
                  </p>
                </div>
                <div className="flex items-center gap-1">
                  <Button size="sm" variant="ghost" onClick={() => openEdit(p)}>Edit</Button>
                  <Button size="sm" variant="ghost" onClick={() => setDeleteTarget(p)}>Delete</Button>
                </div>
              </li>
            ))}
          </ul>
        )}

        <div className="mt-6 pt-4 border-t border-line">
          <h3 className="text-sm font-medium mb-2">Loop prevention</h3>
          <p className="text-xs text-ink-soft">
            We automatically skip auto-replies to mailing lists, automated senders, and messages with a null return path.
            Reply frequency is enforced per sender, so a person who emails you five times receives one reply.
          </p>
        </div>
      </SettingsContent>

      <PeriodEditor
        open={editorOpen}
        onClose={() => setEditorOpen(false)}
        period={editing}
        onSave={onSavePeriod}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => {
          if (!deleteTarget || !state) return;
          void persist({ ...state, periods: state.periods.filter((x) => x.id !== deleteTarget.id) });
          setDeleteTarget(null);
        }}
        title="Delete this period?"
        description="Auto-replies for this range will stop immediately."
        confirmLabel="Delete"
        danger
      />
    </SettingsPage>
  );
}
