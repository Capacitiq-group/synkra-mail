import { useState } from 'react';
import { useBimiState, useSetBimi } from '@/hooks/queries/deliverability';
import { Button, Field, Input, Modal, Skeleton, Switch } from '@/design';
import { Icon } from '@/design/icons';
import { CopyButton } from '@/components/CopyButton';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';

export function BimiWizard({ domain, open, onClose }: { domain: string; open: boolean; onClose: () => void }) {
  const q = useBimiState(domain);
  const save = useSetBimi(domain);
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [logoUrl, setLogoUrl] = useState<string | null>(null);

  const effectiveEnabled = enabled ?? q.data?.enabled ?? false;
  const effectiveLogo = logoUrl ?? q.data?.logo_url ?? '';

  async function apply() {
    try {
      await save.mutateAsync({ enabled: effectiveEnabled, logo_url: effectiveLogo || undefined });
      toast('success', 'BIMI saved', 'Propagation via DNS takes effect within a few hours.');
      onClose();
    } catch (e) {
      toast('error', 'Could not save BIMI', errorMessage(e));
    }
  }

  const blockers: string[] = [];
  if (q.data && q.data.dmarc_phase !== 'quarantine' && q.data.dmarc_phase !== 'reject') {
    blockers.push('DMARC must be at p=quarantine or p=reject. Run the DMARC rollout first.');
  }
  if (effectiveEnabled && !effectiveLogo) {
    blockers.push('Enter a logo URL to enable BIMI.');
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="BIMI setup"
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button variant="primary" onClick={apply} loading={save.isPending} disabled={blockers.length > 0}>
            Save BIMI
          </Button>
        </>
      }
    >
      {q.isLoading && <Skeleton className="h-40 w-full" />}
      {q.isError && <p className="text-sm text-danger">Could not load BIMI state.</p>}
      {q.data && (
        <div className="space-y-4">
          <div className="p-3 border border-line rounded bg-canvas">
            <p className="text-xs uppercase text-ink-mute mb-1">Requirements</p>
            <ul className="text-xs text-ink-soft list-disc pl-5 space-y-1">
              <li>DMARC at <code>p=quarantine</code> or <code>p=reject</code> (currently <strong>{q.data.dmarc_phase}</strong>).</li>
              <li>Logo in SVG Tiny PS format, hosted over HTTPS.</li>
              <li>For Gmail, a Verified Mark Certificate (VMC) from an authorised authority.</li>
            </ul>
          </div>

          <Switch checked={effectiveEnabled} onChange={setEnabled} label="Enable BIMI" />

          <Field
            label="Logo URL"
            hint="Direct HTTPS URL to a square SVG (SVG Tiny PS) or PNG logo, minimum 96×96px. Often hosted at assets.yourdomain.com or the same host as your website."
          >
            <Input
              type="url"
              value={effectiveLogo}
              onChange={(e) => setLogoUrl(e.target.value)}
              placeholder="https://example.com/logo.svg"
            />
          </Field>

          {effectiveLogo && (
            <div className="border border-line rounded p-3">
              <p className="text-xs uppercase text-ink-mute mb-2">Preview</p>
              <div className="flex items-center gap-3">
                <img
                  src={effectiveLogo}
                  alt="BIMI logo preview"
                  className="w-12 h-12 rounded border border-line bg-surface object-contain"
                  onError={(e) => {
                    (e.currentTarget as HTMLImageElement).style.opacity = '0.2';
                  }}
                />
                <div className="text-xs text-ink-soft">
                  <p>Shown in recipients' inbox lists where BIMI is supported.</p>
                  <p className="text-ink-mute">If the logo does not appear here, check the URL is publicly accessible over HTTPS.</p>
                </div>
              </div>
            </div>
          )}

          <div className="border border-line rounded p-3">
            <p className="text-xs uppercase text-ink-mute mb-1">DMARC record (required by BIMI)</p>
            <div className="flex items-center gap-2">
              <code className="text-xs break-all flex-1">{q.data.record_value}</code>
              <CopyButton value={q.data.record_value} small />
            </div>
            <p className="text-xs text-ink-mute mt-1">Host: <code>{q.data.record_name}</code></p>
          </div>

          <div className="border border-line rounded p-3">
            <p className="text-xs uppercase text-ink-mute mb-1">BIMI record to publish</p>
            <div className="flex items-center gap-2">
              <code className="text-xs break-all flex-1">{q.data.record_value}</code>
              <CopyButton value={q.data.record_value} small />
            </div>
            <p className="text-xs text-ink-mute mt-1">
              Host: <code>{q.data.record_name}</code> · TTL 300
            </p>
          </div>

          {blockers.length > 0 && (
            <div className="p-2 border border-warning/40 bg-warning-soft rounded text-xs flex items-start gap-2">
              <Icon name="alert-triangle" size={12} className="text-warning mt-0.5 shrink-0" />
              <ul className="space-y-0.5">
                {blockers.map((b, i) => <li key={i}>{b}</li>)}
              </ul>
            </div>
          )}
        </div>
      )}
    </Modal>
  );
}
