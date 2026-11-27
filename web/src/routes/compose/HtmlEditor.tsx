export function HtmlEditor({ value, onChange }: { value: string; onChange: (html: string) => void }) {
  return (
    <div className="flex-1 flex flex-col min-h-0">
      <div className="px-3 py-1 text-xs text-ink-soft border-b border-line bg-canvas">
        Edit HTML source. Changes take effect on send. Unsafe elements are stripped when the message is displayed to recipients.
      </div>
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="<p>Write your message…</p>"
        className="flex-1 w-full resize-none px-3 py-2 font-mono text-xs bg-transparent focus:outline-none min-h-[240px]"
        aria-label="Message body (HTML source)"
        spellCheck={false}
      />
    </div>
  );
}
