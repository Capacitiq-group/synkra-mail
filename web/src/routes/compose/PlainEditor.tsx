export function PlainEditor({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <textarea
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder="Write your message…"
      className="flex-1 w-full resize-none px-3 py-2 font-mono text-sm bg-transparent focus:outline-none min-h-[240px]"
      aria-label="Message body (plain text)"
      spellCheck
    />
  );
}
