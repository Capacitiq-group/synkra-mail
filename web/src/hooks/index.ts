import { useCallback, useEffect, useRef, useState } from 'react';

export function useDebounce<T>(value: T, delayMs = 250): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(t);
  }, [value, delayMs]);
  return debounced;
}

export function useLocalStorage<T>(key: string, initial: T): [T, (v: T) => void] {
  const [value, setValue] = useState<T>(() => {
    try {
      const raw = localStorage.getItem(key);
      return raw ? (JSON.parse(raw) as T) : initial;
    } catch { return initial; }
  });
  const update = useCallback((v: T) => {
    setValue(v);
    try { localStorage.setItem(key, JSON.stringify(v)); } catch { /* quota */ }
  }, [key]);
  return [value, update];
}

export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() => typeof window !== 'undefined' && window.matchMedia(query).matches);
  useEffect(() => {
    const mq = window.matchMedia(query);
    const handler = () => setMatches(mq.matches);
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, [query]);
  return matches;
}

export function useOnClickOutside<T extends HTMLElement>(ref: React.RefObject<T>, handler: () => void) {
  useEffect(() => {
    const listener = (e: MouseEvent | TouchEvent) => {
      if (!ref.current || ref.current.contains(e.target as Node)) return;
      handler();
    };
    document.addEventListener('mousedown', listener);
    document.addEventListener('touchstart', listener);
    return () => {
      document.removeEventListener('mousedown', listener);
      document.removeEventListener('touchstart', listener);
    };
  }, [ref, handler]);
}

export function useOnlineStatus(): boolean {
  const [online, setOnline] = useState(navigator.onLine);
  useEffect(() => {
    const up = () => setOnline(true);
    const down = () => setOnline(false);
    window.addEventListener('online', up);
    window.addEventListener('offline', down);
    return () => {
      window.removeEventListener('online', up);
      window.removeEventListener('offline', down);
    };
  }, []);
  return online;
}

export function usePageTitle(title: string) {
  useEffect(() => { document.title = title ? `${title} · Synkra Mail` : 'Synkra Mail'; }, [title]);
}

export function useEscapeKey(handler: () => void, enabled = true) {
  useEffect(() => {
    if (!enabled) return;
    const listener = (e: KeyboardEvent) => { if (e.key === 'Escape') handler(); };
    window.addEventListener('keydown', listener);
    return () => window.removeEventListener('keydown', listener);
  }, [handler, enabled]);
}

export interface Shortcut {
  keys: string;
  handler: (e: KeyboardEvent) => void;
  description?: string;
  when?: (e: KeyboardEvent) => boolean;
}

export function useKeyboardShortcuts(shortcuts: Shortcut[], opts: { enabled?: boolean } = {}) {
  const ref = useRef(shortcuts);
  ref.current = shortcuts;
  useEffect(() => {
    if (opts.enabled === false) return;
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      const inField = target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName);
      for (const s of ref.current) {
        const parts = s.keys.toLowerCase().split('+');
        const key = parts.pop()!;
        const wantShift = parts.includes('shift');
        const wantAlt = parts.includes('alt');
        const wantCtrl = parts.includes('ctrl') || parts.includes('cmd');
        if (e.key.toLowerCase() !== key) continue;
        if (!!e.shiftKey !== wantShift) continue;
        if (!!e.altKey !== wantAlt) continue;
        if ((e.ctrlKey || e.metaKey) !== wantCtrl) continue;
        if (inField && !wantCtrl) continue;
        if (s.when && !s.when(e)) continue;
        e.preventDefault();
        s.handler(e);
        return;
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [opts.enabled]);
}

export function useIntersectionObserver(cb: () => void, opts: { rootMargin?: string; enabled?: boolean } = {}) {
  const ref = useRef<HTMLDivElement | null>(null);
  const calledRef = useRef(false);
  useEffect(() => {
    if (!ref.current || opts.enabled === false) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && !calledRef.current) {
          calledRef.current = true;
          cb();
          setTimeout(() => { calledRef.current = false; }, 500);
        }
      },
      { rootMargin: opts.rootMargin ?? '200px' },
    );
    observer.observe(ref.current);
    return () => observer.disconnect();
  }, [cb, opts.rootMargin, opts.enabled]);
  return ref;
}
