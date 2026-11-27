import DOMPurify from 'dompurify'

/**
 * Defence in depth for untrusted mail HTML:
 *  1. DOMPurify strips active content (scripts, handlers, javascript: URLs);
 *  2. the ReadingPane renders the result inside an <iframe sandbox="">
 *     WITHOUT allow-same-origin/allow-scripts — even sanitized HTML gets no
 *     origin, no script, and no access to the app DOM or the bearer token.
 */
export function sanitizeEmailHTML(html: string): string {
  return DOMPurify.sanitize(html, {
    FORBID_TAGS: ['script', 'style', 'iframe', 'object', 'embed', 'form', 'base'],
    FORBID_ATTR: ['onerror', 'onload', 'onclick', 'onmouseover', 'style'],
    ALLOWED_URI_REGEXP: /^(?:https?:|mailto:|tel:|#|data:image\/(?:png|gif|jpeg|webp);base64,)/i,
  })
}

/**
 * Signature HTML is authored BY the user but still sanitized — the editor is
 * rich text, yet pasted content can carry payloads. Same rules, slightly
 * softer (allow inline styles for signature layout).
 */
export function sanitizeSignatureHTML(html: string): string {
  return DOMPurify.sanitize(html, {
    FORBID_TAGS: ['script', 'iframe', 'object', 'embed', 'form', 'base'],
    FORBID_ATTR: ['onerror', 'onload', 'onclick'],
  })
}
