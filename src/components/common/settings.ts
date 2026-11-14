import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

import { events, Settings, type AppSettings } from "../../mygo";

/** The user's settings. They change only through Settings.update/reset or the settings:changed event, which writes the cache, so they are never refetched on their own. */
export const settingsQueryOptions = queryOptions({
  queryKey: ["settings"],
  queryFn: () => Settings.get(),
  staleTime: Infinity,
});

/** useSettings returns the settings query, kept current by events.settingsChanged: a change made in another window, or by Go, replaces the cached settings at once. */
export function useSettings() {
  const queryClient = useQueryClient();
  useEffect(
    () => events.settingsChanged.on((next) => queryClient.setQueryData(settingsQueryOptions.queryKey, next)),
    [queryClient],
  );
  return useQuery(settingsQueryOptions);
}

/** useUpdateSettings returns the mutation that stores new settings. Go clamps them to the allowed ranges and returns what it stored, which replaces the cache; on an error (a sentence to show) the cache keeps the stored settings Go sends back through the event. */
export function useUpdateSettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (next: AppSettings) => Settings.update(next),
    onSuccess: (stored) => queryClient.setQueryData(settingsQueryOptions.queryKey, stored),
    onError: () => queryClient.invalidateQueries({ queryKey: settingsQueryOptions.queryKey }),
  });
}

/** useResetSettings returns the mutation that restores every default. */
export function useResetSettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => Settings.reset(),
    onSuccess: (stored) => queryClient.setQueryData(settingsQueryOptions.queryKey, stored),
    onError: () => queryClient.invalidateQueries({ queryKey: settingsQueryOptions.queryKey }),
  });
}
