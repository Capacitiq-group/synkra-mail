import { useLayoutEffect, useRef, useState } from 'react';

export interface ChartSeries {
  label: string;
  color: string;
  points: { date: string; value: number }[];
}

interface Props {
  series: ChartSeries[];
  height?: number;
  formatY?: (n: number) => string;
  yMax?: number;
  emptyLabel?: string;
}

/**
 * Responsive SVG line chart with hover tooltip. No external chart library —
 * this is deliberately dependency-free so it works offline and has a stable
 * bundle size.
 */
export function TimeSeriesChart({
  series, height = 240, formatY = defaultFormat, yMax, emptyLabel = 'No data for this period.',
}: Props) {
  const [ref, width] = useContainerWidth<HTMLDivElement>();
  const [hoverIdx, setHoverIdx] = useState<number | null>(null);

  const padding = { top: 12, right: 16, bottom: 28, left: 48 };
  const innerW = Math.max(0, width - padding.left - padding.right);
  const innerH = Math.max(0, height - padding.top - padding.bottom);

  // Merge all x dates into a sorted unique axis.
  const dates = Array.from(
    new Set(series.flatMap((s) => s.points.map((p) => p.date))),
  ).sort();

  const maxValue = yMax ?? Math.max(
    1,
    ...series.flatMap((s) => s.points.map((p) => p.value)),
  );
  const niceMax = niceCeil(maxValue);

  const xAt = (i: number) =>
    padding.left + (dates.length <= 1 ? innerW / 2 : (i / (dates.length - 1)) * innerW);
  const yAt = (v: number) => padding.top + innerH - (v / niceMax) * innerH;

  // Y grid lines — 4 steps.
  const ySteps = 4;
  const yGrid = Array.from({ length: ySteps + 1 }, (_, i) => {
    const value = (niceMax / ySteps) * i;
    return { value, y: yAt(value) };
  });

  // X labels — show ~6 evenly spaced.
  const xLabelIdxs = pickLabelIndices(dates.length, 6);

  function onMove(e: React.MouseEvent<SVGSVGElement>) {
    const rect = e.currentTarget.getBoundingClientRect();
    const x = e.clientX - rect.left - padding.left;
    if (dates.length === 0 || innerW <= 0) return;
    const ratio = Math.max(0, Math.min(1, x / innerW));
    const idx = Math.round(ratio * (dates.length - 1));
    setHoverIdx(idx);
  }

  const hoverDate = hoverIdx !== null ? dates[hoverIdx] : null;
  const hoverPoints = hoverDate
    ? series.map((s) => {
        const point = s.points.find((p) => p.date === hoverDate);
        return { label: s.label, color: s.color, value: point?.value ?? 0 };
      })
    : [];

  if (series.every((s) => s.points.length === 0)) {
    return (
      <div ref={ref} className="flex items-center justify-center text-sm text-ink-soft" style={{ height }}>
        {emptyLabel}
      </div>
    );
  }

  return (
    <div ref={ref} className="relative" style={{ height }}>
      <svg
        width={width}
        height={height}
        onMouseMove={onMove}
        onMouseLeave={() => setHoverIdx(null)}
        role="img"
        aria-label="Deliverability time series"
      >
        {/* Y grid + labels */}
        {yGrid.map((g) => (
          <g key={g.value}>
            <line
              x1={padding.left} y1={g.y}
              x2={padding.left + innerW} y2={g.y}
              stroke="rgb(var(--line))" strokeWidth={1}
              strokeDasharray={g.value === 0 ? undefined : '2 3'}
            />
            <text
              x={padding.left - 8} y={g.y + 4}
              textAnchor="end" fontSize={10}
              fill="rgb(var(--ink-mute))"
            >
              {formatY(g.value)}
            </text>
          </g>
        ))}

        {/* X labels */}
        {xLabelIdxs.map((i) => (
          <text
            key={i}
            x={xAt(i)} y={height - 8}
            textAnchor="middle" fontSize={10}
            fill="rgb(var(--ink-mute))"
          >
            {shortDate(dates[i])}
          </text>
        ))}

        {/* Series */}
        {series.map((s) => {
          if (s.points.length === 0) return null;
          const sortedPoints = [...s.points].sort((a, b) => a.date.localeCompare(b.date));
          const segments = sortedPoints.map((p) => {
            const xIdx = dates.indexOf(p.date);
            return { x: xAt(xIdx), y: yAt(p.value) };
          });
          const d = segments
            .map((pt, i) => `${i === 0 ? 'M' : 'L'} ${pt.x.toFixed(2)} ${pt.y.toFixed(2)}`)
            .join(' ');
          const area =
            segments.length > 0
              ? `${d} L ${segments[segments.length - 1].x.toFixed(2)} ${(padding.top + innerH).toFixed(2)} L ${segments[0].x.toFixed(2)} ${(padding.top + innerH).toFixed(2)} Z`
              : '';
          return (
            <g key={s.label}>
              <path d={area} fill={s.color} fillOpacity={0.08} />
              <path d={d} fill="none" stroke={s.color} strokeWidth={1.75} strokeLinejoin="round" strokeLinecap="round" />
            </g>
          );
        })}

        {/* Hover crosshair */}
        {hoverIdx !== null && hoverDate && (
          <g>
            <line
              x1={xAt(hoverIdx)} y1={padding.top}
              x2={xAt(hoverIdx)} y2={padding.top + innerH}
              stroke="rgb(var(--ink-mute))" strokeWidth={1} strokeDasharray="2 3"
            />
            {series.map((s) => {
              const p = s.points.find((x) => x.date === hoverDate);
              if (!p) return null;
              return (
                <circle
                  key={s.label}
                  cx={xAt(hoverIdx)} cy={yAt(p.value)}
                  r={3} fill={s.color} stroke="rgb(var(--surface))" strokeWidth={2}
                />
              );
            })}
          </g>
        )}
      </svg>

      {/* Tooltip */}
      {hoverIdx !== null && hoverDate && (
        <div
          className="absolute top-2 right-2 pointer-events-none rounded shadow-md border border-line bg-surface p-2 text-xs space-y-1 z-10"
          role="status"
        >
          <div className="font-medium">{fullDate(hoverDate)}</div>
          {hoverPoints.map((p) => (
            <div key={p.label} className="flex items-center gap-2">
              <span className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: p.color }} />
              <span className="text-ink-soft flex-1">{p.label}</span>
              <span className="font-medium">{formatY(p.value)}</span>
            </div>
          ))}
        </div>
      )}

      {/* Legend */}
      <div className="absolute bottom-0 right-2 flex flex-wrap gap-2 text-xs text-ink-soft">
        {series.map((s) => (
          <span key={s.label} className="inline-flex items-center gap-1">
            <span className="w-2 h-2 rounded-full" style={{ backgroundColor: s.color }} />
            {s.label}
          </span>
        ))}
      </div>
    </div>
  );
}

function useContainerWidth<T extends HTMLElement>(): [React.RefObject<T>, number] {
  const ref = useRef<T>(null);
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    if (!ref.current) return;
    const el = ref.current;
    const ro = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    ro.observe(el);
    setWidth(el.clientWidth);
    return () => ro.disconnect();
  }, []);
  return [ref, width];
}

function niceCeil(n: number): number {
  if (n <= 5) return 5;
  if (n <= 10) return 10;
  if (n <= 25) return 25;
  if (n <= 50) return 50;
  if (n <= 100) return 100;
  if (n <= 250) return 250;
  if (n <= 500) return 500;
  if (n <= 1000) return 1000;
  const mag = Math.pow(10, Math.floor(Math.log10(n)));
  return Math.ceil(n / mag) * mag;
}

function defaultFormat(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k`;
  return String(Math.round(n));
}

function pickLabelIndices(total: number, count: number): number[] {
  if (total <= count) return Array.from({ length: total }, (_, i) => i);
  const out: number[] = [];
  for (let i = 0; i < count; i++) {
    out.push(Math.round((i / (count - 1)) * (total - 1)));
  }
  return Array.from(new Set(out));
}

function shortDate(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleDateString('en-ZA', { day: 'numeric', month: 'short' });
}

function fullDate(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleDateString('en-ZA', { day: 'numeric', month: 'long', year: 'numeric' });
}
