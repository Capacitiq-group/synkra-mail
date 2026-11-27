import { useEffect, useState } from 'react';
import { useSetTlsRpt, useTlsRptState } from '@/hooks/queries/deliverability';
import { Button, Field, Input, Skeleton, Switch } from '@/design';
import { CopyButton } from '@/components/CopyButton';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';

export function TlsRptCard({ domain }: { domain: string }) {
  const q = useTlsRptState(domain);
  const save = useSetTlsRpt(domain);
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [rua, setRua] = useState<string | null>(null);

  useEffect(() => {
    if (q.data) {
      setEnabled(q.data.enabled);
      setRua(q.data.rua_address ?? `tlsrpt@${domain}`);
    }
  }, [q.data, domain]);

  async function apply() {
    if (enabled === null) return;
    try {
      await save.mutateAsync({ enabled, rua_address: enabled ? (rua || undefined) : undefined });
      toast('success', 'TLS-RPT saved');
    } catch (e) {
      toast('error', 'Could not save TLS-RPT', errorMessage(e));
    }
  }

  const currentEnabled = enabled ?? q.data?.enabled ?? false;
  const currentRua = rua ?? q.data?.rua_address ?? `tlsrpt@${domain}`;

  return (
    <section className="border border-line rounded-lg bg-surface p-4">
      <div className="mb-2">
        <h2 className="font-medium">TLS-RPT</h2>
        <p className="text-xs text-ink-soft mt-0.5">
          Receivers send you daily reports about TLS delivery success and failures. Usually enabled alongside MTA-STS.
        </p>
      </div>

      {q.isLoading && <Skeleton className="h-12 w-full" />}
      {q.data && (
        <div className="space-y-3 mt-2">
          <Switch checked={currentEnabled} onChange={setEnabled} label={currentEnabled ? 'TLS-RPT enabled' : 'TLS-RPT disabled'} />

          {currentEnabled && (
            <>
              <Field label="Reports sent to" hint="Reports are routed to the address you choose — usually a role address or a monitored mailbox.">
                <Input
                  type="email"
                  value={currentRua}
                  onChange={(e) => setRua(e.target.value)}
                  placeholder={`tlsrpt@${domain}`}
                />
              </Field>

              <div className="border border-line rounded p-3">
                <p className="text-xs uppercase text-ink-mute mb-1">DNS TXT record</p>
                <div className="flex items-center gap-2">
                  <code className="text-xs break-all flex-1">{q.data.dns_record_value}</code>
                  <CopyButton value={q.data.dns_record_value} small />
                </div>
                <p className="text-xs text-ink-mute mt-1">Host: <code>{q.data.dns_record_name}</code></p>
              </div>

              {q.data.last_report_at && (
                <div className="grid grid-cols-2 gap-2">
                  <div className="border border-line rounded p-2">
                    <p className="text-xs text-ink-mute">Last report</p>
                    <p className="text-sm font-medium">{new Date(q.data.last_report_at).toLocaleString('en-ZA')}</p>
                  </div>
                  {typeof q.data.success_rate_7d === 'number' && (
                    <div className="border border-line rounded p-2">
                      <p className="text-xs text-ink-mute">7-day TLS success</p>
                      <p className="text-sm font-medium">{(q.data.success_rate_7d * 100).toFixed(1)}%</p>
                    </div>
                  )}
                </div>
              )}
            </>
          )}

          <Button variant="primary" onClick={apply} loading={save.isPending}>Save</Button>
        </div>
      )}
    </section>
  );
}
