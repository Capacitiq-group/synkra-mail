import { useComposeStore } from './useComposeStore';
import { Select } from '@/design';

export function IdentityPicker() {
  const identities = useComposeStore((s) => s.identities);
  const identityId = useComposeStore((s) => s.identityId);
  const setIdentity = useComposeStore((s) => s.setIdentity);

  if (identities.length <= 1) return null;

  return (
    <div className="flex items-center gap-2 px-3 py-1.5 border-b border-line">
      <label htmlFor="identity-from" className="text-sm text-ink-soft w-10 shrink-0">From</label>
      <Select
        id="identity-from"
        value={identityId}
        onChange={(e) => setIdentity(e.target.value)}
        className="max-w-xs text-sm"
      >
        {identities.map((i) => (
          <option key={i.id} value={i.id}>
            {i.display_name ? `${i.display_name} <${i.address}>` : i.address}
            {i.is_default ? ' (default)' : ''}
          </option>
        ))}
      </Select>
    </div>
  );
}
