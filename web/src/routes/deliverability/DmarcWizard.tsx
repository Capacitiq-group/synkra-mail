import { useState } from 'react';
import { useDmarcRollout, useSetDmarcPhase } from '@/hooks/queries/deliverability';
import { Badge, Button, Modal, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { CopyButton } from '@/components/CopyButton';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import type { DmarcPhase } from '@/lib/types';

const PHASES: { id: DmarcPhase; label: string; description: string }[] = [
  { id: 'none', label: 'Monitoring', description: 'Collect data. No effect on delivery. Start here.' },
  { id: 'quarantine', label: 'Quarantine', description: 'Suspect mail goes to spam. Do this after seeing consistently high pass rates.' },
  { id: 'reject', label: 'Reject', description: 'Suspect mail is refused outright. The strongest protection against spoofing.' },
];

export function DmarcWizard({ domain, open, onClose }: { domain: string; open: boolean; onClose: () => void }) {
  const q = useDmarcRollout(domain);
  const set = useSetDmarcPhase(domain);
  const [phase, setPhase] = useState<DmarcPhase | null>(null);

  const current = q.data?.current_phase;
  const target = phase ?? q.data?.next_recommended_phase ?? current ?? 'none';

  async function apply() {
    if (!target) return;
    try {
      await set.mutateAsync(target);
      toast('success', 'DMARC updated', `Phase is now ${target}. Propagation may take up to 24 hours.`);
      onClose();
    } catch (e) {
      toast('error', 'Could not update DMARC', errorMessage(e));
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="DMARC rollout"
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            onClick={apply}
            loading={set.isPending}
            disabled={target === current}
          >
            {target === current ? 'Already at this phase' : `Apply ${target}`}
          </Button>
        </>
      }
    >
      {q.isLoading && <Skeleton className="h-40 w-full" />}
      {q.isError && <p className="text-sm text-danger">Could not load DMARC state.</p>}
      {q.data && (
        <div className="space-y-4">
          <div className="border border-line rounded p-3 bg-canvas">
            <p className="text-xs uppercase text-ink-mute mb-1">Current record</p>
            <div className="flex items-center gap-2">
              <code className="text-xs break-all flex-1">{q.data.record_value}</code>
              <CopyButton value={q.data.record_value} small />
            </div>
            <p className="text-xs text-ink-mute mt-1">Host: <code>_dmarc.{domain}</code></p>
          </div>

          <div>
            <p className="text-sm font-medium mb-2">Choose the next phase</p>
            <div className="space-y-2">
              {PHASES.map((p) => {
                const selected = target === p.id;
                const isCurrent = current === p.id;
                return (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => setPhase(p.id)}
                    className={`w-full text-left p-3 rounded border transition-colors ${selected ? 'border-accent bg-accent-soft' : 'border-line bg-surface hover:border-accent'}`}
                  >
                    <div className="flex items-center gap-2">
                      <input type="radio" checked={selected} onChange={() => setPhase(p.id)} className="rounded" />
                      <p className="text-sm font-medium">{p.label}</p>
                      {isCurrent && <Badge tone="neutral">current</Badge>}
                    </div>
                    <p className="text-xs text-ink-soft mt-1 ml-6">{p.description}</p>
                  </button>
                );
              })}
            </div>
          </div>

          {target !== 'none' && q.data.pass_rate_7d < 0.95 && (
            <div className="p-3 border border-warning/40 bg-warning-soft rounded text-sm flex items-start gap-2">
              <Icon name="alert-triangle" size={14} className="text-warning mt-0.5 shrink-0" />
              <div>
                <p className="font-medium">Pass rate is {(q.data.pass_rate_7d * 100).toFixed(1)}%</p>
                <p className="text-xs text-ink-soft mt-0.5">
                  Move forward only when the pass rate is consistently above 95% for legitimate traffic. Otherwise you
                  risk blocking legitimate senders.
                </p>
              </div>
            </div>
          )}

          <div className="p-3 border border-line rounded text-xs text-ink-soft">
            <p className="font-medium text-ink mb-1">What happens after apply</p>
            <ul className="list-disc pl-5 space-y-0.5">
              <li>The <code>_dmarc.{domain}</code> TXT record is updated.</li>
              <li>Propagation via DNS typically takes minutes, up to 24 hours at worst.</li>
              <li>Receivers start enforcing within a day.</li>
              <li>We keep collecting aggregate reports throughout.</li>
            </ul>
          </div>
        </div>
      )}
    </Modal>
  );
}
