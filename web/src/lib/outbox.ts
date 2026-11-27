// Offline send outbox. The backend's Idempotency-Key replay (register §1.9)
// is what makes this safe: a queued send keeps its original key, so flushing
// it twice can never double-deliver — the API replays the first response.

import type { SendInput } from '@/types/api'

const OUTBOX_KEY = 'synkra.outbox.v1'

export interface OutboxEntry {
  idemKey: string
  input: SendInput
  queuedAt: string
}

let entries: OutboxEntry[] = load()

function load(): OutboxEntry[] {
  try {
    const raw = localStorage.getItem(OUTBOX_KEY)
    return raw ? (JSON.parse(raw) as OutboxEntry[]) : []
  } catch {
    return []
  }
}

function persist(): void {
  try {
    localStorage.setItem(OUTBOX_KEY, JSON.stringify(entries))
  } catch {
    /* storage full/blocked — outbox lives in memory for this tab */
  }
}

const listeners = new Set<() => void>()
function notify(): void {
  for (const l of listeners) l()
}

export const outbox = {
  enqueue(idemKey: string, input: SendInput): void {
    entries.push({ idemKey, input, queuedAt: new Date().toISOString() })
    persist()
    notify()
  },
  pending(): number {
    return entries.length
  },
  subscribe(fn: () => void): () => void {
    listeners.add(fn)
    return () => listeners.delete(fn)
  },
  /** Attempt every queued send in order; keys make retries idempotent. */
  async flush(send: (input: SendInput, idemKey: string) => Promise<unknown>): Promise<number> {
    let delivered = 0
    const remaining: OutboxEntry[] = []
    for (const e of entries) {
      try {
        await send(e.input, e.idemKey)
        delivered++
      } catch (err) {
        // Keep entries that are still unsent (network down, 5xx). Permanent
        // client errors (4xx) are dropped — they would fail again forever —
        // and the caller surfaces a notice.
        const status = (err as { status?: number }).status ?? 0
        if (status === 0 || status >= 500) remaining.push(e)
      }
    }
    if (delivered > 0 || remaining.length !== entries.length) {
      entries = remaining
      persist()
      notify()
    }
    return delivered
  },
}
