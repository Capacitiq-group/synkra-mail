// Search: GET /search?q= runs across all mailboxes the user can access;
// results open in a local reading pane.

import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useMail } from '@/stores/mail'
import { mailApi } from '@/api/endpoints'
import { MessageList } from '@/components/mail/MessageList'
import { ReadingPane } from '@/components/mail/ReadingPane'
import { useUI } from '@/stores/ui'
import { Input, Button } from '@/ui/primitives'
import type { ItemWithMessage, MessageDetail } from '@/types/api'

export default function SearchResults() {
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const [input, setInput] = useState(q)
  const [results, setResults] = useState<ItemWithMessage[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [detail, setDetail] = useState<MessageDetail | null>(null)
  const folders = useMail((s) => s.folders)

  useEffect(() => {
    setInput(q)
    if (!q) {
      setResults(null)
      return
    }
    setLoading(true)
    mailApi
      .search(q)
      .then((r) => setResults(r.results))
      .catch((e) => {
        setResults([])
        useUI.getState().toast('error', 'Search failed', (e as Error).message)
      })
      .finally(() => setLoading(false))
  }, [q])

  useEffect(() => {
    const id = detail?.item.id
    if (results && id && !results.some((r) => r.item.id === id)) setDetail(null)
  }, [results, detail])

  const open = async (it: ItemWithMessage) => {
    setDetail(null)
    const d = await mailApi.message(it.item.message_id)
    setDetail(d)
    if (!it.item.flags.includes('\\Seen')) {
      const flags = [...it.item.flags, '\\Seen']
      try {
        await mailApi.setFlags(it.item.message_id, flags)
        it.item.flags = flags
        setResults((rs) => (rs ? [...rs] : rs))
      } catch {
        /* best-effort */
      }
    }
  }

  return (
    <div className="flex h-full min-h-0">
      <div className="flex w-96 min-w-80 shrink-0 flex-col border-r border-slate-200 bg-white">
        <form
          className="flex gap-2 border-b border-slate-200 p-3"
          onSubmit={(e) => {
            e.preventDefault()
            setParams(input.trim() ? { q: input.trim() } : {})
          }}
        >
          <Input value={input} onChange={(e) => setInput(e.target.value)} placeholder="Search mail…" />
          <Button type="submit" size="sm" loading={loading}>
            Search
          </Button>
        </form>
        <MessageList
          title={`Results for “${q}”`}
          items={results ?? []}
          loading={loading}
          selectedId={detail?.item.id}
          onSelect={(it) => void open(it)}
          emptyTitle={q ? 'No results' : 'Type a query'}
          emptyDetail={q ? 'Nothing matched. Search covers subjects, senders and body text.' : undefined}
        />
      </div>
      <div className="min-w-0 flex-1">
        {detail ? (
          <ReadingPane detail={detail} folders={folders} onClose={() => setDetail(null)} />
        ) : (
          <div className="flex h-full items-center justify-center bg-white text-sm text-slate-400">
            Select a result to read
          </div>
        )}
      </div>
    </div>
  )
}
