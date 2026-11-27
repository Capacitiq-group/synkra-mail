import { create } from 'zustand';
import type { Address, Draft, Identity, MessageDetail } from '@/lib/types';
import type { BodyMode, ComposeMode, ComposeSnapshot, RecipientField, StagedAttachment } from './types';
import { htmlToText } from './quoting';

interface ComposeState {
  initialized: boolean;
  initError: string | null;
  sessionId: string;

  mailboxId: string;
  identities: Identity[];
  identityId: string;

  to: Address[];
  cc: Address[];
  bcc: Address[];
  subject: string;
  showCc: boolean;
  showBcc: boolean;

  bodyMode: BodyMode;
  bodyHtml: string;
  bodyText: string;

  attachmentIds: string[];
  attachments: StagedAttachment[];
  uploading: number;

  draftId?: string;
  inReplyTo?: string;
  references?: string[];
  sourceMessageId?: string;
  sourceMessage?: MessageDetail;
  composeMode: ComposeMode;

  scheduledFor?: string;
  undoSeconds: number;
  sending: boolean;
  sendError: string | null;

  // Actions
  initializeFresh: (mailboxId: string, identities: Identity[]) => void;
  initializeReply: (
    source: MessageDetail,
    mode: Exclude<ComposeMode, 'new'>,
    mailboxId: string,
    identities: Identity[],
    quoted: { html: string; text: string; subject: string; to: Address[]; cc: Address[]; inReplyTo: string; references: string[] },
  ) => void;
  initializeFromDraft: (draft: Draft, mailboxId: string, identities: Identity[]) => void;

  setRecipients: (field: RecipientField, list: Address[]) => void;
  setSubject: (v: string) => void;
  setBodyHtml: (html: string, text: string) => void;
  setBodyText: (text: string) => void;
  setBodyHtmlOnly: (html: string) => void;
  setBodyMode: (mode: BodyMode) => void;
  setShowCc: (v: boolean) => void;
  setShowBcc: (v: boolean) => void;

  setIdentity: (id: string) => void;
  setSchedule: (iso: string | undefined) => void;
  setUndoSeconds: (n: number) => void;
  setSending: (v: boolean) => void;
  setSendError: (v: string | null) => void;

  addAttachment: (att: StagedAttachment) => void;
  removeAttachment: (id: string) => void;
  startUpload: () => void;
  endUpload: (err?: string) => void;

  setSharedContext: (mailboxId: string, threadId: string, displayName: string) => void;
  setSharedReplyOwner: (owner: import('@/lib/types').ReplyOwner | null) => void;
  setSharedClaimState: (state: 'idle' | 'claiming' | 'claimed' | 'competing') => void;
  setDraftId: (id: string) => void;
  markInitialized: () => void;
  setInitError: (msg: string) => void;

  sharedMailboxId?: string;
  sharedSourceThreadId?: string;
  sharedReplyOwner: import('@/lib/types').ReplyOwner | null;
  sharedClaimState: 'idle' | 'claiming' | 'claimed' | 'competing';
  sharedDisplayName?: string;

  toSnapshot: () => ComposeSnapshot | null;
  reset: () => void;
}

function generateSessionId(): string {
  try { return crypto.randomUUID(); } catch { return `s${Date.now()}${Math.random().toString(36).slice(2, 8)}`; }
}

const EMPTY = {
  initialized: false,
  initError: null,
  mailboxId: '',
  identities: [],
  identityId: '',
  to: [],
  cc: [],
  bcc: [],
  subject: '',
  showCc: false,
  showBcc: false,
  bodyMode: 'rich' as BodyMode,
  bodyHtml: '',
  bodyText: '',
  attachmentIds: [],
  attachments: [] as StagedAttachment[],
  uploading: 0,
  draftId: undefined,
  inReplyTo: undefined,
  references: undefined,
  sourceMessageId: undefined,
  sourceMessage: undefined,
  composeMode: 'new' as ComposeMode,
  scheduledFor: undefined,
  undoSeconds: 10,
  sending: false,
  sendError: null,
  sharedMailboxId: undefined,
  sharedSourceThreadId: undefined,
  sharedReplyOwner: null,
  sharedClaimState: 'idle' as 'idle' | 'claiming' | 'claimed' | 'competing',
  sharedDisplayName: undefined,
};

export const useComposeStore = create<ComposeState>((set, get) => ({
  sessionId: generateSessionId(),
  ...EMPTY,

  initializeFresh: (mailboxId, identities) =>
    set({
      ...EMPTY,
      initialized: true,
      sessionId: generateSessionId(),
      mailboxId,
      identities,
      identityId: identities.find((i) => i.is_default)?.id ?? identities[0]?.id ?? '',
      composeMode: 'new',
    }),

  initializeReply: (source, mode, mailboxId, identities, quoted) =>
    set({
      ...EMPTY,
      initialized: true,
      sessionId: generateSessionId(),
      mailboxId,
      identities,
      identityId: identities.find((i) => i.is_default)?.id ?? identities[0]?.id ?? '',
      composeMode: mode,
      sourceMessage: source,
      sourceMessageId: source.id,
      inReplyTo: quoted.inReplyTo,
      references: quoted.references,
      to: quoted.to,
      cc: quoted.cc,
      showCc: quoted.cc.length > 0,
      subject: quoted.subject,
      bodyHtml: quoted.html,
      bodyText: quoted.text,
    }),

  initializeFromDraft: (draft, mailboxId, identities) =>
    set({
      ...EMPTY,
      initialized: true,
      sessionId: generateSessionId(),
      mailboxId,
      identities,
      identityId: identities.find((i) => i.address === draft.from?.email)?.id
        ?? identities.find((i) => i.is_default)?.id
        ?? identities[0]?.id
        ?? '',
      to: draft.to,
      cc: draft.cc,
      bcc: draft.bcc,
      showCc: draft.cc.length > 0,
      showBcc: draft.bcc.length > 0,
      subject: draft.subject,
      bodyHtml: draft.body_html ?? '',
      bodyText: draft.body_text,
      bodyMode: draft.body_html ? 'rich' : 'plain',
      attachmentIds: draft.attachment_ids,
      attachments: draft.attachment_ids.map((id) => ({
        id, filename: 'Attachment', size: 0, content_type: 'application/octet-stream',
      })),
      draftId: draft.id,
      inReplyTo: draft.in_reply_to,
      composeMode: draft.in_reply_to ? 'reply' : 'new',
    }),

  setRecipients: (field, list) => set({ [field]: list } as Pick<ComposeState, 'to' | 'cc' | 'bcc'>),
  setSubject: (v) => set({ subject: v }),
  setShowCc: (v) => set({ showCc: v }),
  setShowBcc: (v) => set({ showBcc: v }),
  setSharedContext: (mailboxId, threadId, displayName) => set({ sharedMailboxId: mailboxId, sharedSourceThreadId: threadId, sharedDisplayName: displayName }),
  setSharedReplyOwner: (owner) => set({ sharedReplyOwner: owner }),
  setSharedClaimState: (state) => set({ sharedClaimState: state }),

  setBodyHtml: (html, text) => set({ bodyHtml: html, bodyText: text }),
  setBodyText: (text) => set({ bodyText: text }),
  setBodyHtmlOnly: (html) => set({ bodyHtml: html, bodyText: htmlToText(html) }),

  setBodyMode: (mode) => {
    const { bodyMode, bodyHtml, bodyText } = get();
    if (mode === bodyMode) return;
    if (mode === 'rich' && !bodyHtml) {
      set({ bodyMode: 'rich', bodyHtml: bodyText ? `<p>${bodyText.replace(/\n/g, '<br>')}</p>` : '' });
      return;
    }
    if (mode === 'plain' && bodyHtml) {
      set({ bodyMode: 'plain', bodyText: htmlToText(bodyHtml) });
      return;
    }
    if (mode === 'html' && !bodyHtml && bodyText) {
      set({ bodyMode: 'html', bodyHtml: `<p>${bodyText.replace(/\n/g, '<br>')}</p>` });
      return;
    }
    set({ bodyMode: mode });
  },

  setIdentity: (id) => set({ identityId: id }),
  setSchedule: (iso) => set({ scheduledFor: iso }),
  setUndoSeconds: (n) => set({ undoSeconds: Math.max(0, Math.min(30, n)) }),
  setSending: (v) => set({ sending: v }),
  setSendError: (v) => set({ sendError: v }),

  addAttachment: (att) =>
    set((s) => ({
      attachments: [...s.attachments, att],
      attachmentIds: [...s.attachmentIds, att.id],
    })),
  removeAttachment: (id) =>
    set((s) => ({
      attachments: s.attachments.filter((a) => a.id !== id),
      attachmentIds: s.attachmentIds.filter((x) => x !== id),
    })),
  startUpload: () => set((s) => ({ uploading: s.uploading + 1 })),
  endUpload: (err) => set((s) => ({ uploading: Math.max(0, s.uploading - 1), sendError: err ?? s.sendError })),

  setDraftId: (id) => set({ draftId: id }),
  markInitialized: () => set({ initialized: true }),
  setInitError: (msg) => set({ initError: msg }),

  toSnapshot: () => {
    const s = get();
    if (!s.initialized) return null;
    return {
      mailboxId: s.mailboxId,
      identityId: s.identityId,
      to: s.to,
      cc: s.cc,
      bcc: s.bcc,
      subject: s.subject,
      bodyMode: s.bodyMode,
      bodyHtml: s.bodyHtml,
      bodyText: s.bodyText,
      attachmentIds: s.attachmentIds,
      attachments: s.attachments,
      inReplyTo: s.inReplyTo,
      references: s.references,
      sourceMessageId: s.sourceMessageId,
      composeMode: s.composeMode,
      draftId: s.draftId,
    };
  },

  reset: () => set({ ...EMPTY, sessionId: generateSessionId() }),
}));
