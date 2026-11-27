// Reading pane: sandboxed body render, security alerts, attachments with
// authenticated download, reply/delete/move/flag actions, trace + tracking
// panels.

import { useEffect, useState } from 'react'
import { mailApi, threadApi } from '@/api/endpoints'
import type { AttachmentView, Folder, ItemWithMessage, MessageDetail, TraceResp, TrackingResp } from '@/types/api'
import { formatBytes, formatDateTime } from '@/lib/format'
import { sanitizeEmailHTML } from '@/lib/sanitize'
import { useCompose } from '@/stores/compose'
import { useMail } from '@/stores/mail'
import { useUI, toastError } from '@/stores/ui'
import { Button } from '@/ui/primitives'
import { EmptyState, LoadingState } from '@/ui/feedback'
import { AuthBadges, SecurityAlerts, alertsFor } from './SecurityAlerts'

export function ReadingPane({
  detail,
  folders,
  onClose,
}: {
  detail: MessageDetail
  folders: Folder[]
  onClose?: () => void
}) {
  const { message, item, body_html, body_text } = detail
  const openReply = useCompose((s) => s.openReply)
  const { setItemFlags, removeItem, selectFolder } = useMail()
  const [attachments, setAttachments] = useState<AttachmentView[] | null>(null)
  const [showTrace, setShowTrace] = useState(false)
  const [showTracking, setShowTracking] = useState(false)
  const [noteOpen, setNoteOpen] = useState(false)
  const [noteText, setNoteText] = useState('')

  useEffect(() => {
    setAttachments(null)
    setShowTrace(false)
    setShowTracking(false)
    if (message.has_attachments) {
      mailApi.attachments(message.id).then((r) => setAttachments(r.attachments)).catch(() => setAttachments([]))
    }
  }, [message.id, message.has_attachments])

  const setFlag = async (flag: string, on: boolean) => {
    const flags = on ? [...new Set([...item.flags, flag])] : item.flags.filter((f) => f !== flag)
    setItemFlags(item.id, flags)
    try {
      await mailApi.setFlags(message.id, flags)
    } catch (err) {
      toastError(err, 'Could not update flags')
    }
  }

  const move = async (folderId: string) => {
    try {
      await mailApi.move(message.id, folderId)
      removeItem(item.id)
      selectFolder(folderId)
      onClose?.()
      useUI.getState().toast('success', 'Moved')
    } catch (err) {
      toastError(err, 'Move failed')
    }
  }

  const del = async () => {
    if (!window.confirm('Delete this message?')) return
    try {
      await mailApi.deleteMessage(message.id)
      removeItem(item.id)
      onClose?.()
      useUI.getState().toast('success', 'Deleted')
    } catch (err) {
      toastError(err, 'Delete failed')
    }
  }

  const download = async (att: AttachmentView) => {
    try {
      const blob = await mailApi.downloadAttachment(att.download_path)
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = att.filename
      a.click()
      setTimeout(() => URL.revokeObjectURL(url), 5000)
    } catch (err) {
      toastError(err, 'Download failed')
    }
  }

  const flagged = item.flags.includes('\\Flagged')

  return (
    <div className="flex h-full min-h-0 flex-col overflow-y-auto bg-white">
      <header className="sticky top-0 z-10 border-b border-slate-200 bg-white px-4 pb-3 pt-2">
        <div className="flex items-start justify-between gap-3">
          <h1 className="text-base font-semibold text-slate-900">{message.subject || '(no subject)'}</h1>
          <div className="flex shrink-0 items-center gap-1">
            <Button variant="ghost" size="sm" onClick={() => void setFlag('\\Flagged', !flagged)}>
              {flagged ? '🚩 Unflag' : '⚐ Flag'}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => openReply(detail, false)}>
              ↩ Reply
            </Button>
            <Button variant="ghost" size="sm" onClick={() => openReply(detail, true)}>
              ↩↩ Reply all
            </Button>
            <MoveSelect folders={folders} onMove={(id) => void move(id)} />
            <Button variant="ghost" size="sm" onClick={() => void del()}>
              🗑️
            </Button>
            {onClose && (
              <Button variant="ghost" size="sm" onClick={onClose}>
                ✕
              </Button>
            )}
          </div>
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-slate-500">
          <span className="font-medium text-slate-700">
            {message.from.name || message.from.email}
            <span className="font-normal text-slate-400"> &lt;{message.from.email}&gt;</span>
          </span>
          <span>to {message.to.map((a) => a.email).join(', ') || '—'}</span>
          {message.cc.length > 0 && <span>cc {message.cc.map((a) => a.email).join(', ')}</span>}
          <span className="ml-auto">{formatDateTime(item.created_at)}</span>
          <span title="UID in folder">uid {item.uid}</span>
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <AuthBadges message={message} />
          <Button variant="secondary" size="sm" onClick={() => setShowTrace((v) => !v)}>
            Delivery trace
          </Button>
          <Button variant="secondary" size="sm" onClick={() => setShowTracking((v) => !v)}>
            Tracking {message.tracking_enabled ? 'on' : 'off'}
          </Button>
          {message.thread_id && (
            <Button variant="secondary" size="sm" onClick={() => setNoteOpen((v) => !v)}>
              Add note
            </Button>
          )}
        </div>
        <SecurityAlerts alerts={alertsFor(message)} />
        {noteOpen && message.thread_id && (
          <div className="mt-2 flex gap-2">
            <input
              className="flex-1 rounded-md border border-slate-300 px-2 py-1 text-xs"
              placeholder="Internal note for this thread (shared inbox)…"
              value={noteText}
              onChange={(e) => setNoteText(e.target.value)}
            />
            <Button
              size="sm"
              onClick={() => {
                threadApi
                  .addNote(message.thread_id!, noteText)
                  .then(() => {
                    setNoteOpen(false)
                    setNoteText('')
                    useUI.getState().toast('success', 'Note saved')
                  })
                  .catch((err) => toastError(err, 'Note failed'))
              }}
            >
              Save note
            </Button>
          </div>
        )}
      </header>

      <div className="px-4 py-3">
        {attachments !== null && attachments.length > 0 && (
          <div className="mb-3 flex flex-wrap gap-2">
            {attachments
              .filter((a) => !a.is_inline)
              .map((a) => (
                <button
                  key={a.id}
                  onClick={() => void download(a)}
                  className="flex items-center gap-2 rounded-lg border border-slate-200 px-3 py-1.5 text-xs hover:border-brand-300 hover:bg-brand-50"
                  title={`${a.content_type} — click to download`}
                >
                  📎 <span className="max-w-40 truncate font-medium">{a.filename}</span>
                  <span className="text-slate-400">{formatBytes(a.size_bytes)}</span>
                </button>
              ))}
          </div>
        )}

        <BodyView html={body_html} text={body_text} />

        {showTrace && <TracePanel messageId={message.id} />}
        {showTracking && <TrackingPanel messageId={message.id} />}
      </div>
    </div>
  )
}

function BodyView({ html, text }: { html: string; text: string }) {
  if (html) {
    // sandbox="" without allow-same-origin/allow-scripts: even if sanitizer
    // missed something, the document gets no origin and no script execution.
    return (
      <iframe
        title="Message body"
        sandbox=""
        className="h-[60vh] min-h-80 w-full rounded-lg border border-slate-200 bg-white"
        srcDoc={`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><style>body{margin:12px;font-family:ui-sans-serif,system-ui,sans-serif;font-size:14px;color:#1e293b;overflow-wrap:break-word}img{max-width:100%}a{color:#4f46e5}</style></head><body>${sanitizeEmailHTML(html)}</body></html>`}
      />
    )
  }
  if (text)
    return (
      <pre className="whitespace-pre-wrap break-words rounded-lg bg-slate-50 p-4 text-sm leading-relaxed text-slate-800">
        {text}
      </pre>
    )
  return <EmptyState title="No body" detail="This message has no text or HTML part." />
}

function TracePanel({ messageId }: { messageId: string }) {
  const [trace, setTrace] = useState<TraceResp | null>(null)
  const [err, setErr] = useState<string | null>(null)
  useEffect(() => {
    mailApi.trace(messageId).then(setTrace).catch((e) => setErr((e as Error).message))
  }, [messageId])

  if (err) return <div className="mt-3 text-xs text-red-500">Trace failed: {err}</div>
  if (!trace) return <LoadingState label="Loading trace…" />
  if (!trace.events.length)
    return <EmptyState title="No delivery events" detail="Nothing has been recorded for this message yet." />

  return (
    <section className="mt-4 rounded-lg border border-slate-200 p-3">
      <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-400">
        Delivery trace (Mail Trace)
      </h2>
      <ol className="relative ml-2 border-l border-slate-200">
        {trace.events.map((e, i) => (
          <li key={i} className="mb-3 ml-4">
            <span className="absolute -left-[5px] h-2.5 w-2.5 rounded-full bg-brand-500" />
            <div className="text-xs font-semibold text-slate-700">
              {e.state} <span className="font-normal text-slate-400">{formatDateTime(e.occurred_at)}</span>
            </div>
            {e.detail && (
              <pre className="mt-1 overflow-x-auto rounded bg-slate-50 p-2 text-[10px] text-slate-500">
                {JSON.stringify(e.detail, null, 2)}
              </pre>
            )}
          </li>
        ))}
      </ol>
    </section>
  )
}

function TrackingPanel({ messageId }: { messageId: string }) {
  const [data, setData] = useState<TrackingResp | null>(null)
  const [err, setErr] = useState<string | null>(null)
  useEffect(() => {
    mailApi.tracking(messageId).then(setData).catch((e) => setErr((e as Error).message))
  }, [messageId])

  if (err) return <div className="mt-3 text-xs text-red-500">Tracking failed: {err}</div>
  if (!data) return <LoadingState label="Loading tracking…" />

  return (
    <section className="mt-4 rounded-lg border border-slate-200 p-3 text-sm">
      <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-400">
        Engagement (three-label honesty model)
      </h2>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <Metric label="pixels" value={data.pixels} />
        <Metric label="opens" value={data.opens} />
        <Metric label="links" value={data.links} />
        <Metric label="clicks" value={data.clicks} />
      </div>
      {!data.tracking_enabled && (
        <p className="mt-2 text-xs text-slate-400">Tracking was disabled for this message.</p>
      )}
    </section>
  )
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-lg bg-slate-50 p-2 text-center">
      <div className="text-lg font-semibold text-slate-800">{value}</div>
      <div className="text-[10px] uppercase tracking-wide text-slate-400">{label}</div>
    </div>
  )
}

function MoveSelect({ folders, onMove }: { folders: Folder[]; onMove: (id: string) => void }) {
  return (
    <select
      className="rounded-md border border-slate-300 bg-white px-1.5 py-1 text-xs text-slate-600"
      defaultValue=""
      aria-label="Move to folder"
      onChange={(e) => {
        if (e.target.value) onMove(e.target.value)
        e.target.value = ''
      }}
    >
      <option value="" disabled>
        Move to…
      </option>
      {folders.map((f) => (
        <option key={f.id} value={f.id}>
          {f.name}
        </option>
      ))}
    </select>
  )
}

export type { ItemWithMessage }
