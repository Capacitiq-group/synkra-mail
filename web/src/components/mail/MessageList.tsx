// Message list rows with cursor pagination ("Load older") — the API pages
// newest-first by UID with before_uid cursors (register §1.8).

import type { ItemWithMessage } from '@/types/api'
import { formatListDate, initials } from '@/lib/format'
import { Button, Spinner } from '@/ui/primitives'
import { EmptyState, ErrorState } from '@/ui/feedback'

export function MessageList({
  title,
  items,
  hasMore,
  loading,
  error,
  selectedId,
  onSelect,
  onLoadOlder,
  emptyTitle,
  emptyDetail,
  onEmptyAction,
}: {
  title: string
  items: ItemWithMessage[]
  hasMore?: boolean
  loading?: boolean
  error?: string | null
  selectedId?: string | null
  onSelect: (item: ItemWithMessage) => void
  onLoadOlder?: () => void
  emptyTitle: string
  emptyDetail?: string
  onEmptyAction?: React.ReactNode
}) {
  if (error) return <ErrorState message={error} onRetry={onLoadOlder} />
  if (!items.length && !loading)
    return <EmptyState title={emptyTitle} detail={emptyDetail} action={onEmptyAction} />

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="border-b border-slate-200 px-4 py-2 text-xs font-semibold uppercase tracking-wide text-slate-400">
        {title}
        {loading && <Spinner className="ml-2 inline h-3 w-3" />}
      </div>
      <ul className="min-h-0 flex-1 divide-y divide-slate-100 overflow-y-auto">
        {items.map(({ item, message }) => {
          const unread = !item.flags.includes('\\Seen')
          const flagged = item.flags.includes('\\Flagged')
          return (
            <li key={item.id}>
              <button
                onClick={() => onSelect({ item, message })}
                className={`w-full px-4 py-2.5 text-left transition-colors ${
                  selectedId === item.id ? 'bg-brand-50' : 'hover:bg-slate-50'
                }`}
              >
                <div className="flex items-baseline gap-2">
                  <span
                    className={`truncate text-sm ${unread ? 'font-semibold text-slate-900' : 'text-slate-600'}`}
                  >
                    {message.from.name || message.from.email}
                  </span>
                  {unread && <span className="h-2 w-2 shrink-0 rounded-full bg-brand-500" aria-label="unread" />}
                  <span className="ml-auto shrink-0 text-[11px] text-slate-400">
                    {formatListDate(item.created_at)}
                  </span>
                </div>
                <div className="mt-0.5 flex items-baseline gap-1.5">
                  <span className={`truncate text-sm ${unread ? 'font-medium text-slate-800' : 'text-slate-700'}`}>
                    {message.subject || '(no subject)'}
                  </span>
                  {flagged && <span title="flagged">🚩</span>}
                  {message.has_attachments && (
                    <span title="has attachments" className="text-slate-400">
                      📎
                    </span>
                  )}
                  {message.direction === 'outbound' && (
                    <span title="sent by you" className="text-slate-300">
                      ↗
                    </span>
                  )}
                </div>
                <div className="mt-0.5 truncate text-xs text-slate-400">
                  <span className="mr-1.5 inline-flex h-4 w-4 items-center justify-center rounded-full bg-slate-200 align-middle text-[8px] font-bold text-slate-500">
                    {initials(message.from.name, message.from.email)}
                  </span>
                  {message.snippet}
                </div>
              </button>
            </li>
          )
        })}
      </ul>
      {hasMore && (
        <div className="border-t border-slate-100 p-2 text-center">
          <Button variant="secondary" size="sm" loading={loading} onClick={onLoadOlder}>
            Load older messages
          </Button>
        </div>
      )}
    </div>
  )
}
