import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";

import { Files } from "../../mygo";

/** How much the Trash holds. It changes outside the app, so it is fetched whenever a page shows it. */
export const trashSizeQueryOptions = queryOptions({
  queryKey: ["files", "trashSize"],
  queryFn: () => Files.trashSize(),
  retry: false,
});

/** useReveal returns the mutation that shows a path selected in Finder or Explorer. */
export function useReveal() {
  return useMutation({ mutationFn: (path: string) => Files.reveal(path) });
}

/** useEmptyTrash returns the mutation that empties the Trash for good (confirm first) and refreshes its size. */
export function useEmptyTrash() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => Files.emptyTrash(),
    onSettled: () => queryClient.invalidateQueries({ queryKey: trashSizeQueryOptions.queryKey }),
  });
}

/** The history list's query key prefix, invalidated after anything the history records. */
export const HISTORY_QUERY_KEY = ["history"] as const;
