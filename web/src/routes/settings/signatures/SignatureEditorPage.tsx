import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useAuth } from '@/stores/auth';
import { useMailboxes } from '@/hooks/queries/mail';
import { useCreateSignature, useSignatures, useUpdateSignature } from '@/hooks/queries/signatures';
import { Button, Checkbox, Field, Input, Skeleton } from '@/design';
import { Icon } from '@/design/icons';
import { SettingsContent, SettingsHeader, SettingsPage } from '../SettingsLayout';
import { SignaturePreview } from './SignaturePreview';
import { SignatureImporterModal } from './SignatureImporterModal';
import { SIGNATURE_TEMPLATES } from './templates';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { resolvePlaceholders, type PlaceholderContext } from '@/lib/resolvePlaceholders';
import type { SignatureImportResult } from '@/lib/types';

export function SignatureEditorPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const me = useAuth((s) => s.me);
  const mailboxesQ = useMailboxes();
  const mailboxId = mailboxesQ.data?.[0]?.id ?? '';
  const signaturesQ = useSignatures(mailboxId);
  const create = useCreateSignature(mailboxId);
  const update = useUpdateSignature(mailboxId);

  const existing = id && id !== 'new'
    ? signaturesQ.data?.find((s) => s.id === id)
    : undefined;

  const [name, setName] = useState('');
  const [html, setHtml] = useState('');
  const [isDefault, setIsDefault] = useState(false);
  const [useForReplies, setUseForReplies] = useState(false);
  const [importerOpen, setImporterOpen] = useState(false);

  useEffect(() => {
    if (!existing) return;
    setName(existing.name);
    setHtml(existing.body_html);
    setIsDefault(existing.is_default);
    setUseForReplies(existing.use_for_replies);
  }, [existing?.id]); // eslint-disable-line react-hooks/exhaustive-deps

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

  async function save() {
    if (!name.trim()) { toast('error', 'Give this signature a name'); return; }
    if (!html.trim()) { toast('error', 'Signature body is empty'); return; }

    try {
      if (existing) {
        await update.mutateAsync({
          id: existing.id,
          patch: { name: name.trim(), body_html: html, is_default: isDefault, use_for_replies: useForReplies },
        });
      } else {
        await create.mutateAsync({
          name: name.trim(),
          body_text: html.replace(/<[^>]+>/g, ''),
          body_html: html,
          is_default: isDefault,
          use_for_replies: useForReplies,
          locked: false,
        });
      }
      toast('success', 'Signature saved');
      navigate('/settings/signatures');
    } catch (e) {
      toast('error', 'Could not save signature', errorMessage(e));
    }
  }

  function importTemplate(t: typeof SIGNATURE_TEMPLATES[number]) {
    setHtml(t.html);
  }

  function imported(res: SignatureImportResult) {
    setHtml(res.body_html);
    if (res.issues.length > 0) {
      toast('info', 'Imported with warnings', `${res.issues.length} compatibility issue${res.issues.length === 1 ? '' : 's'} — see the preview panel.`);
    } else {
      toast('success', 'Imported cleanly');
    }
  }

  if (signaturesQ.isLoading && existing !== undefined) {
    return (
      <SettingsPage maxWidth="xl">
        <SettingsHeader title="Signature" back={{ to: '/settings/signatures', label: 'Signatures' }} />
        <SettingsContent><Skeleton className="h-40 w-full" /></SettingsContent>
      </SettingsPage>
    );
  }

  return (
    <SettingsPage maxWidth="xl">
      <SettingsHeader
        title={existing ? 'Edit signature' : 'New signature'}
        back={{ to: '/settings/signatures', label: 'Signatures' }}
      />
      <SettingsContent>
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          <div className="space-y-4">
            <Field label="Name" required>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Standard work signature" />
            </Field>

            <div>
              <div className="flex items-center justify-between mb-2">
                <span className="text-xs font-medium text-ink-soft">Signature body (HTML)</span>
                <div className="flex gap-1">
                  <Button size="sm" variant="ghost" onClick={() => setImporterOpen(true)}>
                    <Icon name="download" size={12} /> Import HTML
                  </Button>
                </div>
              </div>
              <textarea
                value={html}
                onChange={(e) => setHtml(e.target.value)}
                rows={18}
                className="w-full font-mono text-xs border border-line rounded p-3 bg-surface"
                placeholder="<p>Paste or write HTML here</p>"
                spellCheck={false}
              />
              <p className="text-xs text-ink-mute mt-1">
                Use <code>{'{{employee_name}}'}</code>, <code>{'{{job_title}}'}</code>, <code>{'{{company_name}}'}</code> and other placeholders — they resolve to each user's information at send time.
              </p>
            </div>

            <div className="space-y-2">
              <Checkbox
                label="Set as default signature for new messages"
                checked={isDefault}
                onChange={(e) => setIsDefault(e.target.checked)}
              />
              <Checkbox
                label="Also use this signature for replies"
                checked={useForReplies}
                onChange={(e) => setUseForReplies(e.target.checked)}
              />
            </div>

            <div className="flex gap-2 pt-2">
              <Button variant="primary" onClick={save} loading={create.isPending || update.isPending}>
                Save signature
              </Button>
              <Button variant="secondary" onClick={() => navigate('/settings/signatures')}>Cancel</Button>
            </div>
          </div>

          <div className="space-y-4">
            <div>
              <p className="text-xs font-medium text-ink-soft mb-2">Preview</p>
              <SignaturePreview html={html || '<p style="color:#94a3b8;">(empty)</p>'} placeholderContext={placeholderContext} />
            </div>
            <div>
              <p className="text-xs font-medium text-ink-soft mb-2">Start from a template</p>
              <div className="grid grid-cols-2 gap-2">
                {SIGNATURE_TEMPLATES.map((t) => (
                  <button
                    key={t.id}
                    onClick={() => importTemplate(t)}
                    className="text-left p-2 border border-line rounded hover:border-accent transition-colors"
                  >
                    <p className="text-sm font-medium">{t.name}</p>
                    <p className="text-xs text-ink-soft">{t.description}</p>
                  </button>
                ))}
              </div>
              <p className="text-xs text-ink-mute mt-2">Replaces the current body. Undo with Ctrl/Cmd+Z.</p>
            </div>
          </div>
        </div>
      </SettingsContent>

      <SignatureImporterModal
        open={importerOpen}
        onClose={() => setImporterOpen(false)}
        onImported={imported}
      />
    </SettingsPage>
  );
}

// Silence unused warning when the resolved value is not directly used.
void resolvePlaceholders;
