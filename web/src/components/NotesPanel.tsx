import { useState } from 'react';
import { useCreateNote, useDeleteNote, useNotes } from '@/hooks/queries/notes';
import { Button, Checkbox, EmptyState, Skeleton, Textarea } from '@/design';
import { Icon } from '@/design/icons';
import { formatDateTime } from '@/lib/time';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';

interface Props {
  scope: { messageId?: string; threadId?: string };
  title?: string;
}

/**
 * Private and shared notes on a message or thread. Notes are never exposed
 * on forward or reply — this is enforced by the API, and shown here so the
 * user understands the boundary.
 */
export function NotesPanel({ scope, title = 'Notes' }: Props) {
  const q = useNotes(scope);
  const create = useCreateNote(scope);
  const del = useDeleteNote(scope);
  const [body, setBody] = useState('');
  const [isPrivate, setIsPrivate] = useState(true);

  async function submit() {
    const text = body.trim();
    if (!text) return;
    try {
      await create.mutateAsync({ body: text, isPrivate });
      setBody('');
    } catch (e) {
      toast('error', 'Could not save note', errorMessage(e));
    }
  }

  return (
    <section className="border-t border-line mt-4 pt-4">
      <div className="flex items-center gap-2 mb-2">
        <Icon name="sparkles" size={14} className="text-ink-mute" />
        <h3 className="text-sm font-medium">{title}</h3>
        <span className="text-xs text-ink-mute">— private and never forwarded</span>
      </div>

      <div className="space-y-2">
        <Textarea
          value={body}
          onChange={(e) => setBody(e.target.value)}
          rows={2}
          placeholder="Add a note…"
          className="text-sm"
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') { e.preventDefault(); void submit(); }
          }}
        />
        <div className="flex items-center justify-between gap-2">
          <Checkbox
            label="Private (only visible to me)"
            checked={isPrivate}
            onChange={(e) => setIsPrivate(e.target.checked)}
          />
          <Button
            size="sm" variant="primary"
            onClick={submit}
            loading={create.isPending}
            disabled={!body.trim()}
          >Save note</Button>
        </div>
      </div>

      {q.isLoading && <Skeleton className="h-12 w-full mt-3" />}

      {q.data && q.data.length > 0 && (
        <ul className="space-y-2 mt-3">
          {q.data.map((n) => (
            <li key={n.id} className="border border-line rounded p-2 bg-canvas text-sm">
              <div className="flex items-start gap-2">
                <p className="flex-1 whitespace-pre-wrap break-words">{n.body}</p>
                <button
                  onClick={() => void del.mutateAsync(n.id)}
                  className="text-ink-mute hover:text-danger p-0.5"
                  aria-label="Delete note"
                >
                  <Icon name="trash" size={12} />
                </button>
              </div>
              <p className="text-xs text-ink-mute mt-1">
                {n.author} · {formatDateTime(n.created_at)}
                {n.private && ' · private'}
              </p>
            </li>
          ))}
        </ul>
      )}

      {q.data && q.data.length === 0 && !q.isLoading && (
        <EmptyState title="No notes yet" />
      )}
    </section>
  );
}
