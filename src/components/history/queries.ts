import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

import { events, History } from "../../mygo";
import { HISTORY_QUERY_KEY } from "../common/files";

/** How many runs the page asks for: everything the store keeps (history.MaxRuns). */
export const HISTORY_LIMIT = 500;

/** The recorded runs, newest first. Every page invalidates HISTORY_QUERY_KEY after recording, and events.historyAppended covers runs recorded anywhere else. */
export const historyQueryOptions = queryOptions({
  queryKey: [...HISTORY_QUERY_KEY, "list"],
  queryFn: () => History.list(HISTORY_LIMIT),
});

/** useHistory returns the history query, refetched whenever Go records a run (events.historyAppended). */
export function useHistory() {
  const queryClient = useQueryClient();
  useEffect(
    () =>
      events.historyAppended.on(() => void queryClient.invalidateQueries({ queryKey: HISTORY_QUERY_KEY })),
    [queryClient],
  );
  return useQuery(historyQueryOptions);
}

/** useClearHistory returns the mutation that deletes every recorded run (confirm first) and refreshes the list. */
export function useClearHistory() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => History.clear(),
    onSettled: () => queryClient.invalidateQueries({ queryKey: HISTORY_QUERY_KEY }),
  });
}
