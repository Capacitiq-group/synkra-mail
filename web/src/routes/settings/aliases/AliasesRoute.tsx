import { useMemo, useState } from 'react';
import { useAliases, useDeleteAlias, useUpdateAlias } from '@/hooks/queries/aliases';
import { useMailboxes } from '@/hooks/queries/mail';
import { Button, ConfirmDialog, EmptyState, Menu, MenuItem, Skeleton, Input } from '@/design';
import { Icon } from '@/design/icons';
import { SettingsContent, SettingsHeader, SettingsPage } from '../SettingsLayout';
import { CreateAliasModal } from './CreateAliasModal';
import { BulkCreateModal } from './BulkCreateModal';
import { AliasDetailDrawer } from './AliasDetailDrawer';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { relativeTime } from '@/lib/time';
import { CopyButton } from '@/components/CopyButton';
import type { Alias } from '@/lib/types';

export function AliasesRoute() {
  const mailboxesQ = useMailboxes();
  const mailbox = mailboxesQ.data?.[0];
  const mailboxId = mailbox?.id ?? '';
  const domain = mailbox?.address.split('@')[1] ?? '';

  const aliasesQ = useAliases(mailboxId);
  const update = useUpdateAlias(mailboxId);
  const del = useDeleteAlias(mailboxId);

  const [createOpen, setCreateOpen] = useState(false);
  const [bulkOpen, setBulkOpen] = useState(false);
  const [detailAlias, setDetailAlias] = useState<Alias | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Alias | null>(null);
  const [filter, setFilter] = useState('');

  const filtered = useMemo(() => {
    const all = aliasesQ.data ?? [];
    const q = filter.trim().toLowerCase();
    if (!q) return all;
    return all.filter((a) => a.address.toLowerCase().includes(q) || a.label.toLowerCase().includes(q));
  }, [aliasesQ.data, filter]);

  if (mailboxesQ.isLoading) {
    return (
      <SettingsPage>
        <SettingsHeader title="Aliases" back={{ to: '/settings', label: 'Settings' }} />
        <SettingsContent><Skeleton className="h-32 w-full" /></SettingsContent>
      </SettingsPage>
    );
  }
  if (!mailbox) {
    return (
      <SettingsPage>
        <SettingsHeader title="Aliases" back={{ to: '/settings', label: 'Settings' }} />
        <SettingsContent><EmptyState title="No mailbox" description="You need a mailbox to create aliases." /></SettingsContent>
      </SettingsPage>
    );
  }

  return (
    <SettingsPage maxWidth="xl">
      <SettingsHeader
        title="Aliases"
        description="Disposable and masked addresses that forward to your mailbox. Use them when signing up for services you don't trust with your real address."
        back={{ to: '/settings', label: 'Settings' }}
      />
      <SettingsContent>
        <div className="flex flex-wrap items-center gap-2 mb-4">
          <div className="flex-1 min-w-[200px] relative">
            <Icon name="search" size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-ink-mute" />
            <Input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Search aliases"
              className="pl-8"
            />
          </div>
          <Button variant="secondary" onClick={() => setBulkOpen(true)}>
            <Icon name="plus" size={14} /> Bulk create
          </Button>
          <Button variant="primary" onClick={() => setCreateOpen(true)}>
            <Icon name="plus" size={14} /> New alias
          </Button>
        </div>

        {aliasesQ.isLoading && (
          <div className="space-y-2">
            {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-16 w-full" />)}
          </div>
        )}

        {!aliasesQ.isLoading && filtered.length === 0 && (
          <EmptyState
            icon={<Icon name="label" size={28} />}
            title={filter ? 'No matches' : 'No aliases yet'}
            description={filter ? 'Try a different search.' : 'Create your first alias to start shielding your real address.'}
            action={!filter ? <Button variant="primary" onClick={() => setCreateOpen(true)}>New alias</Button> : undefined}
          />
        )}

        {filtered.length > 0 && (
          <ul className="space-y-2">
            {filtered.map((a) => (
              <li key={a.id}>
                <div className="flex items-start gap-3 p-3 border border-line rounded-lg hover:border-accent transition-colors">
                  <button
                    onClick={() => setDetailAlias(a)}
                    className="flex-1 text-left min-w-0"
                  >
                    <div className="flex items-center gap-2 flex-wrap">
                      <code className="text-sm font-mono break-all">{a.address}</code>
                      {a.disabled ? (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-danger-soft text-danger">Disabled</span>
                      ) : a.single_use ? (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-warning-soft text-warning">Single-use</span>
                      ) : a.expires_at && new Date(a.expires_at) < new Date() ? (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-danger-soft text-danger">Expired</span>
                      ) : null}
                    </div>
                    <p className="text-sm mt-0.5">{a.label} · <span className="capitalize text-ink-soft">{a.category}</span></p>
                    <p className="text-xs text-ink-mute mt-0.5">
                      {a.message_count} message{a.message_count === 1 ? '' : 's'}
                      {a.last_used_at ? ` · last used ${relativeTime(a.last_used_at)}` : ' · never used'}
                    </p>
                  </button>
                  <div className="flex items-center gap-1 shrink-0">
                    <CopyButton value={a.address} small />
                    <Menu trigger={
                      <Button size="icon" variant="ghost" aria-label="Alias actions">
                        <Icon name="more" size={16} />
                      </Button>
                    }>
                      <MenuItem onClick={() => setDetailAlias(a)}>Edit details</MenuItem>
                      <MenuItem onClick={() => void update.mutateAsync({ id: a.id, patch: { disabled: !a.disabled } })}>
                        {a.disabled ? 'Re-enable' : 'Disable'}
                      </MenuItem>
                      <MenuItem danger onClick={() => setDeleteTarget(a)}>Delete</MenuItem>
                    </Menu>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </SettingsContent>

      <CreateAliasModal
        mailboxId={mailboxId}
        domain={domain}
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={() => { /* list refreshes via query invalidation */ }}
      />

      <BulkCreateModal
        mailboxId={mailboxId}
        open={bulkOpen}
        onClose={() => setBulkOpen(false)}
        onCreated={() => { /* list refreshes via query invalidation */ }}
      />

      <AliasDetailDrawer
        mailboxId={mailboxId}
        alias={detailAlias}
        open={!!detailAlias}
        onClose={() => setDetailAlias(null)}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={async () => {
          if (!deleteTarget) return;
          try {
            await del.mutateAsync(deleteTarget.id);
            toast('success', 'Alias deleted', deleteTarget.address);
            setDeleteTarget(null);
          } catch (e) {
            toast('error', 'Could not delete', errorMessage(e));
          }
        }}
        title={`Delete "${deleteTarget?.label}"?`}
        description="The address stops working immediately. This cannot be undone."
        confirmLabel="Delete alias"
        danger
        busy={del.isPending}
      />
    </SettingsPage>
  );
}
