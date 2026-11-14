import { queryOptions } from "@tanstack/react-query";
import { Channel } from "mygo-runtime";
import { useEffect, useState } from "react";

import { Sysinfo, type Vitals } from "../../mygo";
import { appendSample } from "./format";

/** The static facts about the system. They do not change while the app runs, so they are fetched once. */
export const systemInfoQueryOptions = queryOptions({
  queryKey: ["sysinfo", "info"],
  queryFn: () => Sysinfo.info(),
  staleTime: Infinity,
});

export interface VitalsStream {
  /** The samples so far, oldest first: the history Go kept, then one every two seconds. */
  samples: Vitals[];
  /** The stream ended with an error (the call failed); retry starts it again. */
  failed: boolean;
  retry: () => void;
}

/** useVitals watches the machine while the page is mounted: it calls Sysinfo.watch with a channel and closes it on unmount, which stops Go's sampler once no page listens. */
export function useVitals(): VitalsStream {
  const [samples, setSamples] = useState<Vitals[]>([]);
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let active = true;
    const channel = new Channel<Vitals>((v) => {
      if (active) setSamples((prev) => appendSample(prev, v));
    });
    Sysinfo.watch(channel).catch(() => {
      if (active) setFailed(true);
    });
    return () => {
      active = false;
      channel.close();
    };
  }, [attempt]);

  return {
    samples,
    failed,
    retry: () => {
      setFailed(false);
      setAttempt((n) => n + 1);
    },
  };
}
