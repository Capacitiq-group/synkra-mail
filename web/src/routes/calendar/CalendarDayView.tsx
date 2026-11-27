import { useMemo } from 'react';
import { eventsOnDay, formatTimeShort, isToday, layoutOverlapping, minutesSinceMidnight, startOfDay } from '@/lib/dates';
import type { Calendar, CalendarEvent } from '@/lib/types';

interface Props {
  date: Date;
  events: CalendarEvent[];
  calendars: Calendar[];
  onEventClick: (e: CalendarEvent) => void;
  onCreateAt: (start: Date, end: Date) => void;
}

const HOUR_HEIGHT = 60;
const HOURS = 24;

export function CalendarDayView({ date, events, calendars, onEventClick, onCreateAt }: Props) {
  const calendarMap = useMemo(() => new Map(calendars.map((c) => [c.id, c])), [calendars]);
  const dayEvents = layoutOverlapping(eventsOnDay(events, date).filter((e) => !e.all_day));
  const allDay = eventsOnDay(events, date).filter((e) => e.all_day);
  const today = isToday(date);

  function onGridClick(e: React.MouseEvent<HTMLDivElement>) {
    if (e.target !== e.currentTarget) return;
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const y = e.clientY - rect.top;
    const minutes = Math.floor((y / HOUR_HEIGHT) * 60 / 15) * 15;
    const start = new Date(date);
    start.setHours(0, 0, 0, 0);
    start.setMinutes(minutes);
    const end = new Date(start.getTime() + 60 * 60 * 1000);
    onCreateAt(start, end);
  }

  return (
    <div className="h-full flex flex-col bg-surface">
      <div className={`px-4 py-2 border-b border-line ${today ? 'bg-accent-soft' : ''}`}>
        <div className="text-xs text-ink-soft">
          {date.toLocaleDateString('en-ZA', { weekday: 'long' })}
        </div>
        <div className={`text-lg font-semibold ${today ? 'text-accent' : ''}`}>{date.getDate()} {date.toLocaleDateString('en-ZA', { month: 'long' })}</div>
      </div>

      {allDay.length > 0 && (
        <div className="px-4 py-2 border-b border-line space-y-1">
          <p className="text-[10px] uppercase text-ink-mute">All day</p>
          {allDay.map((ev) => {
            const cal = calendarMap.get(ev.calendar_id);
            return (
              <button
                key={ev.id}
                onClick={() => onEventClick(ev)}
                className="w-full text-left px-2 py-1 rounded text-sm"
                style={{ backgroundColor: (cal?.color ?? '#0f766e') + '33', color: cal?.color ?? '#0f766e' }}
              >
                {ev.title}
              </button>
            );
          })}
        </div>
      )}

      <div className="flex-1 overflow-y-auto scroll-thin grid grid-cols-[60px,1fr]">
        <div>
          {Array.from({ length: HOURS }).map((_, h) => (
            <div key={h} style={{ height: HOUR_HEIGHT }} className="text-[10px] text-ink-mute text-right pr-1 border-b border-line">
              {String(h).padStart(2, '0')}:00
            </div>
          ))}
        </div>
        <div
          className="relative border-l border-line"
          onClick={onGridClick}
          style={{ height: HOUR_HEIGHT * HOURS }}
        >
          {Array.from({ length: HOURS }).map((_, h) => (
            <div key={h} className="border-b border-line" style={{ height: HOUR_HEIGHT }} />
          ))}
          {dayEvents.map((ev) => {
            const cal = calendarMap.get(ev.calendar_id);
            const start = new Date(ev.starts_at);
            const end = new Date(ev.ends_at);
            const dayStart = startOfDay(date);
            const startMin = Math.max(0, minutesSinceMidnight(start) + (start < dayStart ? 1440 : 0));
            const endMin = Math.min(1440, minutesSinceMidnight(end) + (end > new Date(dayStart.getTime() + 24 * 3600 * 1000) ? 1440 : 0));
            const top = (startMin / 60) * HOUR_HEIGHT;
            const height = Math.max(24, ((endMin - startMin) / 60) * HOUR_HEIGHT);
            const width = 100 / ev._lanes;
            const left = (ev._lane / ev._lanes) * 100;
            return (
              <button
                key={ev.id}
                onClick={(e) => { e.stopPropagation(); onEventClick(ev); }}
                className="absolute rounded px-2 py-1 text-sm overflow-hidden text-left hover:opacity-90"
                style={{
                  top, height,
                  left: `${left}%`, width: `${width}%`,
                  backgroundColor: (cal?.color ?? '#0f766e') + '44',
                  borderLeft: `3px solid ${cal?.color ?? '#0f766e'}`,
                }}
              >
                <div className="font-medium truncate">{ev.title}</div>
                <div className="text-xs opacity-80">{formatTimeShort(start)} – {formatTimeShort(end)}</div>
                {ev.location && <div className="text-xs opacity-70 truncate">{ev.location}</div>}
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}
