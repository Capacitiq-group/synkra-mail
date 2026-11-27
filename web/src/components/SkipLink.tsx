export function SkipLink({ targetId = 'main-content' }: { targetId?: string }) {
  return (
    <a
      href={`#${targetId}`}
      className="sr-only-focusable fixed top-2 left-2 z-[100] px-3 py-1.5 rounded bg-accent text-white text-sm"
    >
      Skip to main content
    </a>
  );
}
