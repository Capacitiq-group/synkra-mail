import { useEffect, useState } from 'react';
import { useIpRestrictions, useSetIpRestrictions } from '@/hooks/queries/security';
import { Button, Field, Input, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';

const CIDR_RE = /^(\d{1,3}\.){3}\d{1,3}(\/\d{1,2})?$/;

export function IpRestrictionsTab() {
  const q = useIpRestrictions(true);
  const set = useSetIpRestrictions();
  const [entries, setEntries] = useState<string[]>([]);
  const [newCidr, setNewCidr] = useState('');

  useEffect(() => { if (q.data) setEntries(q.data); }, [q.data]);

  function add() {
    const v = newCidr.trim();
    if (!CIDR_RE.test(v)) { toast('error', 'Enter an IPv4 address or CIDR block'); return; }
    if (entries.includes(v)) { toast('info', 'Already listed'); return; }
    setEntries([...entries, v]);
    setNewCidr('');
  }

  async function save() {
    try {
      await set.mutateAsync(entries);
      toast('success', 'IP restrictions saved');
    } catch (e) {
      toast('error', 'Could not save', errorMessage(e));
    }
  }

  if (q.isLoading) return <Skeleton className="h-40 w-full" />;

  return (
    <div className="max-w-2xl space-y-4">
      <p className="text-sm text-ink-soft">
        Restrict where members of your organisation can sign in from. When enabled, connections from other addresses
        are refused at the auth layer.
      </p>

      <div className="border border-line rounded-lg p-4 space-y-3">
        <div className="flex items-end gap-2">
          <div className="flex-1">
            <Field label="Add an address or CIDR block" htmlFor="cidr-input">
              <Input
                id="cidr-input"
                value={newCidr}
                onChange={(e) => setNewCidr(e.target.value)}
                placeholder="196.24.0.0/16"
                onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); add(); } }}
              />
            </Field>
          </div>
          <Button variant="secondary" onClick={add} disabled={!newCidr.trim()}>Add</Button>
        </div>

        {entries.length === 0 ? (
          <p className="text-xs text-ink-soft">No restrictions — any IP can sign in.</p>
        ) : (
          <ul className="space-y-1">
            {entries.map((c) => (
              <li key={c} className="flex items-center justify-between gap-2 px-2 py-1 border border-line rounded font-mono text-xs">
                <span>{c}</span>
                <button
                  onClick={() => setEntries(entries.filter((x) => x !== c))}
                  className="text-ink-mute hover:text-danger"
                  aria-label={`Remove ${c}`}
                ><Icon name="close" size={12} /></button>
              </li>
            ))}
          </ul>
        )}

        <div className="flex justify-end gap-2 pt-2 border-t border-line">
          <Button variant="secondary" onClick={() => setEntries(q.data ?? [])} disabled={!q.data}>Reset</Button>
          <Button variant="primary" onClick={save} loading={set.isPending}>Save restrictions</Button>
        </div>
      </div>
    </div>
  );
}
