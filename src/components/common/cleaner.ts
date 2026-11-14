import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Channel } from "mygo-runtime";
import { useEffect, useRef, useState } from "react";

import { Cleanup, type CleanerID, type CleanRequest, type Progress } from "../../mygo";
import { HISTORY_QUERY_KEY, trashSizeQueryOptions } from "./files";
import {
  defaultSelection,
  NOTHING_SELECTED,
  selectedTotals,
  toCleanRequest,
  type Selection,
} from "./selection";
import { useSettings } from "./settings";

/** Where a cleaner page is: before a scan, scanning, showing the results, cleaning, or showing the done card. */
export type CleanerPhase = "idle" | "scanning" | "results" | "cleaning" | "done";

// A Go method's error arrives as a CallError, an Error whose message is the sentence to show.
function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** useCleaner drives a cleaner page through Cleanup.scan and Cleanup.clean for one CleanerID: the phase, the streamed progress, the scan result with its selection (starting from each category's Selected), the confirmation the user asked for in 设置 (General.confirmBeforeClean), and the result. Leaving the page stops what is running. */
export function useCleaner(cleaner: CleanerID) {
  const queryClient = useQueryClient();
  const settings = useSettings();
  const [progress, setProgress] = useState<Progress | null>(null);
  const [selection, setSelection] = useState<Selection>(NOTHING_SELECTED);
  const [confirming, setConfirming] = useState(false);
  const channel = useRef<Channel<Progress> | null>(null);
  // Set by cancel, so a scan the user stopped returns to idle without an error.
  const [stopped, setStopped] = useState(false);

  const open = () => {
    const ch = new Channel<Progress>((p) => setProgress(p));
    channel.current = ch;
    return ch;
  };

  const scan = useMutation({
    mutationFn: () => Cleanup.scan(cleaner, open()),
    onSuccess: (res) => setSelection(defaultSelection(res.categories)),
    onSettled: () => {
      channel.current = null;
      setProgress(null);
    },
  });

  const clean = useMutation({
    mutationFn: (req: CleanRequest) => Cleanup.clean(cleaner, req, open()),
    onSettled: () => {
      channel.current = null;
      setProgress(null);
      void queryClient.invalidateQueries({ queryKey: trashSizeQueryOptions.queryKey });
      void queryClient.invalidateQueries({ queryKey: HISTORY_QUERY_KEY });
    },
  });

  useEffect(() => () => channel.current?.close(), []);

  const categories = scan.data?.categories ?? [];
  const totals = selectedTotals(categories, selection);

  let phase: CleanerPhase = "idle";
  if (clean.isPending) phase = "cleaning";
  else if (clean.isSuccess || clean.isError) phase = "done";
  else if (scan.isPending) phase = "scanning";
  else if (scan.isSuccess) phase = "results";

  const startClean = () => clean.mutate(toCleanRequest(categories, selection));

  return {
    phase,
    /** The latest progress event of the running scan or clean, or null before the first. */
    progress,
    /** The last scan's result, while there is one. */
    scan: scan.data ?? null,
    /** Why the last scan failed (a sentence to show), unless the user stopped it. */
    scanError: scan.isError && !stopped ? errorMessage(scan.error) : null,
    selection,
    setSelection,
    /** The checked items' count and size. */
    totals,
    /** The clean's result once it is done. */
    result: clean.data ?? null,
    /** Why the clean could not start (a sentence to show). */
    cleanError: clean.isError ? errorMessage(clean.error) : null,
    /** Whether the page should show the confirmation dialog (cleanConfirmation and ConfirmDialog). */
    confirming,
    startScan: () => {
      setStopped(false);
      clean.reset();
      setConfirming(false);
      scan.mutate();
    },
    /** cancel stops the scan (back to idle) or the clean (after the current item; the result says it was interrupted). */
    cancel: () => {
      setStopped(true);
      channel.current?.close();
    },
    /** requestClean cleans the selection, or first asks when the user wants to confirm every clean. */
    requestClean: () => {
      if (totals.count === 0) return;
      if (settings.data?.general.confirmBeforeClean ?? true) setConfirming(true);
      else startClean();
    },
    confirmClean: () => {
      setConfirming(false);
      startClean();
    },
    dismissConfirm: () => setConfirming(false),
    /** finish closes the done card and returns to idle, ready to scan again. */
    finish: () => {
      clean.reset();
      scan.reset();
      setSelection(NOTHING_SELECTED);
    },
  };
}

/** What useCleaner returns. */
export type CleanerFlow = ReturnType<typeof useCleaner>;
