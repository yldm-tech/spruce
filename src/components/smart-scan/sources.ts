import type { Category, Progress, ScanResult, SourceState } from "../../mygo";
import type { PageId } from "../common/pages";
import { defaultSelection, selectedTotals } from "../common/selection";

/** The cleaners 智能扫描 combines, in the order the Go side scans and cleans them (internal/engine/smartscan via main.go). Each one is also a page, which the source's card links to. */
export const SOURCES = ["system-junk", "dev-junk", "installers"] as const satisfies readonly PageId[];

export type SourceId = (typeof SOURCES)[number];

/** What each source looks at, in a few words, for the scanning rows. */
export const SOURCE_SCOPE: Record<SourceId, string> = {
  "system-junk": "应用和浏览器缓存、日志、崩溃报告、未完成的下载",
  "dev-junk": "开发工具的缓存和项目里的构建产物",
  installers: "下载过的安装包和磁盘映像",
};

/** isSource narrows a Category.source or SourceState.id to one of SOURCES. */
export function isSource(id: string | undefined): id is SourceId {
  return (SOURCES as readonly string[]).includes(id ?? "");
}

/** One source's share of a scan, for its summary card and its section of the list. */
export interface SourceSummary {
  id: SourceId;
  /** The source's state, or undefined when the scan did not report it. */
  state: SourceState | undefined;
  categories: Category[];
  /** Everything the source found. */
  bytes: number;
  items: number;
  /** What starts checked. */
  recommendedBytes: number;
  recommendedItems: number;
}

/** summarize splits a merged scan by source, in SOURCES order, with each source's totals and its default selection's totals. */
export function summarize(scan: ScanResult): SourceSummary[] {
  return SOURCES.map((id) => {
    const categories = scan.categories.filter((c) => c.source === id);
    const recommended = selectedTotals(categories, defaultSelection(categories));
    return {
      id,
      state: scan.sources?.find((s) => s.id === id),
      categories,
      bytes: categories.reduce((sum, c) => sum + c.size, 0),
      items: categories.reduce((sum, c) => sum + c.items.length, 0),
      recommendedBytes: recommended.bytes,
      recommendedItems: recommended.count,
    };
  });
}

/** recommendedTotals is the size and count of everything the scan checks by default: the 「可清理」 figure. */
export function recommendedTotals(scan: ScanResult): { count: number; bytes: number } {
  return selectedTotals(scan.categories, defaultSelection(scan.categories));
}

/** One row of the scanning view. */
export interface SourceRow {
  id: SourceId;
  status: SourceState["status"] | "waiting";
  /** The fraction done, or undefined while the source does not know how much there is. */
  fraction: number | undefined;
  bytes: number;
  items: number;
  error?: string;
}

/** sourceRows turns the latest scanning event into one row per source; before the first event every source is waiting. */
export function sourceRows(progress: Progress | null): SourceRow[] {
  return SOURCES.map((id) => {
    const st = progress?.sources?.find((s) => s.id === id);
    if (!st) return { id, status: "waiting", fraction: undefined, bytes: 0, items: 0 };
    let fraction: number | undefined;
    if (st.status !== "scanning") fraction = 1;
    else if (st.total > 0) fraction = Math.min(1, st.done / st.total);
    return { id, status: st.status, fraction, bytes: st.bytes, items: st.items, error: st.error };
  });
}

/** overallFraction is how far the whole scan got, from the event's done and total (which add up the sources' shares), or undefined before the first event. */
export function overallFraction(progress: Progress | null): number | undefined {
  if (!progress || progress.total <= 0) return undefined;
  return Math.min(1, progress.done / progress.total);
}
