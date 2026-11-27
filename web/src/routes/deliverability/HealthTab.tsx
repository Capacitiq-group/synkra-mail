import { Link } from 'react-router-dom';
import { useHealthScore } from '@/hooks/queries/deliverability';
import { Button, Skeleton } from '@/design';
import { Icon } from '@/design/icons';

export function HealthTab() {
  const q = useHealthScore();

  if (q.isLoading) return <Skeleton className="h-60 w-full" />;
  if (q.isError || !q.data) return <p className="text-sm text-danger">Could not load health score.</p>;

  const h = q.data;
  const tone =
    h.score >= 85 ? 'success' :
    h.score >= 65 ? 'accent' :
    h.score >= 40 ? 'warning' :
    'danger';
  const toneCls = {
    success: 'text-success',
    accent: 'text-accent',
    warning: 'text-warning',
    danger: 'text-danger',
  }[tone];

  return (
    <div className="space-y-6">
      <section className="border border-line rounded-lg p-6 bg-surface flex flex-col md:flex-row items-start md:items-center gap-6">
        <div className="flex items-center gap-4">
          <div className="relative w-28 h-28">
            <svg viewBox="0 0 36 36" className="w-full h-full -rotate-90">
              <circle cx="18" cy="18" r="16" fill="none" stroke="rgb(var(--line))" strokeWidth="3" />
              <circle
                cx="18" cy="18" r="16" fill="none"
                stroke={`rgb(var(--${tone === 'accent' ? 'accent' : tone}))`}
                strokeWidth="3"
                strokeLinecap="round"
                strokeDasharray={`${(h.score / 100) * 100.53} 100.53`}
              />
            </svg>
            <div className="absolute inset-0 flex items-center justify-center">
              <span className={`text-2xl font-semibold ${toneCls}`}>{h.score}</span>
            </div>
          </div>
          <div>
            <p className="text-xs text-ink-mute">Business email health</p>
            <p className="text-lg font-semibold capitalize">{tone}</p>
            <p className="text-xs text-ink-soft mt-1">Scored across six dimensions. Improve the lowest first.</p>
          </div>
        </div>
      </section>

      <section className="space-y-3">
        {h.categories.map((c) => (
          <div key={c.key} className="border border-line rounded-lg bg-surface p-4">
            <div className="flex items-center justify-between gap-2">
              <div>
                <p className="font-medium">{c.label}</p>
                <p className="text-xs text-ink-soft capitalize">{c.status}</p>
              </div>
              <p className={`text-xl font-semibold ${catTone(c.status)}`}>{c.score}</p>
            </div>
            <div className="mt-2">
              <div className="h-1.5 bg-line rounded overflow-hidden">
                <div
                  className={`h-full ${catBar(c.status)}`}
                  style={{ width: `${c.score}%` }}
                />
              </div>
            </div>
            {c.improvements.length > 0 && (
              <ul className="mt-3 space-y-2">
                {c.improvements.map((imp, i) => (
                  <li key={i} className="flex items-start gap-2 text-sm">
                    <Icon name="info" size={14} className="text-ink-mute mt-0.5" />
                    <div className="flex-1">
                      <p className="font-medium">{imp.title}</p>
                      <p className="text-xs text-ink-soft">{imp.detail}</p>
                      {imp.link && (
                        <Link to={imp.link} className="text-xs text-accent hover:underline">Fix this →</Link>
                      )}
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>
        ))}
      </section>

      <section className="border border-line rounded-lg bg-surface p-4">
        <p className="text-sm font-medium mb-2">Next steps</p>
        <div className="flex flex-wrap gap-2">
          <Link to="/settings/deliverability?tab=reliability"><Button variant="secondary">View reliability alerts</Button></Link>
          <Link to="/settings/deliverability?tab=domains"><Button variant="secondary">Domains</Button></Link>
        </div>
      </section>
    </div>
  );
}

function catTone(status: 'excellent' | 'good' | 'fair' | 'poor'): string {
  return status === 'excellent' ? 'text-success'
    : status === 'good' ? 'text-accent'
    : status === 'fair' ? 'text-warning'
    : 'text-danger';
}
function catBar(status: 'excellent' | 'good' | 'fair' | 'poor'): string {
  return status === 'excellent' ? 'bg-success'
    : status === 'good' ? 'bg-accent'
    : status === 'fair' ? 'bg-warning'
    : 'bg-danger';
}
