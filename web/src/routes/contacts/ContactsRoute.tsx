import { useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useContacts, useContactGroups, useDeleteContact, useImportContacts } from '@/hooks/queries/contacts';
import { api } from '@/lib/api';
import { Button, ConfirmDialog, EmptyState, Input, Menu, MenuItem, Skeleton, Tabs } from '@/design';
import { Icon } from '@/design/icons';
import { Avatar } from '@/design';
import { toast } from '@/stores/toast';
import { errorMessage } from '@/lib/errors';
import { usePageTitle } from '@/hooks';
import type { Contact } from '@/lib/types';

export function ContactsRoute() {
  usePageTitle('Contacts');
  const navigate = useNavigate();
  const [query, setQuery] = useState('');
  const [groupFilter, setGroupFilter] = useState<string | null>(null);
  const contactsQ = useContacts(query);
  const groupsQ = useContactGroups();
  const del = useDeleteContact();
  const importMut = useImportContacts();
  const [deleteTarget, setDeleteTarget] = useState<Contact | null>(null);

  const filtered = useMemo(() => {
    const all = contactsQ.data ?? [];
    if (!groupFilter) return all;
    return all.filter((c) => c.groups.includes(groupFilter));
  }, [contactsQ.data, groupFilter]);

  const sorted = useMemo(
    () => [...filtered].sort((a, b) => a.display_name.localeCompare(b.display_name)),
    [filtered],
  );

  function onImportFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    importMut.mutate(file, {
      onSuccess: (r) => toast('success', `Imported ${r.imported} contacts`, r.errors > 0 ? `${r.errors} rows skipped` : undefined),
      onError: (err) => toast('error', 'Import failed', errorMessage(err)),
    });
    e.target.value = '';
  }

  return (
    <div className="h-full flex flex-col bg-surface">
      <header className="px-4 py-3 border-b border-line flex items-center gap-2 flex-wrap">
        <h1 className="text-lg font-semibold flex-1">Contacts</h1>
        <div className="flex items-center gap-1">
          <input type="file" accept=".vcf,.csv,text/vcard,text/csv" hidden id="import-contacts" onChange={onImportFile} />
          <label
            htmlFor="import-contacts"
            className="px-3 py-1.5 text-sm border border-line rounded hover:bg-canvas cursor-pointer"
          >
            Import
          </label>
          <a
            href={api.exportContactsUrl()}
            className="px-3 py-1.5 text-sm border border-line rounded hover:bg-canvas"
            download
          >
            Export
          </a>
          <Button variant="primary" onClick={() => navigate('/contacts/new')}>
            <Icon name="plus" size={14} /> New contact
          </Button>
        </div>
      </header>

      <div className="px-4 py-3 border-b border-line">
        <div className="relative">
          <Icon name="search" size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-ink-mute" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search by name, email or company"
            className="pl-8"
          />
        </div>
      </div>

      {(groupsQ.data?.length ?? 0) > 0 && (
        <div className="border-b border-line">
          <Tabs
            items={[
              { id: '__all', label: 'All contacts' },
              ...(groupsQ.data ?? []).map((g) => ({ id: g.id, label: g.name, badge: g.member_count })),
            ]}
            value={groupFilter ?? '__all'}
            onChange={(id) => setGroupFilter(id === '__all' ? null : id)}
          />
        </div>
      )}

      <div className="flex-1 overflow-y-auto scroll-thin">
        {contactsQ.isLoading && (
          <div className="p-4 space-y-3">
            {Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-14 w-full" />)}
          </div>
        )}

        {contactsQ.isError && (
          <EmptyState
            title="Could not load contacts"
            description={errorMessage(contactsQ.error)}
            action={<Button onClick={() => contactsQ.refetch()}>Try again</Button>}
          />
        )}

        {!contactsQ.isLoading && !contactsQ.isError && sorted.length === 0 && (
          <EmptyState
            icon={<Icon name="mail" size={28} />}
            title={query ? 'No matches' : 'No contacts yet'}
            description={query ? 'Try a different search.' : 'Add contacts or import a vCard/CSV file to get started.'}
            action={!query ? <Button variant="primary" onClick={() => navigate('/contacts/new')}>Add contact</Button> : undefined}
          />
        )}

        <ul>
          {sorted.map((c) => (
            <li key={c.id} className="border-b border-line">
              <div className="flex items-center gap-3 px-4 py-2 hover:bg-canvas">
                <Link to={`/contacts/${c.id}`} className="flex items-center gap-3 flex-1 min-w-0">
                  <Avatar name={c.display_name} email={c.emails[0]?.email} src={c.avatar_url} size={32} />
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium truncate">{c.display_name}</p>
                    <p className="text-xs text-ink-soft truncate">
                      {c.emails[0]?.email}
                      {c.company && ` · ${c.company}`}
                    </p>
                  </div>
                  {c.org_visible && (
                    <span className="text-xs text-ink-mute shrink-0" title="Visible to your organisation">org</span>
                  )}
                </Link>
                <Menu
                  trigger={
                    <Button size="icon" variant="ghost" aria-label="Contact actions">
                      <Icon name="more" size={16} />
                    </Button>
                  }
                >
                  <MenuItem onClick={() => navigate(`/contacts/${c.id}`)}>Edit</MenuItem>
                  <MenuItem onClick={() => { navigator.clipboard.writeText(c.emails[0]?.email ?? ''); toast('success', 'Email copied'); }}>
                    Copy email
                  </MenuItem>
                  <MenuItem danger onClick={() => setDeleteTarget(c)}>Delete</MenuItem>
                </Menu>
              </div>
            </li>
          ))}
        </ul>
      </div>

      <ConfirmDialog
        open={!!deleteTarget}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={async () => {
          if (!deleteTarget) return;
          try {
            await del.mutateAsync(deleteTarget.id);
            toast('success', 'Contact deleted');
            setDeleteTarget(null);
          } catch (e) {
            toast('error', 'Could not delete', errorMessage(e));
          }
        }}
        title={`Delete "${deleteTarget?.display_name}"?`}
        description="This contact will be removed from your address book."
        confirmLabel="Delete"
        danger
        busy={del.isPending}
      />
    </div>
  );
}
