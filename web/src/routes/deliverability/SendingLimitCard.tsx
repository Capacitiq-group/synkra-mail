import { ProgressBar } from '@/design';
import { formatNumber } from '@/lib/format';

// Correct imports:
// eslint-disable-next-line
import { formatDateTime as fmtDateTime } from '@/lib/time';

export function SendingLimitCard({ limit }: { limit: { current: number; limit: number; resets_at: string } }) {
  const pct = limit.limit > 0 ? (limit.current / limit.limit) * 100 : 0;
  return (
    <div className="border border-line rounded-lg bg-surface p-3">
      <div className="flex items-center justify-between gap-2">
        <p className="text-sm font-medium">Sending limit</p>
        <p className="text-xs text-ink-soft">Resets {fmtDateTime(limit.resets_at)}</p>
      </div>
      <div className="mt-2">
        <ProgressBar value={limit.current} max={limit.limit} />
        <p className="text-xs text-ink-soft mt-1">
          {formatNumber(limit.current)} of {formatNumber(limit.limit)} sent ({pct.toFixed(0)}%)
        </p>
      </div>
    </div>
  );
}


