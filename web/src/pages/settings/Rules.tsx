// Rules (§12): match_from / match_subject → folder + mark-read. The API
// exposes create/delete only (no PATCH, no server-side dry-run), so the
// catalog's "test runner" is implemented as an honest client-side preview:
// it replays each rule against the newest messages already loaded in the
// mailbox store and shows what WOULD move.

import { useCallback, useEffect, useMemo, useState } from 'react'
import { featuresApi } from '@/api/endpoints'
import { useAuth } from '@/stores/auth'
import { useMail } from '@/stores/mail'
import { useUI, toastError } from '@/stores/ui'
import type { Folder, ItemWithMessage, Rule } from '@/types/api'
import { PageHeader, Card } from '@/ui/data'
import { Badge, EmptyState, LoadingState } from '@/ui/feedback'
import { Button, Field, Input, Select } from '@/ui/primitives'

export default function Rules() {
  const me = useAuth((s) => s.me)
  const mailbox = me?.mailboxes?.[0]
  const { folders, pages, loadFolder } = useMail()
  const [rules, setRules] = useState<Rule[] | null>(null)
  const [preview, setPreview] = useState(false)

  const load = useCallback(() => {
    if (!mailbox) return
    featuresApi
      .rules(mailbox.id)
      .then((r) => setRules(r.rules))
      .catch((e) => toastError(e, 'Could not load rules'))
  }, [mailbox])

  useEffect(load, [load])

  // The preview needs real messages: load the inbox page (cached — a re-run
  // only refetches when the store is empty).
  useEffect(() => {
    if (!preview || !mailbox) return
    const inbox = folders.find((f) => f.special_use.toLowerCase().includes('inbox'))
    if (!inbox) return
    if (!pages[inbox.id]) void loadFolder(inbox.id, { reset: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [preview, mailbox])

  if (!mailbox) return <LoadingState label="No mailbox on this account…" />
  if (!rules) return <LoadingState />

  return (
    <div className="mx-auto max-w-3xl p-6">
      <PageHeader
        title="Mail rules"
        subtitle="Applied server-side as mail is delivered. Order matters — first match wins."
        actions={
          <Button variant="secondary" size="sm" onClick={() => setPreview((v) => !v)}>
            {preview ? 'Hide test run' : 'Test rules on real mail'}
          </Button>
        }
      />
      <CreateCard folders={folders} onCreated={load} />
      <Card title={`Rules (${rules.length})`} className="mt-4">
        {rules.length === 0 ? (
          <EmptyState title="No rules" detail="Create a rule to route incoming mail automatically." />
        ) : (
          <ol className="divide-y divide-slate-100">
            {rules.map((r, i) => (
              <li key={r.id} className="flex items-center justify-between gap-3 py-2.5 text-sm">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="text-xs font-semibold text-slate-400">#{i + 1}</span>
                    {r.match_from && <Chip>from contains “{r.match_from}”</Chip>}
                    {r.match_subject && <Chip>subject contains “{r.match_subject}”</Chip>}
                    <span className="text-slate-400">→</span>
                    {r.action_folder_id && (
                      <Badge tone="brand">
                        {folders.find((f) => f.id === r.action_folder_id)?.name ?? 'folder'}
                      </Badge>
                    )}
                    {r.action_mark_read && <Badge>mark read</Badge>}
                    {!r.enabled && <Badge tone="amber">disabled</Badge>}
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    if (!window.confirm('Delete this rule?')) return
                    featuresApi
                      .deleteRule(mailbox.id, r.id)
                      .then(() => {
                        useUI.getState().toast('success', 'Rule deleted')
                        load()
                      })
                      .catch((e) => toastError(e, 'Delete failed'))
                  }}
                >
                  🗑️
                </Button>
              </li>
            ))}
          </ol>
        )}
      </Card>

      {preview && <TestRunner rules={rules} folders={folders} />}
    </div>
  )
}

function Chip({ children }: { children: React.ReactNode }) {
  return (
    <span className="rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-600">{children}</span>
  )
}

// ---- client-side test runner (honest local preview, no server dry-run) ----

function TestRunner({ rules, folders }: { rules: Rule[]; folders: Folder[] }) {
  const pages = useMail((s) => s.pages)
  const inbox = folders.find((f) => f.special_use.toLowerCase().includes('inbox'))
  const items: ItemWithMessage[] | undefined = inbox ? pages[inbox.id]?.items : undefined

  const results = useMemo(() => {
    if (!items) return null
    return items.slice(0, 25).map((im) => {
      const hit = rules.find(
        (r) =>
          r.enabled &&
          ((r.match_from && im.message.from.email.toLowerCase().includes(r.match_from.toLowerCase())) ||
            (r.match_subject && im.message.subject.toLowerCase().includes(r.match_subject.toLowerCase())))
      )
      return { im, hit }
    })
  }, [items, rules])

  return (
    <Card title="Test run — newest 25 inbox messages" className="mt-4">
      {!results ? (
        <LoadingState label="Loading inbox for preview…" />
      ) : results.every((r) => !r.hit) ? (
        <p className="text-sm text-slate-500">
          No rule matches any of the newest {results.length} messages. Try broadening a condition.
        </p>
      ) : (
        <ul className="divide-y divide-slate-100 text-sm">
          {results
            .filter((r) => r.hit)
            .map(({ im, hit }) => (
              <li key={im.item.id} className="flex items-center justify-between gap-3 py-2">
                <div className="min-w-0">
                  <div className="truncate font-medium text-slate-700">{im.message.subject || '(no subject)'}</div>
                  <div className="truncate text-xs text-slate-400">{im.message.from.email}</div>
                </div>
                <div className="flex shrink-0 items-center gap-1.5">
                  <Badge tone="brand">rule #{rules.indexOf(hit!) + 1}</Badge>
                  {hit!.action_folder_id && (
                    <Badge>{folders.find((f) => f.id === hit!.action_folder_id)?.name}</Badge>
                  )}
                  {hit!.action_mark_read && <Badge>read</Badge>}
                </div>
              </li>
            ))}
        </ul>
      )}
      <p className="mt-2 text-[11px] text-slate-400">
        This preview evaluates locally against the loaded page only — the server applies rules at
        delivery time across every message.
      </p>
    </Card>
  )
}

function CreateCard({ folders, onCreated }: { folders: Folder[]; onCreated: () => void }) {
  const me = useAuth.getState().me
  const [matchFrom, setMatchFrom] = useState('')
  const [matchSubject, setMatchSubject] = useState('')
  const [actionFolderId, setActionFolderId] = useState('')
  const [markRead, setMarkRead] = useState(false)
  const [busy, setBusy] = useState(false)
  const mailbox = me?.mailboxes?.[0]

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!mailbox) return
    if (!matchFrom.trim() && !matchSubject.trim()) {
      toastError({ message: 'At least one condition is required' })
      return
    }
    setBusy(true)
    try {
      await featuresApi.createRule(mailbox.id, {
        match_from: matchFrom.trim() || undefined,
        match_subject: matchSubject.trim() || undefined,
        action_folder_id: actionFolderId || undefined,
        action_mark_read: markRead,
        enabled: true,
      })
      useUI.getState().toast('success', 'Rule created')
      setMatchFrom('')
      setMatchSubject('')
      setActionFolderId('')
      setMarkRead(false)
      onCreated()
    } catch (err) {
      toastError(err, 'Create failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title="New rule">
      <form onSubmit={submit} className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field label="From contains" hint="sender email fragment">
          <Input value={matchFrom} onChange={(e) => setMatchFrom(e.target.value)} placeholder="newsletter@" />
        </Field>
        <Field label="Subject contains">
          <Input value={matchSubject} onChange={(e) => setMatchSubject(e.target.value)} placeholder="invoice" />
        </Field>
        <Field label="Then move to folder">
          <Select value={actionFolderId} onChange={(e) => setActionFolderId(e.target.value)}>
            <option value="">— leave in place —</option>
            {folders.map((f) => (
              <option key={f.id} value={f.id}>
                {f.name}
              </option>
            ))}
          </Select>
        </Field>
        <label className="flex items-end gap-2 pb-2 text-sm text-slate-600">
          <input type="checkbox" checked={markRead} onChange={(e) => setMarkRead(e.target.checked)} />
          Mark as read
        </label>
        <div className="sm:col-span-2">
          <Button type="submit" loading={busy}>
            Create rule
          </Button>
        </div>
      </form>
    </Card>
  )
}
