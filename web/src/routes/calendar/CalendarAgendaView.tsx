import { useMemo } from 'react';
import { formatDayLong, isSameDay } from '@/lib/dates';
import { Icon } from '@/design/icons';
import type { Calendar, CalendarEvent } from '@/lib/types';

interface Props {
  events: CalendarEvent[];
  calendars: Calendar[];
  onEventClick: (e: CalendarEvent) => void;
}

export function CalendarAgendaView({ events, calendars, onEventClick }: Props) {
  const calendarMap = useMemo(() => new Map(calendars.map((c) => [c.id, c])), [calendars]);
  const grouped = useMemo(() => {
    const sorted = [...events].sort((a, b) => new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime());
    const groups: Array<{ date: Date; items: CalendarEvent[] }> = [];
    for (const ev of sorted) {
      const d = new Date(ev.starts_at);
      const last = groups[groups.length - 1];
      if (last && isSameDay(last.date, d)) last.items.push(ev);
      else groups.push({ date: d, items: [ev] });
    }
    return groups;
  }, [events]);

  if (events.length === 0) {
    return (
      <div className="h-full flex items-center justify-center p-6">
        <div className="text-center text-sm text-ink-soft">
          <Icon name="mail" size={28} className="mx-auto mb-2 text-ink-mute" />
          <p>No upcoming events.</p>
        </div>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto scroll-thin">
      <div className="max-w-2xl mx-auto p-4 space-y-6">
        {grouped.map((g, gi) => (
          <section key={gi}>
            <h2 className="text-xs uppercase text-ink-mute mb-2">{formatDayLong(g.date)}</h2>
            <ul className="space-y-2">
              {g.items.map((ev) => {
                const cal = calendarMap.get(ev.calendar_id);
                return (
                  <li key={ev.id}>
                    <button
                      onClick={() => onEventClick(ev)}
                      className="w-full flex items-start gap-3 p-3 border border-line rounded hover:border-accent"
                    >
                      <div className="shrink-0 w-20 text-sm">
                        {ev.all_day ? (
                          <span className="text-ink-mute">All day</span>
                        ) : (
                          <span className="font-mono">
                            {new Date(ev.starts_at).toLocaleTimeString('en-ZA', { hour: '2-digit', minute: '2-digit', hour12: false })}
                          </span>
                        )}
                      </div>
                      <div className="flex-1 min-w-0 text-left">
                        <p className="font-medium truncate flex items-center gap-2">
                          <span
                            className="w-2 h-2 rounded-full shrink-0"
                            style={{ backgroundColor: cal?.color ?? '#0f766e' }}
                            aria-hidden
                          />
                          {ev.title}
                        </p>
                        {ev.location && <p className="text-xs text-ink-soft truncate">{ev.location}</p>}
                        {ev.attendees.length > 0 && (
                          <p className="text-xs text-ink-mute truncate">
                            {ev.attendees.length} attendee{ev.attendees.length === 1 ? '' : 's'}
                          </p>
                        )}
                      </div>
                      {ev.rsvp !== 'needs_action' && ev.rsvp !== 'accepted' && (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-canvas border border-line text-ink-soft">{ev.rsvp}</span>
                      )}
                    </button>
                  </li>
                );
              })}
            </ul>
          </section>
        ))}
      </div>
    </div>
  );
}
