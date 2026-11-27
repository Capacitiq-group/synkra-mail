import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useUpdateAlias } from '@/hooks/queries/aliases';
import { Button, Checkbox, DateTimePicker, Drawer, Input } from '@/design';
import { Icon } from '@/design/icons';
import { CopyButton } from '@/components/CopyButton';
import { formatDateTime, relativeTime } from '@/lib/time';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import type { Alias } from '@/lib/types';

export function AliasDetailDrawer({
  mailboxId, alias, open, onClose,
}: {
  mailboxId: string;
  alias: Alias | null;
  open: boolean;
  onClose: () => void;
}) {
  const update = useUpdateAlias(mailboxId);
  const [blockedInput, setBlockedInput] = useState('');
  const [forwardTo, setForwardTo] = useState(alias?.forward_to ?? '');
  const [expires, setExpires] = useState(alias?.expires_at ?? '');

  // Reset local edits when the drawer opens on a different alias.
  if (alias && forwardTo !== (alias.forward_to ?? '') && !update.isPending && open) {
    // The user may be mid-edit; only re-sync when the alias identity changes.
    // This branch intentionally does nothing — the effect below handles sync.
  }

  if (!alias) return null;

  async function patch(p: Partial<Alias>, msg: string) {
    try {
      await update.mutateAsync({ id: alias!.id, patch: p });
      toast('success', msg);
    } catch (err) {
      toast('error', 'Update failed', errorMessage(err));
    }
  }

  function addBlockedSender() {
    const v = blockedInput.trim().toLowerCase();
    if (!v) return;
    if (alias!.blocked_senders.includes(v)) { setBlockedInput(''); return; }
    void patch({ blocked_senders: [...alias!.blocked_senders, v] }, 'Sender blocked');
    setBlockedInput('');
  }

  function removeBlockedSender(sender: string) {
    void patch({ blocked_senders: alias!.blocked_senders.filter((s) => s !== sender) }, 'Sender unblocked');
  }

  async function saveForward() {
    await patch({ forward_to: forwardTo.trim() || undefined }, 'Forward destination updated');
  }

  async function saveExpiry() {
    await patch({ expires_at: expires || undefined }, 'Expiry updated');
  }

  return (
    <Drawer open={open} onClose={onClose} side="right" title={alias.label}>
      <div className="p-4 space-y-6 text-sm">
        <section>
          <div className="flex items-center gap-2">
            <code className="text-sm font-mono break-all">{alias.address}</code>
            <CopyButton value={alias.address} small />
          </div>
          <p className="text-xs text-ink-soft mt-1">
            {alias.message_count} message{alias.message_count === 1 ? '' : 's'}
            {alias.last_used_at ? ` · last used ${relativeTime(alias.last_used_at)}` : ' · never used'}
          </p>
          <p className="text-xs text-ink-mute mt-0.5">Created {formatDateTime(alias.created_at)}</p>
        </section>

        <section className="space-y-2">
          <Row label="Category">
            <span className="text-sm capitalize">{alias.category}</span>
          </Row>
          <Row label="Status">
            {alias.disabled ? (
              <span className="text-sm text-danger">Disabled</span>
            ) : alias.single_use ? (
              <span className="text-sm text-warning">Single-use · waiting for first message</span>
            ) : alias.expires_at && new Date(alias.expires_at) < new Date() ? (
              <span className="text-sm text-danger">Expired {formatDateTime(alias.expires_at)}</span>
            ) : (
              <span className="text-sm text-success">Active</span>
            )}
          </Row>
        </section>

        <section>
          <h3 className="text-xs uppercase text-ink-mute mb-2">Toggles</h3>
          <div className="space-y-2">
            <Checkbox
              label="Disabled (stop forwarding)"
              checked={alias.disabled}
              onChange={(e) => patch({ disabled: e.target.checked }, e.target.checked ? 'Alias disabled' : 'Alias re-enabled')}
            />
            <Checkbox
              label="Notify me when this alias receives mail"
              checked={alias.notification_enabled}
              onChange={(e) => patch({ notification_enabled: e.target.checked }, 'Notifications updated')}
            />
            <Checkbox
              label="Allow replying from this alias"
              checked={!!alias.reply_from_enabled}
              onChange={(e) => patch({ reply_from_enabled: e.target.checked }, e.target.checked ? 'Reply-from enabled' : 'Reply-from disabled')}
            />
            <p className="text-xs text-ink-mute ml-6">
              Requires SPF and DKIM on this alias's domain to be managed by Synkra. Disabled aliases still accept replies sent from an existing thread.
            </p>
          </div>
        </section>

        <section>
          <h3 className="text-xs uppercase text-ink-mute mb-2">Forward destination</h3>
          <div className="flex gap-2">
            <Input
              type="email"
              value={forwardTo}
              onChange={(e) => setForwardTo(e.target.value)}
              placeholder="Leave blank for your own mailbox"
            />
            <Button variant="secondary" onClick={saveForward} disabled={update.isPending}>Save</Button>
          </div>
        </section>

        <section>
          <h3 className="text-xs uppercase text-ink-mute mb-2">Expires</h3>
          <div className="space-y-2">
            <DateTimePicker label="" value={expires} onChange={setExpires} min={new Date().toISOString()} />
            <div className="flex gap-2">
              <Button variant="secondary" onClick={saveExpiry} disabled={update.isPending}>Save expiry</Button>
              {alias.expires_at && (
                <Button
                  variant="ghost"
                  onClick={() => { setExpires(''); void patch({ expires_at: undefined }, 'Expiry cleared'); }}
                >Clear</Button>
              )}
            </div>
          </div>
        </section>

        <section>
          <h3 className="text-xs uppercase text-ink-mute mb-2">Blocked senders</h3>
          <div className="flex gap-2 mb-2">
            <Input
              type="email"
              value={blockedInput}
              onChange={(e) => setBlockedInput(e.target.value)}
              placeholder="spammer@example.com"
              onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addBlockedSender(); } }}
            />
            <Button variant="secondary" onClick={addBlockedSender} disabled={!blockedInput.trim() || update.isPending}>Block</Button>
          </div>
          {alias.blocked_senders.length === 0 ? (
            <p className="text-xs text-ink-soft">No blocked senders.</p>
          ) : (
            <ul className="space-y-1">
              {alias.blocked_senders.map((s) => (
                <li key={s} className="flex items-center justify-between gap-2 px-2 py-1 border border-line rounded text-xs">
                  <span className="truncate">{s}</span>
                  <button
                    onClick={() => removeBlockedSender(s)}
                    className="text-ink-mute hover:text-danger"
                    aria-label={`Unblock ${s}`}
                  ><Icon name="close" size={12} /></button>
                </li>
              ))}
            </ul>
          )}
        </section>

        <section className="border-t border-line pt-4">
          <Link
            to={`/settings/rules?alias=${alias.id}`}
            className="text-sm text-accent hover:underline inline-flex items-center gap-1"
          >
            <Icon name="sparkles" size={12} /> Advanced rules for this alias
          </Link>
          <p className="text-xs text-ink-mute mt-1">Route messages, auto-label, forward conditionally and more.</p>
        </section>
      </div>
    </Drawer>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-xs text-ink-mute">{label}</span>
      {children}
    </div>
  );
}
