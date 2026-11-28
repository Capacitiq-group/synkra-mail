// DomainsRoute — the registrar-managed domain list (phase 11): lifecycle
// chips (ZARC states + grace windows), partially-managed banner, and the
// entry point to the add-domain wizard.
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { usePageTitle } from '@/hooks';
import { Button, EmptyState, Skeleton } from '@/design';
import { Panel } from '@/components/Banner';
import { AddDomainWizard } from './AddDomainWizard';
import type { DomainLifecycle } from '@/lib/types';

const LIFECYCLE_CHIP: Record<DomainLifecycle, { label: string; tone: string }> = {
  ok: { label: 'Active', tone: 'bg-success/10 text-success' },
  add_grace: { label: 'Add grace', tone: 'bg-info/10 text-info' },
  renew_grace: { label: 'Renewed', tone: 'bg-info/10 text-info' },
  redemption: { label: 'Redemption — act now', tone: 'bg-warning/10 text-warning' },
  pending_delete: { label: 'Pending delete', tone: 'bg-danger/10 text-danger' },
  expired: { label: 'Expired', tone: 'bg-danger/10 text-danger' },
};

function LifecycleChip({ lifecycle, registry }: { lifecycle: DomainLifecycle; registry: string }) {
  const chip = LIFECYCLE_CHIP[lifecycle] ?? LIFECYCLE_CHIP.ok;
  return (
    <span className={`inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded ${chip.tone}`}>
      {chip.label}
      {registry !== 'ok' && registry !== 'pending_create' && <span className="opacity-70">· {registry.replace('_', ' ')}</span>}
    </span>
  );
}

export function DomainsRoute() {
  usePageTitle('Domains');
  const [wizardOpen, setWizardOpen] = useState(false);
  const domainsQ = useQuery({ queryKey: ['domains'], queryFn: api.listDomains, staleTime: 15_000 });

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto space-y-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">Domains</h1>
          <p className="text-sm text-ink-soft">
            Registered through Oner (ZARC-accredited). Mail DNS can run on our authoritative nameservers or stay with yours.
          </p>
        </div>
        <Button variant="primary" onClick={() => setWizardOpen(true)}>Register a domain</Button>
      </div>

      {wizardOpen && <AddDomainWizard onClose={() => setWizardOpen(false)} />}

      {domainsQ.isLoading && <Skeleton className="h-32 w-full" />}
      {domainsQ.isError && <p className="text-sm text-danger">Could not load domains.</p>}
      {domainsQ.data && domainsQ.data.length === 0 && (
        <EmptyState
          icon="globe"
          title="No domains yet"
          description="Register a .co.za domain through the wizard — the zone is provisioned before delegation, so mail is ready the moment DNS switches."
          action={<Button variant="primary" onClick={() => setWizardOpen(true)}>Start the wizard</Button>}
        />
      )}

      <div className="space-y-2">
        {domainsQ.data?.map((d) => (
          <DomainRow key={d.id} id={d.id} name={d.name} status={d.status} />
        ))}
      </div>
    </div>
  );
}

// DomainRow fetches the registrar info per row (status chips come from the
// lifecycle, not the core status).
function DomainRow({ id, name, status }: { id: string; name: string; status: string }) {
  const detailQ = useQuery({
    queryKey: ['domain-detail', id],
    queryFn: () => api.domainDetail(id),
    staleTime: 30_000,
  });
  const reg = detailQ.data?.registrar;
  return (
    <Panel>
      <div>
        <Link to={`/settings/domains/${id}`} className="font-medium hover:underline">{name}</Link>
        <p className="text-xs text-ink-soft">
          {reg?.managed_mode === 'full' && 'Fully managed — our nameservers'}
          {reg?.managed_mode === 'partial' && 'Partially managed — your nameservers, our mail records'}
          {(!reg || reg.managed_mode === 'unmanaged') && 'External domain — bring your own DNS'}
          {reg?.renews_until && ` · renews until ${new Date(reg.renews_until).toLocaleDateString('en-ZA')}`}
        </p>
      </div>
      <div className="flex items-center gap-2">
        {reg && <LifecycleChip lifecycle={reg.lifecycle} registry={reg.registry_status} />}
        {!reg && <span className="text-xs px-2 py-0.5 rounded bg-canvas text-ink-soft">{status}</span>}
        <Link to={`/settings/domains/${id}`}><Button variant="ghost">Manage</Button></Link>
      </div>
    </Panel>
  );
}
