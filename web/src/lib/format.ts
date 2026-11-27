// Formatting helpers — the small glue the whole UI leans on.

export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  const v = n / 1024 ** i
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

export function formatDate(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}

/** List rows: today → time, this year → day+month, older → date. */
export function formatListDate(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const now = new Date()
  const sameDay = d.toDateString() === now.toDateString()
  if (sameDay) return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  if (d.getFullYear() === now.getFullYear())
    return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}

export function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString(undefined, {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function formatPercent(fraction: number): string {
  return `${Math.round(fraction * 100)}%`
}

/** "On <date>, <name> <<email>> wrote:" — the reply quote header. */
export function quoteHeader(from: { name?: string; email: string }, iso: string): string {
  const d = new Date(iso)
  const when = Number.isNaN(d.getTime()) ? 'an unknown date' : d.toLocaleString()
  const who = from.name ? `${from.name} <${from.email}>` : from.email
  return `On ${when}, ${who} wrote:`
}

export function initials(name: string | undefined, email: string): string {
  const src = (name || email || '?').trim()
  const parts = src.split(/[\s._@-]+/).filter(Boolean)
  return ((parts[0]?.[0] ?? '?') + (parts[1]?.[0] ?? '')).toUpperCase()
}

const EMAIL_RE = /^[^\s@]+@[^\s@.]+\.[^\s@]+$/
export function isValidEmail(s: string): boolean {
  return EMAIL_RE.test(s.trim())
}

/** Parse "Name <a@b.c>" / bare "a@b.c" into an Addr. Returns null when unparseable. */
export function parseAddrInput(raw: string): { name?: string; email: string } | null {
  const s = raw.trim()
  if (!s) return null
  const m = s.match(/^(.*)<([^<>]+)>$/)
  if (m) {
    const email = m[2].trim()
    const name = m[1].trim().replace(/^["']|["']$/g, '')
    return EMAIL_RE.test(email) ? { email, name: name || undefined } : null
  }
  return EMAIL_RE.test(s) ? { email: s } : null
}
