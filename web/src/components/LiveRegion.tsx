import { createContext, useCallback, useContext, useState, type ReactNode } from 'react';

interface LiveRegionContext { announce: (message: string) => void }
const Ctx = createContext<LiveRegionContext>({ announce: () => undefined });

export function LiveRegionProvider({ children }: { children: ReactNode }) {
  const [message, setMessage] = useState('');
  const announce = useCallback((m: string) => {
    setMessage('');
    // Force re-announcement by clearing then setting asynchronously
    requestAnimationFrame(() => setMessage(m));
  }, []);
  return (
    <Ctx.Provider value={{ announce }}>
      {children}
      <div aria-live="polite" aria-atomic="true" className="sr-only">{message}</div>
    </Ctx.Provider>
  );
}

export function useAnnounce() { return useContext(Ctx).announce; }
