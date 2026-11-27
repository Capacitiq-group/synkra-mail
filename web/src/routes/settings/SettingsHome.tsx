import { Link } from 'react-router-dom';
import { useAuth } from '@/stores/auth';
import { Icon } from '@/design/icons';
import { SettingsContent, SettingsHeader, SettingsPage } from './SettingsLayout';
import { Avatar } from '@/design';

const SECTIONS: { to: string; title: string; description: string; icon: Parameters<typeof Icon>[0]['name']; adminOnly?: boolean }[] = [
  { to: '/settings/security', title: 'Security', description: 'Password, two-factor authentication, passkeys, active sessions and app passwords.', icon: 'shield-check' },
  { to: '/settings/aliases', title: 'Aliases', description: 'Disposable and masked addresses that forward to your mailbox.', icon: 'label' },
  { to: '/settings/signatures', title: 'Signatures', description: 'Personal signatures with rich text, HTML import and per-reply overrides.', icon: 'sparkles' },
  { to: '/settings/privacy', title: 'Privacy', description: 'What we process, sub-processors, and data export or deletion requests.', icon: 'lock' },
  { to: '/org', title: 'Organisation', description: 'Users, mailboxes, departments, shared mailboxes and billing.', icon: 'inbox', adminOnly: true },
];

export function SettingsHome() {
  const me = useAuth((s) => s.me);
  if (!me) return null;
  const isAdmin = ['owner', 'admin'].includes(me.role);
  const visible = SECTIONS.filter((s) => !s.adminOnly || isAdmin);

  return (
    <SettingsPage>
      <SettingsHeader title="Settings" description="Manage your account, security and privacy." />
      <SettingsContent>
        <div className="flex items-center gap-4 p-4 mb-6 border border-line rounded-lg bg-canvas">
          <Avatar name={me.display_name} email={me.email} size={48} />
          <div className="min-w-0">
            <p className="font-medium truncate">{me.display_name}</p>
            <p className="text-sm text-ink-soft truncate">{me.email}</p>
            {me.org_name && <p className="text-xs text-ink-mute mt-0.5">{me.org_name} · {me.role}</p>}
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          {visible.map((s) => (
            <Link
              key={s.to}
              to={s.to}
              className="group block p-4 border border-line rounded-lg hover:border-accent transition-colors"
            >
              <div className="flex items-start gap-3">
                <span className="w-9 h-9 rounded bg-accent-soft text-accent flex items-center justify-center shrink-0">
                  <Icon name={s.icon} size={16} />
                </span>
                <div className="min-w-0">
                  <p className="font-medium group-hover:text-accent">{s.title}</p>
                  <p className="text-sm text-ink-soft mt-0.5">{s.description}</p>
                </div>
              </div>
            </Link>
          ))}
        </div>
      </SettingsContent>
    </SettingsPage>
  );
}
