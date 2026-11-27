import { create } from 'zustand'
import { sendApi, mailApi } from '@/api/endpoints'
import { outbox } from '@/lib/outbox'
import { useUI, toastError } from './ui'
import type { Addr, AttachmentView, MessageDetail, SendAttachment, SendInput } from '@/types/api'

export interface ComposeAttachment extends SendAttachment {
  localId: string
  size: number
}

interface ComposeState {
  open: boolean
  from: string
  to: Addr[]
  cc: Addr[]
  bcc: Addr[]
  subject: string
  html: string
  text: string
  attachments: ComposeAttachment[]
  inReplyTo?: string
  track: boolean
  newsletter: boolean
  idemKey: string
  dirty: boolean
  savedAt: string | null
  sending: boolean

  openNew: (defaultFrom?: string) => void
  openReply: (detail: MessageDetail, all: boolean) => void
  close: () => void
  setFrom: (v: string) => void
  setRecipients: (field: 'to' | 'cc' | 'bcc', addrs: Addr[]) => void
  setSubject: (v: string) => void
  setBody: (html: string, text: string) => void
  addFiles: (files: File[]) => Promise<void>
  addExistingAttachment: (att: AttachmentView) => void
  removeAttachment: (localId: string) => void
  setTrack: (v: boolean) => void
  setNewsletter: (v: boolean) => void
  markSaved: () => void
  submit: () => Promise<boolean>
  discard: () => void
}

const DRAFT_KEY = 'synkra.draft.v1'

function newIdemKey(): string {
  return crypto.randomUUID()
}

/** Smart attachment warning (catalog Part 3): body mentions attachments but none attached. */
const MENTION_RE = /\b(attach|attached|attachment|attachments|geagle|cv|invoice|see below|sie Bylae)\b/i

export const useCompose = create<ComposeState>((set, get) => ({
  open: false,
  from: '',
  to: [],
  cc: [],
  bcc: [],
  subject: '',
  html: '',
  text: '',
  attachments: [],
  inReplyTo: undefined,
  track: false,
  newsletter: false,
  idemKey: newIdemKey(),
  dirty: false,
  savedAt: null,
  sending: false,

  openNew: (defaultFrom) => {
    // Restore an autosaved draft if one exists and it is non-trivial.
    const saved = restoreDraft()
    if (saved && (saved.subject || saved.to?.length || saved.html)) {
      set({ ...saved, open: true, sending: false, idemKey: saved.idemKey || newIdemKey() })
      return
    }
    set({
      open: true,
      from: defaultFrom ?? '',
      to: [],
      cc: [],
      bcc: [],
      subject: '',
      html: '',
      text: '',
      attachments: [],
      inReplyTo: undefined,
      track: false,
      newsletter: false,
      idemKey: newIdemKey(),
      dirty: false,
      savedAt: null,
      sending: false,
    })
  },

  openReply: (detail, all) => {
    const m = detail.message
    const quoteText = [
      '',
      '',
      `----- ${m.from.name || m.from.email} on ${new Date(m.internal_date).toLocaleString()} -----`,
      detail.body_text || '',
    ].join('\n')
    const quoteHtml = `<br/><br/><blockquote style="border-left:2px solid #cbd5e1;padding-left:12px;color:#475569"><p></p></blockquote>`
    const to: Addr[] = m.direction === 'inbound' ? [m.from] : m.to
    set({
      open: true,
      from: '',
      to,
      cc: all ? m.cc : [],
      bcc: [],
      subject: /^re:/i.test(m.subject) ? m.subject : `Re: ${m.subject}`,
      html: quoteHtml,
      text: quoteText,
      attachments: [],
      inReplyTo: m.id,
      track: false,
      newsletter: false,
      idemKey: newIdemKey(),
      dirty: true,
      savedAt: null,
      sending: false,
    })
  },

  close: () => set({ open: false }),

  setFrom: (v) => set({ from: v, dirty: true }),
  setRecipients: (field, addrs) => set({ [field]: addrs, dirty: true } as Partial<ComposeState>),
  setSubject: (v) => set({ subject: v, dirty: true }),
  setBody: (html, text) => set({ html, text, dirty: true }),

  addFiles: async (files) => {
    const additions: ComposeAttachment[] = []
    for (const f of files) {
      const content = await fileToBase64(f)
      additions.push({
        localId: crypto.randomUUID(),
        filename: f.name,
        content_type: f.type || 'application/octet-stream',
        content,
        size: f.size,
      })
    }
    set((s) => ({ attachments: [...s.attachments, ...additions], dirty: true }))
  },

  addExistingAttachment: (att) => {
    set((s) => ({
      attachments: [
        ...s.attachments,
        {
          localId: crypto.randomUUID(),
          filename: att.filename,
          content_type: att.content_type,
          content: '', // forwarded attachment content is fetched at submit time
          size: att.size_bytes,
        },
      ],
      dirty: true,
    }))
  },

  removeAttachment: (localId) =>
    set((s) => ({ attachments: s.attachments.filter((a) => a.localId !== localId), dirty: true })),

  setTrack: (v) => set({ track: v, dirty: true }),
  setNewsletter: (v) => set({ newsletter: v, dirty: true }),
  markSaved: () => set({ savedAt: new Date().toISOString(), dirty: false }),

  submit: async () => {
    const s = get()
    if (s.to.length + s.cc.length + s.bcc.length === 0) {
      useUI.getState().toast('error', 'No recipients', 'Add at least one recipient')
      return false
    }
    const input: SendInput = {
      from: s.from || undefined,
      to: s.to,
      cc: s.cc.length ? s.cc : undefined,
      bcc: s.bcc.length ? s.bcc : undefined,
      subject: s.subject,
      text: s.text || htmlToText(s.html),
      html: s.html || undefined,
      in_reply_to: s.inReplyTo,
      track: s.track,
      newsletter: s.newsletter,
      attachments: s.attachments.length
        ? s.attachments.map(({ filename, content_type, content }) => ({ filename, content_type, content }))
        : undefined,
    }

    // Smart warning (advisory — the user can send anyway)
    if (!input.attachments && MENTION_RE.test(s.subject) === false && MENTION_RE.test(input.text)) {
      const proceed = window.confirm(
        'Your message mentions an attachment but none is attached. Send anyway?'
      )
      if (!proceed) return false
    }

    set({ sending: true })
    try {
      await sendApi.send(input, s.idemKey)
      set({ sending: false })
      discardDraft()
      const key = s.idemKey
      set({ open: false, dirty: false, idemKey: newIdemKey() })
      void key
      useUI.getState().toast('success', 'Message queued', 'Your message is on its way')
      return true
    } catch (err) {
      const e = err as { status?: number; code?: string }
      if (e.status === 0) {
        // Network down: queue for later with the SAME idempotency key.
        outbox.enqueue(s.idemKey, input)
        set({ sending: false, open: false, dirty: false, idemKey: newIdemKey() })
        discardDraft()
        useUI.getState().toast('info', 'Queued offline', 'Will send when the connection returns')
        return true
      }
      set({ sending: false })
      toastError(err, 'Send failed')
      return false
    }
  },

  discard: () => {
    discardDraft()
    set({
      open: false,
      to: [],
      cc: [],
      bcc: [],
      subject: '',
      html: '',
      text: '',
      attachments: [],
      inReplyTo: undefined,
      dirty: false,
      idemKey: newIdemKey(),
    })
  },
}))

// ---- autosave (client-side; there is no server drafts endpoint yet) ----

export function persistDraft(): void {
  const s = useCompose.getState()
  if (!s.dirty && !s.savedAt) return
  try {
    localStorage.setItem(
      DRAFT_KEY,
      JSON.stringify({
        from: s.from,
        to: s.to,
        cc: s.cc,
        bcc: s.bcc,
        subject: s.subject,
        html: s.html,
        text: s.text,
        track: s.track,
        newsletter: s.newsletter,
        idemKey: s.idemKey,
      })
    )
    s.markSaved()
  } catch {
    /* storage full — autosave is best-effort */
  }
}

function restoreDraft(): Partial<ComposeState> | null {
  try {
    const raw = localStorage.getItem(DRAFT_KEY)
    return raw ? (JSON.parse(raw) as Partial<ComposeState>) : null
  } catch {
    return null
  }
}

function discardDraft(): void {
  try {
    localStorage.removeItem(DRAFT_KEY)
  } catch {
    /* ignore */
  }
}

// ---- helpers ----

function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader()
    r.onload = () => {
      const result = String(r.result)
      resolve(result.slice(result.indexOf(',') + 1))
    }
    r.onerror = () => reject(r.error)
    r.readAsDataURL(file)
  })
}

export function htmlToText(html: string): string {
  const div = document.createElement('div')
  div.innerHTML = html
    .replace(/<br\s*\/?>/gi, '\n')
    .replace(/<\/(p|div|li|blockquote)>/gi, '\n')
  return (div.textContent ?? '').replace(/\n{3,}/g, '\n\n').trim()
}

/** Flag risky executable attachments (advisory, catalog Part 3). */
export function riskyAttachments(atts: { filename: string }[]): string[] {
  const RISKY = /\.(exe|scr|bat|cmd|js|vbs|ps1|msi|jar|hta|cpl)$/i
  return atts.filter((a) => RISKY.test(a.filename)).map((a) => a.filename)
}

/** Storage widget helper reused by the topbar. */
export async function fetchStorage() {
  return mailApi.storage()
}
