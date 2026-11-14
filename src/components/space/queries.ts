import { queryOptions } from "@tanstack/react-query";

import { Permissions, Space } from "../../mygo";

/** Every query of the page starts with this key; a new scan or a move to the Trash refreshes what depends on the tree. */
export const SPACE_QUERY_KEY = ["space"] as const;

/** The last scan the service keeps, or null before the first. It changes only through Space.scan and Space.trash, whose results replace it. */
export const currentScanQueryOptions = queryOptions({
  queryKey: [...SPACE_QUERY_KEY, "current"],
  queryFn: () => Space.current(),
  staleTime: Infinity,
});

/** The disks and folders of the start screen. Free space changes outside the app, so they are fetched whenever the start screen shows. */
export const locationsQueryOptions = queryOptions({
  queryKey: [...SPACE_QUERY_KEY, "locations"],
  queryFn: () => Space.locations(),
});

/** One folder of the scan scanId with its children. The tree changes only when the page moves something to the Trash, which invalidates these. */
export function childrenQueryOptions(scanId: string, id: number) {
  return queryOptions({
    queryKey: [...SPACE_QUERY_KEY, "children", scanId, id],
    queryFn: () => Space.children(scanId, id),
    staleTime: Infinity,
  });
}

/** The largest files of the scan scanId. */
export function largeFilesQueryOptions(scanId: string) {
  return queryOptions({
    queryKey: [...SPACE_QUERY_KEY, "large", scanId],
    queryFn: () => Space.largeFiles(scanId),
    staleTime: Infinity,
  });
}

/** Whether the app holds Full Disk Access, under the same key as the 维护 page, so a grant made on either page shows on both. */
export const fullDiskAccessQueryOptions = queryOptions({
  queryKey: ["permissions", "fullDiskAccess"],
  queryFn: () => Permissions.fullDiskAccess(),
});
