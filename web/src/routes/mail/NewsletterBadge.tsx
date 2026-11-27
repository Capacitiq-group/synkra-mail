import { Icon } from '@/design/icons';

/**
 * Small visual indicator on the message list for messages classified as
 * newsletters. Uses the same API classification the Newsletters tab uses.
 */
export function NewsletterBadge({ classification }: { classification: 'newsletter' | undefined }) {
  if (classification !== 'newsletter') return null;
  return (
    <span title="Newsletter" aria-label="Newsletter">
      <Icon name="mail-open" size={12} className="text-ink-mute" />
    </span>
  );
}
