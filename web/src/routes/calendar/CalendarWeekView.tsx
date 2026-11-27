import { useMemo, useRef } from 'react';
import { eventsOnDay, formatTimeShort, getWeekDays, isToday, layoutOverlapping, startOfDay, minutesSinceMidnight, WEEKDAY_LABELS, weekdayLabelsFor } from '@/lib/dates';
import type { Calendar, CalendarEvent } from '@/lib/types';

interface Props {
  date: Date;
  events: CalendarEvent[];
  calendars: Calendar[];
  onEventClick: (e: CalendarEvent) => void;
  onCreateAt: (start: Date, end: Date) => void;
  onDayHeaderClick: (d: Date) => void;
}

const HOUR_HEIGHT = 40;
const HOURS = 24;

export function CalendarWeekView({ date, events, calendars, onEventClick, onCreateAt, onDayHeaderClick }: Props) {
  const days = useMemo(() => getWeekDays(date, 1), [date]);
  const calendarMap = useMemo(() => new Map(calendars.map((c) => [c.id, c])), [calendars]);
  const labels = weekdayLabelsFor(1);
  const gridRef = useRef<HTMLDivElement>(null);

  const allDayByDay = days.map((d) => eventsOnDay(events, d).filter((e) => e.all_day));
  const hasAllDay = allDayByDay.some((list) => list.length > 0);

  function onGridClick(e: React.MouseEvent<HTMLDivElement>, day: Date) {
    if (e.target !== e.currentTarget) return;
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const y = e.clientY - rect.top + (e.currentTarget.parentElement?.scrollTop ?? 0);
    const minutes = Math.floor((y / HOUR_HEIGHT) * 60 / 30) * 30;
    const start = new Date(day);
    start.setHours(0, 0, 0, 0);
    start.setMinutes(minutes);
    const end = new Date(start.getTime() + 60 * 60 * 1000);
    onCreateAt(start, end);
  }

  return (
    <div className="h-full flex flex-col bg-surface">
      {/* Day headers */}
      <div className="grid grid-cols-[60px,repeat(7,1fr)] border-b border-line sticky top-0 z-10 bg-surface">
        <div aria-hidden />
        {days.map((d, i) => {
          const today = isToday(d);
          return (
            <button
              key={i}
              onClick={() => onDayHeaderClick(d)}
              className={`px-1 py-2 text-xs text-center border-l border-line hover:bg-canvas ${today ? 'bg-accent-soft' : ''}`}
            >
              <div className="text-ink-soft">{labels[i]}</div>
              <div className={`text-base font-medium ${today ? 'text-accent' : ''}`}>{d.getDate()}</div>
            </button>
          );
        })}
      </div>

      {/* All-day events row */}
      {hasAllDay && (
        <div className="grid grid-cols-[60px,repeat(7,1fr)] border-b border-line">
          <div className="px-1 py-1 text-[10px] text-ink-mute text-right">All day</div>
          {allDayByDay.map((list, i) => (
            <div key={i} className="border-l border-line px-0.5 py-0.5 space-y-0.5 min-h-[24px]">
              {list.map((ev) => {
                const cal = calendarMap.get(ev.calendar_id);
                return (
                  <button
                    key={ev.id}
                    onClick={() => onEventClick(ev)}
                    className="w-full text-left text-xs px-1 py-0.5 rounded truncate"
                    style={{ backgroundColor: (cal?.color ?? '#0f766e') + '33', color: cal?.color ?? '#0f766e' }}
                  >
                    {ev.title}
                  </button>
                );
              })}
            </div>
          ))}
        </div>
      )}

      {/* Time grid */}
      <div className="flex-1 overflow-y-auto scroll-thin">
        <div className="grid grid-cols-[60px,repeat(7,1fr)]" ref={gridRef}>
          <div>
            {Array.from({ length: HOURS }).map((_, h) => (
              <div key={h} style={{ height: HOUR_HEIGHT }} className="text-[10px] text-ink-mute text-right pr-1 border-b border-line">
                {h === 0 ? '' : `${String(h).padStart(2, '0')}:00`}
              </div>
            ))}
          </div>
          {days.map((d, i) => {
            const dayEvents = layoutOverlapping(eventsOnDay(events, d).filter((e) => !e.all_day));
            return (
              <div
                key={i}
                className="relative border-l border-line"
                onClick={(e) => onGridClick(e, d)}
                style={{ height: HOUR_HEIGHT * HOURS }}
              >
                {/* Hour grid lines */}
                {Array.from({ length: HOURS }).map((_, h) => (
                  <div key={h} className="border-b border-line" style={{ height: HOUR_HEIGHT }} />
                ))}
                {/* Events */}
                {dayEvents.map((ev) => {
                  const cal = calendarMap.get(ev.calendar_id);
                  const start = new Date(ev.starts_at);
                  const end = new Date(ev.ends_at);
                  const dayStart = startOfDay(d);
                  // Clamp to this day for events that span
                  const startMin = Math.max(0, minutesSinceMidnight(start) + (start < dayStart ? 1440 : 0));
                  const endMin = Math.min(1440, minutesSinceMidnight(end) + (end > new Date(dayStart.getTime() + 24 * 3600 * 1000) ? 1440 : 0));
                  const top = (Math.max(0, startMin) / 60) * HOUR_HEIGHT;
                  const height = Math.max(16, ((Math.max(endMin, startMin + 30) - Math.max(0, startMin)) / 60) * HOUR_HEIGHT);
                  const width = 100 / ev._lanes;
                  const left = (ev._lane / ev._lanes) * 100;
                  return (
                    <button
                      key={ev.id}
                      onClick={(e) => { e.stopPropagation(); onEventClick(ev); }}
                      className="absolute rounded px-1 text-[11px] overflow-hidden text-left hover:opacity-90"
                      style={{
                        top,
                        height,
                        left: `${left}%`,
                        width: `${width}%`,
                        backgroundColor: (cal?.color ?? '#0f766e') + '44',
                        borderLeft: `3px solid ${cal?.color ?? '#0f766e'}`,
                      }}
                      title={`${ev.title} — ${formatTimeShort(start)}–${formatTimeShort(end)}`}
                    >
                      <div className="font-medium truncate">{ev.title}</div>
                      <div className="truncate opacity-80">{formatTimeShort(start)}</div>
                    </button>
                  );
                })}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

// Suppress unused import.
void WEEKDAY_LABELS;
