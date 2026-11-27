// SSE shim — the Go API has no browser push channel yet (the register notes
// polling as the interim contract). Same signature the layout expects: marks
// the connection "live" and drives cache invalidation on a polling interval
// instead of server events.
import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useSse } from '@/stores/sse'
import { qk } from '@/lib/queryKeys'

export function useSSE(enabled: boolean) {
  const qc = useQueryClient()
  const setConnected = useSse((s) => s.setConnected)
  const touch = useSse((s) => s.touch)

  useEffect(() => {
    if (!enabled) return
    setConnected(true)
    const interval = window.setInterval(() => {
      touch()
      // Refresh folder counters (unread badges) and mailbox metadata; the
      // open message list refetches through react-query staleness.
      qc.invalidateQueries({ queryKey: qk.mailboxes() as unknown as unknown[] })
      qc.invalidateQueries({ queryKey: ['folders'] })
    }, 30_000)
    return () => {
      window.clearInterval(interval)
      setConnected(false)
    }
  }, [enabled, qc, setConnected, touch])
}
