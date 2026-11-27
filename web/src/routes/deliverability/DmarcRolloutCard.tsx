import { useState } from 'react';
import { useDmarcRollout } from '@/hooks/queries/deliverability';
import { Badge, Button, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { DmarcWizard } from './DmarcWizard';
import type { DmarcPhase } from '@/lib/types';

const PHASE_LABEL: Record<DmarcPhase, string> = {
  none: 'Monitoring (p=none)',
  quarantine: 'Quarantine (p=quarantine)',
  reject: 'Reject (p=reject)',
};

export function DmarcRolloutCard({ domain }: { domain: string; currentPhase?: DmarcPhase }) {
  const q = useDmarcRollout(domain);
  const [wizardOpen, setWizardOpen] = useState(false);

  return (
    <section className="border border-line rounded-lg bg-surface p-4">
      <div className="flex items-start justify-between gap-3 mb-3">
        <div>
          <h2 className="font-medium">DMARC</h2>
          <p className="text-xs text-ink-soft mt-0.5">
            DMARC tells receivers what to do when SPF or DKIM fail. Moving to quarantine and then reject prevents spoofing.
          </p>
        </div>
        <Button variant="primary" onClick={() => setWizardOpen(true)}>
          {q.data?.current_phase === 'reject' ? 'Review DMARC' : 'Start rollout'}
        </Button>
      </div>

      {q.isLoading && <Skeleton className="h-16 w-full" />}
      {q.data && (
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <div className="border border-line rounded p-3">
            <p className="text-xs text-ink-mute">Current phase</p>
            <p className="text-sm font-medium mt-1">
              <Badge tone={q.data.current_phase === 'reject' ? 'success' : q.data.current_phase === 'quarantine' ? 'warning' : 'neutral'}>
                {PHASE_LABEL[q.data.current_phase]}
              </Badge>
            </p>
          </div>
          <div className="border border-line rounded p-3">
            <p className="text-xs text-ink-mute">7-day pass rate</p>
            <p className="text-sm font-medium mt-1">{(q.data.pass_rate_7d * 100).toFixed(1)}%</p>
            <p className="text-xs text-ink-mute">{q.data.message_volume_7d.toLocaleString('en-ZA')} messages</p>
          </div>
          <div className="border border-line rounded p-3">
            <p className="text-xs text-ink-mute">Aggregate reports to</p>
            <p className="text-sm font-medium mt-1 break-all">{q.data.rua_address}</p>
          </div>
        </div>
      )}

      {q.data?.next_recommended_phase && (
        <div className="mt-3 p-2 border border-accent/40 bg-accent-soft rounded text-sm flex items-start gap-2">
          <Icon name="info" size={14} className="text-accent mt-0.5" />
          <div>
            <p className="font-medium">Recommended next step</p>
            <p className="text-xs text-ink-soft">
              Move to <strong>{PHASE_LABEL[q.data.next_recommended_phase]}</strong>. {q.data.next_phase_reason}
            </p>
          </div>
        </div>
      )}

      <DmarcWizard domain={domain} open={wizardOpen} onClose={() => setWizardOpen(false)} />
    </section>
  );
}
