import { queryOptions } from "@tanstack/react-query";

import { Maintenance, Permissions } from "../../mygo";

/** The maintenance tasks of this platform. They are static for a build, so they are fetched once. */
export const catalogQueryOptions = queryOptions({
  queryKey: ["maintenance", "catalog"],
  queryFn: () => Maintenance.catalog(),
  staleTime: Infinity,
});

/** Whether the app holds Full Disk Access. It changes outside the app (System Settings), so it is checked when the page opens and again after a run or a guide. */
export const fullDiskAccessQueryOptions = queryOptions({
  queryKey: ["permissions", "fullDiskAccess"],
  queryFn: () => Permissions.fullDiskAccess(),
});
