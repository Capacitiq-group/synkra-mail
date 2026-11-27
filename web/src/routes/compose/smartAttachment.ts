const ATTACHMENT_HINTS = [
  /\b(attach|attached|attachment|attaching|enclos(ed|ing|ure))\b/i,
  /\bplease find\b/i,
  /\bsee (the )?attach(ed|ment)/i,
  /\bfind (the )?attach(ed|ment)/i,
];

export function shouldWarnAboutMissingAttachment(bodyText: string, attachmentCount: number): boolean {
  if (attachmentCount > 0) return false;
  if (!bodyText.trim()) return false;
  return ATTACHMENT_HINTS.some((re) => re.test(bodyText));
}
