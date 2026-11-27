// Compose overlay — catalog Part 3: TipTap rich text, recipient chips,
// quoting (reply prefills), smart attachment warnings, client-side autosave
// with the SAME Idempotency-Key across retries, offline outbox handoff.

import { useEffect, useRef, useState } from 'react'
import { useEditor, EditorContent } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import Link from '@tiptap/extension-link'
import Underline from '@tiptap/extension-underline'
import { useCompose, persistDraft, riskyAttachments } from '@/stores/compose'
import { useAuth } from '@/stores/auth'
import { addressesApi } from '@/api/endpoints'
import { formatBytes, initials, isValidEmail, parseAddrInput } from '@/lib/format'
import type { AddressView, Addr } from '@/types/api'
import { Button, Input, Select } from '@/ui/primitives'
import { toastError } from '@/stores/ui'

export function ComposeOverlay() {
  const me = useAuth((s) => s.me)
  const store = useCompose()
  const [addresses, setAddresses] = useState<AddressView[]>([])
  const [warnRisky, setWarnRisky] = useState<string[]>([])

  const editor = useEditor({
    extensions: [
      StarterKit.configure({ heading: { levels: [1, 2] } }),
      Underline,
      Link.configure({ openOnClick: false, autolink: true }),
    ],
    content: store.html || '<p></p>',
    onUpdate: ({ editor }) => {
      useCompose.getState().setBody(editor.getHTML(), editor.getText())
    },
  })

  // Keep the editor in sync when a reply quote loads after mount.
  useEffect(() => {
    if (editor && store.html && editor.getHTML() !== store.html && !store.dirty) {
      editor.commands.setContent(store.html)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [store.inReplyTo])

  useEffect(() => {
    addressesApi
      .list()
      .then((r) =>
        setAddresses(
          r.addresses.filter(
            (a) => a.kind !== 'forward' && (a.email || (a.local && a.domain))
          )
        )
      )
      .catch(() => undefined)
  }, [])

  // Autosave heartbeat: flush the draft every 5s while dirty.
  useEffect(() => {
    const timer = setInterval(() => {
      if (useCompose.getState().dirty) persistDraft()
    }, 5000)
    return () => {
      clearInterval(timer)
      persistDraft()
    }
  }, [])

  // Executive-style warning when risky executables are attached.
  useEffect(() => {
    setWarnRisky(riskyAttachments(store.attachments))
  }, [store.attachments])

  const fromOptions = addresses.map((a) => ({
    value: a.email ?? `${a.local}@${a.domain}`,
    label: a.email ?? `${a.local}@${a.domain}`,
  }))
  const fromValue = store.from || fromOptions[0]?.value || ''

  if (!store.open) return null

  return (
    <div className="pointer-events-auto absolute bottom-0 right-4 top-0 flex w-full max-w-2xl flex-col rounded-l-xl border border-slate-200 bg-white shadow-2xl">
      <header className="flex items-center justify-between border-b border-slate-200 px-4 py-2.5">
        <h2 className="text-sm font-semibold text-slate-800">
          {store.inReplyTo ? 'Reply' : 'New message'}
        </h2>
        <div className="flex items-center gap-2 text-[11px] text-slate-400">
          {store.savedAt && <span>draft saved {new Date(store.savedAt).toLocaleTimeString()}</span>}
          <Button variant="ghost" size="sm" onClick={store.close} aria-label="Minimise composer">
            —
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              if (window.confirm('Discard this draft?')) store.discard()
            }}
            aria-label="Discard draft"
          >
            ✕
          </Button>
        </div>
      </header>

      <div className="flex min-h-0 flex-1 flex-col divide-y divide-slate-100">
        {fromOptions.length > 1 && (
          <Row label="From">
            <Select value={fromValue} onChange={(e) => store.setFrom(e.target.value)}>
              {fromOptions.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </Select>
          </Row>
        )}
        <Row label="To">
          <RecipientChips
            addrs={store.to}
            onChange={(a) => store.setRecipients('to', a)}
            placeholder="name@company.co.za"
          />
        </Row>
        <Row label="Cc">
          <RecipientChips
            addrs={store.cc}
            onChange={(a) => store.setRecipients('cc', a)}
            placeholder="cc recipients"
          />
        </Row>
        <Row label="Bcc">
          <RecipientChips
            addrs={store.bcc}
            onChange={(a) => store.setRecipients('bcc', a)}
            placeholder="bcc recipients"
          />
        </Row>
        <Row label="Subject">
          <Input
            value={store.subject}
            onChange={(e) => store.setSubject(e.target.value)}
            placeholder="Subject"
            className="border-0 focus:outline-none focus:ring-0"
          />
        </Row>

        <div className="flex min-h-0 flex-1 flex-col">
          {editor && (
            <div className="flex items-center gap-1 border-b border-slate-100 px-3 py-1 text-xs text-slate-500">
              <ToolbarButton editor={editor} action={() => editor.chain().focus().toggleBold().run()} label="B" bold />
              <ToolbarButton editor={editor} action={() => editor.chain().focus().toggleItalic().run()} label="I" italic />
              <ToolbarButton editor={editor} action={() => editor.chain().focus().toggleUnderline().run()} label="U" underline />
              <ToolbarButton editor={editor} action={() => editor.chain().focus().toggleBulletList().run()} label="• List" />
              <ToolbarButton editor={editor} action={() => editor.chain().focus().toggleBlockquote().run()} label="❝" />
              <ToolbarButton
                editor={editor}
                action={() => {
                  const url = window.prompt('Link URL')
                  if (url) editor.chain().focus().setLink({ href: url }).run()
                }}
                label="🔗"
              />
            </div>
          )}
          <EditorContent editor={editor} className="flex min-h-0 flex-1 flex-col" />
        </div>

        <div className="px-4 py-2">
          <div className="flex flex-wrap items-center gap-2">
            <label className="cursor-pointer text-xs font-medium text-brand-600 hover:underline">
              📎 Attach files
              <input
                type="file"
                multiple
                className="hidden"
                onChange={(e) => {
                  const files = Array.from(e.target.files ?? [])
                  if (files.length) void store.addFiles(files)
                  e.target.value = ''
                }}
              />
            </label>
            <label className="flex items-center gap-1 text-xs text-slate-500" title="Insert an open-tracking pixel">
              <input type="checkbox" checked={store.track} onChange={(e) => store.setTrack(e.target.checked)} />
              Track opens
            </label>
            <label className="flex items-center gap-1 text-xs text-slate-500" title="Newsletter: enables one-click unsubscribe handling">
              <input
                type="checkbox"
                checked={store.newsletter}
                onChange={(e) => store.setNewsletter(e.target.checked)}
              />
              Newsletter
            </label>
          </div>
          {store.attachments.length > 0 && (
            <ul className="mt-2 flex flex-wrap gap-1.5">
              {store.attachments.map((a) => (
                <li
                  key={a.localId}
                  className="flex items-center gap-1.5 rounded-md bg-slate-100 px-2 py-1 text-xs text-slate-600"
                >
                  📎 <span className="max-w-40 truncate">{a.filename}</span>
                  <span className="text-slate-400">{formatBytes(a.size)}</span>
                  <button
                    className="text-slate-400 hover:text-red-500"
                    onClick={() => store.removeAttachment(a.localId)}
                    aria-label={`Remove ${a.filename}`}
                  >
                    ✕
                  </button>
                </li>
              ))}
            </ul>
          )}
          {warnRisky.length > 0 && (
            <p className="mt-1.5 text-[11px] text-amber-600" role="alert">
              ⚠️ Executable attachment{warnRisky.length > 1 ? 's' : ''} ({warnRisky.join(', ')}) are
              blocked by most recipients' spam filters and are a common malware vector. Consider zipping
              or sending a download link.
            </p>
          )}
        </div>
      </div>

      <footer className="flex items-center justify-between border-t border-slate-200 px-4 py-2.5">
        <div className="text-[11px] text-slate-400">
          Sending as {fromValue || me?.user.email}
        </div>
        <div className="flex items-center gap-2">
          <Button variant="secondary" onClick={store.discard}>
            Discard
          </Button>
          <Button
            loading={store.sending}
            onClick={() =>
              store
                .submit()
                .then((sent) => {
                  if (sent && editor) editor.commands.setContent('<p></p>')
                })
                .catch((e) => toastError(e, 'Send failed'))
            }
          >
            Send ➤
          </Button>
        </div>
      </footer>
    </div>
  )
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3 px-4 py-1.5 text-sm">
      <span className="w-14 shrink-0 text-right text-xs text-slate-400">{label}</span>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  )
}

function ToolbarButton({
  editor,
  action,
  label,
  bold,
  italic,
  underline,
}: {
  editor: { isActive: (name: string, attrs?: Record<string, unknown>) => boolean }
  action: () => void
  label: string
  bold?: boolean
  italic?: boolean
  underline?: boolean
}) {
  const active =
    (bold && editor.isActive('bold')) ||
    (italic && editor.isActive('italic')) ||
    (underline && editor.isActive('underline')) ||
    (!bold && !italic && !underline && (editor.isActive('bulletList') || editor.isActive('blockquote')))
  return (
    <button
      type="button"
      onMouseDown={(e) => e.preventDefault()}
      onClick={action}
      className={`rounded px-1.5 py-0.5 font-medium hover:bg-slate-100 ${active ? 'bg-brand-50 text-brand-700' : ''} ${
        bold ? 'font-bold' : ''
      } ${italic ? 'italic' : ''} ${underline ? 'underline' : ''}`}
    >
      {label}
    </button>
  )
}

// ---- recipient chips (tokenized input) ----

function RecipientChips({
  addrs,
  onChange,
  placeholder,
}: {
  addrs: Addr[]
  onChange: (a: Addr[]) => void
  placeholder?: string
}) {
  const [raw, setRaw] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  const commit = () => {
    const parsed = parseAddrInput(raw)
    if (!parsed) {
      if (raw.trim()) toastError({ message: `"${raw.trim()}" is not a valid email address` })
      return
    }
    if (addrs.some((a) => a.email.toLowerCase() === parsed.email.toLowerCase())) {
      setRaw('')
      return
    }
    onChange([...addrs, parsed])
    setRaw('')
  }

  return (
    <div
      className="flex flex-wrap items-center gap-1"
      onClick={() => inputRef.current?.focus()}
    >
      {addrs.map((a) => (
        <span
          key={a.email}
          className="inline-flex max-w-56 items-center gap-1 rounded-full bg-brand-50 py-0.5 pl-1 pr-2 text-xs font-medium text-brand-800"
          title={a.name ? `${a.name} <${a.email}>` : a.email}
        >
          <span className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-brand-200 text-[8px] font-bold">
            {initials(a.name, a.email)}
          </span>
          <span className="truncate">{a.email}</span>
          <button
            className="text-brand-400 hover:text-brand-700"
            onClick={(e) => {
              e.stopPropagation()
              onChange(addrs.filter((x) => x.email !== a.email))
            }}
            aria-label={`Remove ${a.email}`}
          >
            ✕
          </button>
        </span>
      ))}
      <input
        ref={inputRef}
        value={raw}
        onChange={(e) => setRaw(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ',') {
            e.preventDefault()
            commit()
          } else if (e.key === 'Backspace' && !raw && addrs.length) {
            onChange(addrs.slice(0, -1))
          }
        }}
        onBlur={commit}
        placeholder={placeholder}
        className="min-w-32 flex-1 border-0 py-0.5 text-sm outline-none placeholder:text-slate-300"
        aria-label={placeholder}
      />
      {raw && !isValidEmail(raw) && !raw.includes('<') && (
        <span className="text-[10px] text-amber-600">press Enter to add</span>
      )}
    </div>
  )
}
