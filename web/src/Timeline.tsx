import { useCallback, useMemo, useRef } from "react";
import { Pause, Play, Radio, SkipBack } from "lucide-react";
import { cn } from "@/lib/utils";
import { KIND_COLOR } from "./statusTheme";
import { dayFloor, formatDay, type ActivitySeries, type TimeRange } from "./timeline";

// Playback bar: play/pause, a speed, and a scrubber drawn over a chart of the
// tracker's history (issues still open in sky, finished in emerald, merged
// pull requests as a violet line). Dragging the chart moves the date; "Live"
// returns to today.

export const SPEEDS: { label: string; perSecond: number }[] = [
  { label: "1 day/s", perSecond: 86_400_000 },
  { label: "3 days/s", perSecond: 3 * 86_400_000 },
  { label: "1 week/s", perSecond: 7 * 86_400_000 },
  { label: "1 month/s", perSecond: 30 * 86_400_000 },
];

export function Timeline({
  range,
  series,
  at,
  playing,
  speed,
  onScrub,
  onPlay,
  onSpeed,
  onLive,
}: {
  range: TimeRange;
  series: ActivitySeries;
  at: number | null;
  playing: boolean;
  speed: number;
  onScrub: (ms: number) => void;
  onPlay: () => void;
  onSpeed: (i: number) => void;
  onLive: () => void;
}) {
  const chart = useRef<HTMLDivElement>(null);
  const span = Math.max(1, range.end - range.start);
  const cursor = at ?? range.end;
  const frac = Math.min(1, Math.max(0, (cursor - range.start) / span));

  const dayIndex = Math.min(series.days.length - 1, Math.max(0, Math.round((dayFloor(cursor) - series.days[0]) / 86_400_000)));
  const opened = series.opened[dayIndex] ?? 0;
  const closed = series.closed[dayIndex] ?? 0;
  const merged = series.merged[dayIndex] ?? 0;

  const paths = useMemo(() => {
    const n = series.days.length;
    const max = Math.max(1, series.opened[n - 1] ?? 1, series.merged[n - 1] ?? 1);
    const W = 1000;
    const H = 100;
    const x = (i: number) => (n === 1 ? W : (i / (n - 1)) * W);
    const y = (v: number) => H - (v / max) * (H - 4);
    const area = (vals: number[]) => {
      let d = `M0,${H}`;
      for (let i = 0; i < n; i++) d += ` L${x(i).toFixed(1)},${y(vals[i]).toFixed(1)}`;
      return d + ` L${W},${H} Z`;
    };
    const line = (vals: number[]) => {
      let d = "";
      for (let i = 0; i < n; i++) d += `${i ? " L" : "M"}${x(i).toFixed(1)},${y(vals[i]).toFixed(1)}`;
      return d;
    };
    return { opened: area(series.opened), closed: area(series.closed), merged: line(series.merged) };
  }, [series]);

  const scrubTo = useCallback(
    (clientX: number) => {
      const el = chart.current;
      if (!el) return;
      const r = el.getBoundingClientRect();
      const f = Math.min(1, Math.max(0, (clientX - r.left) / Math.max(1, r.width)));
      onScrub(range.start + f * span);
    },
    [onScrub, range.start, span]
  );

  const onPointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    e.currentTarget.setPointerCapture(e.pointerId);
    scrubTo(e.clientX);
  };
  const onPointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (e.buttons & 1) scrubTo(e.clientX);
  };

  const live = at === null;

  return (
    <div className="flex items-center gap-3 border-t px-3 py-1.5">
      <div className="flex items-center gap-1">
        <button
          type="button"
          onClick={onPlay}
          className="inline-flex h-7 w-7 items-center justify-center rounded-md border bg-card text-foreground hover:bg-secondary"
          title={playing ? "Pause" : live ? "Replay the tracker from the first issue" : "Play"}
          aria-label={playing ? "Pause" : "Play"}
        >
          {playing ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
        </button>
        <button
          type="button"
          onClick={() => onScrub(range.start)}
          className="inline-flex h-7 w-7 items-center justify-center rounded-md border bg-card text-muted-foreground hover:bg-secondary"
          title="Back to the first issue"
          aria-label="Back to the start"
        >
          <SkipBack className="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          onClick={() => onSpeed((speed + 1) % SPEEDS.length)}
          className="inline-flex h-7 min-w-[74px] items-center justify-center rounded-md border bg-card px-2 text-[11px] font-medium tabular-nums text-muted-foreground hover:bg-secondary"
          title="Playback speed"
        >
          {SPEEDS[speed].label}
        </button>
      </div>

      <div
        ref={chart}
        className="relative h-11 min-w-0 flex-1 cursor-ew-resize select-none touch-none"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        role="slider"
        aria-label="Date"
        aria-valuemin={range.start}
        aria-valuemax={range.end}
        aria-valuenow={cursor}
        aria-valuetext={formatDay(cursor)}
        tabIndex={0}
        onKeyDown={(e) => {
          const step = 86_400_000 * (e.shiftKey ? 7 : 1);
          if (e.key === "ArrowLeft") onScrub(Math.max(range.start, cursor - step));
          if (e.key === "ArrowRight") onScrub(Math.min(range.end, cursor + step));
        }}
      >
        <svg className="absolute inset-0 h-full w-full" viewBox="0 0 1000 100" preserveAspectRatio="none" aria-hidden>
          <path d={paths.opened} fill={KIND_COLOR.open} fillOpacity="0.28" />
          <path d={paths.closed} fill={KIND_COLOR.closed} fillOpacity="0.55" />
          <path d={paths.merged} fill="none" stroke={KIND_COLOR.pr} strokeWidth="1.5" vectorEffect="non-scaling-stroke" strokeOpacity="0.9" />
        </svg>
        {/* The future, dimmed. */}
        <div
          className="pointer-events-none absolute inset-y-0 right-0 bg-background/70 transition-[left] duration-75"
          style={{ left: `${frac * 100}%` }}
        />
        <div className="pointer-events-none absolute inset-y-0 w-px bg-foreground" style={{ left: `${frac * 100}%` }}>
          <div className="absolute -left-[3px] top-0 h-[7px] w-[7px] rounded-full bg-foreground" />
        </div>
      </div>

      <div className="flex w-[300px] shrink-0 items-center justify-end gap-3 text-[11px]">
        <div className="text-right leading-tight">
          <div className={cn("font-semibold tabular-nums", live && "text-muted-foreground")}>{live ? "Today" : formatDay(cursor)}</div>
          <div className="text-muted-foreground tabular-nums">
            <span style={{ color: KIND_COLOR.open }}>{opened - closed} open</span> · <span style={{ color: KIND_COLOR.closed }}>{closed} finished</span> ·{" "}
            <span style={{ color: KIND_COLOR.pr }}>{merged} merged</span>
          </div>
        </div>
        <button
          type="button"
          onClick={onLive}
          disabled={live}
          className={cn(
            "inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-[11.5px] font-medium",
            live ? "border-border text-muted-foreground" : "border-foreground/40 bg-secondary text-foreground hover:bg-secondary/80"
          )}
          title="Back to today"
        >
          <Radio className={cn("h-3 w-3", live && "text-emerald-500")} /> Live
        </button>
      </div>
    </div>
  );
}
