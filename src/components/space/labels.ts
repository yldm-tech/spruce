import type { SpaceEntry, SpaceVolume } from "../../mygo";
import { formatBytes } from "../common/format";

/** entryLabel is what the page calls an entry: its name, or 「其他 N 项」 and 「N 个小文件」 for the groups the scan folded. */
export function entryLabel(entry: Pick<SpaceEntry, "kind" | "name" | "count">): string {
  switch (entry.kind) {
    case "other":
      return `其他 ${entry.count ?? 0} 项`;
    case "files":
      return `${entry.count ?? 0} 个小文件`;
    default:
      return entry.name;
  }
}

/** isGroup reports whether an entry stands for several folded entries, which can be neither opened nor moved. */
export function isGroup(entry: Pick<SpaceEntry, "kind">): boolean {
  return entry.kind === "other" || entry.kind === "files";
}

/** shareOf is part's share of whole, 0 to 1. */
export function shareOf(part: number, whole: number): number {
  return whole > 0 ? Math.min(1, Math.max(0, part / whole)) : 0;
}

/** formatShare renders a share as a percent, e.g. "42%", "0.4%" or "<0.1%". */
export function formatShare(share: number): string {
  const percent = share * 100;
  if (percent === 0) return "0%";
  if (percent < 0.1) return "<0.1%";
  if (percent < 10) return `${percent.toFixed(1).replace(/\.0$/, "")}%`;
  return `${Math.round(percent)}%`;
}

/** volumeUsage describes a disk's capacity: what is used of the total, and what is free. */
export function volumeUsage(volume: Pick<SpaceVolume, "total" | "free">): {
  used: number;
  share: number;
  text: string;
} {
  const used = Math.max(0, volume.total - volume.free);
  return {
    used,
    share: shareOf(used, volume.total),
    text: `可用 ${formatBytes(volume.free)}，共 ${formatBytes(volume.total)}`,
  };
}

/** fileCount renders how many files an entry holds, e.g. "1,204 个文件". */
export function fileCount(files: number): string {
  return `${files.toLocaleString("zh-CN")} 个文件`;
}
