import { useEffect, type ReactNode } from 'react';
import { ErrorBoundary, ThemeProvider, ToastViewport } from '@/components/system';
import { LiveRegionProvider } from '@/components/LiveRegion';
import { useAuth } from '@/stores/auth';
import { api } from '@/lib/api';
import { queryClient } from '@/lib/queryClient';

export function AppProviders({ children }: { children: ReactNode }) {
  // Fetch /me once at boot and drive auth state from it. 401 → unauthenticated.
  useEffect(() => {
    const controller = new AbortController();
    const { setMe, setStatus } = useAuth.getState();
    setStatus('loading');
    api.me(controller.signal)
      .then((me) => setMe(me))
      .catch((err) => {
        if (controller.signal.aborted) return;
        const status = (err as { status?: number }).status;
        if (status === 401) { setMe(null); setStatus('unauthenticated'); }
        else { setStatus('error'); }
      });
    return () => controller.abort();
  }, []);

  // Wire 401 from any mutation/query to the auth store.
  useEffect(() => {
    const unsub = queryClient.getQueryCache().subscribe((ev) => {
      if (ev.type === 'updated' && ev.action.type === 'error') {
        const err = ev.action.error as { status?: number };
        if (err && err.status === 401) useAuth.getState().setMe(null);
      }
    });
    return () => unsub();
  }, []);

  return (
    <ErrorBoundary>
      <ThemeProvider>
        <LiveRegionProvider>
          {children}
          <ToastViewport />
        </LiveRegionProvider>
      </ThemeProvider>
    </ErrorBoundary>
  );
}
