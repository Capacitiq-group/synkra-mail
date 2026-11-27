import { useState } from 'react';
import { useBimiState } from '@/hooks/queries/deliverability';
import { Badge, Button, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { BimiWizard } from './BimiWizard';

export function BimiCard({ domain }: { domain: string }) {
  const q = useBimiState(domain);
  const [open, setOpen] = useState(false);

  return (
    <section className="border border-line rounded-lg bg-surface p-4">
      <div className="flex items-start justify-between gap-3 mb-2">
        <div>
          <h2 className="font-medium flex items-center gap-2">
            BIMI
            {q.data && <Badge tone={q.data.enabled ? 'success' : 'neutral'}>{q.data.enabled ? 'enabled' : 'not configured'}</Badge>}
          </h2>
          <p className="text-xs text-ink-soft mt-0.5">
            Display your verified logo next to your emails in Gmail, Apple Mail and other clients that support BIMI.
          </p>
        </div>
        <Button variant="secondary" onClick={() => setOpen(true)}>
          {q.data?.enabled ? 'Manage' : 'Set up'}
        </Button>
      </div>

      {q.isLoading && <Skeleton className="h-12 w-full" />}
      {q.data && (
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mt-3">
          <div className="border border-line rounded p-2">
            <p className="text-xs text-ink-mute">DMARC phase</p>
            <p className="text-sm font-medium">{q.data.dmarc_phase}</p>
          </div>
          <div className="border border-line rounded p-2">
            <p className="text-xs text-ink-mute">Logo</p>
            <p className="text-sm font-medium">{q.data.logo_verified ? 'Verified' : q.data.logo_url ? 'Unverified' : '—'}</p>
          </div>
          <div className="border border-line rounded p-2">
            <p className="text-xs text-ink-mute">VMC</p>
            <p className="text-sm font-medium">{q.data.vmc_status}</p>
          </div>
        </div>
      )}

      {q.data && q.data.blockers.length > 0 && (
        <div className="mt-3 p-2 border border-warning/40 bg-warning-soft rounded text-xs flex items-start gap-2">
          <Icon name="alert-triangle" size={12} className="text-warning mt-0.5" />
          <ul className="space-y-0.5">
            {q.data.blockers.map((b, i) => <li key={i}>{b}</li>)}
          </ul>
        </div>
      )}

      <BimiWizard domain={domain} open={open} onClose={() => setOpen(false)} />
    </section>
  );
}
