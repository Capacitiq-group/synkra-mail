const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function isEmail(s: string): boolean { return EMAIL_RE.test(s.trim()); }

export function isValidAddressList(list: { email: string }[]): boolean {
  return list.length > 0 && list.every((a) => isEmail(a.email));
}

/** Parses "Name <email>, other@example.com" into structured addresses. */
export function parseAddressInput(input: string): { email: string; name?: string }[] {
  return input
    .split(/[,;\n]/)
    .map((s) => s.trim())
    .filter(Boolean)
    .map((part) => {
      const m = part.match(/^(.*)<([^>]+)>$/);
      if (m) return { name: m[1].trim().replace(/^"|"$/g, ''), email: m[2].trim() };
      return { email: part };
    })
    .filter((a) => isEmail(a.email));
}

export function looksLikeVerificationToken(s: string): boolean {
  return /^[a-zA-Z0-9_-]{16,}$/.test(s);
}

export function passwordScore(pw: string): { score: 0 | 1 | 2 | 3 | 4; hint: string } {
  let score = 0;
  if (pw.length >= 12) score++;
  if (/[a-z]/.test(pw) && /[A-Z]/.test(pw)) score++;
  if (/\d/.test(pw)) score++;
  if (/[^A-Za-z0-9]/.test(pw)) score++;
  const hints = ['Too short', 'Add length', 'Add upper and lower case', 'Add a number', 'Add a symbol'];
  return { score: score as 0 | 1 | 2 | 3 | 4, hint: hints[score] };
}
