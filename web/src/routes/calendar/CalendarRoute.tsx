import { useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useCalendars, useEventsForRange } from '@/hooks/queries/calendar';
import { useMediaQuery, usePageTitle } from '@/hooks';
import { Button, Menu, MenuItem } from '@/design';
import { Icon } from '@/design/icons';
import { addDays, addMonths, endOfDay, endOfMonth, endOfWeek, formatDayLong, formatMonthYear, formatWeekRange, isSameDay, startOfDay, startOfMonth, startOfWeek } from '@/lib/dates';
import { CalendarMonthView } from './CalendarMonthView';
import { CalendarWeekView } from './CalendarWeekView';
import { CalendarDayView } from './CalendarDayView';
import { CalendarAgendaView } from './CalendarAgendaView';
import { EventModal } from './EventModal';
import { CalendarSidebar } from './CalendarSidebar';
import type { CalendarEvent, WeekStart } from './types';

type ViewMode = 'month' | 'week' | 'day' | 'agenda';

const WEEK_START: WeekStart = 1;

export function CalendarRoute() {
  usePageTitle('Calendar');
  const navigate = useNavigate();
  const isDesktop = useMediaQuery('(min-width: 768px)');
  const [params, setParams] = useSearchParams();
  const initialView: ViewMode = (params.get('view') as ViewMode) ?? (isDesktop ? 'week' : 'agenda');
  const [view, setView] = useState<ViewMode>(initialView);
  const [date, setDate] = useState<Date>(params.get('date') ? new Date(params.get('date')!) : new Date());
  const [visibleIds, setVisibleIds] = useState<Set<string> | null>(null);
  const [eventModal, setEventModal] = useState<{ open: boolean; event?: CalendarEvent; defaults?: { start: Date; end: Date } }>({ open: false });

  const calendarsQ = useCalendars();
  const calendars = calendarsQ.data ?? [];

  // Default to all visible once calendars load.
  const effectiveVisible = useMemo(() => {
    if (visibleIds === null && calendars.length > 0) return new Set(calendars.map((c) => c.id));
    return visibleIds ?? new Set<string>();
  }, [visibleIds, calendars]);

  const { from, to } = useMemo(() => computeRange(view, date, WEEK_START), [view, date]);

  const eventsQ = useEventsForRange(
    Array.from(effectiveVisible),
    from.toISOString(),
    to.toISOString(),
  );

  const events = useMemo(() => {
    // Filter out cancelled events for display.
    return (eventsQ.data ?? []).filter((e) => e.status !== 'cancelled');
  }, [eventsQ.data]);

  function setViewAndSync(v: ViewMode) {
    setView(v);
    const next = new URLSearchParams(params);
    next.set('view', v);
    next.set('date', date.toISOString());
    setParams(next, { replace: true });
  }

  function setDateAndSync(d: Date) {
    setDate(d);
    const next = new URLSearchParams(params);
    next.set('view', view);
    next.set('date', d.toISOString());
    setParams(next, { replace: true });
  }

  function goToday() { setDateAndSync(new Date()); }
  function goPrev() {
    if (view === 'month') setDateAndSync(addMonths(date, -1));
    else if (view === 'week') setDateAndSync(addDays(date, -7));
    else if (view === 'day') setDateAndSync(addDays(date, -1));
    else setDateAndSync(addDays(date, -30));
  }
  function goNext() {
    if (view === 'month') setDateAndSync(addMonths(date, 1));
    else if (view === 'week') setDateAndSync(addDays(date, 7));
    else if (view === 'day') setDateAndSync(addDays(date, 1));
    else setDateAndSync(addDays(date, 30));
  }

  function createEvent(defaults?: { start: Date; end: Date }) {
    setEventModal({ open: true, defaults });
  }

  function editEvent(ev: CalendarEvent) {
    setEventModal({ open: true, event: ev });
  }

  function onDayClick(d: Date) {
    const start = new Date(d); start.setHours(9, 0, 0, 0);
    const end = new Date(d); end.setHours(10, 0, 0, 0);
    createEvent({ start, end });
  }

  const headerTitle =
    view === 'month' ? formatMonthYear(date)
    : view === 'week' ? formatWeekRange(date, WEEK_START)
    : view === 'day' ? formatDayLong(date)
    : 'Upcoming';

  if (calendarsQ.isLoading) {
    return <CalendarSkeleton />;
  }

  if (calendars.length === 0) {
    return (
      <div className="p-6">
        <EmptyCalendarState onCreate={() => navigate('/calendar')} />
      </div>
    );
  }

  return (
    <div className="h-full flex bg-surface">
      {isDesktop && (
        <aside className="w-60 shrink-0 border-r border-line flex flex-col">
          <CalendarSidebar
            calendars={calendars}
            visibleIds={effectiveVisible}
            onToggle={(id) => {
              const next = new Set(effectiveVisible);
              if (next.has(id)) next.delete(id); else next.add(id);
              setVisibleIds(next);
            }}
            onCreate={createEvent}
            onManageCalendars={() => navigate('/calendar/settings')}
            onManageScheduling={() => navigate('/calendar/scheduling-links')}
          />
        </aside>
      )}

      <main className="flex-1 min-w-0 flex flex-col">
        <header className="flex items-center gap-2 px-3 py-2 border-b border-line flex-wrap">
          <Button variant="primary" onClick={() => createEvent()}>
            <Icon name="plus" size={14} /> New event
          </Button>
          <div className="flex items-center border border-line rounded">
            <button onClick={goPrev} className="p-1.5 hover:bg-canvas" aria-label="Previous"><Icon name="chevron-left" size={14} /></button>
            <button onClick={goToday} className="px-3 py-1.5 text-sm hover:bg-canvas border-x border-line">Today</button>
            <button onClick={goNext} className="p-1.5 hover:bg-canvas" aria-label="Next"><Icon name="chevron-right" size={14} /></button>
          </div>
          <h1 className="text-lg font-medium flex-1 min-w-0 truncate">{headerTitle}</h1>
          <div className="flex border border-line rounded overflow-hidden">
            {(['month', 'week', 'day', 'agenda'] as const).map((v) => (
              <button
                key={v}
                onClick={() => setViewAndSync(v)}
                className={`px-3 py-1.5 text-sm capitalize ${
                  view === v ? 'bg-accent text-white' : 'hover:bg-canvas'
                }`}
              >
                {v}
              </button>
            ))}
          </div>
          <Menu
            trigger={
              <Button size="icon" variant="ghost" aria-label="Calendar options">
                <Icon name="more" size={16} />
              </Button>
            }
          >
            <MenuItem onClick={() => navigate('/calendar/settings')}>Manage calendars</MenuItem>
            <MenuItem onClick={() => navigate('/calendar/scheduling-links')}>Scheduling links</MenuItem>
            <MenuItem onClick={() => setDateAndSync(new Date())}>Go to today</MenuItem>
          </Menu>
        </header>

        <div className="flex-1 min-h-0 overflow-hidden">
          {eventsQ.isLoading && <div className="p-4 text-sm text-ink-soft">Loading events…</div>}
          {view === 'month' && (
            <CalendarMonthView
              date={date}
              events={events}
              calendars={calendars}
              onEventClick={editEvent}
              onDayClick={onDayClick}
              onDayNumberClick={(d) => { setViewAndSync('day'); setDateAndSync(d); }}
            />
          )}
          {view === 'week' && (
            <CalendarWeekView
              date={date}
              events={events}
              calendars={calendars}
              onEventClick={editEvent}
              onCreateAt={(start, end) => createEvent({ start, end })}
              onDayHeaderClick={(d) => { setViewAndSync('day'); setDateAndSync(d); }}
            />
          )}
          {view === 'day' && (
            <CalendarDayView
              date={date}
              events={events}
              calendars={calendars}
              onEventClick={editEvent}
              onCreateAt={(start, end) => createEvent({ start, end })}
            />
          )}
          {view === 'agenda' && (
            <CalendarAgendaView
              events={events}
              calendars={calendars}
              onEventClick={editEvent}
            />
          )}
        </div>
      </main>

      {eventModal.open && (
        <EventModal
          open={eventModal.open}
          onClose={() => setEventModal({ open: false })}
          event={eventModal.event}
          defaults={eventModal.defaults}
          calendars={calendars}
          defaultCalendarId={calendars.find((c) => c.kind === 'personal')?.id ?? calendars[0].id}
        />
      )}
    </div>
  );
}

function computeRange(view: ViewMode, date: Date, weekStart: WeekStart): { from: Date; to: Date } {
  if (view === 'month') {
    return {
      from: startOfWeek(startOfMonth(date), weekStart),
      to: endOfWeek(endOfMonth(date), weekStart),
    };
  }
  if (view === 'week') {
    return { from: startOfWeek(date, weekStart), to: endOfWeek(date, weekStart) };
  }
  if (view === 'day') {
    return { from: startOfDay(date), to: endOfDay(date) };
  }
  // agenda: 30 days
  return { from: startOfDay(date), to: endOfDay(addDays(date, 30)) };
}

function CalendarSkeleton() {
  return (
    <div className="p-4 space-y-2">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="h-16 bg-canvas rounded animate-pulse" />
      ))}
    </div>
  );
}

function EmptyCalendarState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="max-w-md mx-auto text-center">
      <div className="w-12 h-12 rounded bg-accent-soft text-accent flex items-center justify-center mx-auto mb-3">
        <Icon name="mail" size={24} />
      </div>
      <h2 className="text-lg font-semibold">Your calendar is empty</h2>
      <p className="text-sm text-ink-soft mt-1">Create your first calendar to start scheduling.</p>
      <button onClick={onCreate} className="mt-4 px-4 py-2 rounded bg-accent text-white text-sm">New event</button>
    </div>
  );
}

// Suppress unused import warnings for isSameDay — used indirectly by views.
void isSameDay;
