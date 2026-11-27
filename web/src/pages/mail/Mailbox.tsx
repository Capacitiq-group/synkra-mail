// The mailbox route: list (left) + reading pane (right). The URL is the
// state — /mail/:folderId/:messageId — so back/forward and deep links work.

import { useEffect } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useMail } from '@/stores/mail'
import { MessageList } from '@/components/mail/MessageList'
import { ReadingPane } from '@/components/mail/ReadingPane'
import { Button } from '@/ui/primitives'
import { EmptyState } from '@/ui/feedback'
import type { ItemWithMessage } from '@/types/api'

export default function Mailbox() {
  const { folderId, messageId } = useParams()
  const navigate = useNavigate()
  const { folders, pages, detail, detailLoading, loadOlder, openMessage, clearMessage } = useMail()

  const page = (folderId && pages[folderId]) || null

  useEffect(() => {
    if (!messageId) clearMessage()
  }, [messageId, clearMessage])

  useEffect(() => {
    if (!messageId) return
    void openMessage(messageId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [messageId])

  const select = (it: ItemWithMessage) => {
    navigate(`/mail/${folderId}/${it.item.id}`)
  }

  if (!folderId || !page)
    return (
      <div className="flex h-full">
        <div className="flex w-96 min-w-80 flex-col border-r border-slate-200 bg-white">
          <EmptyState title="Select a folder" />
        </div>
        <div className="flex-1 bg-white" />
      </div>
    )

  const folderName = page.folder?.name ?? 'Mailbox'

  return (
    <div className="flex h-full min-h-0">
      <div className="flex w-96 min-w-80 shrink-0 flex-col border-r border-slate-200 bg-white">
        <MessageList
          title={folderName}
          items={page.items}
          hasMore={page.hasMore}
          loading={page.loading}
          error={page.error}
          selectedId={messageId}
          onSelect={select}
          onLoadOlder={() => void loadOlder()}
          emptyTitle="No messages"
          emptyDetail="This folder is empty — new mail appears automatically."
          onEmptyAction={
            page.error ? (
              <Button variant="secondary" size="sm" onClick={() => void loadOlder()}>
                Refresh
              </Button>
            ) : undefined
          }
        />
      </div>

      <div className="min-w-0 flex-1">
        {detailLoading ? (
          <div className="flex h-full items-center justify-center text-sm text-slate-400">
            Loading message…
          </div>
        ) : detail ? (
          <ReadingPane
            detail={detail}
            folders={folders}
            onClose={() => navigate(`/mail/${folderId}`)}
          />
        ) : (
          <div className="flex h-full items-center justify-center bg-white text-sm text-slate-400">
            Select a message to read
          </div>
        )}
      </div>
    </div>
  )
}
