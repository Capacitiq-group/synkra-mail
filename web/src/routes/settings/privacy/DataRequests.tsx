import { useCreateDsar, useDsarRequests } from '@/hooks/queries/privacy';
import { Button, ConfirmDialog, Skeleton } from '@/design';
import { useState } from 'react';
import { formatDateTime } from '@/lib/time';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { Icon } from '@/design/icons';
import type { DsarRequest } from '@/lib/types';

export function DataRequests() {
  const q = useDsarRequests();
  const create = useCreateDsar();
  const [confirmKind, setConfirmKind] = useState<'export' | 'delete' | null>(null);

  async function submit(kind: 'export' | 'delete') {
    try {
      await create.mutateAsync(kind);
      toast('success', kind === 'export' ? 'Export requested' : 'Deletion requested', 'You will be notified when it completes.');
      setConfirmKind(null);
    } catch (e) {
      toast('error', 'Request failed', errorMessage(e));
    }
  }

  return (
    <div className="space-y-4 max-w-3xl">
      <p className="text-sm text-ink-soft">
        Your POPIA rights include access to your data and deletion of it. Requests are processed within 30 days; most complete within minutes.
      </p>

      <div className="flex gap-2">
        <Button variant="secondary" onClick={() => setConfirmKind('export')}>
          <Icon name="download" size={14} /> Export my data
        </Button>
        <Button variant="danger" onClick={() => setConfirmKind('delete')}>
          <Icon name="trash" size={14} /> Delete my account and data
        </Button>
      </div>

      <div>
        <h3 className="text-sm font-medium mb-2">Request history</h3>
        {q.isLoading && <Skeleton className="h-20 w-full" />}
        {q.data && q.data.length === 0 && <p className="text-sm text-ink-soft">No requests yet.</p>}
        {q.data && q.data.length > 0 && (
          <ul className="space-y-2">
            {q.data.map((r: DsarRequest) => (
              <li key={r.id} className="flex items-center gap-3 p-3 border border-line rounded">
                <StatusBadge status={r.status} />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium">{r.kind === 'export' ? 'Export' : 'Deletion'}</p>
                  <p className="text-xs text-ink-soft">Requested {formatDateTime(r.created_at)}</p>
                </div>
                {r.status === 'ready' && r.download_url && (
                  <a
                    href={r.download_url}
                    className="text-sm text-accent hover:underline"
                    download
                  >Download</a>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>

      <ConfirmDialog
        open={confirmKind === 'export'}
        onCancel={() => setConfirmKind(null)}
        onConfirm={() => submit('export')}
        title="Export all my data?"
        description="We will assemble a JSON archive of your messages, contacts, calendar, files and account details and send you a download link."
        confirmLabel="Request export"
        busy={create.isPending}
      />
      <ConfirmDialog
        open={confirmKind === 'delete'}
        onCancel={() => setConfirmKind(null)}
        onConfirm={() => submit('delete')}
        title="Delete my account and data?"
        description="This removes your mailbox, messages and personal data. Domain registration records required by ZACR are retained for the life of the registration and are handled separately."
        confirmLabel="Request deletion"
        danger
        busy={create.isPending}
      />
    </div>
  );
}

function StatusBadge({ status }: { status: DsarRequest['status'] }) {
  const map: Record<DsarRequest['status'], { label: string; cls: string }> = {
    queued: { label: 'Queued', cls: 'bg-canvas text-ink-soft border border-line' },
    processing: { label: 'Processing', cls: 'bg-warning-soft text-warning' },
    ready: { label: 'Ready', cls: 'bg-success-soft text-success' },
    completed: { label: 'Completed', cls: 'bg-success-soft text-success' },
    rejected: { label: 'Rejected', cls: 'bg-danger-soft text-danger' },
  };
  const { label, cls } = map[status];
  return <span className={`text-xs px-2 py-0.5 rounded ${cls}`}>{label}</span>;
}
