import { Navigate, Route, Routes } from 'react-router-dom';
import { useAuth } from '@/stores/auth';
import { SkipLink } from '@/components/SkipLink';
import { Splash } from '@/components/system';
import { AppShell } from '@/design/layout';
import { HomeRoute } from '@/routes/Home';
import {
  ForgotPasswordRoute, LoginRoute, LogoutRoute, NotFoundRoute, ResetPasswordRoute,
} from '@/routes/auth';
import { MailRoute } from '@/routes/mail/MailRoute';
import { ComposeRoute } from '@/routes/compose/ComposeRoute';
import { SettingsHome } from '@/routes/settings/SettingsHome';
import { SecurityRoute } from '@/routes/settings/security/SecurityRoute';
import { AliasesRoute } from '@/routes/settings/aliases/AliasesRoute';
import { SignaturesRoute } from '@/routes/settings/signatures/SignaturesRoute';
import { SignatureEditorPage } from '@/routes/settings/signatures/SignatureEditorPage';
import { PrivacyRoute } from '@/routes/settings/privacy/PrivacyRoute';
import { RulesRoute } from '@/routes/settings/rules/RulesRoute';
import { RuleBuilder } from '@/routes/settings/rules/RuleBuilder';
import { OooRoute } from '@/routes/settings/ooo/OooRoute';
import { NewslettersRoute } from '@/routes/settings/newsletters/NewslettersRoute';
import { ContactsRoute } from '@/routes/contacts/ContactsRoute';
import { ContactEditPage } from '@/routes/contacts/ContactEditPage';
import { CalendarRoute } from '@/routes/calendar/CalendarRoute';
import { SchedulingLinksRoute } from '@/routes/calendar/SchedulingLinksRoute';
import { TasksRoute } from '@/routes/tasks/TasksRoute';
import { SharedMailboxListRoute } from '@/routes/shared/SharedMailboxListRoute';
import { SharedMailboxShellRoute } from '@/routes/shared/SharedMailboxShellRoute';
import { SharedActivityRoute } from '@/routes/shared/SharedActivityRoute';
import { DeliverabilityHubRoute } from '@/routes/deliverability/HubRoute';
import { DomainDashboardRoute } from '@/routes/deliverability/DomainDashboardRoute';
import { MailTraceRoute } from '@/routes/deliverability/MailTraceRoute';

function RequireAuth({ children }: { children: React.ReactNode }) {
  const status = useAuth((s) => s.status);
  const me = useAuth((s) => s.me);
  if (status === 'loading' || status === 'idle') return <Splash />;
  if (!me) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

// Final router: part 1 base with every delivery part's fragments merged
// (parts 2-9 — mail, compose, settings, automation, contacts/calendar/tasks,
// shared mailboxes, deliverability).
export default function App() {
  return (
    <>
      <SkipLink />
      <Routes>
        <Route path="/login" element={<LoginRoute />} />
        <Route path="/logout" element={<LogoutRoute />} />
        <Route path="/forgot-password" element={<ForgotPasswordRoute />} />
        <Route path="/reset-password" element={<ResetPasswordRoute />} />

        <Route element={<RequireAuth><AppShell /></RequireAuth>}>
          <Route index element={<HomeRoute />} />
          {/* Mail core (part 2) */}
          <Route path="mail" element={<MailRoute />} />
          <Route path="mail/:mailboxId" element={<MailRoute />} />
          <Route path="mail/:mailboxId/:folderId" element={<MailRoute />} />
          <Route path="mail/:mailboxId/:folderId/:messageId" element={<MailRoute />} />
          {/* Composer (part 3) */}
          <Route path="compose" element={<ComposeRoute />} />
          {/* Settings (parts 4-6) */}
          <Route path="settings" element={<SettingsHome />} />
          <Route path="settings/security" element={<SecurityRoute />} />
          <Route path="settings/aliases" element={<AliasesRoute />} />
          <Route path="settings/signatures" element={<SignaturesRoute />} />
          <Route path="settings/signatures/:id" element={<SignatureEditorPage />} />
          <Route path="settings/privacy" element={<PrivacyRoute />} />
          <Route path="settings/rules" element={<RulesRoute />} />
          <Route path="settings/rules/:id" element={<RuleBuilder />} />
          <Route path="settings/ooo" element={<OooRoute />} />
          <Route path="settings/newsletters" element={<NewslettersRoute />} />
          {/* Contacts / calendar / tasks (part 7) */}
          <Route path="contacts" element={<ContactsRoute />} />
          <Route path="contacts/:id" element={<ContactEditPage />} />
          <Route path="calendar" element={<CalendarRoute />} />
          <Route path="calendar/scheduling-links" element={<SchedulingLinksRoute />} />
          <Route path="tasks" element={<TasksRoute />} />
          {/* Shared mailboxes (part 8) — ordered so activity wins over :folderId */}
          <Route path="shared" element={<SharedMailboxListRoute />} />
          <Route path="shared/:sharedMailboxId/activity" element={<SharedActivityRoute />} />
          <Route path="shared/:sharedMailboxId" element={<SharedMailboxShellRoute />} />
          <Route path="shared/:sharedMailboxId/:folderId" element={<SharedMailboxShellRoute />} />
          <Route path="shared/:sharedMailboxId/:folderId/:messageId" element={<SharedMailboxShellRoute />} />
          {/* Deliverability hub (part 9) */}
          <Route path="settings/deliverability" element={<DeliverabilityHubRoute />} />
          <Route path="settings/deliverability/:domain" element={<DomainDashboardRoute />} />
          <Route path="mail-trace/:messageId" element={<MailTraceRoute />} />
        </Route>

        <Route path="*" element={<NotFoundRoute />} />
      </Routes>
    </>
  );
}
