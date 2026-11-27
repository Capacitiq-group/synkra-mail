import { useEffect } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { useAuth } from '@/stores/auth'
import AppShell from '@/shell/AppShell'
import Login from '@/pages/Login'
import VerifyEmail from '@/pages/VerifyEmail'
import Mailbox from '@/pages/mail/Mailbox'
import SearchResults from '@/pages/mail/SearchResults'
import SettingsHome from '@/pages/settings/SettingsHome'
import Aliases from '@/pages/settings/Aliases'
import Signatures from '@/pages/settings/Signatures'
import Security from '@/pages/settings/Security'
import Rules from '@/pages/settings/Rules'
import Autoresponder from '@/pages/settings/Autoresponder'
import OrgHome from '@/pages/org/OrgHome'
import Domains from '@/pages/org/Domains'
import OrgUsers from '@/pages/org/OrgUsers'
import Insights from '@/pages/org/Insights'
import Quarantine from '@/pages/org/Quarantine'
import Suppression from '@/pages/org/Suppression'
import Webhooks from '@/pages/org/Webhooks'
import ApiKeys from '@/pages/org/ApiKeys'
import type { ReactNode } from 'react'

function RequireAuth({ children }: { children: ReactNode }) {
  const status = useAuth((s) => s.status)
  if (status === 'loading')
    return (
      <div className="flex h-screen items-center justify-center text-sm text-slate-400">
        Signing in…
      </div>
    )
  if (status === 'anon') return <Navigate to="/login" replace />
  return <>{children}</>
}

function RequireOrgAdmin({ children }: { children: ReactNode }) {
  const me = useAuth((s) => s.me)
  if (me && me.user.role !== 'owner' && me.user.role !== 'admin')
    return <Navigate to="/settings" replace />
  return <>{children}</>
}

export default function App() {
  const init = useAuth((s) => s.init)
  useEffect(() => {
    void init()
  }, [init])

  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/verify" element={<VerifyEmail />} />

      <Route
        element={
          <RequireAuth>
            <AppShell />
          </RequireAuth>
        }
      >
        <Route path="/" element={<Navigate to="/mail" replace />} />
        <Route path="/mail" element={<Navigate to="/mail/inbox" replace />} />
        <Route path="/mail/:folderId" element={<Mailbox />} />
        <Route path="/mail/:folderId/:messageId" element={<Mailbox />} />
        <Route path="/search" element={<SearchResults />} />

        <Route path="/settings" element={<SettingsHome />} />
        <Route path="/settings/aliases" element={<Aliases />} />
        <Route path="/settings/signatures" element={<Signatures />} />
        <Route path="/settings/security" element={<Security />} />
        <Route path="/settings/rules" element={<Rules />} />
        <Route path="/settings/autoresponder" element={<Autoresponder />} />

        <Route
          path="/org"
          element={
            <RequireOrgAdmin>
              <OrgHome />
            </RequireOrgAdmin>
          }
        />
        <Route
          path="/org/domains"
          element={
            <RequireOrgAdmin>
              <Domains />
            </RequireOrgAdmin>
          }
        />
        <Route
          path="/org/users"
          element={
            <RequireOrgAdmin>
              <OrgUsers />
            </RequireOrgAdmin>
          }
        />
        <Route
          path="/org/insights"
          element={
            <RequireOrgAdmin>
              <Insights />
            </RequireOrgAdmin>
          }
        />
        <Route
          path="/org/quarantine"
          element={
            <RequireOrgAdmin>
              <Quarantine />
            </RequireOrgAdmin>
          }
        />
        <Route
          path="/org/suppression"
          element={
            <RequireOrgAdmin>
              <Suppression />
            </RequireOrgAdmin>
          }
        />
        <Route
          path="/org/webhooks"
          element={
            <RequireOrgAdmin>
              <Webhooks />
            </RequireOrgAdmin>
          }
        />
        <Route
          path="/org/api-keys"
          element={
            <RequireOrgAdmin>
              <ApiKeys />
            </RequireOrgAdmin>
          }
        />
      </Route>

      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
