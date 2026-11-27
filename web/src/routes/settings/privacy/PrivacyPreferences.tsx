import { usePrivacySettings, useUpdatePrivacySettings } from '@/hooks/queries/privacy';
import { Checkbox, Skeleton } from '@/design';
import { errorMessage } from '@/lib/errors';
import { toast } from '@/stores/toast';

export function PrivacyPreferences() {
  const q = usePrivacySettings();
  const update = useUpdatePrivacySettings();

  if (q.isLoading) return <Skeleton className="h-40 w-full" />;
  if (!q.data) return <p className="text-sm text-ink-soft">Could not load privacy settings.</p>;

  async function set(key: 'block_remote_images' | 'strip_tracking_pixels' | 'external_link_warnings', value: boolean) {
    try {
      await update.mutateAsync({ [key]: value } as never);
      toast('success', 'Updated');
    } catch (e) {
      toast('error', 'Could not update', errorMessage(e));
    }
  }

  return (
    <div className="space-y-4 max-w-2xl">
      <p className="text-sm text-ink-soft">
        These settings apply to how <strong>we display mail</strong> in the webmail. They do not change what we scan for spam, malware and deliverability.
      </p>

      <div className="space-y-3 border border-line rounded-lg p-4">
        <Checkbox
          label="Block remote images by default"
          checked={q.data.block_remote_images}
          onChange={(e) => set('block_remote_images', e.target.checked)}
        />
        <p className="text-xs text-ink-mute ml-6 -mt-1">
          Remote images can tell senders you opened the message. You can show images per sender from any message.
        </p>

        <Checkbox
          label="Strip tracking pixels automatically"
          checked={q.data.strip_tracking_pixels}
          onChange={(e) => set('strip_tracking_pixels', e.target.checked)}
        />
        <p className="text-xs text-ink-mute ml-6 -mt-1">
          Removes 1×1 tracking pixels. Images you opt to load will still be proxied through us.
        </p>

        <Checkbox
          label="Warn about external links"
          checked={q.data.external_link_warnings}
          onChange={(e) => set('external_link_warnings', e.target.checked)}
        />
        <p className="text-xs text-ink-mute ml-6 -mt-1">
          Show a banner when a message contains links pointing outside your organisation.
        </p>
      </div>

      <div className="bg-canvas border border-line rounded-lg p-4 text-sm text-ink-soft">
        <p className="font-medium text-ink">What we process</p>
        <p className="mt-1">
          Message content is processed for spam, malware and deliverability only. We never profile content for advertising.
          Metadata (headers, delivery events, and classification) is retained per the schedule in the processing register.
        </p>
        <p className="mt-2">
          <a href="?tab=register" className="text-accent underline">See processing register</a>
          {' · '}
          <a href="?tab=sub-processors" className="text-accent underline">Sub-processors</a>
        </p>
      </div>
    </div>
  );
}
