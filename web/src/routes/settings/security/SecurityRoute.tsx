import { useSearchParams } from 'react-router-dom';
import { useAuth } from '@/stores/auth';
import { Tabs } from '@/design';
import { SettingsContent, SettingsHeader, SettingsPage } from '../SettingsLayout';
import { PasswordTab } from './PasswordTab';
import { TotpTab } from './TotpTab';
import { PasskeysTab } from './PasskeysTab';
import { SessionsTab } from './SessionsTab';
import { DevicesTab } from './DevicesTab';
import { AppPasswordsTab } from './AppPasswordsTab';
import { IpRestrictionsTab } from './IpRestrictionsTab';
import { SsoTab } from './SsoTab';

const BASE_TABS = [
  { id: 'password', label: 'Password' },
  { id: 'totp', label: 'Two-factor' },
  { id: 'passkeys', label: 'Passkeys' },
  { id: 'sessions', label: 'Sessions' },
  { id: 'devices', label: 'Devices' },
  { id: 'app-passwords', label: 'App passwords' },
];

export function SecurityRoute() {
  const [params, setParams] = useSearchParams();
  const me = useAuth((s) => s.me);
  const isAdmin = !!me && ['owner', 'admin'].includes(me.role);

  const tabs = [...BASE_TABS];
  if (isAdmin) {
    tabs.push({ id: 'ip', label: 'IP restrictions' });
    tabs.push({ id: 'sso', label: 'SSO' });
  }

  const active = params.get('tab') ?? 'password';

  return (
    <SettingsPage maxWidth="xl">
      <SettingsHeader
        title="Security"
        description="Protect your account and control where it can be used."
        back={{ to: '/settings', label: 'Settings' }}
      />
      <div className="px-4 md:px-8 border-b border-line overflow-x-auto">
        <Tabs
          items={tabs}
          value={active}
          onChange={(id) => {
            const next = new URLSearchParams(params);
            next.set('tab', id);
            setParams(next, { replace: true });
          }}
        />
      </div>
      <SettingsContent>
        {active === 'password' && <PasswordTab />}
        {active === 'totp' && <TotpTab />}
        {active === 'passkeys' && <PasskeysTab />}
        {active === 'sessions' && <SessionsTab />}
        {active === 'devices' && <DevicesTab />}
        {active === 'app-passwords' && <AppPasswordsTab />}
        {active === 'ip' && isAdmin && <IpRestrictionsTab />}
        {active === 'sso' && isAdmin && <SsoTab />}
      </SettingsContent>
    </SettingsPage>
  );
}
