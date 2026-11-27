import { Icon } from '@/design/icons';

export function BlacklistPanel({
  blacklists,
}: {
  blacklists: { name: string; listed: boolean; delist_url?: string }[];
}) {
  if (blacklists.length === 0) {
    return <p className="text-sm text-ink-soft">No blacklists are monitored for this domain yet.</p>;
  }
  const listed = blacklists.filter((b) => b.listed);
  return (
    <div className="border border-line rounded-lg bg-surface overflow-hidden">
      <div className={`px-3 py-2 flex items-center gap-2 ${listed.length > 0 ? 'bg-danger-soft' : 'bg-success-soft'}`}>
        <Icon
          name={listed.length > 0 ? 'alert-triangle' : 'shield-check'}
          size={14}
          className={listed.length > 0 ? 'text-danger' : 'text-success'}
        />
        <p className="text-sm font-medium">
          {listed.length === 0
            ? 'Not currently listed on any monitored blacklist.'
            : `Listed on ${listed.length} blacklist${listed.length === 1 ? '' : 's'}.`}
        </p>
      </div>
      <table className="w-full text-sm">
        <thead className="bg-canvas text-left border-t border-line">
          <tr>
            <th className="p-2 font-medium">Blacklist</th>
            <th className="p-2 font-medium">Status</th>
            <th className="p-2 font-medium"></th>
          </tr>
        </thead>
        <tbody>
          {blacklists.map((b) => (
            <tr key={b.name} className="border-t border-line">
              <td className="p-2">{b.name}</td>
              <td className="p-2">
                {b.listed ? (
                  <span className="text-danger inline-flex items-center gap-1">
                    <Icon name="x-circle" size={12} /> Listed
                  </span>
                ) : (
                  <span className="text-success inline-flex items-center gap-1">
                    <Icon name="check" size={12} /> Clean
                  </span>
                )}
              </td>
              <td className="p-2 text-right">
                {b.listed && b.delist_url && (
                  <a href={b.delist_url} target="_blank" rel="noopener noreferrer" className="text-accent hover:underline text-xs">
                    Delist
                  </a>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
