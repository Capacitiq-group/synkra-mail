import { useMemo, useState } from 'react';
import { NavLink, useNavigate } from 'react-router-dom';
import clsx from 'clsx';
import { useCreateFolder, useDeleteFolder, useRenameFolder, useLabels } from '@/hooks/queries/mail';
import { Icon } from '@/design/icons';
import { Button, ConfirmDialog, Field, Input, Menu, MenuItem, Modal } from '@/design';
import { toast } from '@/stores/toast';
import { errorMessage } from '@/lib/errors';
import type { Folder, Mailbox } from '@/lib/types';
import { useMailboxActions } from './useMailboxActions';

interface SidebarProps {
  mailbox: Mailbox;
  mailboxes: Mailbox[];
  folders: Folder[];
  activeFolderId: string;
  onNavigate?: () => void;
}

const ROLE_ICONS: Record<string, Parameters<typeof Icon>[0]['name']> = {
  inbox: 'inbox',
  sent: 'sent',
  drafts: 'drafts',
  archive: 'archive',
  spam: 'spam',
  trash: 'trash',
};

export function FolderSidebar({ mailbox, mailboxes, folders, activeFolderId, onNavigate }: SidebarProps) {
  const navigate = useNavigate();
  const createFolder = useCreateFolder(mailbox.id);
  const renameFolder = useRenameFolder(mailbox.id);
  const deleteFolder = useDeleteFolder(mailbox.id);
  const labelsQ = useLabels();

  const [showNewFolder, setShowNewFolder] = useState(false);
  const [newFolderName, setNewFolderName] = useState('');
  const [renameTarget, setRenameTarget] = useState<Folder | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<Folder | null>(null);

  const { order } = useMemo(() => partitionFolders(folders), [folders]);
  const inbox = folders.find((f) => f.role === 'inbox');
  const custom = order.filter((f) => f.role === null);
  const systemInbox = inbox ? [inbox] : [];
  const systemOther = order.filter((f) => f.role && f.role !== 'inbox');

  async function handleCreateFolder(e: React.FormEvent) {
    e.preventDefault();
    if (!newFolderName.trim()) return;
    try {
      const f = await createFolder.mutateAsync({ name: newFolderName.trim() });
      setShowNewFolder(false);
      setNewFolderName('');
      toast('success', 'Folder created');
      navigate(`/mail/${mailbox.id}/${f.id}`);
      onNavigate?.();
    } catch (err) {
      toast('error', 'Could not create folder', errorMessage(err));
    }
  }

  async function handleRenameFolder(e: React.FormEvent) {
    e.preventDefault();
    if (!renameTarget || !renameValue.trim()) return;
    try {
      await renameFolder.mutateAsync({ folderId: renameTarget.id, name: renameValue.trim() });
      setRenameTarget(null);
      toast('success', 'Folder renamed');
    } catch (err) {
      toast('error', 'Could not rename folder', errorMessage(err));
    }
  }

  async function handleDeleteFolder() {
    if (!deleteTarget) return;
    try {
      await deleteFolder.mutateAsync(deleteTarget.id);
      setDeleteTarget(null);
      if (deleteTarget.id === activeFolderId) {
        const fallback = inbox ?? folders[0];
        if (fallback) navigate(`/mail/${mailbox.id}/${fallback.id}`);
      }
      toast('success', 'Folder deleted');
    } catch (err) {
      toast('error', 'Could not delete folder', errorMessage(err));
    }
  }

  return (
    <div className="flex flex-col h-full">
      <MailboxSwitcher mailbox={mailbox} mailboxes={mailboxes} onNavigate={onNavigate} />

      <div className="px-2 py-2">
        <Button
          variant="primary"
          className="w-full justify-center"
          onClick={() => { navigate('/compose'); onNavigate?.(); }}
        >
          <Icon name="plus" /> Compose
        </Button>
      </div>

      <nav aria-label="Folders" className="flex-1 overflow-y-auto scroll-thin px-2 pb-2 space-y-0.5">
        {systemInbox.map((f) => (
          <FolderRow
            key={f.id}
            mailboxId={mailbox.id}
            folder={f}
            active={f.id === activeFolderId}
            onNavigate={onNavigate}
            onRename={() => { setRenameTarget(f); setRenameValue(f.name); }}
            onDelete={() => setDeleteTarget(f)}
          />
        ))}

        {custom.map((f) => (
          <FolderRow
            key={f.id}
            mailboxId={mailbox.id}
            folder={f}
            active={f.id === activeFolderId}
            onNavigate={onNavigate}
            onRename={() => { setRenameTarget(f); setRenameValue(f.name); }}
            onDelete={() => setDeleteTarget(f)}
          />
        ))}

        <div className="pt-3 pb-1">
          <button
            type="button"
            onClick={() => setShowNewFolder(true)}
            className="w-full flex items-center gap-2 px-2 py-1 text-xs text-ink-soft hover:text-ink rounded hover:bg-canvas"
          >
            <Icon name="folder-plus" size={14} /> New folder
          </button>
        </div>

        <div className="border-t border-line my-2" />

        {systemOther.map((f) => (
          <FolderRow
            key={f.id}
            mailboxId={mailbox.id}
            folder={f}
            active={f.id === activeFolderId}
            onNavigate={onNavigate}
            onRename={() => { setRenameTarget(f); setRenameValue(f.name); }}
            onDelete={() => setDeleteTarget(f)}
          />
        ))}

        {labelsQ.data && labelsQ.data.length > 0 && (
          <>
            <div className="border-t border-line my-2" />
            <p className="px-2 pt-1 pb-1 text-xs uppercase text-ink-mute">Labels</p>
            {labelsQ.data.map((l) => (
              <NavLink
                key={l.id}
                to={`/mail/${mailbox.id}/${activeFolderId}?label=${l.id}`}
                className={({ isActive }) =>
                  clsx('flex items-center gap-2 px-2 py-1 text-sm rounded hover:bg-canvas', isActive && 'bg-canvas')
                }
                onClick={onNavigate}
              >
                <span
                  className="w-3 h-3 rounded-full shrink-0"
                  style={{ backgroundColor: l.color }}
                  aria-hidden
                />
                <span className="truncate">{l.name}</span>
              </NavLink>
            ))}
          </>
        )}
      </nav>

      {/* New folder modal */}
      <Modal
        open={showNewFolder}
        onClose={() => setShowNewFolder(false)}
        title="New folder"
        footer={
          <>
            <Button variant="secondary" onClick={() => setShowNewFolder(false)}>Cancel</Button>
            <Button
              variant="primary"
              onClick={handleCreateFolder}
              loading={createFolder.isPending}
              disabled={!newFolderName.trim()}
            >Create</Button>
          </>
        }
      >
        <form onSubmit={handleCreateFolder}>
          <Field label="Folder name" htmlFor="new-folder-name">
            <Input
              id="new-folder-name"
              autoFocus
              value={newFolderName}
              onChange={(e) => setNewFolderName(e.target.value)}
              placeholder="e.g. Invoices"
            />
          </Field>
        </form>
      </Modal>

      {/* Rename modal */}
      <Modal
        open={!!renameTarget}
        onClose={() => setRenameTarget(null)}
        title="Rename folder"
        footer={
          <>
            <Button variant="secondary" onClick={() => setRenameTarget(null)}>Cancel</Button>
            <Button
              variant="primary"
              onClick={handleRenameFolder}
              loading={renameFolder.isPending}
              disabled={!renameValue.trim()}
            >Save</Button>
          </>
        }
      >
        <form onSubmit={handleRenameFolder}>
          <Field label="Folder name" htmlFor="rename-folder-name">
            <Input
              id="rename-folder-name"
              autoFocus
              value={renameValue}
              onChange={(e) => setRenameValue(e.target.value)}
            />
          </Field>
        </form>
      </Modal>

      <ConfirmDialog
        open={!!deleteTarget}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={handleDeleteFolder}
        title={`Delete "${deleteTarget?.name}"?`}
        description="All messages inside will be moved to Trash."
        confirmLabel="Delete folder"
        danger
        busy={deleteFolder.isPending}
      />
    </div>
  );
}

// ===== Folder row =====
interface FolderRowProps {
  mailboxId: string;
  folder: Folder;
  active: boolean;
  onNavigate?: () => void;
  onRename: () => void;
  onDelete: () => void;
}
function FolderRow({ mailboxId, folder, active, onNavigate, onRename, onDelete }: FolderRowProps) {
  const iconName = folder.role ? ROLE_ICONS[folder.role] ?? 'folder' : 'folder';
  return (
    <div className="group flex items-center">
      <NavLink
        to={`/mail/${mailboxId}/${folder.id}`}
        className={clsx(
          'flex-1 flex items-center gap-2 px-2 py-1.5 text-sm rounded truncate',
          active ? 'bg-canvas text-ink font-medium' : 'text-ink-soft hover:bg-canvas hover:text-ink',
        )}
        onClick={onNavigate}
      >
        <Icon name={iconName as Parameters<typeof Icon>[0]['name']} size={16} />
        <span className="truncate flex-1">{folder.name}</span>
        {folder.unread > 0 && (
          <span className="text-xs text-ink font-medium">{folder.unread}</span>
        )}
      </NavLink>
      {folder.role === null && (
        <Menu
          trigger={
            <button
              type="button"
              className="opacity-0 group-hover:opacity-100 focus:opacity-100 p-1 rounded hover:bg-canvas"
              aria-label={`Folder actions for ${folder.name}`}
            >
              <Icon name="more" size={14} />
            </button>
          }
        >
          <MenuItem onClick={onRename}>Rename</MenuItem>
          <MenuItem onClick={onDelete} danger>Delete</MenuItem>
        </Menu>
      )}
    </div>
  );
}

// ===== Mailbox switcher =====
function MailboxSwitcher({ mailbox, mailboxes, onNavigate }: { mailbox: Mailbox; mailboxes: Mailbox[]; onNavigate?: () => void }) {
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  void open;
  const { isOrgAdmin } = useMailboxActions();

  return (
    <div className="px-3 pt-3 pb-2 border-b border-line">
      <div className="flex items-center gap-2">
        <Menu
          trigger={
            <button
              type="button"
              onClick={() => setOpen((o) => !o)}
              className="flex-1 flex items-center gap-2 px-2 py-1.5 rounded border border-line bg-canvas text-sm text-left hover:border-ink-mute min-w-0"
            >
              <span className="flex-1 truncate font-medium">{mailbox.address}</span>
              <Icon name="chevron-down" size={14} />
            </button>
          }
        >
          {mailboxes.map((m) => (
            <MenuItem
              key={m.id}
              onClick={() => { navigate(`/mail/${m.id}`); onNavigate?.(); }}
            >
              <div className="truncate text-sm">
                {m.display_name && <div className="text-ink-soft text-xs">{m.display_name}</div>}
                <div>{m.address}</div>
              </div>
            </MenuItem>
          ))}
          {isOrgAdmin && (
            <>
              <div className="border-t border-line my-1" />
              <MenuItem onClick={() => navigate('/org/mailboxes')}>Manage mailboxes…</MenuItem>
            </>
          )}
        </Menu>
      </div>
    </div>
  );
}

// ===== Ordering =====
function partitionFolders(folders: Folder[]) {
  const order: Folder[] = [];
  const roleRank: Record<string, number> = {
    inbox: 0,
    sent: 5,
    drafts: 6,
    archive: 7,
    spam: 8,
    trash: 9,
  };
  const sorted = [...folders].sort((a, b) => {
    const ra = a.role ? roleRank[a.role] ?? 10 : 1;
    const rb = b.role ? roleRank[b.role] ?? 10 : 1;
    if (ra !== rb) return ra - rb;
    return a.name.localeCompare(b.name);
  });
  order.push(...sorted);
  return { order };
}
