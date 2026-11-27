import { useMemo } from 'react';
import { eventsOnDay, getMonthGrid, isToday, WEEKDAY_LABELS, weekdayLabelsFor } from '@/lib/dates';
import type { Calendar, CalendarEvent } from '@/lib/types';

interface Props {
  date: Date;
  events: CalendarEvent[];
  calendars: Calendar[];
  onEventClick: (e: CalendarEvent) => void;
  onDayClick: (d: Date) => void;
  onDayNumberClick: (d: Date) => void;
}

export function CalendarMonthView({ date, events, calendars, onEventClick, onDayClick, onDayNumberClick }: Props) {
  const grid = useMemo(() => getMonthGrid(date, 1), [date]);
  const calendarMap = useMemo(() => new Map(calendars.map((c) => [c.id, c])), [calendars]);
  const currentMonth = date.getMonth();
  const labels = weekdayLabelsFor(1);

  return (
    <div className="h-full flex flex-col bg-canvas">
      <div className="grid grid-cols-7 border-b border-line bg-surface">
        {labels.map((l) => (
          <div key={l} className="px-2 py-1 text-xs text-ink-soft text-center border-l border-line first:border-l-0">
            {l}
          </div>
        ))}
      </div>
      <div className="flex-1 grid grid-cols-7 grid-rows-6 min-h-0">
        {grid.map((cell, i) => {
          const cellEvents = eventsOnDay(events, cell);
          const isOtherMonth = cell.getMonth() !== currentMonth;
          const today = isToday(cell);
          return (
            <div
              key={i}
              className={`border-l border-b border-line bg-surface flex flex-col min-h-0 overflow-hidden ${
                isOtherMonth ? 'bg-canvas' : ''
              } first:border-l-0`}
              onClick={(e) => {
                // Only fire when clicking empty space (not an event).
                if (e.target === e.currentTarget) onDayClick(cell);
              }}
            >
              <div className="flex items-center justify-between px-1.5 py-1">
                <button
                  type="button"
                  onClick={() => onDayNumberClick(cell)}
                  className={`text-xs px-1.5 py-0.5 rounded ${
                    today
                      ? 'bg-accent text-white font-semibold'
                      : isOtherMonth
                      ? 'text-ink-mute hover:bg-canvas'
                      : 'text-ink-soft hover:bg-canvas'
                  }`}
                >
                  {cell.getDate()}
                </button>
              </div>
              <div className="flex-1 overflow-y-auto scroll-thin px-1 pb-1 space-y-0.5">
                {cellEvents.slice(0, 3).map((ev) => {
                  const cal = calendarMap.get(ev.calendar_id);
                  return (
                    <button
                      key={ev.id}
                      onClick={(e) => { e.stopPropagation(); onEventClick(ev); }}
                      className="w-full text-left px-1 py-0.5 rounded text-xs truncate hover:opacity-80"
                      style={{ backgroundColor: (cal?.color ?? '#0f766e') + '22', color: cal?.color ?? '#0f766e' }}
                      title={ev.title}
                    >
                      {!ev.all_day && (
                        <span className="opacity-70 mr-1">
                          {new Date(ev.starts_at).toLocaleTimeString('en-ZA', { hour: '2-digit', minute: '2-digit', hour12: false })}
                        </span>
                      )}
                      {ev.title}
                    </button>
                  );
                })}
                {cellEvents.length > 3 && (
                  <button
                    onClick={(e) => { e.stopPropagation(); onDayNumberClick(cell); }}
                    className="text-xs text-ink-mute hover:text-ink px-1"
                  >
                    +{cellEvents.length - 3} more
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// Suppress unused import.
void WEEKDAY_LABELS;
