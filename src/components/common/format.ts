import { filesize } from "filesize";

import { isWindows } from "../../platform";

export interface FormatBytesOptions {
  /** 1024-based units, as Windows Explorer counts; default: on Windows. macOS Finder counts in 1000s. */
  binary?: boolean;
}

/** formatBytes renders a byte count the way the system's file manager does, e.g. "1.2 GB" (1000-based on macOS, 1024-based with the same KB/MB/GB symbols on Windows), with one decimal from MB up. */
export function formatBytes(bytes: number, options: FormatBytesOptions = {}): string {
  const binary = options.binary ?? isWindows();
  const value = Math.max(0, Number.isFinite(bytes) ? bytes : 0);
  return filesize(value, {
    base: binary ? 2 : 10,
    standard: binary ? "jedec" : "si",
    round: value < (binary ? 1024 ** 2 : 1e6) ? 0 : 1,
    symbols: { kB: "KB" },
  });
}

/** formatDuration renders a Go duration (nanoseconds) in Chinese, e.g. "不到 1 秒", "12 秒", "3 分 20 秒", "1 小时 5 分". */
export function formatDuration(nanoseconds: number): string {
  const seconds = Math.floor(Math.max(0, nanoseconds) / 1e9);
  if (seconds < 1) return "不到 1 秒";
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    const rest = seconds % 60;
    return rest === 0 || minutes >= 10 ? `${minutes} 分钟` : `${minutes} 分 ${rest} 秒`;
  }
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest === 0 ? `${hours} 小时` : `${hours} 小时 ${rest} 分`;
}

const relative = new Intl.RelativeTimeFormat("zh-CN", { numeric: "auto" });
const fullDate = new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "long", day: "numeric" });
const sameYearDate = new Intl.DateTimeFormat("zh-CN", { month: "long", day: "numeric" });

/** formatRelativeTime renders a time (an RFC 3339 string from Go, or a Date) relative to now in Chinese: "刚刚", "5 分钟前", "3 小时前", "昨天", "3 天前", then the date ("11月2日", or "2025年11月2日" in another year). Go's zero time and invalid input give "". */
export function formatRelativeTime(time: string | Date, now: Date = new Date()): string {
  const date = typeof time === "string" ? new Date(time) : time;
  const ms = date.getTime();
  if (!Number.isFinite(ms) || date.getUTCFullYear() <= 1) return "";
  const diff = (now.getTime() - ms) / 1000;
  if (diff < 0) return sameYearDate.format(date);
  if (diff < 60) return "刚刚";
  if (diff < 3600) return relative.format(-Math.floor(diff / 60), "minute");
  if (diff < 86400) return relative.format(-Math.floor(diff / 3600), "hour");
  const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((startOfDay(now) - startOfDay(date)) / 86400e3);
  if (days < 7) return relative.format(-Math.max(1, days), "day");
  return date.getFullYear() === now.getFullYear() ? sameYearDate.format(date) : fullDate.format(date);
}

/** truncateMiddle shortens text to at most max characters by replacing its middle with "…", keeping the end, which for a path is the file name. */
export function truncateMiddle(text: string, max: number): string {
  if (text.length <= max) return text;
  const keep = max - 1;
  const tail = Math.ceil(keep * 0.6);
  return `${text.slice(0, keep - tail)}…${text.slice(text.length - tail)}`;
}

/** baseName returns the last component of a path, for either separator. */
export function baseName(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, "");
  const i = Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\"));
  return i < 0 ? trimmed : trimmed.slice(i + 1);
}

/** parentPath returns path without its last component. */
export function parentPath(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, "");
  const i = Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\"));
  return i <= 0 ? trimmed.slice(0, i + 1) : trimmed.slice(0, i);
}

/** trashName is what the system calls its Trash: 废纸篓 on macOS, 回收站 on Windows. */
export function trashName(): string {
  return isWindows() ? "回收站" : "废纸篓";
}

/** fileManagerName is the system's file manager: 访达 on macOS, 资源管理器 on Windows. */
export function fileManagerName(): string {
  return isWindows() ? "资源管理器" : "访达";
}
