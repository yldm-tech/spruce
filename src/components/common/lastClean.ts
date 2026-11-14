import { queryOptions, useQuery } from "@tanstack/react-query";

import { History, type Kind, type Run } from "../../mygo";
import { HISTORY_QUERY_KEY } from "./files";
import { formatBytes, formatRelativeTime, trashName } from "./format";

/** How many recent runs are searched for a page's last clean. */
const LAST_CLEAN_LOOKBACK = 100;

/** lastCleanQueryOptions finds the newest recorded run of kind among the recent history, or null. It sits under HISTORY_QUERY_KEY, so it refreshes after every clean. */
export function lastCleanQueryOptions(kind: Kind) {
  return queryOptions({
    queryKey: [...HISTORY_QUERY_KEY, "last", kind],
    queryFn: async (): Promise<Run | null> =>
      (await History.list(LAST_CLEAN_LOOKBACK)).find((r) => r.kind === kind) ?? null,
  });
}

/** useLastClean returns the newest recorded clean of kind (null when there is none). */
export function useLastClean(kind: Kind) {
  return useQuery(lastCleanQueryOptions(kind));
}

/** lastCleanText describes a recorded clean in one line, e.g. "上次清理：3 天前，释放 1.2 GB". */
export function lastCleanText(run: Run, now: Date = new Date()): string {
  const parts: string[] = [];
  const freed = run.summary?.freed ?? 0;
  const trashed = run.summary?.trashed ?? 0;
  if (freed > 0) parts.push(`释放 ${formatBytes(freed)}`);
  if (trashed > 0) parts.push(`移到${trashName()} ${formatBytes(trashed)}`);
  const when = formatRelativeTime(run.endedAt, now) || formatRelativeTime(run.startedAt, now);
  return [`上次清理：${when}`, ...parts].join("，");
}
