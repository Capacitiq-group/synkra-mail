import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '@/stores/auth';
import { useMailboxes } from '@/hooks/queries/mail';
import { useDeleteSignature, useSetDefaultSignature, useSignatures } from '@/hooks/queries/signatures';
import { Button, ConfirmDialog, EmptyState, Menu, MenuItem, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { SettingsContent, SettingsHeader, SettingsPage } from '../SettingsLayout';
import { SignaturePreview } from './SignaturePreview';
import { useMemo, useState } from 'react';
import { formatDateTime } from '@/lib/time';
import { toast } from '@/stores/toast';
import { errorMessage } from '@/lib/errors';
import type { Signature } from '@/lib/types';
import type { PlaceholderContext } from '@/lib/resolvePlaceholders';

export function SignaturesRoute() {
  const navigate = useNavigate();
  const me = useAuth((s) => s.me);
  const mailboxesQ = useMailboxes();
  const mailboxId = mailboxesQ.data?.[0]?.id ?? '';
  const signaturesQ = useSignatures(mailboxId);
  const setDefault = useSetDefaultSignature(mailboxId);
  const del = useDeleteSignature(mailboxId);
  const [deleteTarget, setDeleteTarget] = useState<Signature | null>(null);

  const placeholderContext: PlaceholderContext = useMemo(() => {
    if (!me) return {};
    return {
      employee_name: me.display_name,
      email: me.email,
      job_title: me.job_title,
      department: me.department,
      phone: me.phone,
      company_name: me.org_name,
      company_website: `https://${me.email.split('@')[1] ?? ''}`,
    };
  }, [me]);

  return (
    <SettingsPage maxWidth="xl">
      <SettingsHeader
        title="Signatures"
        description="Create reusable signatures. We lint them for compatibility with Gmail, Outlook and Apple Mail."
        back={{ to: '/settings', label: 'Settings' }}
      />
      <SettingsContent>
        <div className="flex justify-end mb-4">
          <Button variant="primary" onClick={() => navigate('/settings/signatures/new')}>
            <Icon name="plus" size={14} /> New signature
          </Button>
        </div>

        {signaturesQ.isLoading && (
          <div className="space-y-3">
            {Array.from({ length: 2 }).map((_, i) => <Skeleton key={i} className="h-40 w-full" />)}
          </div>
        )}

        {!signaturesQ.isLoading && (signaturesQ.data ?? []).length === 0 && (
          <EmptyState
            icon={<Icon name="sparkles" size={28} />}
            title="No signatures yet"
            description="Create your first signature to have it automatically appended to outgoing messages."
            action={<Button variant="primary" onClick={() => navigate('/settings/signatures/new')}>New signature</Button>}
          />
        )}

        <ul className="space-y-4">
          {(signaturesQ.data ?? []).map((sig) => (
            <li key={sig.id} className="border border-line rounded-lg overflow-hidden">
              <div className="flex items-start gap-3 p-3">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <p className="font-medium">{sig.name}</p>
                    {sig.is_default && <span className="text-xs px-1.5 py-0.5 rounded bg-accent-soft text-accent">Default</span>}
                    {sig.use_for_replies && <span className="text-xs px-1.5 py-0.5 rounded bg-canvas border border-line text-ink-soft">Replies</span>}
                    {sig.locked && <span className="text-xs px-1.5 py-0.5 rounded bg-warning-soft text-warning">Locked by org</span>}
                  </div>
                </div>
                <Menu trigger={
                  <Button size="icon" variant="ghost" aria-label="Signature actions">
                    <Icon name="more" size={16} />
                  </Button>
                }>
                  <MenuItem onClick={() => navigate(`/settings/signatures/${sig.id}`)}>Edit</MenuItem>
                  {!sig.is_default && (
                    <MenuItem onClick={async () => {
                      try { await setDefault.mutateAsync(sig.id); toast('success', 'Default updated'); }
                      catch (e) { toast('error', 'Update failed', errorMessage(e)); }
                    }}>Set as default</MenuItem>
                  )}
                  <MenuItem danger onClick={() => setDeleteTarget(sig)}>Delete</MenuItem>
                </Menu>
              </div>
              <div className="bg-canvas border-t border-line p-3">
                <SignaturePreview html={sig.body_html} placeholderContext={placeholderContext} showLint={false} />
              </div>
            </li>
          ))}
        </ul>
      </SettingsContent>

      <ConfirmDialog
        open={!!deleteTarget}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={async () => {
          if (!deleteTarget) return;
          try {
            await del.mutateAsync(deleteTarget.id);
            toast('success', 'Signature deleted');
            setDeleteTarget(null);
          } catch (e) {
            toast('error', 'Could not delete', errorMessage(e));
          }
        }}
        title={`Delete "${deleteTarget?.name}"?`}
        description="This signature will be removed from your account. Messages already sent are unaffected."
        confirmLabel="Delete"
        danger
        busy={del.isPending}
      />
    </SettingsPage>
  );
}

// Suppress unused import warning.
void Link;
void formatDateTime;
