import { useEffect, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { Button, ConfirmDialog, Modal, Skeleton, Tabs } from '@/design';
import { Icon } from '@/design/icons';
import { formatDateTime } from '@/lib/time';
import { ComposeHeader } from './ComposeHeader';
import { RichEditor } from './RichEditor';
import { PlainEditor } from './PlainEditor';
import { HtmlEditor } from './HtmlEditor';
import { AttachmentBar } from './AttachmentBar';
import { TemplatesMenu } from './TemplatesMenu';
import { SendLaterDialog } from './SendLaterDialog';
import { useComposeStore } from './useComposeStore';
import { useDraftAutosave, clearComposePersistence } from './useDraftAutosave';
import { useSendCompose } from './useSendCompose';
import { buildQuoted } from './quoting';
import { shouldWarnAboutMissingAttachment } from './smartAttachment';
import { readLocalAutosave, clearLocalAutosave } from './autosave';
import type { BodyMode, ComposeMode } from './types';

const MAX_ATTACHMENT_BYTES = 25 * 1024 * 1024;
const MAX_TOTAL_BYTES = 25 * 1024 * 1024;

export function ComposeRoute() {
  const [params] = useSearchParams();
  const urlKey = [
    params.get('draft') ?? '',
    params.get('mode') ?? '',
    params.get('source') ?? '',
    params.get('mailbox') ?? '',
  ].join('|');

  return <ComposeRouteInner key={urlKey} />;
}

function ComposeRouteInner() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const send = useSendCompose();
  const loadedRef = useRef(false);
  const [localRecoveryPrompt, setLocalRecoveryPrompt] = useState<ReturnType<typeof readLocalAutosave>>(null);
  const [showSendLater, setShowSendLater] = useState(false);
  const [confirmDiscard, setConfirmDiscard] = useState(false);

  const initialized = useComposeStore((s) => s.initialized);
  const initError = useComposeStore((s) => s.initError);
  const mailboxId = useComposeStore((s) => s.mailboxId);

  useDraftAutosave();

  // Initialise the compose store.
  useEffect(() => {
    if (loadedRef.current) return;
    loadedRef.current = true;

    async function init() {
      try {
        const boxes = await api.mailboxes();
        if (boxes.length === 0) { navigate('/'); return; }
        const targetMailboxId = params.get('mailbox') ?? boxes[0].id;
        const target = boxes.find((b) => b.id === targetMailboxId);
        if (!target) { navigate('/'); return; }

        const identities = await api.identities(targetMailboxId);

        // Crash recovery: offer a local draft from the last 24 h if it exists.
        const recovery = readLocalAutosave();
        if (recovery && (recovery.snapshot.to.length > 0 || recovery.snapshot.bodyText.trim() || recovery.snapshot.subject.trim())) {
          setLocalRecoveryPrompt(recovery);
        }

        const draftId = params.get('draft');
        const mode = params.get('mode') as ComposeMode | null;
        const sourceId = params.get('source');

        if (draftId) {
          const draft = await api.getDraft(draftId);
          useComposeStore.getState().initializeFromDraft(draft, targetMailboxId, identities);
          return;
        }

        if ((mode === 'reply' || mode === 'reply_all' || mode === 'forward') && sourceId) {
          const source = await api.getMessage(sourceId);
          const identity = identities.find((i) => i.is_default) ?? identities[0];
          const quoted = buildQuoted(
            mode === 'forward' ? 'forward' : mode,
            source,
            identity?.address ?? '',
          );
          useComposeStore.getState().initializeReply(source, mode, targetMailboxId, identities, quoted);
          return;
        }

        useComposeStore.getState().initializeFresh(targetMailboxId, identities);
      } catch (e) {
        useComposeStore.getState().setInitError(errorMessage(e));
      }
    }

    void init();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Keep the URL in sync with the draft id once the server assigns one so
  // a refresh mid-compose reloads the same draft.
  const draftId = useComposeStore((s) => s.draftId);
  useEffect(() => {
    if (!initialized) return;
    const current = params.get('draft');
    if (draftId && current !== draftId) {
      const next = new URLSearchParams(params);
      next.set('draft', draftId);
      next.set('mailbox', mailboxId);
      setParams(next, { replace: true });
    }
  }, [draftId, initialized, mailboxId, params, setParams]);

  if (initError) {
    return (
      <div className="p-6">
        <p className="text-sm text-danger">{initError}</p>
        <Button className="mt-3" onClick={() => navigate(-1 as never)}>Go back</Button>
      </div>
    );
  }

  if (!initialized) return <ComposeSkeleton />;

  return (
    <>
      <ComposeSurface
        onDiscard={() => setConfirmDiscard(true)}
        onOpenSendLater={() => setShowSendLater(true)}
        onSend={() => void send.send()}
      />

      <SendLaterDialog
        open={showSendLater}
        onClose={() => setShowSendLater(false)}
        onSchedule={(iso) => {
          useComposeStore.getState().setSchedule(iso);
          setShowSendLater(false);
          toast('info', `Scheduled for ${formatDateTime(iso)}`);
          void send.send();
        }}
      />

      <ConfirmDialog
        open={confirmDiscard}
        onCancel={() => setConfirmDiscard(false)}
        onConfirm={async () => {
          const s = useComposeStore.getState();
          clearComposePersistence(s.mailboxId, s.draftId);
          if (s.draftId) {
            try { await api.deleteDraft(s.draftId); } catch { /* best effort */ }
            qc.invalidateQueries({ queryKey: ['drafts', s.mailboxId] });
          }
          s.reset();
          setConfirmDiscard(false);
          navigate(-1 as never);
        }}
        title="Discard this message?"
        description="The draft will be deleted and cannot be recovered."
        confirmLabel="Discard"
        danger
      />

      <LocalRecoveryModal
        record={localRecoveryPrompt}
        onRestore={() => {
          if (!localRecoveryPrompt) return;
          const s = useComposeStore.getState();
          const snap = localRecoveryPrompt.snapshot;
          useComposeStore.setState({
            ...s,
            ...snap,
            initialized: true,
            identities: s.identities,
          });
          toast('success', 'Recovered unsaved draft');
          setLocalRecoveryPrompt(null);
        }}
        onDiscard={() => { clearLocalAutosave(); setLocalRecoveryPrompt(null); }}
      />
    </>
  );
}

// ===== Surface =====
function ComposeSurface({
  onDiscard, onOpenSendLater, onSend,
}: { onDiscard: () => void; onOpenSendLater: () => void; onSend: () => void }) {
  const navigate = useNavigate();
  const { uploadAttachment } = useComposeUpload();
  const subject = useComposeStore((s) => s.subject);
  const sending = useComposeStore((s) => s.sending);
  const sendError = useComposeStore((s) => s.sendError);
  const scheduledFor = useComposeStore((s) => s.scheduledFor);
  const undoSeconds = useComposeStore((s) => s.undoSeconds);
  const setUndoSeconds = useComposeStore((s) => s.setUndoSeconds);
  const scheduledClear = useComposeStore((s) => s.setSchedule);
  const bodyText = useComposeStore((s) => s.bodyText);
  const bodyHtml = useComposeStore((s) => s.bodyHtml);
  const bodyMode = useComposeStore((s) => s.bodyMode);
  const setBodyMode = useComposeStore((s) => s.setBodyMode);
  const setBodyHtml = useComposeStore((s) => s.setBodyHtml);
  const setBodyText = useComposeStore((s) => s.setBodyText);
  const setBodyHtmlOnly = useComposeStore((s) => s.setBodyHtmlOnly);
  const attachments = useComposeStore((s) => s.attachments);
  const to = useComposeStore((s) => s.to);
  const cc = useComposeStore((s) => s.cc);
  const bcc = useComposeStore((s) => s.bcc);
  const identityId = useComposeStore((s) => s.identityId);
  const smartWarn = shouldWarnAboutMissingAttachment(bodyText, attachments.length);
  const canSend = to.length + cc.length + bcc.length > 0 && !!identityId && !sending;

  return (
    <div className="h-full flex flex-col bg-surface">
      <div className="flex items-center gap-2 px-3 py-2 border-b border-line">
        <Button size="icon" variant="ghost" onClick={() => navigate(-1 as never)} aria-label="Close composer">
          <Icon name="close" size={18} />
        </Button>
        <h1 className="font-medium">{scheduledFor ? 'Scheduled message' : 'New message'}</h1>
        <div className="flex-1" />
        <TemplatesMenu />
      </div>

      <ComposeHeader autoFocusTo={!subject && bodyText.length === 0} />

      <BodyTabs mode={bodyMode} onChange={setBodyMode} />

      <div className="flex-1 min-h-0 flex">
        {bodyMode === 'rich' && (
          <RichEditor
            value={bodyHtml}
            onChange={setBodyHtml}
            onInlineImage={async (file) => {
              try {
                const res = await api.uploadAttachment(file);
                return { id: res.attachment_id, filename: res.filename };
              } catch (e) {
                toast('error', 'Could not upload image', errorMessage(e));
                return null;
              }
            }}
            autoFocus={!subject && bodyText.length === 0}
          />
        )}
        {bodyMode === 'plain' && (
          <PlainEditor value={bodyText} onChange={setBodyText} />
        )}
        {bodyMode === 'html' && (
          <HtmlEditor value={bodyHtml} onChange={setBodyHtmlOnly} />
        )}
      </div>

      {smartWarn && (
        <div className="border-t border-warning/40 bg-warning-soft px-3 py-2 text-xs flex items-start gap-2">
          <Icon name="alert-triangle" size={14} className="text-warning mt-0.5" />
          <span>It looks like you mentioned an attachment but did not add one.</span>
        </div>
      )}

      <AttachmentBar onPick={(files) => files.forEach((f) => uploadAttachment(f))} />

      {sendError && (
        <div role="alert" className="border-t border-danger/40 bg-danger-soft px-3 py-2 text-sm text-danger">
          {sendError}
        </div>
      )}

      <footer className="border-t border-line px-3 py-2 flex items-center gap-2">
        <Button variant="primary" onClick={onSend} disabled={!canSend} loading={sending}>
          {scheduledFor ? `Send scheduled` : 'Send'}
        </Button>

        <Button variant="secondary" onClick={onOpenSendLater} disabled={sending}>
          <Icon name="clock" size={14} /> Send later
        </Button>

        {scheduledFor && (
          <button
            type="button"
            onClick={() => scheduledClear(undefined)}
            className="text-xs text-ink-soft hover:text-ink underline"
          >
            Cancel schedule
          </button>
        )}

        <div className="flex-1" />

        <label className="text-xs text-ink-soft flex items-center gap-1">
          Undo send
          <select
            value={undoSeconds}
            onChange={(e) => setUndoSeconds(Number(e.target.value))}
            className="border border-line rounded px-1 py-0.5 text-xs bg-surface"
            aria-label="Undo send delay"
          >
            <option value={0}>off</option>
            <option value={5}>5s</option>
            <option value={10}>10s</option>
            <option value={20}>20s</option>
            <option value={30}>30s</option>
          </select>
        </label>

        <Button variant="ghost" onClick={onDiscard} disabled={sending}>
          Discard
        </Button>
      </footer>
    </div>
  );
}

function BodyTabs({ mode, onChange }: { mode: BodyMode; onChange: (m: BodyMode) => void }) {
  return (
    <div className="px-3 border-b border-line">
      <Tabs
        items={[
          { id: 'rich', label: 'Rich text' },
          { id: 'plain', label: 'Plain text' },
          { id: 'html', label: 'HTML' },
        ]}
        value={mode}
        onChange={(id) => onChange(id as BodyMode)}
      />
    </div>
  );
}

function ComposeSkeleton() {
  return (
    <div className="h-full flex flex-col bg-surface">
      <div className="border-b border-line px-3 py-2"><Skeleton className="h-6 w-32" /></div>
      <div className="border-b border-line px-3 py-2 space-y-2">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
      <div className="p-4 space-y-2">
        <Skeleton className="h-4 w-1/2" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-2/3" />
      </div>
    </div>
  );
}

// ===== Upload hook =====
function useComposeUpload() {
  const start = useComposeStore((s) => s.startUpload);
  const end = useComposeStore((s) => s.endUpload);
  const add = useComposeStore((s) => s.addAttachment);

  async function uploadAttachment(file: File) {
    const state = useComposeStore.getState();
    const currentTotal = state.attachments.reduce((sum, a) => sum + a.size, 0);
    if (file.size > MAX_ATTACHMENT_BYTES) {
      toast('error', 'Attachment too large', `The limit is ${Math.round(MAX_ATTACHMENT_BYTES / 1024 / 1024)} MB per file`);
      return;
    }
    if (currentTotal + file.size > MAX_TOTAL_BYTES) {
      toast('error', 'Attachments exceed size limit', `Total attachments are limited to ${Math.round(MAX_TOTAL_BYTES / 1024 / 1024)} MB`);
      return;
    }

    const tempId = `tmp_${Math.random().toString(36).slice(2, 10)}`;
    add({ id: tempId, filename: file.name, size: file.size, content_type: file.type, uploading: true });
    start();
    try {
      const res = await api.uploadAttachment(file);
      // Replace the placeholder with the real id.
      useComposeStore.setState((s) => {
        const attachments = s.attachments.map((a) =>
          a.id === tempId
            ? { id: res.attachment_id, filename: res.filename, size: res.size, content_type: res.content_type, uploading: false }
            : a,
        );
        const attachmentIds = s.attachmentIds
          .filter((id) => id !== tempId)
          .concat(res.attachment_id);
        return { attachments, attachmentIds };
      });
    } catch (e) {
      useComposeStore.setState((s) => ({
        attachments: s.attachments.map((a) =>
          a.id === tempId ? { ...a, uploading: false, error: errorMessage(e) } : a,
        ),
      }));
      toast('error', 'Upload failed', errorMessage(e));
    } finally {
      end();
    }
  }

  return { uploadAttachment };
}

// ===== Local recovery modal =====
function LocalRecoveryModal({
  record, onRestore, onDiscard,
}: {
  record: ReturnType<typeof readLocalAutosave>;
  onRestore: () => void;
  onDiscard: () => void;
}) {
  if (!record) return null;
  const when = new Date(record.savedAt);
  const summary = record.snapshot.subject || record.snapshot.bodyText.slice(0, 80) || '(no subject)';
  return (
    <Modal
      open
      onClose={onDiscard}
      title="Recover unsaved message?"
      description="We found a draft saved on this device that was never delivered to the server."
      footer={
        <>
          <Button variant="secondary" onClick={onDiscard}>Discard</Button>
          <Button variant="primary" onClick={onRestore}>Restore</Button>
        </>
      }
    >
      <div className="text-sm space-y-2">
        <p className="text-ink-soft">Saved at {formatDateTime(when.toISOString())}</p>
        <p className="font-medium truncate">{summary}</p>
        <p className="text-xs text-ink-mute">
          To: {record.snapshot.to.map((a) => a.email).join(', ') || '(none)'}
        </p>
      </div>
    </Modal>
  );
}
