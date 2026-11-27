import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { useDeliverabilitySummary } from '@/hooks/queries/deliverability';
import { Badge, Button, EmptyState, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { formatNumber, formatPercent } from '@/lib/format';

export function DomainsOverviewTab() {
  const domainsQ = useQuery({ queryKey: ['domains'], queryFn: api.listDomains, staleTime: 60_000 });

  if (domainsQ.isLoading) {
    return <div className="space-y-3">{Array.from({ length: 2 }).map((_, i) => <Skeleton key={i} className="h-40 w-full rounded-lg" />)}</div>;
  }
  if (domainsQ.isError) {
    return <p className="text-sm text-danger">Could not load domains.</p>;
  }
  if (!domainsQ.data || domainsQ.data.length === 0) {
    return (
      <EmptyState
        icon={<Icon name="inbox" size={28} />}
        title="No domains yet"
        description="Add a domain to start seeing deliverability for it."
        action={<Link to="/settings/domains"><Button variant="primary">Add a domain</Button></Link>}
      />
    );
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
      {domainsQ.data.map((d) => (
        <DomainCard key={d.id} domain={d.name} verified={d.status === 'verified'} />
      ))}
    </div>
  );
}

function DomainCard({ domain, verified }: { domain: string; verified: boolean }) {
  const q = useDeliverabilitySummary(domain, 30);

  if (q.isLoading) {
    return (
      <div className="border border-line rounded-lg p-4">
        <Skeleton className="h-5 w-1/2 mb-3" />
        <Skeleton className="h-20 w-full" />
      </div>
    );
  }
  if (q.isError || !q.data) {
    return (
      <div className="border border-line rounded-lg p-4">
        <p className="font-medium truncate">{domain}</p>
        <p className="text-xs text-danger mt-1">Could not load deliverability.</p>
      </div>
    );
  }

  const d = q.data;
  const reputationTone =
    d.reputation === 'excellent' ? 'success' :
    d.reputation === 'good' ? 'accent' :
    d.reputation === 'fair' ? 'warning' :
    'danger';

  return (
    <Link
      to={`/settings/deliverability/${encodeURIComponent(domain)}`}
      className="block border border-line rounded-lg p-4 hover:border-accent transition-colors bg-surface"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="font-medium truncate">{domain}</p>
          <p className="text-xs text-ink-soft mt-0.5">{verified ? 'Verified' : 'Pending verification'} · last 30 days</p>
        </div>
        <Badge tone={reputationTone as 'success' | 'accent' | 'warning' | 'danger'}>{d.reputation}</Badge>
      </div>

      <div className="mt-3 grid grid-cols-3 gap-2">
        <Stat label="Delivered" value={formatNumber(d.delivered)} sub={formatPercent(d.success_rate / 100)} />
        <Stat label="Bounced" value={formatNumber(d.bounced)} sub={formatPercent(d.bounce_rate / 100)} />
        <Stat label="Deferred" value={formatNumber(d.deferred)} sub="" />
      </div>

      <div className="mt-3 flex flex-wrap gap-1">
        <AuthChip label="SPF" ok={d.auth.spf} />
        <AuthChip label="DKIM" ok={d.auth.dkim} />
        <AuthChip label="DMARC" ok={d.auth.dmarc} />
        <AuthChip label="MTA-STS" ok={d.auth.mta_sts} />
        <AuthChip label="BIMI" ok={d.auth.bimi} />
      </div>
    </Link>
  );
}

function Stat({ label, value, sub }: { label: string; value: string; sub: string }) {
  return (
    <div>
      <p className="text-xs text-ink-mute">{label}</p>
      <p className="text-sm font-medium truncate">{value}</p>
      {sub && <p className="text-xs text-ink-mute">{sub}</p>}
    </div>
  );
}

function AuthChip({ label, ok }: { label: string; ok: boolean }) {
  return (
    <span className={`inline-flex items-center gap-0.5 text-[10px] px-1.5 py-0.5 rounded ${ok ? 'bg-success-soft text-success' : 'bg-canvas border border-line text-ink-mute'}`}>
      <Icon name={ok ? 'check' : 'minus'} size={8} />
      {label}
    </span>
  );
}
