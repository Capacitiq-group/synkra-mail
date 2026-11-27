import { useAuth } from '@/stores/auth';

export function useMailboxActions() {
  const me = useAuth((s) => s.me);
  const isOrgAdmin = !!me && ['owner', 'admin'].includes(me.role);
  return { isOrgAdmin };
}
