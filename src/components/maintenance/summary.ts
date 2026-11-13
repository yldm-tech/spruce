import type { Alert, PermissionState, Result, RunSummary, TaskView } from "../../mygo";

/** Tasks that read other apps' data under ~/Library and therefore need Full Disk Access on macOS. The Windows catalog has none of these IDs. */
export const FULL_DISK_ACCESS_TASKS: ReadonlySet<string> = new Set(["databases"]);

/** Whether the page should offer the Full Disk Access guide for a permission state: "denied" and "unknown" both mean the task may be skipped. */
export function needsFullDiskAccess(state: PermissionState | undefined): boolean {
  return state === "denied" || state === "unknown";
}

/** formatEstimate turns the summed EstimatedSeconds of the selected tasks into the footer's 预计用时 value. */
export function formatEstimate(seconds: number): string {
  if (seconds <= 0) return "0 分钟";
  if (seconds < 60) return "不到 1 分钟";
  return `约 ${Math.round(seconds / 60)} 分钟`;
}

/** formatAlertTitle renders the banner title, e.g. "Xcode 已经持续占用 48% CPU 有 12 分钟". */
export function formatAlertTitle(alert: Alert): string {
  const percent = Math.round(alert.cpuPercent);
  // Duration is in nanoseconds; the monitor only alerts after its Sustain period (minutes), so at least 1 minute is shown.
  const minutes = Math.max(1, Math.floor(alert.duration / 60e9));
  return `${alert.name} 已经持续占用 ${percent}% CPU 有 ${minutes} 分钟`;
}

/** okCount is the done overlay's big number: tasks that applied a change or found nothing to change. */
export function okCount(summary: RunSummary): number {
  return summary.applied + summary.unchanged;
}

/** A line of the done overlay about tasks that did not complete, grouped by their shared message so a declined admin prompt reads as one line. */
export interface IssueLine {
  titles: string[];
  message: string;
}

/** issueLines lists the skipped, unavailable and failed results of a run, in catalog order, grouped by message. */
export function issueLines(results: Result[], catalog: TaskView[]): IssueLine[] {
  const titles = new Map(catalog.map((t) => [t.id, t.title]));
  const lines: IssueLine[] = [];
  const byMessage = new Map<string, IssueLine>();
  for (const r of results) {
    if (r.outcome === "applied" || r.outcome === "unchanged") continue;
    const title = titles.get(r.taskId) ?? r.taskId;
    const existing = byMessage.get(r.message);
    if (existing) {
      existing.titles.push(title);
      continue;
    }
    const line = { titles: [title], message: r.message };
    byMessage.set(r.message, line);
    lines.push(line);
  }
  return lines;
}

/** formatIssueLine renders an IssueLine, e.g. "优化应用数据库：Safari 仍在运行，已跳过". */
export function formatIssueLine(line: IssueLine): string {
  return `${line.titles.join("、")}：${line.message}`;
}

/** doneTitle is the done overlay's heading for a run. */
export function doneTitle(summary: RunSummary): string {
  if (summary.interrupted) return "维护已中断";
  if (okCount(summary) === 0) return "维护未完成";
  return "维护完成";
}

/** fullDiskAccessBlocked reports whether a run left a Full Disk Access task undone, so the done overlay offers 去授权 (together with the current permission state). */
export function fullDiskAccessBlocked(results: Result[]): boolean {
  return results.some(
    (r) => FULL_DISK_ACCESS_TASKS.has(r.taskId) && (r.outcome === "skipped" || r.outcome === "unavailable"),
  );
}
