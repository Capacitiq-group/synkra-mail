import { useComposeStore } from './useComposeStore';
import { RecipientField } from './RecipientField';
import { IdentityPicker } from './IdentityPicker';

export function ComposeHeader({ autoFocusTo }: { autoFocusTo: boolean }) {
  const to = useComposeStore((s) => s.to);
  const cc = useComposeStore((s) => s.cc);
  const bcc = useComposeStore((s) => s.bcc);
  const showCc = useComposeStore((s) => s.showCc);
  const showBcc = useComposeStore((s) => s.showBcc);
  const subject = useComposeStore((s) => s.subject);
  const setRecipients = useComposeStore((s) => s.setRecipients);
  const setSubject = useComposeStore((s) => s.setSubject);
  const setShowCc = useComposeStore((s) => s.setShowCc);
  const setShowBcc = useComposeStore((s) => s.setShowBcc);

  return (
    <>
      <IdentityPicker />
      <RecipientField
        label="To"
        values={to}
        onChange={(list) => setRecipients('to', list)}
        placeholder="Recipients"
        autoFocus={autoFocusTo}
      />
      {showCc && (
        <RecipientField
          label="Cc"
          values={cc}
          onChange={(list) => setRecipients('cc', list)}
          placeholder="Cc"
        />
      )}
      {showBcc && (
        <RecipientField
          label="Bcc"
          values={bcc}
          onChange={(list) => setRecipients('bcc', list)}
          placeholder="Bcc"
        />
      )}
      {!showCc && !showBcc && (
        <div className="flex items-center gap-3 px-3 py-1.5 text-xs text-ink-soft">
          <button type="button" onClick={() => setShowCc(true)} className="hover:text-ink underline">Cc</button>
          <button type="button" onClick={() => setShowBcc(true)} className="hover:text-ink underline">Bcc</button>
        </div>
      )}
      {!showCc && showBcc && (
        <div className="flex items-center gap-3 px-3 py-1.5 text-xs text-ink-soft">
          <button type="button" onClick={() => setShowCc(true)} className="hover:text-ink underline">Cc</button>
        </div>
      )}
      {showCc && !showBcc && (
        <div className="flex items-center gap-3 px-3 py-1.5 text-xs text-ink-soft">
          <button type="button" onClick={() => setShowBcc(true)} className="hover:text-ink underline">Bcc</button>
        </div>
      )}
      <div className="flex items-center gap-2 px-3 py-1.5 border-b border-line">
        <label htmlFor="compose-subject" className="text-sm text-ink-soft w-10 shrink-0">Subject</label>
        <input
          id="compose-subject"
          type="text"
          value={subject}
          onChange={(e) => setSubject(e.target.value)}
          placeholder="Subject"
          className="flex-1 bg-transparent text-sm focus:outline-none"
        />
      </div>
    </>
  );
}
