// The authenticated app shell: sidebar (folders + nav), topbar (search,
// refresh, account), org-status banners, and the polling heartbeat that
// keeps folder unread counts and the open folder fresh (no SSE channel in
// the API — polling is the contract, see web/README.md).

import { useCallback, useEffect, useState } from 'react'
import { NavLink, Outlet, useNavigate, useParams } from 'react-router-dom'
import { useAuth } from '@/stores/auth'
import { useMail } from '@/stores/mail'
import { useCompose, fetchStorage } from '@/stores/compose'
import { outbox } from '@/lib/outbox'
import { mailApi, sendApi } from '@/api/endpoints'
import { formatBytes } from '@/lib/format'
import type { Storage } from '@/types/api'
import { Button, Input, Spinner } from '@/ui/primitives'
import { useUI } from '@/stores/ui'

export default function AppShell() {
  const { me } = useAuth()
  const { folderId } = useParams()
  const { folders, foldersLoading, loadFolders, selectFolder, loadFolder } = useMail()
  const [storage, setStorage] = useState<Storage | null>(null)
  const [pendingSends, setPendingSends] = useState(outbox.pending())
  const [online, setOnline] = useState(navigator.onLine)

  // Boot: folders + storage, then keep them warm.
  useEffect(() => {
    void loadFolders()
    fetchStorage().then(setStorage).catch(() => undefined)
    const foldersTimer = setInterval(() => void loadFolders(), 30_000)
    const storageTimer = setInterval(() => {
      fetchStorage().then(setStorage).catch(() => undefined)
    }, 120_000)
    return () => {
      clearInterval(foldersTimer)
      clearInterval(storageTimer)
    }
  }, [loadFolders])

  // Current folder polling (list rows refresh under the cursor).
  useEffect(() => {
    if (!folderId) return
    selectFolder(folderId)
    void loadFolder(folderId, { reset: true })
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') void loadFolder(folderId, { reset: true })
    }, 25_000)
    return () => clearInterval(timer)
  }, [folderId, selectFolder, loadFolder])

  // Outbox: flush on reconnect, show pending count.
  useEffect(() => {
    const flush = () =>
      outbox
        .flush(async (input, key) => {
          await sendApi.send(input, key)
        })
        .then((n) => {
          if (n > 0) useUI.getState().toast('success', `${n} queued message${n > 1 ? 's' : ''} sent`)
        })
        .catch(() => undefined)
    const onOnline = () => {
      setOnline(true)
      void flush()
    }
    const onOffline = () => setOnline(false)
    const unsub = outbox.subscribe(() => setPendingSends(outbox.pending()))
    window.addEventListener('online', onOnline)
    window.addEventListener('offline', onOffline)
    if (online) void flush()
    return () => {
      window.removeEventListener('online', onOnline)
      window.removeEventListener('offline', onOffline)
      unsub()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const createFolder = useCallback(async () => {
    const mailbox = me?.mailboxes?.[0]
    if (!mailbox) return
    const name = window.prompt('New folder name')
    if (!name?.trim()) return
    try {
      await mailApi.createFolder(mailbox.id, name.trim())
      await loadFolders()
    } catch (err) {
      useUI.getState().toast('error', 'Could not create folder', (err as Error).message)
    }
  }, [me, loadFolders])

  if (!me) return null
  const isAdmin = me.user.role === 'owner' || me.user.role === 'admin'
  const suspended = me.org.status === 'suspended'

  return (
    <div className="flex h-screen bg-slate-100 text-slate-800">
      <Sidebar
        me={me}
        folders={folders}
        foldersLoading={foldersLoading}
        folderId={folderId}
        isAdmin={isAdmin}
        storage={storage}
        onNewFolder={createFolder}
      />

      <main className="flex min-w-0 flex-1 flex-col">
        {/* status banners */}
        {suspended && (
          <Banner tone="red">
            <strong>{me.org.name}</strong> is suspended — you can view your status and log out, but
            sending and admin actions are unavailable until an administrator resumes the organisation.
          </Banner>
        )}
        {me.impersonated && (
          <Banner tone="amber">
            Impersonation session — staff support is acting in your account. All mail reads are being
            audited.
          </Banner>
        )}
        {!online && (
          <Banner tone="amber">
            You are offline. Mail read from cache; sends queue in the outbox
            {pendingSends > 0 ? ` (${pendingSends} pending)` : ''} and flush automatically.
          </Banner>
        )}

        <Topbar />

        <div className="min-h-0 flex-1 overflow-hidden">
          <Outlet />
        </div>
      </main>

      <ComposeOverlayWrapper />
    </div>
  )
}

// ---- sidebar ----

function Sidebar({
  me,
  folders,
  foldersLoading,
  folderId,
  isAdmin,
  storage,
  onNewFolder,
}: {
  me: ReturnType<typeof useAuth.getState>['me']
  folders: ReturnType<typeof useMail.getState>['folders']
  foldersLoading: boolean
  folderId?: string
  isAdmin: boolean
  storage: Storage | null
  onNewFolder: () => void
}) {
  const navigate = useNavigate()
  const openCompose = useCompose((s) => s.openNew)
  if (!me) return null

  const usedPercent = storage ? Math.min(100, storage.used_percent) : 0

  return (
    <aside className="flex w-sidebar shrink-0 flex-col border-r border-slate-200 bg-white">
      <div className="flex items-center gap-2 border-b border-slate-200 px-4 py-3.5">
        <img src="/icon.svg" alt="" className="h-7 w-7" />
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold text-slate-900">{me.org.name}</div>
          <div className="text-[11px] text-slate-400">{me.org.tier} tier</div>
        </div>
      </div>

      <div className="p-3">
        <Button className="w-full" onClick={() => openCompose(me.mailboxes?.[0]?.name ?? undefined)}>
          ✏️ Compose
        </Button>
      </div>

      <nav className="min-h-0 flex-1 overflow-y-auto px-2 pb-3 text-sm">
        <div className="flex items-center justify-between px-2 pb-1 pt-2">
          <span className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Folders</span>
          <button
            className="text-slate-300 hover:text-brand-600"
            onClick={onNewFolder}
            title="New folder"
            aria-label="New folder"
          >
            ＋
          </button>
        </div>
        {foldersLoading && !folders.length && (
          <div className="flex items-center gap-2 px-3 py-2 text-xs text-slate-400">
            <Spinner className="h-3 w-3" /> folders…
          </div>
        )}
        {folders.map((f) => (
          <NavLink
            key={f.id}
            to={`/mail/${f.id}`}
            className={({ isActive }) =>
              `flex items-center justify-between rounded-md px-3 py-1.5 ${
                isActive || folderId === f.id
                  ? 'bg-brand-50 font-medium text-brand-700'
                  : 'text-slate-600 hover:bg-slate-50'
              }`
            }
          >
            <span className="truncate">{folderIcon(f.special_use)} {f.name}</span>
            {f.unread > 0 && (
              <span className="ml-2 rounded-full bg-brand-600 px-1.5 text-[10px] font-semibold text-white">
                {f.unread > 99 ? '99+' : f.unread}
              </span>
            )}
          </NavLink>
        ))}

        <SidebarSection label="Settings" />
        <SidebarLink to="/settings" label="Overview" />
        <SidebarLink to="/settings/aliases" label="Aliases & forwards" />
        <SidebarLink to="/settings/signatures" label="Signatures" />
        <SidebarLink to="/settings/rules" label="Rules" />
        <SidebarLink to="/settings/autoresponder" label="Autoresponder" />
        <SidebarLink to="/settings/security" label="Security & sessions" />

        {isAdmin && (
          <>
            <SidebarSection label="Organisation" />
            <SidebarLink to="/org" label="Overview" />
            <SidebarLink to="/org/domains" label="Domains & DKIM" />
            <SidebarLink to="/org/users" label="Users" />
            <SidebarLink to="/org/insights" label="Deliverability" />
            <SidebarLink to="/org/quarantine" label="Quarantine" />
            <SidebarLink to="/org/suppression" label="Suppression" />
            <SidebarLink to="/org/webhooks" label="Webhooks" />
            <SidebarLink to="/org/api-keys" label="API keys" />
          </>
        )}
      </nav>

      {storage && (
        <div className="border-t border-slate-200 px-4 py-3 text-xs text-slate-500">
          <div className="mb-1 flex justify-between">
            <span>Storage</span>
            <span>
              {formatBytes(storage.used_bytes)} / {formatBytes(storage.included_bytes)}
            </span>
          </div>
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-200">
            <div
              className={`h-full rounded-full ${usedPercent > 90 ? 'bg-red-500' : usedPercent > 75 ? 'bg-amber-500' : 'bg-brand-500'}`}
              style={{ width: `${Math.max(2, usedPercent)}%` }}
            />
          </div>
          {storage.full && (
            <button
              className="mt-2 font-medium text-red-600 hover:underline"
              onClick={() => navigate('/settings')}
            >
              Mailbox full — read-only mode
            </button>
          )}
          <div className="mt-1 text-[10px] text-slate-400">Mail is never auto-deleted</div>
        </div>
      )}
    </aside>
  )
}

function SidebarSection({ label }: { label: string }) {
  return (
    <div className="px-2 pb-1 pt-4 text-[11px] font-semibold uppercase tracking-wide text-slate-400">
      {label}
    </div>
  )
}

function SidebarLink({ to, label }: { to: string; label: string }) {
  return (
    <NavLink
      to={to}
      end={to === '/settings' || to === '/org'}
      className={({ isActive }) =>
        `block rounded-md px-3 py-1.5 ${isActive ? 'bg-slate-100 font-medium text-slate-800' : 'text-slate-600 hover:bg-slate-50'}`
      }
    >
      {label}
    </NavLink>
  )
}

function folderIcon(specialUse: string): string {
  const su = specialUse.replace(/\\/g, '').toLowerCase()
  if (su.includes('inbox')) return '📥'
  if (su.includes('sent')) return '📤'
  if (su.includes('draft')) return '📝'
  if (su.includes('trash')) return '🗑️'
  return '📁'
}

// ---- topbar ----

function Topbar() {
  const { me, logout } = useAuth()
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const openCompose = useCompose((s) => s.openNew)

  const submitSearch = (e: React.FormEvent) => {
    e.preventDefault()
    if (q.trim()) navigate(`/search?q=${encodeURIComponent(q.trim())}`)
  }

  return (
    <header className="flex items-center gap-3 border-b border-slate-200 bg-white px-4 py-2.5">
      <form onSubmit={submitSearch} className="relative min-w-0 flex-1 max-w-lg">
        <Input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search mail…"
          className="pl-8"
          aria-label="Search mail"
        />
        <span className="pointer-events-none absolute left-2.5 top-2.5 text-slate-400">🔍</span>
      </form>
      <div className="ml-auto flex items-center gap-2">
        <Button variant="ghost" size="sm" onClick={() => openCompose()}>
          New message
        </Button>
        {me && (
          <div className="flex items-center gap-2 border-l border-slate-200 pl-3">
            <div className="text-right">
              <div className="text-xs font-medium text-slate-700">{me.user.display_name || me.user.email}</div>
              <div className="text-[10px] uppercase tracking-wide text-slate-400">{me.user.role}</div>
            </div>
            <Button variant="secondary" size="sm" onClick={() => void logout()}>
              Sign out
            </Button>
          </div>
        )}
      </div>
    </header>
  )
}

// ---- shared bits ----

function Banner({ tone, children }: { tone: 'red' | 'amber'; children: React.ReactNode }) {
  return (
    <div
      className={`px-4 py-2 text-xs ${
        tone === 'red' ? 'bg-red-50 text-red-700' : 'bg-amber-50 text-amber-800'
      }`}
      role="alert"
    >
      {children}
    </div>
  )
}

// Kept separate so the overlay mounts inside the shell without dragging the
// editor bundle into the shell's first render.
import { ComposeOverlay } from '@/pages/mail/Compose'
function ComposeOverlayWrapper() {
  const open = useCompose((s) => s.open)
  if (!open) return null
  return (
    <div className="pointer-events-none fixed inset-0 z-30">
      <ComposeOverlay />
    </div>
  )
}
