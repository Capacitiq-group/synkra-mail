/**
 * Email-signature compatibility linter.
 *
 * The linter is intentionally opinionated toward what renders acceptably in
 * Outlook Desktop (2016/2019/365, Windows), Gmail, Apple Mail, and Outlook
 * on the web. It does NOT simulate rendering — no system can guarantee
 * pixel-identical results — but it catches the ~90% of signatures that break
 * for well-known reasons. The rule names are stable and referenced in the UI.
 */

export type LintSeverity = 'error' | 'warning' | 'info';

export interface LintIssue {
  rule: string;
  severity: LintSeverity;
  message: string;
  fix?: string;
  where?: string;
}

const GENERIC_FONTS = /^(sans-serif|serif|monospace|cursive|fantasy|system-ui)$/i;

export function lintSignatureHtml(html: string): LintIssue[] {
  const issues: LintIssue[] = [];
  let doc: Document;
  try {
    doc = new DOMParser().parseFromString(`<div id="__root">${html}</div>`, 'text/html');
  } catch {
    return [{ rule: 'unparseable', severity: 'error', message: 'Could not parse this HTML.' }];
  }
  const root = doc.getElementById('__root');
  if (!root) return issues;

  // 1. <style> blocks
  if (root.querySelector('style')) {
    issues.push({
      rule: 'no-style-block',
      severity: 'error',
      message: 'A <style> block is present.',
      fix: 'Gmail and some Outlook versions strip <style> blocks. Use inline style="…" attributes instead.',
      where: '<style>',
    });
  }

  // 2. External stylesheets
  if (root.querySelector('link[rel="stylesheet" i]')) {
    issues.push({
      rule: 'no-external-css',
      severity: 'error',
      message: 'An external stylesheet is linked.',
      fix: 'Most email clients block external CSS. Inline the styles instead.',
      where: '<link rel="stylesheet">',
    });
  }

  // 3. Flexbox / grid
  const flexElements = Array.from(root.querySelectorAll('[style]')).filter((el) =>
    /display\s*:\s*(flex|inline-flex|grid|inline-grid)/i.test(el.getAttribute('style') || ''),
  );
  if (flexElements.length > 0) {
    issues.push({
      rule: 'no-flex-grid',
      severity: 'error',
      message: `Flexbox or grid is used in ${flexElements.length} element(s).`,
      fix: 'Outlook Desktop does not support flexbox or grid. Use a <table>-based layout for any side-by-side content.',
    });
  }

  // 4. CSS variables
  const varElements = Array.from(root.querySelectorAll('[style]')).filter((el) =>
    /var\(--/i.test(el.getAttribute('style') || ''),
  );
  if (varElements.length > 0) {
    issues.push({
      rule: 'no-css-vars',
      severity: 'error',
      message: `CSS variables are used in ${varElements.length} element(s).`,
      fix: 'Outlook and some Gmail versions do not resolve CSS variables. Inline the resolved values instead.',
    });
  }

  // 5. background-image with remote URL
  const bgImageElements = Array.from(root.querySelectorAll('[style]')).filter((el) =>
    /background(?:-image)?\s*:[^;]*url\(\s*['"]?https?:/i.test(el.getAttribute('style') || ''),
  );
  if (bgImageElements.length > 0) {
    issues.push({
      rule: 'remote-background-image',
      severity: 'warning',
      message: 'A background image is loaded from a remote URL.',
      fix: 'Many clients block remote background images. Use an <img> tag inside a table cell for critical visuals.',
    });
  }

  // 6. Fixed widths over 600px
  const wideElements: Element[] = [];
  root.querySelectorAll('[width]').forEach((el) => {
    const v = parseInt(el.getAttribute('width') || '', 10);
    if (!Number.isNaN(v) && v > 600) wideElements.push(el);
  });
  root.querySelectorAll('[style]').forEach((el) => {
    const m = (el.getAttribute('style') || '').match(/(?:^|;)\s*width\s*:\s*(\d{3,})px/i);
    if (m && parseInt(m[1], 10) > 600) wideElements.push(el);
  });
  if (wideElements.length > 0) {
    issues.push({
      rule: 'max-width-600',
      severity: 'warning',
      message: `${wideElements.length} element(s) have a fixed width over 600px.`,
      fix: 'Signatures wider than 600px get clipped or scroll horizontally on mobile. Keep the outer width at 600px or below.',
    });
  }

  // 7. Unhosted images (data URIs and http:// URLs)
  let dataUriCount = 0;
  let httpImageCount = 0;
  let missingAltCount = 0;
  root.querySelectorAll('img').forEach((img) => {
    const src = img.getAttribute('src') || '';
    if (/^data:/i.test(src)) dataUriCount++;
    if (/^http:\/\//i.test(src)) httpImageCount++;
    if (!img.getAttribute('alt')) missingAltCount++;
  });
  if (dataUriCount > 0) {
    issues.push({
      rule: 'no-data-uri-images',
      severity: 'error',
      message: `${dataUriCount} image(s) are embedded as data URIs.`,
      fix: 'Outlook Desktop strips data URI images. Upload images to our asset host and reference them by HTTPS URL.',
    });
  }
  if (httpImageCount > 0) {
    issues.push({
      rule: 'https-images-only',
      severity: 'warning',
      message: `${httpImageCount} image(s) load over plain HTTP.`,
      fix: 'Gmail and Outlook warn on insecure images. Use HTTPS URLs.',
    });
  }
  if (missingAltCount > 0) {
    issues.push({
      rule: 'image-alt-text',
      severity: 'info',
      message: `${missingAltCount} image(s) are missing alt text.`,
      fix: 'Add alt="…" so the signature still reads when images are blocked (most clients block them by default).',
    });
  }

  // 8. javascript: URLs
  const jsHrefs = Array.from(root.querySelectorAll('[href], [src]')).filter((el) => {
    const href = el.getAttribute('href') || '';
    const src = el.getAttribute('src') || '';
    return /^javascript:/i.test(href) || /^javascript:/i.test(src);
  });
  if (jsHrefs.length > 0) {
    issues.push({
      rule: 'no-javascript-urls',
      severity: 'error',
      message: 'JavaScript URLs are present.',
      fix: 'javascript: URLs are stripped by every mail client and will be flagged as suspicious. Use a normal HTTPS link.',
    });
  }

  // 9. Forms and inputs
  if (root.querySelector('form, input, select, textarea, button')) {
    issues.push({
      rule: 'no-forms',
      severity: 'error',
      message: 'Form controls are present.',
      fix: 'Mail clients do not support forms. Use links to a hosted form instead.',
    });
  }

  // 10. Font fallback
  const noFallback: string[] = [];
  root.querySelectorAll('[style]').forEach((el) => {
    const style = el.getAttribute('style') || '';
    const m = style.match(/font-family\s*:\s*([^;]+)/i);
    if (!m) return;
    const family = m[1];
    const parts = family.split(',').map((p) => p.trim().replace(/^["']|["']$/g, ''));
    const last = parts[parts.length - 1] ?? '';
    if (!GENERIC_FONTS.test(last)) noFallback.push(family);
  });
  if (noFallback.length > 0) {
    issues.push({
      rule: 'font-fallback',
      severity: 'warning',
      message: `A font stack without a generic fallback is used (${noFallback[0]}).`,
      fix: 'End every font-family with a generic family, e.g. "Helvetica, Arial, sans-serif".',
    });
  }

  // 11. Layout hint — no tables but many block elements
  const blockCount = root.querySelectorAll('div, p, h1, h2, h3, h4, section, article').length;
  const tableCount = root.querySelectorAll('table').length;
  if (tableCount === 0 && blockCount > 4) {
    issues.push({
      rule: 'prefer-tables',
      severity: 'info',
      message: `No tables are used in a fairly complex signature (${blockCount} block elements).`,
      fix: 'Outlook renders complex <div> layouts unpredictably. A two-column layout inside a <table> is more reliable.',
    });
  }

  return issues;
}

export function hasBlockingIssues(issues: LintIssue[]): boolean {
  return issues.some((i) => i.severity === 'error');
}
