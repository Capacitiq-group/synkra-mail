// Mail surface: mailboxes, folders, labels, messages, attachments, compose,
// templates, drafts. Real endpoints for everything the Go API ships; the rest
// resolves honestly (see apiCore.ts header).

import { request, qs, notAvailable, localId, nowIso, getToken, registerAttachment, takeAttachments,
  mapFolder, mapMailbox, mapMessageDetail, mapMessageSummary, mapAttachment,
  type MailboxRaw, type FolderRaw, type FolderPageRaw, type ItemWithMessageRaw, type MessageDetailRaw, type AttachmentRaw, type MeRaw } from './apiCore'
import { ApiError } from './errors'
import type {
  CursorPage, Draft, Folder, Identity, Label, Mailbox, MessageDetail, MessageSummary,
  SearchQuery, SendRequest, SendResponse, Thread, EmailTemplate,
} from './types'

// Client-side draft mirror (no server drafts endpoint — autosave is local).
const draftStore = new Map<string, Draft>()

export const apiMail = {
  // ===== Mailboxes / folders =====
  mailboxes: async (): Promise<Mailbox[]> => {
    // Prefer /me (mailboxes arrive with the auth payload); fall back to /mailboxes.
    try {
      const me = await request<MeRaw>('/me')
      return me.mailboxes.map(mapMailbox)
    } catch (err) {
      if (getToken() === null) throw err
      const { mailboxes } = await request<{ mailboxes: MailboxRaw[] }>('/mailboxes')
      return mailboxes.map(mapMailbox)
    }
  },
  mailbox: async (id: string): Promise<Mailbox> => {
    const all = await apiMail.mailboxes()
    const found = all.find((m) => m.id === id)
    if (!found) throw new ApiError(404, 'not_found', 'Mailbox not found')
    return found
  },

  folders: async (mailboxId: string): Promise<Folder[]> => {
    const { folders } = await request<{ folders: FolderRaw[] }>('/folders')
    return folders.map((f, i) => mapFolder(f, i, mailboxId))
  },
  createFolder: async (mailboxId: string, name: string, parentId?: string): Promise<Folder> => {
    void parentId
    const r = await request<{ folder: FolderRaw }>('/folders', { method: 'POST', body: JSON.stringify({ mailbox_id: mailboxId, name }) })
    return mapFolder(r.folder, 99, mailboxId)
  },
  renameFolder: (mailboxId: string, folderId: string, _name: string): Promise<Folder> => {
    void mailboxId; void folderId
    notAvailable('Folder rename', 'IMAP completeness (register 5.x)')
  },
  deleteFolder: async (mailboxId: string, folderId: string): Promise<void> => {
    await request<{ ok: boolean }>(`/folders/${folderId}`, { method: 'DELETE' })
    void mailboxId
  },

  // ===== Labels (no backend module yet) =====
  labels: async (): Promise<Label[]> => [],
  createLabel: (name: string, color: string): Promise<Label> => {
    void name; void color
    notAvailable('Labels', 'mail core expansion (register 12)')
  },
  updateLabel: (_id: string, _patch: Partial<Pick<Label, 'name' | 'color'>>): Promise<Label> => { void _id; void _patch; return notAvailable('Labels', 'mail core expansion (register 12)') },
  deleteLabel: (_id: string): Promise<void> => { void _id; return notAvailable('Labels', 'mail core expansion (register 12)') },

  // ===== Messages =====
  listMessages: async (
    mailboxId: string,
    params: { folder?: string; cursor?: string; limit?: number } & Partial<SearchQuery>,
  ): Promise<CursorPage<MessageSummary>> => {
    const raw = await request<FolderPageRaw>(`/messages${qs({ folder_id: params.folder, before_uid: params.cursor, limit: params.limit })}`)
    return {
      items: raw.items.map((i) => mapMessageSummary(i, mailboxId)),
      next_cursor: raw.has_more && raw.next_before_uid !== undefined ? String(raw.next_before_uid) : null,
      has_more: raw.has_more,
    }
  },

  searchMessages: async (mailboxId: string, query: SearchQuery, cursor?: string): Promise<CursorPage<MessageSummary>> => {
    const { results } = await request<{ results: ItemWithMessageRaw[] }>(`/search${qs({ q: query.q })}`)
    let items = results.map((i) => mapMessageSummary(i, mailboxId))
    // The search endpoint is full-text only — post-filter locally for the
    // list filters the UI supports.
    if (query.is_unread) items = items.filter((m) => !m.is_read)
    if (query.has_attachment) items = items.filter((m) => m.has_attachments)
    if (query.from) items = items.filter((m) => m.from.email.toLowerCase().includes(String(query.from).toLowerCase()))
    if (query.subject) items = items.filter((m) => m.subject.toLowerCase().includes(String(query.subject).toLowerCase()))
    if (cursor) items = [] // search pagination is not supported server-side — one page
    return { items, next_cursor: null, has_more: false }
  },

  getMessage: async (id: string, signal?: AbortSignal): Promise<MessageDetail> => {
    const [detail, atts] = await Promise.all([
      request<MessageDetailRaw>(`/messages/${id}`, {}, signal),
      request<{ attachments: AttachmentRaw[] }>(`/messages/${id}/attachments`, {}, signal).catch(() => ({ attachments: [] as AttachmentRaw[] })),
    ])
    const mapped = mapMessageDetail(detail, '')
    return { ...mapped, attachments: (atts.attachments ?? []).map(mapAttachment) }
  },

  getThread: async (threadId: string): Promise<Thread> => {
    // No thread-read endpoint yet: a thread renders as its anchor message.
    if (threadId.startsWith('t-')) {
      const detail = await apiMail.getMessage(threadId.slice(2))
      return { id: threadId, messages: [detail] }
    }
    try {
      const detail = await apiMail.getMessage(threadId)
      return { id: threadId, messages: [detail] }
    } catch {
      return { id: threadId, messages: [] }
    }
  },

  setFlags: async (id: string, patch: { is_read?: boolean; flagged?: boolean; pinned?: boolean; snoozed_until?: string }): Promise<void> => {
    void patch.pinned; void patch.snoozed_until
    // Flags PATCH replaces the set — read the current flags first.
    const detail = await request<MessageDetailRaw>(`/messages/${id}`)
    const flags = new Set(detail.item.flags)
    const setFlag = (name: string, on: boolean) => {
      if (on) flags.add(name)
      else {
        for (const f of [...flags]) if (f.toLowerCase().includes(name.toLowerCase())) flags.delete(f)
      }
    }
    if (patch.is_read !== undefined) setFlag('\\Seen', patch.is_read)
    if (patch.flagged !== undefined) setFlag('\\Flagged', patch.flagged)
    await request<{ flags: string[] }>(`/messages/${id}/flags`, {
      method: 'PATCH', body: JSON.stringify({ flags: [...flags] }),
    })
  },

  moveMessage: async (id: string, folderId: string): Promise<void> => {
    await request(`/messages/${id}/move`, { method: 'POST', body: JSON.stringify({ folder_id: folderId }) })
  },
  deleteMessage: async (id: string): Promise<void> => {
    await request(`/messages/${id}`, { method: 'DELETE' })
  },

  bulkAction: async (
    ids: string[],
    action: 'read' | 'unread' | 'flag' | 'unflag' | 'archive' | 'trash' | 'delete' | 'move',
    folderId?: string,
  ): Promise<{ affected: number }> => {
    let affected = 0
    for (const id of ids) {
      try {
        if (action === 'delete') await apiMail.deleteMessage(id)
        else if (action === 'move' || action === 'archive' || action === 'trash') {
          if (folderId) await apiMail.moveMessage(id, folderId)
        } else {
          await apiMail.setFlags(id, {
            is_read: action === 'read' ? true : action === 'unread' ? false : undefined,
            flagged: action === 'flag' ? true : action === 'unflag' ? false : undefined,
          })
        }
        affected++
      } catch { /* per-message failure must not abort the batch */ }
    }
    return { affected }
  },

  addLabelToMessage: (_messageId: string, _labelId: string): Promise<void> => { void _messageId; void _labelId; return notAvailable('Labels', 'mail core expansion (register 12)') },
  removeLabelFromMessage: (_messageId: string, _labelId: string): Promise<void> => { void _messageId; void _labelId; return notAvailable('Labels', 'mail core expansion (register 12)') },

  // ===== Attachments =====
  // No upload endpoint: files register client-side and ride the send JSON.
  uploadAttachment: async (file: File): Promise<{ attachment_id: string; filename: string; size: number; content_type: string }> => {
    const base64 = await new Promise<string>((resolve, reject) => {
      const r = new FileReader()
      r.onload = () => resolve(String(r.result).slice(String(r.result).indexOf(',') + 1))
      r.onerror = () => reject(r.error)
      r.readAsDataURL(file)
    })
    const id = registerAttachment(file, base64)
    return { attachment_id: id, filename: file.name, size: file.size, content_type: file.type || 'application/octet-stream' }
  },
  attachmentUrl: (messageId: string, attachmentId: string): string => `/api/v1/messages/${messageId}/attachments/${attachmentId}`,
  fetchAttachmentBlob: async (messageId: string, attachmentId: string): Promise<Blob> => {
    // Raw fetch with the bearer header — the API has no cookie session.
    const headers: Record<string, string> = {}
    const t = getToken()
    if (t) headers.Authorization = `Bearer ${t}`
    const res = await fetch(apiMail.attachmentUrl(messageId, attachmentId), { headers })
    if (!res.ok) throw new ApiError(res.status, 'attachment_fetch_failed', 'Could not fetch attachment')
    return res.blob()
  },

  // ===== Compose =====
  identities: async (mailboxId: string): Promise<Identity[]> => {
    const boxes = await apiMail.mailboxes()
    const primary = boxes.find((b) => b.id === mailboxId) ?? boxes[0]
    const out: Identity[] = primary
      ? [{ id: `id-${primary.id}`, mailbox_id: primary.id, address: primary.address, display_name: primary.display_name, is_default: true, kind: 'primary' }]
      : []
    try {
      const { addresses } = await request<{ addresses: { id: string; address: string; kind: string; is_primary?: boolean }[] }>('/addresses')
      for (const a of addresses) {
        if (a.kind === 'forward' || !a.address.includes('@') || a.address.endsWith('@')) continue
        if (primary && a.address === primary.address) continue
        out.push({ id: `id-${a.id}`, mailbox_id: mailboxId, address: a.address, display_name: a.address, is_default: false, kind: 'alias' })
      }
    } catch { /* aliases are admin-managed; primary identity still works */ }
    return out
  },

  send: async (req: SendRequest): Promise<SendResponse> => {
    if (req.schedule_at) notAvailable('Scheduled sending', 'composer expansion (register 12e)')
    const attachments = takeAttachments(req.attachment_ids)
    const resp = await request<{ message_id: string; recipients: unknown }>('/send', {
      method: 'POST',
      body: JSON.stringify({
        from: req.from?.email,
        to: req.to,
        cc: req.cc.length ? req.cc : undefined,
        bcc: req.bcc.length ? req.bcc : undefined,
        subject: req.subject,
        text: req.body_text,
        html: req.body_html || undefined,
        in_reply_to: req.in_reply_to,
        track: req.tracking,
        attachments: attachments.length ? attachments : undefined,
      }),
      headers: { 'Idempotency-Key': crypto.randomUUID() },
    })
    const accepted = Array.isArray(resp.recipients) ? (resp.recipients as string[]) : []
    return { message_id: resp.message_id, accepted, suppressed: [] }
  },
  cancelScheduledSend: (_messageId: string): Promise<void> => { void _messageId; return notAvailable('Scheduled sending', 'composer expansion (register 12e)') },

  // ===== Templates (client-side; no backend module) =====
  templates: async (): Promise<EmailTemplate[]> => [],
  createTemplate: (t: Omit<EmailTemplate, 'id'>): Promise<EmailTemplate> => {
    void t
    notAvailable('Templates', 'composer expansion (register 12)')
  },
  updateTemplate: (): Promise<EmailTemplate> => notAvailable('Templates', 'composer expansion (register 12)'),
  deleteTemplate: (): Promise<void> => notAvailable('Templates', 'composer expansion (register 12)'),

  // ===== Drafts (client-side mirror; autosave is local by design) =====
  listDrafts: async (mailboxId: string): Promise<Draft[]> =>
    [...draftStore.values()].filter((d) => d.mailbox_id === mailboxId),
  getDraft: async (id: string): Promise<Draft> => {
    const d = draftStore.get(id)
    if (!d) throw new ApiError(404, 'not_found', 'Draft not found')
    return d
  },
  saveDraft: async (draft: Omit<Draft, 'id' | 'updated_at'> & { id?: string }): Promise<Draft> => {
    const saved: Draft = { ...draft, id: draft.id ?? localId('draft'), updated_at: nowIso() }
    draftStore.set(saved.id, saved)
    return saved
  },
  deleteDraft: async (id: string): Promise<void> => { draftStore.delete(id) },
}
