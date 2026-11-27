const ZAR = new Intl.NumberFormat('en-ZA', { style: 'currency', currency: 'ZAR' });
const NUM = new Intl.NumberFormat('en-ZA');

export function formatZar(minorUnits: number): string {
  return ZAR.format(minorUnits / 100);
}

export function formatNumber(n: number): string {
  return NUM.format(n);
}

export function formatBytes(n: number, precision = 1): string {
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < units.length - 1);
  return `${n.toFixed(precision)} ${units[i]}`;
}

export function formatPercent(ratio: number, precision = 1): string {
  return `${(ratio * 100).toFixed(precision)}%`;
}

export function initials(name: string | undefined, email?: string): string {
  const src = (name || email || '').trim();
  if (!src) return '?';
  const parts = src.split(/\s+/);
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

export function truncate(s: string, max: number): string {
  return s.length > max ? s.slice(0, max - 1) + '…' : s;
}
