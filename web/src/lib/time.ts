export function nowIso(): string { return new Date().toISOString(); }

export function toLocalTimezone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}

export function formatTime(iso: string, tz?: string): string {
  return new Date(iso).toLocaleTimeString('en-ZA', { hour: '2-digit', minute: '2-digit', timeZone: tz });
}

export function formatDate(iso: string, tz?: string): string {
  return new Date(iso).toLocaleDateString('en-ZA', { year: 'numeric', month: 'short', day: 'numeric', timeZone: tz });
}

export function formatDateTime(iso: string, tz?: string): string {
  return new Date(iso).toLocaleString('en-ZA', {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', timeZone: tz,
  });
}

export function isToday(iso: string): boolean {
  const d = new Date(iso), n = new Date();
  return d.getFullYear() === n.getFullYear() && d.getMonth() === n.getMonth() && d.getDate() === n.getDate();
}

export function isWithinLastWeek(iso: string): boolean {
  return Date.now() - new Date(iso).getTime() < 7 * 24 * 3600 * 1000;
}

export function relativeTime(iso: string): string {
  const diffMs = Date.now() - new Date(iso).getTime();
  const s = Math.round(diffMs / 1000);
  if (s < 45) return 'just now';
  if (s < 3600) return `${Math.round(s / 60)}m ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  if (s < 7 * 86400) return `${Math.round(s / 86400)}d ago`;
  return formatDate(iso);
}

/** Human-friendly message-list timestamp: time today, weekday this week, date otherwise. */
export function listTime(iso: string): string {
  if (isToday(iso)) return formatTime(iso);
  if (isWithinLastWeek(iso)) return new Date(iso).toLocaleDateString('en-ZA', { weekday: 'short' });
  return formatDate(iso);
}

export function minutesFromNow(minutes: number): string {
  return new Date(Date.now() + minutes * 60_000).toISOString();
}

export function addDays(iso: string, days: number): string {
  const d = new Date(iso);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString();
}
