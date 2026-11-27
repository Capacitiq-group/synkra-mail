import DOMPurify from 'dompurify';

const ALLOWED_TAGS = [
  'a','abbr','address','article','aside','b','blockquote','br','caption','center','cite',
  'code','col','colgroup','dd','div','dl','dt','em','figcaption','figure','font','footer',
  'h1','h2','h3','h4','h5','h6','header','hr','i','img','ins','del','kbd','li','main','mark',
  'nav','ol','p','pre','q','s','samp','section','small','span','strike','strong','sub','summary',
  'sup','table','tbody','td','tfoot','th','thead','tr','u','ul','var',
];
const ALLOWED_ATTR = [
  'href','target','rel','src','srcset','alt','title','width','height','style','align',
  'valign','colspan','rowspan','border','cellpadding','cellspacing','bgcolor','color',
  'face','size','dir','lang','role','aria-label','aria-hidden',
];

const REMOTE = /^https?:/i;
const UNSAFE_URL = /^(javascript:|vbscript:|file:|data:(?!image\/(?:png|jpe?g|gif|webp|bmp|svg\+xml)))/i;

export interface RenderedEmail {
  html: string;
  blockedImages: string[];
  externalLinks: string[];
  hadRemoteImages: boolean;
}

export interface RenderOptions {
  allowRemoteImages: boolean;
  imageProxy?: (url: string) => string;
}

export function renderEmail(rawHtml: string, opts: RenderOptions): RenderedEmail {
  const blockedImages: string[] = [];
  const externalLinks: string[] = [];

  const clean = DOMPurify.sanitize(rawHtml, {
    ALLOWED_TAGS,
    ALLOWED_ATTR,
    ALLOW_DATA_ATTR: false,
    FORBID_TAGS: ['style', 'form', 'input', 'button', 'textarea', 'select', 'iframe', 'object', 'embed'],
    FORBID_ATTR: ['onerror', 'onload', 'onclick', 'onmouseover', 'onfocus', 'onblur', 'onchange', 'onsubmit'],
    KEEP_CONTENT: true,
  });

  const doc = new DOMParser().parseFromString(clean, 'text/html');

  doc.querySelectorAll('a[href]').forEach((a) => {
    const raw = a.getAttribute('href') || '';
    if (UNSAFE_URL.test(raw)) { a.removeAttribute('href'); return; }
    if (REMOTE.test(raw)) {
      externalLinks.push(raw);
      a.setAttribute('target', '_blank');
      a.setAttribute('rel', 'noopener noreferrer nofollow');
    }
  });

  doc.querySelectorAll('img[src]').forEach((img) => {
    const raw = img.getAttribute('src') || '';
    if (UNSAFE_URL.test(raw)) { img.removeAttribute('src'); return; }
    if (REMOTE.test(raw) && !opts.allowRemoteImages) {
      blockedImages.push(raw);
      img.setAttribute('data-blocked-src', raw);
      img.setAttribute('src', 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7');
      img.setAttribute('alt', img.getAttribute('alt') || '[image blocked]');
      return;
    }
    if (REMOTE.test(raw) && opts.imageProxy) {
      img.setAttribute('src', opts.imageProxy(raw));
    }
  });

  // Strip <style> blocks already done above; strip inline expression() and url() javascript:
  doc.querySelectorAll('[style]').forEach((el) => {
    const style = el.getAttribute('style') || '';
    if (/(expression\(|javascript:|behavior:)/i.test(style)) {
      el.removeAttribute('style');
    }
  });

  const body = doc.body.innerHTML;
  const html = `<div class="email-body">${body}</div>`;
  return { html, blockedImages, externalLinks, hadRemoteImages: blockedImages.length > 0 };
}

export function wrapForIframe(bodyHtml: string, opts: { allowRemoteImages: boolean }): string {
  const imgSrc = opts.allowRemoteImages ? 'https: data:' : 'data:';
  const csp = [
    "default-src 'none'",
    `img-src ${imgSrc}`,
    "style-src 'unsafe-inline'",
    "font-src data:",
    "media-src 'none'",
    "script-src 'none'",
    "object-src 'none'",
  ].join('; ');
  return `<!doctype html><html><head><meta charset="utf-8">
    <meta http-equiv="Content-Security-Policy" content="${csp}">
    <base target="_blank">
    <style>
      html,body { margin:0; padding:0; background:transparent; }
      .email-body { padding: 16px; font: 14px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; color: #0f172a; word-break: break-word; }
      .email-body img { max-width: 100%; height: auto; }
      .email-body table { max-width: 100%; }
      .email-body a { color: #0f766e; }
    </style>
    </head><body>${bodyHtml}</body></html>`;
}
