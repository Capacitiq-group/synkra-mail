/**
 * Resolves {{field}} placeholders in signature templates. Used in the editor
 * preview and — with the same keys — by the backend at send time.
 */
export interface PlaceholderContext {
  employee_name?: string;
  job_title?: string;
  department?: string;
  phone?: string;
  email?: string;
  company_name?: string;
  company_website?: string;
  company_logo?: string;
  office_address?: string;
  linkedin?: string;
}

const ALL_PLACEHOLDERS: (keyof PlaceholderContext)[] = [
  'employee_name', 'job_title', 'department', 'phone', 'email',
  'company_name', 'company_website', 'company_logo',
  'office_address', 'linkedin',
];

export function resolvePlaceholders(html: string, ctx: PlaceholderContext): string {
  let out = html;
  for (const key of ALL_PLACEHOLDERS) {
    const value = ctx[key] ?? '';
    out = out.replaceAll(`{{${key}}}`, escapePlaceholderValue(value));
  }
  return out;
}

export function detectPlaceholders(html: string): string[] {
  const found = new Set<string>();
  const re = /\{\{\s*([a-z_]+)\s*\}\}/gi;
  let m: RegExpExecArray | null;
  while ((m = re.exec(html)) !== null) {
    if ((ALL_PLACEHOLDERS as string[]).includes(m[1].toLowerCase())) {
      found.add(m[1].toLowerCase());
    }
  }
  return Array.from(found);
}

function escapePlaceholderValue(v: string): string {
  return v
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}
