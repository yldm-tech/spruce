import type {
  CleanResult,
  UninstallApp,
  UninstallAppDetails,
  UninstallLeftover,
  UninstallLeftoverKind,
  UninstallOutcome,
  UninstallUsage,
} from "../../mygo";
import { baseName, formatBytes, formatRelativeTime, trashName } from "../common/format";

/** What Measure has streamed for one app so far. */
export interface AppDetails {
  size?: number;
  usage?: UninstallUsage;
  lastUsed?: string;
  icon?: string;
}

/** mergeDetails adds one streamed update to what is known of the apps. */
export function mergeDetails(
  known: ReadonlyMap<string, AppDetails>,
  updates: readonly UninstallAppDetails[],
): Map<string, AppDetails> {
  const next = new Map(known);
  for (const u of updates) {
    const cur = { ...next.get(u.id) };
    if (u.size !== undefined && u.size !== null) cur.size = u.size;
    if (u.usage) {
      cur.usage = u.usage;
      cur.lastUsed = u.lastUsed;
    }
    if (u.icon) cur.icon = u.icon;
    next.set(u.id, cur);
  }
  return next;
}

/** How the list is ordered. On Windows the third order is by install date, since Windows keeps no record of last use. */
export type SortKey = "size" | "name" | "recent";

const collator = new Intl.Collator("zh-CN", { numeric: true, sensitivity: "base" });

function time(value: string | undefined): number {
  if (!value) return Number.NEGATIVE_INFINITY;
  const ms = new Date(value).getTime();
  return Number.isFinite(ms) && new Date(value).getUTCFullYear() > 1 ? ms : Number.NEGATIVE_INFINITY;
}

/** recentTime is the time the 最近使用 order uses: the last use on macOS, the install date on Windows. */
export function recentTime(app: UninstallApp, details: AppDetails | undefined, windows: boolean): number {
  if (windows) return time(app.installDate);
  return details?.usage === "used" ? time(details.lastUsed) : Number.NEGATIVE_INFINITY;
}

/** visibleApps filters the apps by the search text (name, bundle ID or publisher, ignoring case) and sorts them: largest, by name, or most recent first; unknown sizes and times go last, ties by name. */
export function visibleApps(
  apps: readonly UninstallApp[],
  details: ReadonlyMap<string, AppDetails>,
  query: string,
  sort: SortKey,
  windows: boolean,
): UninstallApp[] {
  const q = query.trim().toLocaleLowerCase();
  const matches = q
    ? apps.filter((a) => [a.name, a.bundleId, a.publisher].some((s) => s?.toLocaleLowerCase().includes(q)))
    : [...apps];
  const byName = (a: UninstallApp, b: UninstallApp) => collator.compare(a.name, b.name);
  switch (sort) {
    case "name":
      return matches.sort(byName);
    case "size":
      return matches.sort((a, b) => {
        const sa = details.get(a.id)?.size ?? -1;
        const sb = details.get(b.id)?.size ?? -1;
        return sb - sa || byName(a, b);
      });
    case "recent":
      return matches.sort((a, b) => {
        const ta = recentTime(a, details.get(a.id), windows);
        const tb = recentTime(b, details.get(b.id), windows);
        if (ta === tb) return byName(a, b);
        return tb > ta ? 1 : -1;
      });
  }
}

/** usageText is the 最近使用 line of an app: the relative time, 从未使用, or "" when unknown. On Windows it is the install date. */
export function usageText(app: UninstallApp, details: AppDetails | undefined, windows: boolean): string {
  if (windows) {
    const when = app.installDate ? formatRelativeTime(app.installDate) : "";
    return when ? `安装于 ${when}` : "";
  }
  switch (details?.usage) {
    case "used": {
      const when = details.lastUsed ? formatRelativeTime(details.lastUsed) : "";
      return when ? `${when}使用过` : "";
    }
    case "never":
      return "从未使用";
    default:
      return "";
  }
}

/** sizeText is the size of an app as the list shows it while sizes stream in. */
export function sizeText(details: AppDetails | undefined, measuring: boolean): string {
  if (details?.size !== undefined) return formatBytes(details.size);
  return measuring ? "正在计算…" : "大小未知";
}

/** What each kind of leftover is called. */
export const KIND_LABELS: Record<UninstallLeftoverKind, string> = {
  app: "应用本身",
  support: "应用支持文件",
  caches: "缓存",
  preferences: "偏好设置",
  containers: "沙盒容器",
  groupContainers: "共享容器",
  savedState: "窗口状态",
  httpStorage: "网络存储",
  webkit: "网页数据",
  logs: "日志",
  cookies: "Cookie",
  launchAgents: "登录启动项",
  appData: "用户数据（Roaming）",
  localAppData: "本地数据（Local）",
  programs: "安装文件夹",
};

/** defaultChecked is the set of leftovers that start checked. */
export function defaultChecked(leftovers: readonly UninstallLeftover[]): Set<string> {
  return new Set(leftovers.filter((l) => l.selected).map((l) => l.id));
}

/** checkedTotals counts and sizes the checked leftovers. */
export function checkedTotals(
  leftovers: readonly UninstallLeftover[],
  checked: ReadonlySet<string>,
): { count: number; bytes: number } {
  let count = 0;
  let bytes = 0;
  for (const l of leftovers) {
    if (!checked.has(l.id)) continue;
    count++;
    bytes += l.size;
  }
  return { count, bytes };
}

/** confirmMessage is the question asked before an app goes to the Trash (macOS). */
export function confirmMessage(name: string, leftovers: number, bytes: number): string {
  const bin = trashName();
  const what = leftovers > 0 ? `「${name}」和勾选的 ${leftovers} 个相关文件` : `「${name}」`;
  return `${what}（共 ${formatBytes(bytes)}）会移到${bin}，可以随时放回。`;
}

/** What a done card shows. */
export interface DoneSummary {
  title: string;
  big: string;
  lines: string[];
  succeeded: boolean;
}

const MAX_FAILURE_LINES = 8;

function leftoverLines(r: CleanResult, bin: string, skipCategory?: string): string[] {
  const lines: string[] = [];
  let moved = 0;
  let failed = 0;
  for (const c of r.categories) {
    if (c.categoryId === skipCategory) continue;
    moved += c.removed;
    failed += c.failed;
  }
  if (moved > 0) lines.push(`相关文件：${moved} 项已移到${bin}`);
  if (failed > 0) lines.push(`相关文件：${failed} 项未能处理`);
  for (const f of r.failed.slice(0, MAX_FAILURE_LINES)) lines.push(`${baseName(f.path)}：${f.reason}`);
  if (r.failed.length > MAX_FAILURE_LINES)
    lines.push(`另有 ${r.failed.length - MAX_FAILURE_LINES} 项未能处理`);
  if (r.skipped > 0) lines.push(`${r.skipped} 项在查找后已不存在，已跳过`);
  if (r.interrupted) lines.push("卸载在中途停止，其余项目没有改动");
  if (r.trashed > 0) lines.push(`移到${bin}的项目在清空${bin}后才会释放空间`);
  return lines;
}

/** removeSummary is the done card after the app (macOS) or its leftovers (Windows) went to the Trash. */
export function removeSummary(outcome: UninstallOutcome, windows: boolean): DoneSummary {
  const bin = trashName();
  const r = outcome.result;
  if (outcome.bundleFailure) {
    return {
      title: `没能卸载「${outcome.app}」`,
      big: "未卸载",
      lines: [outcome.bundleFailure, "它的相关文件也没有改动。"],
      succeeded: false,
    };
  }
  const head = windows ? [`卸载程序已移除「${outcome.app}」`] : [`「${outcome.app}」已移到${bin}`];
  return {
    title: r.interrupted ? "卸载已中断" : `已卸载「${outcome.app}」`,
    big: formatBytes(r.trashed),
    lines: [...head, ...leftoverLines(r, bin, windows ? undefined : "app")],
    succeeded: windows ? r.removed > 0 || r.failed.length === 0 : outcome.bundleRemoved,
  };
}

/** uninstalledSummary is the done card of a Windows program its uninstaller removed and that left nothing behind (or whose leftovers the user kept). */
export function uninstalledSummary(name: string, leftoversFound: number): DoneSummary {
  return {
    title: `已卸载「${name}」`,
    big: "1 个程序",
    lines: [
      `卸载程序已移除「${name}」`,
      leftoversFound > 0 ? `留下的 ${leftoversFound} 个相关文件没有改动` : "没有找到它留下的文件",
    ],
    succeeded: true,
  };
}
