import { useSearchParams } from 'react-router-dom';
import { Tabs } from '@/design';
import { SettingsContent, SettingsHeader, SettingsPage } from '../SettingsLayout';
import { PrivacyPreferences } from './PrivacyPreferences';
import { ProcessingRegister } from './ProcessingRegister';
import { SubProcessors } from './SubProcessors';
import { DataRequests } from './DataRequests';

const TABS = [
  { id: 'preferences', label: 'Preferences' },
  { id: 'register', label: 'Processing register' },
  { id: 'sub-processors', label: 'Sub-processors' },
  { id: 'requests', label: 'Data requests' },
];

export function PrivacyRoute() {
  const [params, setParams] = useSearchParams();
  const tab = params.get('tab') ?? 'preferences';

  return (
    <SettingsPage maxWidth="xl">
      <SettingsHeader
        title="Privacy"
        description="What we process, why, and who else is involved. Manage your preferences and exercise your POPIA rights."
        back={{ to: '/settings', label: 'Settings' }}
      />
      <div className="px-4 md:px-8 border-b border-line">
        <Tabs items={TABS} value={tab} onChange={(id) => {
          const next = new URLSearchParams(params);
          next.set('tab', id);
          setParams(next, { replace: true });
        }} />
      </div>
      <SettingsContent>
        {tab === 'preferences' && <PrivacyPreferences />}
        {tab === 'register' && <ProcessingRegister />}
        {tab === 'sub-processors' && <SubProcessors />}
        {tab === 'requests' && <DataRequests />}
      </SettingsContent>
    </SettingsPage>
  );
}
