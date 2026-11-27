import { useEffect, useRef } from 'react';

/**
 * Detects a two-key sequence (e.g. "g" then "i") within 800ms.
 * Respects input focus: ignored while typing in a field.
 */
export function useSequenceShortcut(
  sequence: string[],
  handler: () => void,
  enabled = true,
) {
  const ref = useRef({ step: 0, timer: 0 as unknown as ReturnType<typeof setTimeout> });
  useEffect(() => {
    if (!enabled) return;
    const listener = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      const inField =
        target.isContentEditable ||
        ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName);
      if (inField) return;
      if (e.ctrlKey || e.metaKey || e.altKey) return;

      const key = e.key.toLowerCase();
      if (ref.current.step === 0) {
        if (key === sequence[0]) {
          ref.current.step = 1;
          clearTimeout(ref.current.timer);
          ref.current.timer = setTimeout(() => {
            ref.current.step = 0;
          }, 800);
        }
        return;
      }
      if (ref.current.step === 1) {
        if (key === sequence[1]) {
          e.preventDefault();
          handler();
        }
        ref.current.step = 0;
        clearTimeout(ref.current.timer);
      }
    };
    window.addEventListener('keydown', listener);
    return () => {
      clearTimeout(ref.current.timer);
      window.removeEventListener('keydown', listener);
    };
  }, [sequence, handler, enabled]);
}
