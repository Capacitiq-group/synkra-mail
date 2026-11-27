import { Link, NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useEffect } from 'react';
import clsx from 'clsx';
import { useAuth } from '@/stores/auth';
import { useLayout } from '@/stores/layout';
import { useTheme } from '@/stores/theme';
import { useMediaQuery, usePageTitle } from '@/hooks';
import { Avatar, Button, Drawer, Menu, MenuItem } from './primitives';
import { api } from '@/lib/api';
import { toast } from '@/stores/toast';
import { useQueryClient } from '@tanstack/react-query';
import { useSSE } from '@/hooks/useSSE';
import { useOnlineStatus } from '@/hooks';

interface NavEntry { to: string; label: string; icon: string; roles?: string[] }

const PRIMARY_NAV: NavEntry[] = [
  { to: '/mail', label: 'Mail', icon: 'M3 6l9 6 9-6M3 6h18v12H3z' },
  { to: '/shared', label: 'Shared', icon: 'M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z' },
  { to: '/contacts', label: 'Contacts', icon: 'M16 14a4 4 0 10-8 0M12 10a4 4 0 100-8 4 4 0 000 8z' },
  { to: '/calendar', label: 'Calendar', icon: 'M4 5h16v15H4zM4 9h16M8 3v4M16 3v4' },
  { to: '/tasks', label: 'Tasks', icon: 'M4 6h16M4 12h16M4 18h10' },
];

const SECONDARY_NAV: NavEntry[] = [
  { to: '/settings/security', label: 'Security', icon: 'M12 3l8 3v6c0 4-3 7-8 9-5-2-8-5-8-9V6l8-3z' },
  { to: '/settings/aliases', label: 'Aliases', icon: 'M3 6h18M3 12h18M3 18h18' },
  { to: '/settings/signatures', label: 'Signatures', icon: 'M4 20h16M6 16l8-8 4 4-8 8H6z' },
  { to: '/settings/rules', label: 'Rules', icon: 'M4 6h16M4 12h16M4 18h16' },
  { to: '/settings/ooo', label: 'Out of office', icon: 'M12 7v5l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z' },
  { to: '/settings/newsletters', label: 'Newsletters', icon: 'M4 6h16v12H4zM4 8l8 6 8-6' },
  { to: '/settings/domains', label: 'Domains', icon: 'M4 6h16v12H4zM4 10h16' },
  { to: '/settings/deliverability', label: 'Deliverability', icon: 'M4 6l8 6 8-6M4 18V6h16v12H4z' },
  { to: '/settings/privacy', label: 'Privacy', icon: 'M12 3l8 3v6c0 4-3 7-8 9-5-2-8-5-8-9V6l8-3z' },
];

const ADMIN_NAV: NavEntry[] = [
  { to: '/settings/billing', label: 'Billing', icon: 'M4 6h16v12H4zM4 10h16' },
  { to: '/org', label: 'Organisation', icon: 'M4 20V8l8-4 8 4v12M9 20v-6h6v6', roles: ['owner', 'admin'] },
  { to: '/admin', label: 'Platform admin', icon: 'M4 20V8l8-4 8 4v12M12 8v12', roles: ['staff'] },
];

function Icon({ path, className }: { path: string; className?: string }) {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" className={className} aria-hidden>
      <path d={path} />
    </svg>
  );
}

export function AppShell() {
  const { me } = useAuth();
  const navigate = useNavigate();
  const online = useOnlineStatus();
  const isDesktop = useMediaQuery('(min-width: 768px)');
  const { sidebarCollapsed, mobileNavOpen, setMobileNavOpen } = useLayout();
  const qc = useQueryClient();
  usePageTitle('');
  useSSE(!!me);

  async function logout() {
    try { await api.logout(); } catch { /* even on failure, clear client state */ }
    qc.clear();
    useAuth.getState().setMe(null);
    toast('info', 'Signed out');
    navigate('/login', { replace: true });
  }

  if (!isDesktop && mobileNavOpen) {
    return (
      <Drawer open={mobileNavOpen} onClose={() => setMobileNavOpen(false)} title="Menu">
        <SidebarContent onNavigate={() => setMobileNavOpen(false)} />
      </Drawer>
    );
  }

  return (
    <div className="h-full flex bg-canvas">
      {isDesktop && (
        <aside
          className={clsx(
            'shrink-0 border-r border-line bg-surface flex flex-col transition-[width]',
            sidebarCollapsed ? 'w-16' : 'w-60',
          )}
        >
          <SidebarContent collapsed={sidebarCollapsed} />
        </aside>
      )}
      <div className="flex-1 min-w-0 flex flex-col">
        <Topbar onLogout={logout} onOpenMobileNav={() => setMobileNavOpen(true)} />
        {!online && (
          <div className="px-3 py-1.5 text-xs bg-warning-soft text-warning border-b border-line" role="status">
            You are offline. Changes will sync when you reconnect.
          </div>
        )}
        <main className="flex-1 min-h-0 overflow-hidden"><Outlet /></main>
      </div>
    </div>
  );
}

function SidebarContent({ collapsed, onNavigate }: { collapsed?: boolean; onNavigate?: () => void }) {
  const me = useAuth((s) => s.me);
  const navigate = useNavigate();

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-2 px-3 py-3 border-b border-line">
        <div className="w-8 h-8 rounded bg-accent text-white flex items-center justify-center font-bold">S</div>
        {!collapsed && <span className="font-semibold">Synkra Mail</span>}
      </div>
      <div className="p-2">
        <Button variant="primary" size="md" className="w-full justify-start" onClick={() => { navigate('/compose'); onNavigate?.(); }}>
          <span className="text-lg leading-none">+</span>{!collapsed && ' Compose'}
        </Button>
      </div>
      <nav className="flex-1 overflow-y-auto scroll-thin px-2 py-2 space-y-0.5">
        {PRIMARY_NAV.map((n) => <SidebarLink key={n.to} entry={n} collapsed={collapsed} onNavigate={onNavigate} />)}
        {!collapsed && <h3 className="px-2 pt-4 pb-1 text-xs uppercase text-ink-mute">Settings</h3>}
        {SECONDARY_NAV.map((n) => <SidebarLink key={n.to} entry={n} collapsed={collapsed} onNavigate={onNavigate} />)}
        {me && (ADMIN_NAV.filter((n) => !n.roles || n.roles.includes(me.role)).length > 0) && (
          <>
            {!collapsed && <h3 className="px-2 pt-4 pb-1 text-xs uppercase text-ink-mute">Admin</h3>}
            {ADMIN_NAV.filter((n) => !n.roles || n.roles.includes(me.role)).map((n) => (
              <SidebarLink key={n.to} entry={n} collapsed={collapsed} onNavigate={onNavigate} />
            ))}
          </>
        )}
      </nav>
    </div>
  );
}

function SidebarLink({ entry, collapsed, onNavigate }: { entry: NavEntry; collapsed?: boolean; onNavigate?: () => void }) {
  return (
    <NavLink
      to={entry.to}
      onClick={onNavigate}
      end={entry.to === '/mail'}
      className={({ isActive }) =>
        clsx(
          'flex items-center gap-2 px-2 py-1.5 rounded text-sm',
          isActive ? 'bg-canvas text-ink font-medium' : 'text-ink-soft hover:bg-canvas hover:text-ink',
        )
      }
    >
      <Icon path={entry.icon} />
      {!collapsed && <span className="truncate">{entry.label}</span>}
    </NavLink>
  );
}

function Topbar({ onLogout, onOpenMobileNav }: { onLogout: () => void; onOpenMobileNav: () => void }) {
  const me = useAuth((s) => s.me);
  const { mode, setMode, apply } = useTheme();
  const toggleSidebar = useLayout((s) => s.toggleSidebar);
  const isDesktop = useMediaQuery('(min-width: 768px)');

  useEffect(() => { apply(); }, [apply]);

  return (
    <header className="flex items-center gap-3 px-3 py-2 border-b border-line bg-surface">
      {!isDesktop && (
        <button onClick={onOpenMobileNav} className="p-1.5 rounded hover:bg-canvas" aria-label="Open menu">
          <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden><path d="M3 6h14M3 10h14M3 14h14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>
        </button>
      )}
      {isDesktop && (
        <button onClick={toggleSidebar} className="p-1.5 rounded hover:bg-canvas" aria-label="Toggle sidebar">
          <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden><path d="M3 5h14M3 10h14M3 15h14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>
        </button>
      )}
      <Link to="/" className="font-semibold md:hidden">Synkra</Link>
      <div className="flex-1" />
      <button
        onClick={() => setMode(mode === 'dark' ? 'light' : mode === 'light' ? 'system' : 'dark')}
        className="p-1.5 rounded hover:bg-canvas"
        aria-label={`Theme: ${mode}. Click to change.`}
        title={`Theme: ${mode}`}
      >
        {mode === 'dark' ? '🌙' : mode === 'light' ? '☀️' : '🖥️'}
      </button>
      {me && (
        <Menu
          trigger={
            <button className="flex items-center gap-2 p-1 rounded hover:bg-canvas" aria-label="Account menu">
              <Avatar name={me.display_name} email={me.email} size={28} />
              <span className="hidden md:inline text-sm">{me.display_name}</span>
            </button>
          }
        >
          <MenuItem onClick={() => { window.location.href = '/settings'; }}>Account settings</MenuItem>
          <MenuItem onClick={() => { window.location.href = '/settings/security'; }}>Security</MenuItem>
          <MenuItem onClick={onLogout} danger>Sign out</MenuItem>
        </Menu>
      )}
    </header>
  );
}
