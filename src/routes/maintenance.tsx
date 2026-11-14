import { createFileRoute } from "@tanstack/react-router";

import { MaintenancePage } from "../components/maintenance/MaintenancePage";
import { catalogQueryOptions, fullDiskAccessQueryOptions } from "../components/maintenance/queries";

export const Route = createFileRoute("/maintenance")({
  // Start both calls while the route loads; the page reads them from the query cache and shows its own error for a failed catalog.
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(catalogQueryOptions);
    void context.queryClient.prefetchQuery(fullDiskAccessQueryOptions);
  },
  component: MaintenancePage,
});
