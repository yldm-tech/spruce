import type { Kind, Result, Run, TaskView } from "../../mygo";
import { formatBytes, formatDuration, trashName } from "../common/format";
import { PAGES, type PageId, type PageMeta } from "../common/pages";
import { isIssue } from "../maintenance/summary";

/** The kinds in the order the filter chips list them: the sidebar's order. */
export const KIND_ORDER: readonly Kind[] = [
  "smartScan",
  "systemJunk",
  "devJunk",
  "installers",
  "uninstall",
  "space",
  "maintenance",
];

const KIND_PAGES: Record<Kind, PageId> = {
  maintenance: "maintenance",
  systemJunk: "system-junk",
  devJunk: "dev-junk",
  installers: "installers",
  uninstall: "uninstall",
  space: "space",
  smartScan: "smart-scan",
};

/** kindMeta is the page a run's kind belongs to (its title, tile colour and glyph). A kind this build does not know, written by a newer version, is shown as the history page's own. */
export function kindMeta(kind: Kind): PageMeta {
  const page = KIND_PAGES[kind] as PageId | undefined;
  return page ? PAGES[page] : { ...PAGES.history, title: "其他操作" };
}

/** The runs of one calendar day (local time), newest first. */
export interface DayGroup {
  /** YYYY-MM-DD, unique per group. */
  key: string;
  /** 今天, 昨天, or the date: "11月2日", "2025年11月2日" in another year. */
  label: string;
  runs: Run[];
}

const sameYearDate = new Intl.DateTimeFormat("zh-CN", { month: "long", day: "numeric" });
const fullDate = new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "long", day: "numeric" });
const clock = new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
const clockSeconds = new Intl.DateTimeFormat("zh-CN", {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
});

function dayKey(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** dayLabel names the local day of date as seen on now's day. */
export function dayLabel(date: Date, now: Date): string {
  const today = dayKey(now);
  if (dayKey(date) === today) return "今天";
  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1);
  if (dayKey(date) === dayKey(yesterday)) return "昨天";
  return date.getFullYear() === now.getFullYear() ? sameYearDate.format(date) : fullDate.format(date);
}

/** groupByDay splits runs (newest first, as History.list returns them) into local days by their start, keeping the order. */
export function groupByDay(runs: readonly Run[], now: Date = new Date()): DayGroup[] {
  const groups: DayGroup[] = [];
  const byKey = new Map<string, DayGroup>();
  for (const run of runs) {
    const started = new Date(run.startedAt);
    const key = dayKey(started);
    let group = byKey.get(key);
    if (!group) {
      group = { key, label: dayLabel(started, now), runs: [] };
      byKey.set(key, group);
      groups.push(group);
    }
    group.runs.push(run);
  }
  return groups;
}

/** formatClock renders the local time of day of an RFC 3339 time, e.g. "14:03", or with seconds "14:03:12". */
export function formatClock(time: string, seconds = false): string {
  return (seconds ? clockSeconds : clock).format(new Date(time));
}

/** runDuration is how long a run took, e.g. "28 秒". */
export function runDuration(run: Run): string {
  const ms = new Date(run.endedAt).getTime() - new Date(run.startedAt).getTime();
  return formatDuration(Number.isFinite(ms) ? ms * 1e6 : 0);
}

/** completedCount counts the maintenance tasks that completed, as the 维护 done card's big number does: applied or nothing to change. */
export function completedCount(results: readonly Result[]): number {
  return results.filter((r) => r.outcome === "applied" || r.outcome === "unchanged").length;
}

/** runSummary is a row's one line: for maintenance "完成 5 项，1 项未完成"; for a clean-up the space freed, what went to the Trash, the items and the failures, e.g. "释放 1.2 GB，34 项，2 项未能处理". */
export function runSummary(run: Run): string {
  if (run.kind === "maintenance") {
    const done = completedCount(run.results);
    const left = run.results.length - done;
    return left > 0 ? `完成 ${done} 项，${left} 项未完成` : `完成 ${done} 项`;
  }
  const s = run.summary;
  if (!s) return "没有记录结果";
  const trashed = s.trashed ?? 0;
  const parts: string[] = [];
  if (s.freed > 0 || trashed === 0) parts.push(`释放 ${formatBytes(s.freed)}`);
  if (trashed > 0) parts.push(`移到${trashName()} ${formatBytes(trashed)}`);
  parts.push(`${s.items} 项`);
  if (s.failed > 0) parts.push(`${s.failed} 项未能处理`);
  return parts.join("，");
}

/** totalFreed adds up the space the clean-ups among runs freed for good (what went to the Trash is freed only once it is emptied). cleanups counts those runs. */
export function totalFreed(runs: readonly Run[]): { bytes: number; cleanups: number } {
  let bytes = 0;
  let cleanups = 0;
  for (const r of runs) {
    if (!r.summary) continue;
    bytes += r.summary.freed;
    cleanups++;
  }
  return { bytes, cleanups };
}

/** kindsIn lists the kinds that occur in runs, in KIND_ORDER (unknown kinds last). */
export function kindsIn(runs: readonly Run[]): Kind[] {
  const present = new Set(runs.map((r) => r.kind));
  const rank = (k: Kind) => {
    const i = KIND_ORDER.indexOf(k);
    return i < 0 ? KIND_ORDER.length : i;
  };
  return [...present].sort((a, b) => rank(a) - rank(b));
}

/** One task of a recorded maintenance run. */
export interface TaskLine {
  taskId: string;
  title: string;
  /** 已完成, 部分完成, 无需处理, 已跳过, 不可用 or 失败. */
  status: string;
  /** The task's own sentence, as the 维护 done card shows it. */
  message: string;
  /** Set when the task needs the user's attention (see isIssue). */
  attention: boolean;
}

/** statusLabel names how a maintenance task ended. */
export function statusLabel(r: Result): string {
  switch (r.outcome) {
    case "applied":
      return r.partial ? "部分完成" : "已完成";
    case "unchanged":
      return "无需处理";
    case "skipped":
      return "已跳过";
    case "unavailable":
      return "不可用";
    case "failed":
      return "失败";
  }
  return r.outcome;
}

/** taskLines lists a maintenance run's tasks as the done card orders them: those needing attention first, each part in catalog order. A task that is not in this build's catalog shows its ID. */
export function taskLines(results: readonly Result[], catalog: readonly TaskView[]): TaskLine[] {
  const titles = new Map(catalog.map((t) => [t.id, t.title]));
  const order = new Map(catalog.map((t, i) => [t.id, i]));
  const rank = (r: Result) =>
    (isIssue(r) ? 0 : 1) * (catalog.length + 1) + (order.get(r.taskId) ?? catalog.length);
  return [...results]
    .sort((a, b) => rank(a) - rank(b))
    .map((r) => ({
      taskId: r.taskId,
      title: titles.get(r.taskId) ?? r.taskId,
      status: statusLabel(r),
      message: r.message,
      attention: isIssue(r),
    }));
}
