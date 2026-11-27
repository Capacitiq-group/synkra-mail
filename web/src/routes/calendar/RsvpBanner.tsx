import { useState } from 'react';
import { useRsvpEvent } from '@/hooks/queries/calendar';
import { Button, Textarea } from '@/design';
import { Icon } from '@/design/icons';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';
import { formatDateTime } from '@/lib/time';
import type { CalendarEvent } from '@/lib/types';

interface Props {
  event: CalendarEvent;
}

export function RsvpBanner({ event }: Props) {
  const rsvp = useRsvpEvent();
  const [showMessage, setShowMessage] = useState(false);
  const [message, setMessage] = useState('');
  const [done, setDone] = useState<string | null>(null);

  async function submit(response: 'accepted' | 'declined' | 'tentative') {
    try {
      await rsvp.mutateAsync({ id: event.id, response, message: message.trim() || undefined });
      setDone(response);
      toast('success', response === 'accepted' ? 'Accepted' : response === 'declined' ? 'Declined' : 'Tentatively accepted');
    } catch (e) {
      toast('error', 'Could not update RSVP', errorMessage(e));
    }
  }

  return (
    <div className="mb-3 p-3 border border-accent/40 bg-accent-soft rounded-lg text-sm">
      <div className="flex items-start gap-2 mb-2">
        <Icon name="mail" size={14} className="text-accent mt-0.5" />
        <div className="flex-1">
          <p className="font-medium">{event.title}</p>
          <p className="text-xs text-ink-soft">
            {formatDateTime(event.starts_at)} – {new Date(event.ends_at).toLocaleTimeString('en-ZA', { hour: '2-digit', minute: '2-digit' })}
            {event.location && ` · ${event.location}`}
          </p>
        </div>
      </div>

      {done ? (
        <p className="text-xs">
          {done === 'accepted' ? 'You accepted this invitation.' : done === 'declined' ? 'You declined this invitation.' : 'You tentatively accepted.'}
        </p>
      ) : (
        <>
          <div className="flex gap-2 flex-wrap">
            <Button size="sm" variant="primary" onClick={() => submit('accepted')} loading={rsvp.isPending}>Accept</Button>
            <Button size="sm" variant="secondary" onClick={() => submit('tentative')}>Tentative</Button>
            <Button size="sm" variant="secondary" onClick={() => submit('declined')}>Decline</Button>
            <Button size="sm" variant="ghost" onClick={() => setShowMessage(!showMessage)}>
              {showMessage ? 'Hide message' : 'Add a message'}
            </Button>
          </div>
          {showMessage && (
            <Textarea
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              rows={2}
              placeholder="Optional message to the organiser"
              className="mt-2 text-sm"
            />
          )}
        </>
      )}
    </div>
  );
}
