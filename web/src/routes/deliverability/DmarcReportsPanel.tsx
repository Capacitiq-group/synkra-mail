import { useDmarcReports } from '@/hooks/queries/deliverability';
import { EmptyState, Skeleton } from '@/design';
import { formatDate } from '@/lib/time';

export function DmarcReportsPanel({ domain }: { domain: string }) {
  const q = useDmarcReports(domain);

  if (q.isLoading) return <Skeleton className="h-40 w-full" />;
  if (q.isError) return <p className="text-sm text-danger">Could not load reports.</p>;
  if (!q.data || q.data.length === 0) {
    return (
      <EmptyState
        title="No DMARC reports yet"
        description="Aggregate reports arrive daily once your DMARC record includes a rua address and receivers start sending reports."
      />
    );
  }

  return (
    <div className="space-y-3">
      <p className="text-sm text-ink-soft">
        Aggregate reports sent by receiving servers. Use them to identify legitimate senders that fail DMARC before
        moving to a stricter policy.
      </p>
      <div className="border border-line rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-canvas text-left">
            <tr>
              <th className="p-2">Reporter</th>
              <th className="p-2">Date range</th>
              <th className="p-2 text-right">Total</th>
              <th className="p-2 text-right">Pass</th>
              <th className="p-2 text-right">Fail</th>
            </tr>
          </thead>
          <tbody>
            {q.data.map((r) => (
              <tr key={r.id} className="border-t border-line">
                <td className="p-2 font-mono text-xs">{r.reporter}</td>
                <td className="p-2 text-xs">
                  {formatDate(r.date_range_start)} – {formatDate(r.date_range_end)}
                </td>
                <td className="p-2 text-right">{r.total_messages.toLocaleString('en-ZA')}</td>
                <td className="p-2 text-right text-success">{r.pass_count.toLocaleString('en-ZA')}</td>
                <td className={`p-2 text-right ${r.fail_count > 0 ? 'text-danger' : ''}`}>{r.fail_count.toLocaleString('en-ZA')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
