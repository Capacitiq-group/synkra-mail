import type { Address, MessageDetail } from '@/lib/types';

interface QuotedResult {
  html: string;
  text: string;
  subject: string;
  to: Address[];
  cc: Address[];
  inReplyTo: string;
  references: string[];
}

function dedupeAddresses(list: Address[], exclude?: string): Address[] {
  const seen = new Set<string>();
  const out: Address[] = [];
  for (const a of list) {
    const key = a.email.toLowerCase().trim();
    if (!key) continue;
    if (exclude && key === exclude.toLowerCase()) continue;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(a);
  }
  return out;
}

function replySubject(subject: string): string {
  if (/^\s*re:/i.test(subject)) return subject;
  return `Re: ${subject}`;
}

function forwardSubject(subject: string): string {
  if (/^\s*fwd?:/i.test(subject)) return subject;
  return `Fwd: ${subject}`;
}

function blockquoteHtml(bodyHtml: string): string {
  // Wrap in blockquote with a small left rule that is email-safe.
  return `<blockquote style="margin:0;padding-left:12px;border-left:2px solid #cbd5e1;">${bodyHtml}</blockquote>`;
}

function quoteHeaderHtml(message: MessageDetail): string {
  const from = escapeHtml(message.from.name ? `${message.from.name} <${message.from.email}>` : message.from.email);
  const date = new Date(message.received_at).toUTCString();
  const to = message.to.map((a) => escapeHtml(a.email)).join(', ');
  const subject = escapeHtml(message.subject || '(no subject)');
  return `<p style="color:#64748b;font-size:12px;margin:16px 0 8px;">On ${escapeHtml(date)}, ${from} wrote:<br>To: ${to}<br>Subject: ${subject}</p>`;
}

function quoteHeaderText(message: MessageDetail): string {
  const from = message.from.name ? `${message.from.name} <${message.from.email}>` : message.from.email;
  const date = new Date(message.received_at).toUTCString();
  return `On ${date}, ${from} wrote:`;
}

function forwardHeaderHtml(message: MessageDetail): string {
  const fields: [string, string][] = [
    ['From', message.from.name ? `${message.from.name} <${message.from.email}>` : message.from.email],
    ['Date', new Date(message.received_at).toUTCString()],
    ['Subject', message.subject || '(no subject)'],
    ['To', message.to.map((a) => a.email).join(', ')],
  ];
  if (message.cc.length > 0) fields.push(['Cc', message.cc.map((a) => a.email).join(', ')]);
  const rows = fields
    .map(([k, v]) => `<tr><td style="padding:2px 8px 2px 0;color:#64748b;font-size:12px;">${escapeHtml(k)}:</td><td style="padding:2px 0;font-size:12px;">${escapeHtml(v)}</td></tr>`)
    .join('');
  return `<p style="margin:16px 0 4px;color:#64748b;font-size:12px;">---------- Forwarded message ----------</p><table style="border-collapse:collapse;margin-bottom:8px;">${rows}</table>`;
}

function forwardHeaderText(message: MessageDetail): string {
  return [
    '---------- Forwarded message ----------',
    `From: ${message.from.name ? `${message.from.name} <${message.from.email}>` : message.from.email}`,
    `Date: ${new Date(message.received_at).toUTCString()}`,
    `Subject: ${message.subject || '(no subject)'}`,
    `To: ${message.to.map((a) => a.email).join(', ')}`,
    message.cc.length > 0 ? `Cc: ${message.cc.map((a) => a.email).join(', ')}` : '',
  ]
    .filter(Boolean)
    .join('\n');
}

export function buildQuoted(
  mode: 'reply' | 'reply_all' | 'forward',
  message: MessageDetail,
  selfAddress: string,
): QuotedResult {
  const references = [...(message.references ?? []), message.id].filter(Boolean);

  if (mode === 'forward') {
    const html = `${forwardHeaderHtml(message)}<div>${message.body_html ?? textToHtml(message.body_text ?? '')}</div>`;
    const text = `${forwardHeaderText(message)}\n\n${message.body_text ?? ''}`;
    return {
      html,
      text,
      subject: forwardSubject(message.subject || ''),
      to: [],
      cc: [],
      inReplyTo: message.id,
      references,
    };
  }

  // reply / reply_all
  const to = [message.from];
  const cc: Address[] = [];
  if (mode === 'reply_all') {
    for (const a of message.to) to.push(a);
    for (const a of message.cc) cc.push(a);
    if (message.reply_to) for (const a of message.reply_to) cc.push(a);
  }
  const html = `${quoteHeaderHtml(message)}${blockquoteHtml(message.body_html ?? textToHtml(message.body_text ?? ''))}`;
  const text = `\n\n${quoteHeaderText(message)}\n> ${(message.body_text ?? '').replace(/\n/g, '\n> ')}`;
  return {
    html,
    text,
    subject: replySubject(message.subject || ''),
    to: dedupeAddresses(to, selfAddress),
    cc: dedupeAddresses(cc, selfAddress),
    inReplyTo: message.id,
    references,
  };
}

export function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

export function textToHtml(text: string): string {
  return escapeHtml(text)
    .split(/\n{2,}/)
    .map((para) => `<p>${para.replace(/\n/g, '<br>')}</p>`)
    .join('');
}

export function htmlToText(html: string): string {
  const doc = new DOMParser().parseFromString(html, 'text/html');
  const blocks = Array.from(doc.body.children);
  if (blocks.length === 0) return doc.body.textContent ?? '';
  return blocks
    .map((el) => el.textContent ?? '')
    .filter((s) => s.trim().length > 0)
    .join('\n\n');
}
