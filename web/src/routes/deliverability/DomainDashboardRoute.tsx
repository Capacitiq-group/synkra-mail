import { useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useDeliverabilitySummary, useDeliverabilityTimeseries } from '@/hooks/queries/deliverability';
import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { Button, Skeleton, Tabs } from '@/design';
import { Icon } from '@/design/icons';
import { TimeSeriesChart } from '@/components/TimeSeriesChart';
import { usePageTitle } from '@/hooks';
import { formatNumber, formatPercent } from '@/lib/format';
import { SettingsHeader, SettingsPage } from '@/routes/settings/SettingsLayout';
import { AuthPanel } from './AuthPanel';
import { BlacklistPanel } from './BlacklistPanel';
import { DmarcRolloutCard } from './DmarcRolloutCard';
import { MtaStsCard } from './MtaStsCard';
import { TlsRptCard } from './TlsRptCard';
import { BimiCard } from './BimiCard';
import { SendingLimitCard } from './SendingLimitCard';
import { DmarcReportsPanel } from './DmarcReportsPanel';

type Period = 7 | 30 | 90;

export function DomainDashboardRoute() {
  const { domain = '' } = useParams<{ domain: string }>();
  const navigate = useNavigate();
  const [period, setPeriod] = useState<Period>(30);
  const [tab, setTab] = useState<'overview' | 'hardening' | 'dmarc-reports'>('overview');
  usePageTitle(`Deliverability · ${domain}`);

  const summaryQ = useDeliverabilitySummary(domain, period);
  const timeseriesQ = useDeliverabilityTimeseries(domain, period);
  const domainsQ = useQuery({ queryKey: ['domains'], queryFn: api.listDomains, staleTime: 60_000 });

  if (summaryQ.isLoading) {
    return <div className="p-6 space-y-3"><Skeleton className="h-32 w-full" /><Skeleton className="h-64 w-full" /></div>;
  }
  if (summaryQ.isError || !summaryQ.data) {
    return (
      <div className="p-6">
        <p className="text-sm text-danger">Could not load deliverability for {domain}.</p>
        <Button className="mt-3" onClick={() => navigate('/settings/deliverability')}>Back</Button>
      </div>
    );
  }

  const d = summaryQ.data;

  return (
    <SettingsPage maxWidth="xl">
      <SettingsHeader
        title={domain}
        description="Deliverability, authentication and reputation."
        back={{ to: '/settings/deliverability', label: 'All domains' }}
      />
      <div className="px-4 md:px-8 py-3 border-b border-line flex items-center gap-2 flex-wrap">
        <Tabs
          items={[
            { id: 'overview', label: 'Overview' },
            { id: 'hardening', label: 'Authentication hardening' },
            { id: 'dmarc-reports', label: 'DMARC reports' },
          ]}
          value={tab}
          onChange={(id) => setTab(id as typeof tab)}
        />
        <div className="ml-auto flex items-center border border-line rounded">
          {([7, 30, 90] as const).map((p) => (
            <button
              key={p}
              onClick={() => setPeriod(p)}
              className={`px-3 py-1 text-sm ${period === p ? 'bg-accent text-white' : 'hover:bg-canvas'}`}
            >
              {p}d
            </button>
          ))}
        </div>
      </div>

      <div className="p-4 md:p-8 space-y-6">
        {tab === 'overview' && (
          <>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
              <BigStat label="Sent" value={formatNumber(d.sent)} />
              <BigStat label="Delivered" value={formatNumber(d.delivered)} sub={formatPercent(d.success_rate / 100)} tone="success" />
              <BigStat label="Bounced" value={formatNumber(d.bounced)} sub={formatPercent(d.bounce_rate / 100)} tone={d.bounce_rate > 5 ? 'danger' : 'neutral'} />
              <BigStat label="Complained" value={formatNumber(d.complained)} sub={formatPercent(d.complaint_rate / 100)} tone={d.complaint_rate > 0.1 ? 'danger' : 'neutral'} />
            </div>

            <SendingLimitCard limit={d.sending_limit} />

            <section className="border border-line rounded-lg bg-surface p-4">
              <h2 className="font-medium mb-3">Delivery timeline</h2>
              {timeseriesQ.data && (
                <TimeSeriesChart
                  series={[
                    { label: 'Sent', color: 'rgb(var(--accent))', points: timeseriesQ.data.buckets.map((b) => ({ date: b.date, value: b.sent })) },
                    { label: 'Delivered', color: 'rgb(var(--success))', points: timeseriesQ.data.buckets.map((b) => ({ date: b.date, value: b.delivered })) },
                    { label: 'Bounced', color: 'rgb(var(--danger))', points: timeseriesQ.data.buckets.map((b) => ({ date: b.date, value: b.bounced })) },
                  ]}
                />
              )}
              {timeseriesQ.isLoading && <Skeleton className="h-60 w-full" />}
            </section>

            <section>
              <h2 className="font-medium mb-3">Authentication</h2>
              <AuthPanel domain={domain} auth={d.auth} />
            </section>

            <section>
              <h2 className="font-medium mb-3">Blacklists</h2>
              <BlacklistPanel blacklists={d.blacklists} />
            </section>
          </>
        )}

        {tab === 'hardening' && (
          <>
            <DmarcRolloutCard domain={domain} currentPhase={d.auth.dmarc ? 'reject' : 'none'} />
            <BimiCard domain={domain} />
            <MtaStsCard domain={domain} />
            <TlsRptCard domain={domain} />
          </>
        )}

        {tab === 'dmarc-reports' && (
          <DmarcReportsPanel domain={domain} />
        )}
      </div>

      {/* Suppress unused warnings */}
      {domainsQ.data && null}
    </SettingsPage>
  );
}

function BigStat({
  label, value, sub, tone = 'neutral',
}: { label: string; value: string; sub?: string; tone?: 'neutral' | 'success' | 'danger' }) {
  const toneCls = tone === 'success' ? 'text-success' : tone === 'danger' ? 'text-danger' : '';
  return (
    <div className="border border-line rounded-lg p-3 bg-surface">
      <p className="text-xs text-ink-mute">{label}</p>
      <p className={`text-xl font-semibold ${toneCls}`}>{value}</p>
      {sub && <p className="text-xs text-ink-soft mt-0.5">{sub}</p>}
    </div>
  );
}

// Suppress unused import warning.
void Icon;
