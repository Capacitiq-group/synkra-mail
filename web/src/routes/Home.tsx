import { Navigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { Splash } from '@/components/system';

/**
 * Root redirect: after login, land on the first mailbox or the Compose
 * screen if the account has no mailbox yet. This route exists so that
 * bookmarks to "/" always resolve.
 */
export function HomeRoute() {
  const q = useQuery({ queryKey: ['mailboxes'], queryFn: api.mailboxes, staleTime: 30_000 });
  if (q.isLoading) return <Splash />;
  if (q.isError) return <Splash />;
  const first = q.data?.[0];
  if (first) return <Navigate to={`/mail/${first.id}`} replace />;
  return (
    <div className="min-h-full flex items-center justify-center p-6">
      <div className="text-center">
        <p className="text-sm text-ink-soft">Your organisation has no mailbox yet.</p>
        <p className="text-xs text-ink-mute mt-1">Contact your organisation administrator to be added to a mailbox.</p>
      </div>
    </div>
  );
}
