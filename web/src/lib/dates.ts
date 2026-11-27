/**
 * Compact calendar date helpers. All operations are on JS Dates in the user's
 * local timezone. Week starts on Monday (ISO 8601) by default; callers can
 * override.
 */

export type WeekStart = 0 | 1;

export function startOfDay(d: Date): Date {
  const x = new Date(d);
  x.setHours(0, 0, 0, 0);
  return x;
}

export function endOfDay(d: Date): Date {
  const x = new Date(d);
  x.setHours(23, 59, 59, 999);
  return x;
}

export function addDays(d: Date, n: number): Date {
  const x = new Date(d);
  x.setDate(x.getDate() + n);
  return x;
}

export function addWeeks(d: Date, n: number): Date {
  return addDays(d, n * 7);
}

export function addMonths(d: Date, n: number): Date {
  const x = new Date(d);
  const day = x.getDate();
  x.setDate(1);
  x.setMonth(x.getMonth() + n);
  const last = daysInMonth(x.getFullYear(), x.getMonth());
  x.setDate(Math.min(day, last));
  return x;
}

export function daysInMonth(year: number, month: number): number {
  return new Date(year, month + 1, 0).getDate();
}

export function startOfWeek(d: Date, weekStart: WeekStart = 1): Date {
  const x = startOfDay(d);
  const day = x.getDay();
  const diff = (day - weekStart + 7) % 7;
  return addDays(x, -diff);
}

export function endOfWeek(d: Date, weekStart: WeekStart = 1): Date {
  return endOfDay(addDays(startOfWeek(d, weekStart), 6));
}

export function startOfMonth(d: Date): Date {
  const x = startOfDay(d);
  x.setDate(1);
  return x;
}

export function endOfMonth(d: Date): Date {
  const x = endOfDay(d);
  x.setDate(daysInMonth(x.getFullYear(), x.getMonth()));
  return x;
}

export function isSameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

export function isToday(d: Date): boolean {
  return isSameDay(d, new Date());
}

export function isWeekend(d: Date): boolean {
  const day = d.getDay();
  return day === 0 || day === 6;
}

/**
 * 42 dates for a month view (6 weeks × 7 days), starting on the week start of
 * the month's first week. Always exactly 42 cells so the grid height is stable
 * across months.
 */
export function getMonthGrid(monthDate: Date, weekStart: WeekStart = 1): Date[] {
  const start = startOfWeek(startOfMonth(monthDate), weekStart);
  const cells: Date[] = [];
  for (let i = 0; i < 42; i++) cells.push(addDays(start, i));
  return cells;
}

export function getWeekDays(weekDate: Date, weekStart: WeekStart = 1): Date[] {
  const start = startOfWeek(weekDate, weekStart);
  const days: Date[] = [];
  for (let i = 0; i < 7; i++) days.push(addDays(start, i));
  return days;
}

export function minutesSinceMidnight(d: Date): number {
  return d.getHours() * 60 + d.getMinutes();
}

export function formatTimeShort(d: Date | string): string {
  const x = typeof d === 'string' ? new Date(d) : d;
  return x.toLocaleTimeString('en-ZA', { hour: '2-digit', minute: '2-digit', hour12: false });
}

export function formatMonthYear(d: Date): string {
  return d.toLocaleDateString('en-ZA', { month: 'long', year: 'numeric' });
}

export function formatDayLong(d: Date): string {
  return d.toLocaleDateString('en-ZA', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
}

export function formatWeekRange(weekDate: Date, weekStart: WeekStart = 1): string {
  const start = startOfWeek(weekDate, weekStart);
  const end = addDays(start, 6);
  const sameMonth = start.getMonth() === end.getMonth();
  const startStr = start.toLocaleDateString('en-ZA', { day: 'numeric', month: sameMonth ? undefined : 'short' });
  const endStr = end.toLocaleDateString('en-ZA', { day: 'numeric', month: 'short', year: 'numeric' });
  return `${startStr} – ${endStr}`;
}

export const WEEKDAY_LABELS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

export function weekdayLabelsFor(weekStart: WeekStart): string[] {
  if (weekStart === 1) return WEEKDAY_LABELS;
  return [...WEEKDAY_LABELS.slice(6), ...WEEKDAY_LABELS.slice(0, 6)];
}

/** Convert a date to a "yyyy-mm-dd" string for `<input type="date">`. */
export function toDateInputValue(d: Date | string): string {
  const x = typeof d === 'string' ? new Date(d) : d;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${x.getFullYear()}-${pad(x.getMonth() + 1)}-${pad(x.getDate())}`;
}

/** Convert a date to "HH:mm" for `<input type="time">`. */
export function toTimeInputValue(d: Date | string): string {
  const x = typeof d === 'string' ? new Date(d) : d;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(x.getHours())}:${pad(x.getMinutes())}`;
}

/** Combine a "yyyy-mm-dd" and "HH:mm" into an ISO string, in local time. */
export function combineDateTime(dateStr: string, timeStr: string): string {
  const [y, m, d] = dateStr.split('-').map(Number);
  const [hh, mm] = timeStr.split(':').map(Number);
  return new Date(y, (m ?? 1) - 1, d ?? 1, hh ?? 0, mm ?? 0, 0, 0).toISOString();
}

export interface TimeRange { start: string; end: string }

/**
 * Given an event list and a day, filter events visible on that day (including
 * events that straddle the day boundary and all-day events).
 */
export function eventsOnDay<T extends { starts_at: string; ends_at: string; all_day: boolean }>(
  events: T[],
  day: Date,
): T[] {
  const dayStart = startOfDay(day).getTime();
  const dayEnd = endOfDay(day).getTime();
  return events.filter((e) => {
    const s = new Date(e.starts_at).getTime();
    const en = new Date(e.ends_at).getTime();
    return s <= dayEnd && en >= dayStart;
  });
}

/** Assign lane indices to overlapping events for week/day views. */
export function layoutOverlapping<T extends { starts_at: string; ends_at: string; all_day: boolean }>(
  events: T[],
): Array<T & { _lane: number; _lanes: number }> {
  const timed = events.filter((e) => !e.all_day).sort((a, b) =>
    new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime(),
  );
  if (timed.length === 0) return [];

  // Cluster events that overlap in time.
  const clusters: Array<{ start: number; end: number; events: typeof timed }> = [];
  for (const e of timed) {
    const s = new Date(e.starts_at).getTime();
    const en = new Date(e.ends_at).getTime();
    const cluster = clusters[clusters.length - 1];
    if (cluster && s < cluster.end) {
      cluster.end = Math.max(cluster.end, en);
      cluster.events.push(e);
    } else {
      clusters.push({ start: s, end: en, events: [e] });
    }
  }

  const out: Array<T & { _lane: number; _lanes: number }> = [];
  for (const cluster of clusters) {
    const laneEnds: number[] = [];
    const placed: Array<{ e: typeof timed[number]; lane: number }> = [];
    for (const e of cluster.events) {
      const s = new Date(e.starts_at).getTime();
      const en = new Date(e.ends_at).getTime();
      let lane = laneEnds.findIndex((end) => end <= s);
      if (lane === -1) { lane = laneEnds.length; laneEnds.push(en); }
      else { laneEnds[lane] = en; }
      placed.push({ e, lane });
    }
    const lanes = laneEnds.length;
    for (const { e, lane } of placed) {
      out.push({ ...e, _lane: lane, _lanes: lanes });
    }
  }
  return out;
}
