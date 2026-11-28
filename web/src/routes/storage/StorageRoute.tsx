// StorageRoute — storage usage (phase 11): totals with the quota state,
// per-folder breakdown, largest messages, deterministic cleanup suggestions.
import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { usePageTitle } from '@/hooks';
import { Skeleton } from '@/design';
import { Panel } from '@/components/Banner';

function fmtBytes(n: number): string {
  if (n >= 1 << 30) return `${(n / (1 << 30)).toFixed(2)} GB`;
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1)} MB`;
  if (n >= 1 << 10) return `${(n / (1 << 10)).toFixed(0)} KB`;
  return `${n} B`;
}

export function StorageRoute() {
  usePageTitle('Storage');
  const q = useQuery({ queryKey: ['storage-usage'], queryFn: () => api.storageReport(), staleTime: 30_000 });
  if (q.isLoading) return <div className="p-6"><Skeleton className="h-64 w-full" /></div>;
  if (q.isError || !q.data) return <p className="p-6 text-sm text-danger">Could not load storage usage.</p>;
  const { totals, by_folder, largest, suggestions } = q.data;

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto space-y-4">
      <div>
        <h1 className="text-lg font-semibold">Storage</h1>
        <p className="text-sm text-ink-soft">Mail is never auto-deleted — cleanup is always your call.</p>
      </div>

      <Panel title="Usage">
        <div className="flex justify-between text-sm">
          <span>{fmtBytes(totals.used_bytes)} used of {fmtBytes(totals.included_bytes)} included{totals.grace_bytes > 0 ? ` (+${fmtBytes(totals.grace_bytes)} grace)` : ''}</span>
          <span className="font-medium">{Math.round(totals.used_percent)}%</span>
        </div>
        <div className="h-3 bg-canvas rounded overflow-hidden">
          <div className={`h-full ${totals.full ? 'bg-danger' : totals.used_percent > 80 ? 'bg-warning' : 'bg-accent'}`} style={{ width: `${Math.min(100, totals.used_percent)}%` }} />
        </div>
        {totals.beyond_grace && <p className="text-xs text-warning">Beyond grace — new mail delivery may be deferred until you clean up or upgrade.</p>}
      </Panel>

      <Panel title="By folder">
        {by_folder.length === 0 && <p className="text-sm text-ink-soft">No messages yet.</p>}
        <div className="space-y-1">
          {by_folder.map((f) => (
            <div key={`${f.mailbox}/${f.folder}`} className="flex items-center gap-3 text-sm">
              <span className="w-44 truncate">{f.mailbox} / {f.folder}</span>
              <div className="flex-1 h-2 bg-canvas rounded overflow-hidden">
                <div className="h-full bg-accent/60" style={{ width: `${Math.max(2, (f.bytes / Math.max(1, by_folder[0].bytes)) * 100)}%` }} />
              </div>
              <span className="w-20 text-right text-ink-soft">{fmtBytes(f.bytes)}</span>
              <span className="w-16 text-right text-ink-mute text-xs">{f.messages} msgs</span>
            </div>
          ))}
        </div>
      </Panel>

      <Panel title="Largest messages">
        {largest.length === 0 && <p className="text-sm text-ink-soft">Nothing big enough to list yet.</p>}
        <div className="space-y-1">
          {largest.map((m) => (
            <div key={m.id} className="flex items-center justify-between text-sm">
              <span className="truncate">{m.subject || '(no subject)'} <span className="text-ink-mute text-xs">· {m.mailbox}/{m.folder} · {new Date(m.received_at).toLocaleDateString('en-ZA')}</span></span>
              <span className="text-ink-soft">{fmtBytes(m.size_bytes)}</span>
            </div>
          ))}
        </div>
      </Panel>

      <Panel title="Cleanup suggestions">
        {suggestions.length === 0 && <p className="text-sm text-ink-soft">Nothing to clean right now.</p>}
        <div className="space-y-2">
          {suggestions.map((s) => (
            <div key={s.kind} className="flex items-center justify-between gap-3 border border-line rounded p-3 text-sm">
              <div>
                <p className="font-medium">{s.detail}</p>
                <p className="text-xs text-ink-soft">{s.count} message(s){s.bytes > 0 ? ` · ${fmtBytes(s.bytes)} reclaimable` : ''}</p>
              </div>
              <span className="text-xs px-2 py-1 rounded bg-canvas text-ink-soft">{s.action}</span>
            </div>
          ))}
        </div>
      </Panel>
    </div>
  );
}
