import { queryOptions, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";

import { Settings, type AppSettings } from "../../mygo";
import { settingsQueryOptions, useUpdateSettings } from "../common/settings";

/** The ranges Go clamps every number to (the settings package's constants). They are fixed for a build. */
export const limitsQueryOptions = queryOptions({
  queryKey: ["settings", "limits"],
  queryFn: () => Settings.limits(),
  staleTime: Infinity,
});

/** The app's version and data folder, fixed while it runs. */
export const aboutQueryOptions = queryOptions({
  queryKey: ["settings", "about"],
  queryFn: () => Settings.about(),
  staleTime: Infinity,
});

// A Go method's error arrives as a CallError, an Error whose message is the error's text.
export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** What became of a save: the settings Go stored, or the sentence it failed with. */
export type SaveOutcome = { ok: true; stored: AppSettings } | { ok: false; message: string };

/** useSaveSettings returns save(apply): it applies the change to the cached settings at once (so every control shows it), sends the result to Settings.update and resolves with what Go stored, which replaces the cache. On an error useUpdateSettings refetches the stored settings, which replace the optimistic ones. */
export function useSaveSettings() {
  const queryClient = useQueryClient();
  const { mutateAsync } = useUpdateSettings();
  return useCallback(
    async (apply: (s: AppSettings) => AppSettings): Promise<SaveOutcome> => {
      const current = queryClient.getQueryData(settingsQueryOptions.queryKey);
      if (!current) return { ok: false, message: "设置还没有读取完成，请稍后再试" };
      const next = apply(current);
      queryClient.setQueryData(settingsQueryOptions.queryKey, next);
      try {
        return { ok: true, stored: await mutateAsync(next) };
      } catch (err) {
        return { ok: false, message: errorMessage(err) };
      }
    },
    [queryClient, mutateAsync],
  );
}

/** How one control reads and writes its field of AppSettings. */
export interface FieldSpec<T> {
  get: (s: AppSettings) => T;
  put: (s: AppSettings, value: T) => AppSettings;
  /** Whether Go stored what was sent; Object.is by default. */
  equal?: (a: T, b: T) => boolean;
  /** The note shown when Go stored something else (it clamps and cleans every value); by default "已调整为 <value>". */
  adjusted?: (stored: T) => string;
}

/** One control's view of its setting. */
export interface Setting<T> {
  /** What the control shows: the value being saved, else the stored one. */
  value: T;
  /** Saves value, at once or after the debounce. */
  change: (value: T) => void;
  /** Sends a debounced save now, e.g. when the control loses focus. */
  flush: () => void;
  /** The sentence the last save failed with. */
  error: string | null;
  /** Set when Go stored something other than what was sent, saying what. */
  note: string | null;
}

/** useSetting connects one control to one field: change shows the value at once and saves it (after debounceMs without another change, for sliders and text; a pending save is sent when the control unmounts), then reports a failure or an adjustment Go made. */
export function useSetting<T>(settings: AppSettings, spec: FieldSpec<T>, debounceMs = 0): Setting<T> {
  const save = useSaveSettings();
  const [draft, setDraft] = useState<{ value: T } | null>(null);
  const [status, setStatus] = useState<{ error: string | null; note: string | null }>({
    error: null,
    note: null,
  });
  const specRef = useRef(spec);
  const pending = useRef<{ value: T } | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const seq = useRef(0);
  useEffect(() => {
    specRef.current = spec;
  });

  const commit = useCallback(
    async (value: T) => {
      const id = ++seq.current;
      const { get, put, equal = Object.is, adjusted } = specRef.current;
      const outcome = await save((s) => put(s, value));
      // A newer change is on its way; its own result decides what to show.
      if (id !== seq.current || pending.current) return;
      setDraft(null);
      if (!outcome.ok) {
        setStatus({ error: outcome.message, note: null });
        return;
      }
      const stored = get(outcome.stored);
      setStatus({
        error: null,
        note: equal(stored, value) ? null : adjusted ? adjusted(stored) : `已调整为 ${String(stored)}`,
      });
    },
    [save],
  );

  const flush = useCallback(() => {
    clearTimeout(timer.current);
    timer.current = undefined;
    const p = pending.current;
    pending.current = null;
    if (p) void commit(p.value);
  }, [commit]);

  const flushRef = useRef(flush);
  useEffect(() => {
    flushRef.current = flush;
  });
  useEffect(() => () => flushRef.current(), []);

  const change = useCallback(
    (value: T) => {
      setDraft({ value });
      setStatus({ error: null, note: null });
      if (debounceMs <= 0) {
        void commit(value);
        return;
      }
      pending.current = { value };
      clearTimeout(timer.current);
      timer.current = setTimeout(flush, debounceMs);
    },
    [commit, flush, debounceMs],
  );

  return { value: draft ? draft.value : spec.get(settings), change, flush, ...status };
}
