import { Component, useEffect, type ErrorInfo, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { ApiError } from '@/lib/errors';
import { Button, EmptyState } from '@/design';
import { useToast } from '@/stores/toast';
import { useTheme } from '@/stores/theme';

// ===== ErrorBoundary =====
interface EBProps { children: ReactNode }
interface EBState { error: Error | null }

export class ErrorBoundary extends Component<EBProps, EBState> {
  state: EBState = { error: null };
  static getDerivedStateFromError(error: Error): EBState { return { error }; }
  componentDidCatch(error: Error, info: ErrorInfo) {
    // eslint-disable-next-line no-console
    console.error('Unhandled render error', error, info);
  }
  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div className="min-h-full flex items-center justify-center p-6 bg-canvas">
        <div className="max-w-md w-full text-center">
          <EmptyState
            title="Something went wrong"
            description={this.state.error.message || 'An unexpected error occurred.'}
            action={
              <div className="flex gap-2 justify-center">
                <Button onClick={() => this.setState({ error: null })}>Try again</Button>
                <Button variant="secondary" onClick={() => { window.location.href = '/'; }}>Back to mail</Button>
              </div>
            }
          />
        </div>
      </div>
    );
  }
}

// ===== ThemeProvider =====
export function ThemeProvider({ children }: { children: ReactNode }) {
  const apply = useTheme((s) => s.apply);
  useEffect(() => { apply(); }, [apply]);
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const handler = () => { if (useTheme.getState().mode === 'system') apply(); };
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, [apply]);
  return <>{children}</>;
}

// ===== Toast viewport =====
export function ToastViewport() {
  const items = useToast((s) => s.items);
  const remove = useToast((s) => s.remove);
  const tones: Record<string, string> = {
    info: 'border-line bg-surface',
    success: 'border-success bg-success-soft',
    warning: 'border-warning bg-warning-soft',
    error: 'border-danger bg-danger-soft',
  };
  return (
    <div aria-live="polite" aria-atomic="false" className="fixed bottom-4 right-4 z-[60] flex flex-col gap-2 max-w-sm">
      {items.map((t) => (
        <div key={t.id} className={`rounded border shadow-md p-3 animate-slide-up ${tones[t.kind]}`} role="status">
          <div className="flex items-start gap-2">
            <div className="flex-1">
              <div className="text-sm font-medium">{t.title}</div>
              {t.description && <div className="text-xs text-ink-soft mt-0.5">{t.description}</div>}
            </div>
            <button onClick={() => remove(t.id)} className="text-xs text-ink-soft hover:text-ink" aria-label="Dismiss">×</button>
          </div>
          {t.action && (
            <button onClick={t.action.onClick} className="mt-2 text-xs underline">{t.action.label}</button>
          )}
        </div>
      ))}
    </div>
  );
}

// ===== Offline banner =====
export function OfflineBanner() {
  const online = typeof navigator !== 'undefined' ? navigator.onLine : true;
  if (online) return null;
  return (
    <div className="px-3 py-1.5 text-xs bg-warning-soft text-warning border-b border-line" role="status">
      You are offline. Changes will sync when you reconnect.
    </div>
  );
}

// ===== Splash =====
export function Splash() {
  return (
    <div className="min-h-full flex items-center justify-center bg-canvas">
      <div className="text-center">
        <div className="w-12 h-12 rounded-lg bg-accent text-white flex items-center justify-center text-xl font-bold mx-auto">S</div>
        <p className="mt-3 text-sm text-ink-soft">Loading Synkra Mail…</p>
      </div>
    </div>
  );
}

// ===== API error view =====
export function ApiErrorView({ error, retry }: { error: unknown; retry?: () => void }) {
  const surface = error instanceof ApiError ? error : null;
  return (
    <div className="p-6">
      <EmptyState
        title={surface?.status === 404 ? 'Not found' : surface?.status === 403 ? 'No access' : 'Could not load'}
        description={surface?.message || 'An unexpected error occurred.'}
        action={
          <div className="flex gap-2">
            {retry && <Button onClick={retry}>Try again</Button>}
            <Link to="/"><Button variant="secondary">Back to mail</Button></Link>
          </div>
        }
      />
    </div>
  );
}
