import { useProcessingRegister } from '@/hooks/queries/privacy';
import { EmptyState, Skeleton } from '@/design';

export function ProcessingRegister() {
  const q = useProcessingRegister();

  if (q.isLoading) return <Skeleton className="h-40 w-full" />;
  if (q.isError) return <p className="text-sm text-danger">Could not load processing register.</p>;
  if (!q.data || q.data.length === 0) {
    return <EmptyState title="No entries" description="The processing register has no entries yet." />;
  }

  return (
    <div className="space-y-3 max-w-3xl">
      <p className="text-sm text-ink-soft">
        Every processing activity we perform, with its lawful basis, categories of data and retention. This is a POPIA
        Section 18 obligation for the responsible party, and we operate on your behalf as an operator (Section 21).
      </p>

      <ul className="space-y-3">
        {q.data.map((e) => (
          <li key={e.id} className="border border-line rounded-lg p-4">
            <p className="font-medium">{e.purpose}</p>
            <dl className="mt-2 text-sm space-y-1">
              <Row label="Lawful basis" value={e.lawful_basis} />
              <Row label="Data categories" value={e.data_categories.join(', ')} />
              <Row label="Retention" value={e.retention} />
              <Row label="Recipients" value={e.recipients.join(', ')} />
            </dl>
          </li>
        ))}
      </ul>
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-wrap gap-2">
      <dt className="w-32 text-ink-mute shrink-0">{label}</dt>
      <dd className="flex-1 min-w-0">{value}</dd>
    </div>
  );
}
