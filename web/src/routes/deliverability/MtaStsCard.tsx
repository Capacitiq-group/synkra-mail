import { useEffect, useState } from 'react';
import { useMtaStsState, useSetMtaSts } from '@/hooks/queries/deliverability';
import { Button, Field, Select, Skeleton, Switch } from '@/design';
import { CopyButton } from '@/components/CopyButton';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { Icon } from '@/design/icons';
import type { MtaStsMode } from '@/lib/types';

const MAX_AGE_OPTIONS = [
  { label: '1 hour (initial rollout)', value: 3600 },
  { label: '1 day (recommended)', value: 86400 },
  { label: '1 week (steady state)', value: 604800 },
];

export function MtaStsCard({ domain }: { domain: string }) {
  const q = useMtaStsState(domain);
  const save = useSetMtaSts(domain);
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [mode, setMode] = useState<MtaStsMode | null>(null);
  const [maxAge, setMaxAge] = useState<number | null>(null);

  useEffect(() => {
    if (q.data) {
      setEnabled(q.data.enabled);
      setMode(q.data.mode);
      setMaxAge(q.data.max_age_seconds);
    }
  }, [q.data]);

  async function apply() {
    if (enabled === null || !mode || !maxAge) return;
    try {
      await save.mutateAsync({ enabled, mode, max_age_seconds: maxAge });
      toast('success', 'MTA-STS saved');
    } catch (e) {
      toast('error', 'Could not save MTA-STS', errorMessage(e));
    }
  }

  const currentMode = mode ?? q.data?.mode ?? 'none';
  const currentMaxAge = maxAge ?? q.data?.max_age_seconds ?? 86400;
  const currentEnabled = enabled ?? q.data?.enabled ?? false;

  return (
    <section className="border border-line rounded-lg bg-surface p-4">
      <div className="flex items-start justify-between gap-3 mb-2">
        <div>
          <h2 className="font-medium">MTA-STS</h2>
          <p className="text-xs text-ink-soft mt-0.5">
            Enforces TLS when others send to you. Prevents downgrade attacks that strip encryption in transit.
          </p>
        </div>
      </div>

      {q.isLoading && <Skeleton className="h-12 w-full" />}
      {q.data && (
        <div className="space-y-3 mt-2">
          <Switch
            checked={currentEnabled}
            onChange={setEnabled}
            label={currentEnabled ? 'MTA-STS enabled' : 'MTA-STS disabled'}
          />

          {currentEnabled && (
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <Field label="Enforcement mode">
                <Select value={currentMode} onChange={(e) => setMode(e.target.value as MtaStsMode)}>
                  <option value="none">None — publish policy only</option>
                  <option value="testing">Testing — report failures, do not block</option>
                  <option value="enforce">Enforce — block non-compliant senders</option>
                </Select>
              </Field>
              <Field label="Policy max-age">
                <Select value={currentMaxAge} onChange={(e) => setMaxAge(Number(e.target.value))}>
                  {MAX_AGE_OPTIONS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
                </Select>
              </Field>
            </div>
          )}

          {currentEnabled && currentMode === 'enforce' && (
            <div className="p-2 border border-warning/40 bg-warning-soft rounded text-xs flex items-start gap-2">
              <Icon name="alert-triangle" size={12} className="text-warning mt-0.5" />
              <p>Make sure every receiver that legitimately sends to you supports TLS. Otherwise you will lose legitimate email.</p>
            </div>
          )}

          <div className="border border-line rounded p-3">
            <p className="text-xs uppercase text-ink-mute mb-1">DNS TXT record</p>
            <div className="flex items-center gap-2">
              <code className="text-xs break-all flex-1">{q.data.dns_record_value}</code>
              <CopyButton value={q.data.dns_record_value} small />
            </div>
            <p className="text-xs text-ink-mute mt-1">
              Host: <code>{q.data.dns_record_name}</code>
            </p>
          </div>

          <div className="border border-line rounded p-3">
            <p className="text-xs uppercase text-ink-mute mb-1">Policy file (hosted automatically)</p>
            <p className="text-xs break-all">{q.data.policy_url}</p>
            <p className="text-xs text-ink-mute mt-1">
              {q.data.mx_hosts.length > 0
                ? `Covers MX: ${q.data.mx_hosts.join(', ')}`
                : 'No MX hosts configured yet.'}
            </p>
          </div>

          <div className="flex gap-2">
            <Button variant="primary" onClick={apply} loading={save.isPending}>Save</Button>
            {q.data.last_published_at && (
              <p className="text-xs text-ink-mute self-center">
                Last published {new Date(q.data.last_published_at).toLocaleString('en-ZA')}
              </p>
            )}
          </div>
        </div>
      )}
    </section>
  );
}
