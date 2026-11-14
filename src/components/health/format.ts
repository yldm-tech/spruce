import type { BatteryState, MemoryPressure, SystemInfo, Vitals } from "../../mygo";
import { formatBytes } from "../common/format";

/** formatPercent renders a percent rounded to a whole number, e.g. "23%". */
export function formatPercent(value: number): string {
  return `${Math.round(Math.max(0, value))}%`;
}

/** formatProcessCPU renders a process's CPU percent (of one logical CPU, so it can pass 100) with one decimal below 10, as Activity Monitor does: "0.4%", "12%", "250%". */
export function formatProcessCPU(value: number): string {
  const v = Math.max(0, value);
  return v < 10 ? `${v.toFixed(1)}%` : `${Math.round(v)}%`;
}

/** formatRate renders a throughput in bytes per second, e.g. "1.2 MB/s" (units as formatBytes counts them on this system). */
export function formatRate(bytesPerSecond: number): string {
  return `${formatBytes(bytesPerSecond)}/s`;
}

/** formatLoad renders the 1, 5 and 15 minute load averages, e.g. "2.31 / 1.98 / 1.75". */
export function formatLoad(load: { one: number; five: number; fifteen: number }): string {
  return [load.one, load.five, load.fifteen].map((n) => n.toFixed(2)).join(" / ");
}

/** formatMinutes renders a battery time estimate, e.g. "45 分钟", "2 小时 5 分钟", "3 小时". */
export function formatMinutes(minutes: number): string {
  const m = Math.max(0, Math.round(minutes));
  if (m < 60) return `${m} 分钟`;
  const hours = Math.floor(m / 60);
  const rest = m % 60;
  return rest === 0 ? `${hours} 小时` : `${hours} 小时 ${rest} 分钟`;
}

/** formatUptime renders how long the system has been up, e.g. "12 分钟", "5 小时 3 分钟", "3 天 4 小时". */
export function formatUptime(milliseconds: number): string {
  const minutes = Math.floor(Math.max(0, milliseconds) / 60_000);
  if (minutes < 1) return "不到 1 分钟";
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    const rest = minutes % 60;
    return rest === 0 ? `${hours} 小时` : `${hours} 小时 ${rest} 分钟`;
  }
  const days = Math.floor(hours / 24);
  const rest = hours % 24;
  return rest === 0 ? `${days} 天` : `${days} 天 ${rest} 小时`;
}

const bootDate = new Intl.DateTimeFormat("zh-CN", {
  month: "long",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
});

/** formatBootTime renders when the system started, e.g. "11月11日 09:12"; "" for an unknown time. */
export function formatBootTime(time: string | undefined): string {
  if (!time) return "";
  const date = new Date(time);
  if (!Number.isFinite(date.getTime()) || date.getUTCFullYear() <= 1) return "";
  return bootDate.format(date);
}

/** batteryStateText describes what the battery is doing, or "" when unknown. */
export function batteryStateText(state: BatteryState): string {
  switch (state) {
    case "charging":
      return "正在充电";
    case "discharging":
      return "正在使用电池";
    case "full":
      return "已充满";
    case "notCharging":
      return "已接通电源，未在充电";
    default:
      return "";
  }
}

/** batteryTimeText is the time estimate for the battery's state, e.g. "剩余约 3 小时 5 分钟" or "充满还需 38 分钟"; "" when there is none. */
export function batteryTimeText(state: BatteryState, minutes: number | undefined): string {
  if (minutes === undefined || minutes <= 0) return "";
  if (state === "discharging") return `剩余约 ${formatMinutes(minutes)}`;
  if (state === "charging") return `充满还需 ${formatMinutes(minutes)}`;
  return "";
}

/** pressureText names a memory pressure level as Activity Monitor's graph colours it. */
export function pressureText(pressure: MemoryPressure): string {
  switch (pressure) {
    case "warning":
      return "偏高";
    case "critical":
      return "紧张";
    default:
      return "正常";
  }
}

/** coresText describes the CPU's cores, e.g. "12 核（8 性能核 + 4 能效核）" or "8 核 16 线程"; "" when unknown. */
export function coresText(info: SystemInfo): string {
  const { physicalCores: physical, logicalCores: logical, performanceCores: p, efficiencyCores: e } = info;
  if (physical <= 0 && logical <= 0) return "";
  if (physical <= 0) return `${logical} 线程`;
  let text = `${physical} 核`;
  if (p && e) text += `（${p} 性能核 + ${e} 能效核）`;
  if (logical > physical) text += ` ${logical} 线程`;
  return text;
}

/** osText renders the system's name, version and build, e.g. "macOS 26.1（25B78）" or "Windows 11 Pro 24H2（26100.2314）". */
export function osText(info: SystemInfo): string {
  const name = [info.osName, info.osVersion].filter(Boolean).join(" ");
  if (!name) return "";
  return info.osBuild ? `${name}（${info.osBuild}）` : name;
}

/** The longest gap between two samples a chart draws a line across; a longer one (sampling stopped while the page was away) leaves the line broken. */
export const MAX_SAMPLE_GAP_SECONDS = 5;

/** chartData turns samples (oldest first) into uPlot's columns: x in Unix seconds, then one column per picker, null where a value is missing. A null row is inserted into a gap longer than MAX_SAMPLE_GAP_SECONDS so the line breaks there. */
export function chartData(
  samples: readonly Vitals[],
  pickers: readonly ((v: Vitals) => number | null | undefined)[],
): [number[], ...(number | null)[][]] {
  const xs: number[] = [];
  const ys: (number | null)[][] = pickers.map(() => []);
  let prev: number | undefined;
  for (const v of samples) {
    const x = Date.parse(v.at) / 1000;
    if (!Number.isFinite(x)) continue;
    if (prev !== undefined && x - prev > MAX_SAMPLE_GAP_SECONDS) {
      xs.push((prev + x) / 2);
      for (const col of ys) col.push(null);
    }
    xs.push(x);
    ys.forEach((col, i) => col.push(pickers[i]?.(v) ?? null));
    prev = x;
  }
  return [xs, ...ys];
}

/** HISTORY_LENGTH is how many samples the page keeps: two minutes at one every two seconds, like the Go sampler. */
export const HISTORY_LENGTH = 60;

/** appendSample adds a sample to the page's history, oldest first, dropping one already there (the history Go sends again when the page reconnects) and keeping the newest HISTORY_LENGTH. */
export function appendSample(samples: Vitals[], next: Vitals): Vitals[] {
  const last = samples.at(-1);
  if (last && Date.parse(next.at) <= Date.parse(last.at)) return samples;
  const out = [...samples, next];
  return out.length > HISTORY_LENGTH ? out.slice(out.length - HISTORY_LENGTH) : out;
}
