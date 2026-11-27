import { useAuth } from '@/stores/auth';
import { useComposeStore } from './useComposeStore';
import { Icon } from '@/design/icons';
import { relativeTime } from '@/lib/time';
import { Badge } from '@/design';

export function SharedComposerBanner() {
  const me = useAuth((s) => s.me);
  const sharedMailboxId = useComposeStore((s) => s.sharedMailboxId);
  const sharedDisplayName = useComposeStore((s) => s.sharedDisplayName);
  const owner = useComposeStore((s) => s.sharedReplyOwner);
  const state = useComposeStore((s) => s.sharedClaimState);

  if (!sharedMailboxId) return null;

  return (
    <div className="px-3 py-1.5 border-b border-line bg-accent-soft text-xs flex items-center gap-2 flex-wrap">
      <Badge tone="accent">Shared</Badge>
      <span>
        Replying from <strong>{sharedDisplayName}</strong>
        {state === 'claimed' && ' · you own this reply'}
        {state === 'claiming' && ' · claiming…'}
      </span>
      {state === 'competing' && owner && (
        <span className="ml-auto text-warning inline-flex items-center gap-1">
          <Icon name="alert-triangle" size={12} />
          {owner.user.name ?? owner.user.email} started a reply {relativeTime(owner.claimed_at)}
          {' — they will be notified if you send'}
        </span>
      )}
      {state === 'claimed' && me && (
        <span className="ml-auto text-success inline-flex items-center gap-1">
          <Icon name="check-circle" size={12} />
          Claim active
        </span>
      )}
    </div>
  );
}
