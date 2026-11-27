import { useSubProcessors } from '@/hooks/queries/privacy';
import { EmptyState, Skeleton } from '@/design';

export function SubProcessors() {
  const q = useSubProcessors();
  if (q.isLoading) return <Skeleton className="h-40 w-full" />;
  if (q.isError) return <p className="text-sm text-danger">Could not load sub-processor list.</p>;
  if (!q.data || q.data.length === 0) return <EmptyState title="No sub-processors" />;

  return (
    <div className="space-y-3 max-w-3xl">
      <p className="text-sm text-ink-soft">
        Organisations we use to deliver the service. Where a sub-processor is outside South Africa, a Section 72 transfer
        safeguard applies.
      </p>
      <div className="overflow-x-auto">
        <table className="w-full text-sm border border-line rounded-lg overflow-hidden">
          <thead className="bg-canvas text-left">
            <tr>
              <th className="p-2">Name</th>
              <th className="p-2">Purpose</th>
              <th className="p-2">Country</th>
              <th className="p-2">Transfer safeguard</th>
            </tr>
          </thead>
          <tbody>
            {q.data.map((p, i) => (
              <tr key={i} className="border-t border-line">
                <td className="p-2 font-medium">{p.name}</td>
                <td className="p-2 text-ink-soft">{p.purpose}</td>
                <td className="p-2">{p.country}</td>
                <td className="p-2 text-ink-soft">{p.transfer_safeguard ?? '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
