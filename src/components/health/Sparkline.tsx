import { useEffect, useEffectEvent, useRef } from "react";
import uPlot from "uplot";

import "uplot/dist/uPlot.min.css";

export interface SparklineSeries {
  /** A colour token of tokens.css for the line, e.g. "--chart-cpu". */
  stroke: string;
  /** A colour token for the area under the line; none leaves it unfilled. */
  fill?: string;
}

interface SparklineProps {
  /** uPlot's columns: x in Unix seconds, then one column per series (see chartData). */
  data: uPlot.AlignedData;
  series: readonly SparklineSeries[];
  /** How many seconds the x axis spans, ending at the newest sample. */
  windowSeconds: number;
  /** A fixed top of the y axis (100 for percentages); without it the axis fits the data, but never below minTop. */
  max?: number;
  minTop?: number;
  height?: number;
}

function token(el: Element, name: string): string {
  return getComputedStyle(el).getPropertyValue(name).trim();
}

/** Sparkline is a small line chart without axes, drawn with uPlot in the design's colours. It is decorative: the card around it states the numbers. */
export function Sparkline({ data, series, windowSeconds, max, minTop = 1, height = 56 }: SparklineProps) {
  const host = useRef<HTMLDivElement>(null);
  const plot = useRef<uPlot | null>(null);

  // Creates the chart with the current options and data; an effect event, so the effect below runs only when the look changes and data updates go through setData.
  const create = useEffectEvent((el: HTMLDivElement) => {
    const finite = (n: number) => (Number.isFinite(n) ? n : 0);
    const opts: uPlot.Options = {
      width: Math.max(1, el.clientWidth),
      height,
      cursor: { show: false },
      legend: { show: false },
      scales: {
        x: { time: false, range: (_u, _min, newest) => [finite(newest) - windowSeconds, finite(newest)] },
        y: { range: (_u, _min, top) => [0, max ?? Math.max(finite(top), minTop) * 1.15] },
      },
      axes: [{ show: false }, { show: false }],
      series: [
        {},
        ...series.map((s) => ({
          stroke: token(el, s.stroke),
          fill: s.fill ? token(el, s.fill) : undefined,
          width: 1.5,
          points: { show: false },
        })),
      ],
    };
    return new uPlot(opts, data, el);
  });

  // seriesKey stands for the series' colours.
  const seriesKey = series.map((s) => `${s.stroke} ${s.fill ?? ""}`).join(",");
  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const chart = create(el);
    plot.current = chart;
    const resize = new ResizeObserver(() => chart.setSize({ width: Math.max(1, el.clientWidth), height }));
    resize.observe(el);
    return () => {
      resize.disconnect();
      chart.destroy();
      plot.current = null;
    };
  }, [seriesKey, windowSeconds, max, minTop, height]);

  useEffect(() => {
    plot.current?.setData(data);
  }, [data]);

  return <div ref={host} aria-hidden="true" className="sparkline" style={{ height }} />;
}
