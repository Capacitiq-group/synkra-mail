import { useSearchParams } from 'react-router-dom';
import { Tabs } from '@/design';
import { SettingsContent, SettingsHeader, SettingsPage } from '@/routes/settings/SettingsLayout';
import { usePageTitle } from '@/hooks';
import { DomainsOverviewTab } from './DomainsOverviewTab';
import { HealthTab } from './HealthTab';
import { ReliabilityTab } from './ReliabilityTab';

const TABS = [
  { id: 'domains', label: 'Domains' },
  { id: 'health', label: 'Health score' },
  { id: 'reliability', label: 'Reliability' },
];

export function DeliverabilityHubRoute() {
  usePageTitle('Deliverability');
  const [params, setParams] = useSearchParams();
  const tab = params.get('tab') ?? 'domains';

  return (
    <SettingsPage maxWidth="xl">
      <SettingsHeader
        title="Deliverability"
        description="Authentication, reputation, delivery results and infrastructure health for every domain you send from."
        back={{ to: '/settings', label: 'Settings' }}
      />
      <div className="px-4 md:px-8 border-b border-line">
        <Tabs
          items={TABS}
          value={tab}
          onChange={(id) => {
            const next = new URLSearchParams(params);
            next.set('tab', id);
            setParams(next, { replace: true });
          }}
        />
      </div>
      <SettingsContent>
        {tab === 'domains' && <DomainsOverviewTab />}
        {tab === 'health' && <HealthTab />}
        {tab === 'reliability' && <ReliabilityTab />}
      </SettingsContent>
    </SettingsPage>
  );
}
