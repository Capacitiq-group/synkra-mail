// Signatures (§12): CRUD + the catalog's HTML signature linter — checks the
// authored HTML for scripts, external/tracking images, huge tables, missing
// alt text and inline style overuse before it ships in every mail you send.

import { useCallback, useEffect, useState } from 'react'
import { featuresApi } from '@/api/endpoints'
import { useUI, toastError } from '@/stores/ui'
import { sanitizeSignatureHTML } from '@/lib/sanitize'
import type { Signature } from '@/types/api'
import { PageHeader, Card } from '@/ui/data'
import { Badge, EmptyState, LoadingState } from '@/ui/feedback'
import { Button, Field, Input, Textarea } from '@/ui/primitives'

export interface LintIssue {
  severity: 'error' | 'warn' | 'info'
  message: string
}

export function lintSignatureHTML(html: string): LintIssue[] {
  const issues: LintIssue[] = []
  const lower = html.toLowerCase()
  if (/<script|on\w+\s*=|javascript:/i.test(html))
    issues.push({ severity: 'error', message: 'Script content or inline handlers detected — stripped on save, and spam filters flag signatures with scripts.' })
  if (lower.includes('<img')) {
    const srcs = [...html.matchAll(/<img[^>]+src=["']([^"']+)["']/gi)].map((m) => m[1])
    for (const src of srcs) {
      if (/^https?:/i.test(src))
        issues.push({ severity: 'warn', message: `External image (${new URL(src, 'https://x').host}) — hotlinked images break offline and can leak opens; prefer a data: URI or hosted path.` })
      if (src.startsWith('data:') && src.length > 100_000)
        issues.push({ severity: 'warn', message: 'Embedded image over ~100 KB inflates every message you send.' })
    }
    if (/<img(?![^>]*alt=)/i.test(html))
      issues.push({ severity: 'info', message: 'One or more images have no alt text.' })
  }
  if (/<table[^>]*>/i.test(lower) && (html.match(/<table/gi)?.length ?? 0) > 1)
    issues.push({ severity: 'info', message: 'Multiple nested tables — complex table layouts render inconsistently on mobile clients.' })
  if (/font-size:\s*(4[0-9]|[5-9]\d)px/i.test(lower))
    issues.push({ severity: 'warn', message: 'Very large font sizes (≥40px) look broken in desktop clients.' })
  if (html.length > 20_000)
    issues.push({ severity: 'warn', message: `Signature is ${(html.length / 1000).toFixed(1)} KB — keep it under 20 KB; it ships with every message.` })
  if (/!important/i.test(lower))
    issues.push({ severity: 'info', message: '!important rules — some clients strip them; rely on inline styles instead.' })
  return issues
}

export default function Signatures() {
  const [signatures, setSignatures] = useState<Signature[] | null>(null)

  const load = useCallback(() => {
    featuresApi
      .signatures()
      .then((r) => setSignatures(r.signatures))
      .catch((e) => toastError(e, 'Could not load signatures'))
  }, [])

  useEffect(load, [load])

  if (!signatures) return <LoadingState />

  return (
    <div className="mx-auto max-w-3xl p-6">
      <PageHeader
        title="Signatures"
        subtitle="HTML signatures appended to outgoing mail. The linter runs live as you type."
      />
      <CreateCard onCreated={load} />
      <Card title={`Saved (${signatures.length})`} className="mt-4">
        {signatures.length === 0 ? (
          <EmptyState title="No signatures" detail="Create one — it can be auto-inserted in the composer." />
        ) : (
          <ul className="divide-y divide-slate-100">
            {signatures.map((s) => (
              <li key={s.id} className="py-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-slate-800">{s.name}</span>
                    {s.is_default && <Badge tone="brand">default</Badge>}
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      if (!window.confirm(`Delete signature "${s.name}"?`)) return
                      featuresApi
                        .deleteSignature(s.id)
                        .then(() => {
                          useUI.getState().toast('success', 'Signature deleted')
                          load()
                        })
                        .catch((e) => toastError(e, 'Delete failed'))
                    }}
                  >
                    🗑️
                  </Button>
                </div>
                <div
                  className="mt-2 rounded-lg border border-slate-100 bg-slate-50 p-3 text-sm [&_a]:text-brand-600"
                  dangerouslySetInnerHTML={{ __html: sanitizeSignatureHTML(s.html) }}
                />
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  )
}

function CreateCard({ onCreated }: { onCreated: () => void }) {
  const [name, setName] = useState('')
  const [html, setHtml] = useState('')
  const [isDefault, setIsDefault] = useState(false)
  const [busy, setBusy] = useState(false)
  const issues = lintSignatureHTML(html)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (issues.some((i) => i.severity === 'error')) {
      if (!window.confirm('The HTML has blocking issues (scripts/handlers). They will be stripped — continue?'))
        return
    }
    setBusy(true)
    try {
      await featuresApi.createSignature(name.trim(), sanitizeSignatureHTML(html), htmlToText(html), isDefault)
      useUI.getState().toast('success', 'Signature saved')
      setName('')
      setHtml('')
      setIsDefault(false)
      onCreated()
    } catch (err) {
      toastError(err, 'Save failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title="New signature">
      <form onSubmit={submit} className="flex flex-col gap-3">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Name" required>
            <Input value={name} onChange={(e) => setName(e.target.value)} required placeholder="Work signature" />
          </Field>
          <label className="flex items-end gap-2 pb-2 text-sm text-slate-600">
            <input type="checkbox" checked={isDefault} onChange={(e) => setIsDefault(e.target.checked)} />
            Use as default in new mail
          </label>
        </div>
        <Field label="HTML" hint="paste your signature HTML or type plain markup">
          <Textarea
            value={html}
            onChange={(e) => setHtml(e.target.value)}
            className="font-mono text-xs"
            rows={6}
            placeholder={'<p>Thabo Mokoena<br/>Founder — Acme (Pty) Ltd<br/><a href="https://acme.co.za">acme.co.za</a></p>'}
          />
        </Field>

        {html && (
          <div className="rounded-lg border border-slate-200 p-3">
            <h3 className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-slate-400">
              Signature linter
            </h3>
            {issues.length === 0 ? (
              <p className="text-xs text-emerald-600">✓ No issues found — clean signature.</p>
            ) : (
              <ul className="flex flex-col gap-1 text-xs">
                {issues.map((i, idx) => (
                  <li key={idx} className={i.severity === 'error' ? 'text-red-600' : i.severity === 'warn' ? 'text-amber-600' : 'text-slate-500'}>
                    {i.severity === 'error' ? '⛔' : i.severity === 'warn' ? '⚠️' : 'ℹ️'} {i.message}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}

        <Button type="submit" loading={busy} disabled={!name.trim() || !html} className="w-fit">
          Save signature
        </Button>
      </form>
    </Card>
  )
}

function htmlToText(html: string): string {
  const div = document.createElement('div')
  div.innerHTML = html.replace(/<br\s*\/?>/gi, '\n').replace(/<\/(p|div)>/gi, '\n')
  return (div.textContent ?? '').trim()
}
