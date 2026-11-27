import type { SearchQuery } from '@/lib/types';

export interface SearchChip {
  key: string;
  label: string;
  /** If present, clicking the chip removes the associated token. */
  token: string;
}

export interface ParsedSearch {
  query: SearchQuery;
  chips: SearchChip[];
}

/**
 * Parses Gmail-style operators:
 *   from:alice@example.com  to:bob@example.com  subject:invoice
 *   has:attachment  is:unread  in:inbox  after:2024-01-01  before:2024-06-30
 *   label:"Team leads"
 *
 * Free text becomes `q`. Multiple operator instances of the same type combine
 * with the free text using the backend's default semantics (AND for differing
 * fields; the last value wins within a field).
 */
export function parseSearch(input: string): ParsedSearch {
  const tokens = tokenize(input);
  const query: SearchQuery = {};
  const chips: SearchChip[] = [];
  const free: string[] = [];

  for (const raw of tokens) {
    const colon = raw.indexOf(':');
    if (colon <= 0) { free.push(raw); continue; }
    const key = raw.slice(0, colon).toLowerCase();
    const value = stripQuotes(raw.slice(colon + 1));
    if (!value) { free.push(raw); continue; }

    switch (key) {
      case 'from':
        query.from = value; chips.push({ key: 'from', label: `from: ${value}`, token: raw }); break;
      case 'to':
        query.to = value; chips.push({ key: 'to', label: `to: ${value}`, token: raw }); break;
      case 'subject':
        query.subject = value; chips.push({ key: 'subject', label: `subject: ${value}`, token: raw }); break;
      case 'in':
      case 'folder':
        query.folder = value; chips.push({ key: 'in', label: `in: ${value}`, token: raw }); break;
      case 'label':
        query.label = value; chips.push({ key: 'label', label: `label: ${value}`, token: raw }); break;
      case 'after':
        query.after = value; chips.push({ key: 'after', label: `after: ${value}`, token: raw }); break;
      case 'before':
        query.before = value; chips.push({ key: 'before', label: `before: ${value}`, token: raw }); break;
      case 'has':
        if (value.toLowerCase() === 'attachment') {
          query.has_attachment = true; chips.push({ key: 'has', label: 'has: attachment', token: raw });
        } else { free.push(raw); }
        break;
      case 'is':
        if (value.toLowerCase() === 'unread') {
          query.is_unread = true; chips.push({ key: 'is', label: 'is: unread', token: raw });
        } else if (value.toLowerCase() === 'read') {
          query.is_unread = false; chips.push({ key: 'is', label: 'is: read', token: raw });
        } else { free.push(raw); }
        break;
      default:
        free.push(raw);
    }
  }
  if (free.length > 0) query.q = free.join(' ');
  return { query, chips };
}

function tokenize(input: string): string[] {
  const out: string[] = [];
  const re = /(?:[^\s"]+|"[^"]*")+/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(input)) !== null) out.push(m[0]);
  return out;
}

function stripQuotes(s: string): string {
  if (s.length >= 2 && s.startsWith('"') && s.endsWith('"')) return s.slice(1, -1);
  return s;
}

export function appendToken(input: string, token: string): string {
  const tokens = tokenize(input);
  if (tokens.includes(token)) return input;
  return [...tokens, token].join(' ');
}

export function removeToken(input: string, token: string): string {
  return tokenize(input).filter((t) => t !== token).join(' ');
}
