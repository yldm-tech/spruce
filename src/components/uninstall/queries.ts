import { queryOptions } from "@tanstack/react-query";
import { Channel } from "mygo-runtime";
import { useEffect, useState } from "react";

import { Uninstall, type UninstallAppDetails } from "../../mygo";
import { mergeDetails, type AppDetails } from "./model";

/** The installed apps. They change outside the app, so the page refetches them after an uninstall and on 刷新. */
export const appsQueryOptions = queryOptions({
  queryKey: ["uninstall", "apps"],
  queryFn: () => Uninstall.apps(),
});

/** The leftovers of one app, measured with it. They are looked up again whenever an app is picked, since the app may have run and written new files meanwhile. */
export function planQueryOptions(appId: string) {
  return queryOptions({
    queryKey: ["uninstall", "plan", appId],
    queryFn: () => Uninstall.leftovers(appId),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
}

/** How often streamed details reach the list, so a few hundred updates do not mean a few hundred renders. */
const FLUSH_MS = 120;

/** useAppDetails streams the sizes, icons and last use of the listed apps through Uninstall.measure, once per fetch of the list (listedAt is the list's dataUpdatedAt; 0 while there is none). Leaving the page stops the measuring. */
export function useAppDetails(listedAt: number) {
  const [details, setDetails] = useState<ReadonlyMap<string, AppDetails>>(() => new Map());
  // The listedAt whose measuring finished; measuring is derived from it rather than set when the effect starts.
  const [measuredAt, setMeasuredAt] = useState(0);

  useEffect(() => {
    if (listedAt === 0) return;
    let pending: UninstallAppDetails[] = [];
    let timer: ReturnType<typeof setTimeout> | undefined;
    let live = true;
    const flush = () => {
      timer = undefined;
      if (!live || pending.length === 0) return;
      const batch = pending;
      pending = [];
      setDetails((prev) => mergeDetails(prev, batch));
    };
    const channel = new Channel<UninstallAppDetails>((d) => {
      pending.push(d);
      timer ??= setTimeout(flush, FLUSH_MS);
    });
    Uninstall.measure(channel)
      // A failed measure leaves the sizes it did not reach unknown; the list says so.
      .catch(() => {})
      .finally(() => {
        if (!live) return;
        flush();
        setMeasuredAt(listedAt);
      });
    return () => {
      live = false;
      if (timer) clearTimeout(timer);
      channel.close();
    };
  }, [listedAt]);

  return { details, measuring: listedAt !== 0 && measuredAt !== listedAt };
}
