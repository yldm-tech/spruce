import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Channel } from "mygo-runtime";
import { useEffect, useRef, useState } from "react";

import { Space, type SpaceProgress } from "../../mygo";
import { ActionBar } from "../common/ActionBar";
import { baseName, formatBytes, formatRelativeTime } from "../common/format";
import { Page, PageHeader } from "../common/Page";
import { PAGES } from "../common/pages";
import { currentScanQueryOptions, SPACE_QUERY_KEY } from "./queries";
import { ResultsView } from "./ResultsView";
import { ScanningView } from "./ScanningView";
import { StartView, type ScanRequest } from "./StartView";

import "./space.css";

// A Go method's error arrives as a CallError, an Error whose message is the sentence to show.
function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** SpacePage is the 空间透视 page: pick a disk or a folder, watch the scan, then browse where the space went and move what is not needed to the Trash. The service keeps the last scan, so coming back to the page shows it again at once. Leaving the page stops a running scan. */
export function SpacePage() {
  const queryClient = useQueryClient();
  const current = useQuery(currentScanQueryOptions);
  // Set by 更换位置: the start screen shows although there is a scan to return to.
  const [picking, setPicking] = useState(false);
  const [scanning, setScanning] = useState<ScanRequest | null>(null);
  const [progress, setProgress] = useState<SpaceProgress | null>(null);
  const [error, setError] = useState<string | null>(null);
  const channel = useRef<Channel<SpaceProgress> | null>(null);
  // Set by 停止, so a scan the user stopped goes back without an error.
  const stopped = useRef(false);

  const scan = useMutation({
    mutationFn: (request: ScanRequest) => {
      const ch = new Channel<SpaceProgress>((p) => setProgress(p));
      channel.current = ch;
      return Space.scan(request.path, ch);
    },
    onMutate: (request) => {
      stopped.current = false;
      setError(null);
      setProgress(null);
      setScanning(request);
    },
    onSuccess: (summary) => {
      queryClient.removeQueries({ queryKey: [...SPACE_QUERY_KEY, "children"] });
      queryClient.removeQueries({ queryKey: [...SPACE_QUERY_KEY, "large"] });
      queryClient.setQueryData(currentScanQueryOptions.queryKey, summary);
      setPicking(false);
    },
    onError: (err) => {
      if (!stopped.current) setError(errorMessage(err));
    },
    onSettled: () => {
      channel.current = null;
      setProgress(null);
      setScanning(null);
    },
  });

  const choose = useMutation({
    mutationFn: () => Space.chooseFolder(),
    onMutate: () => setError(null),
    onSuccess: (path) => {
      if (path) scan.mutate({ path, name: baseName(path) || path });
    },
    onError: (err) => setError(errorMessage(err)),
  });

  useEffect(() => () => channel.current?.close(), []);

  const summary = current.data ?? null;
  let phase: "loading" | "start" | "scanning" | "results" = "start";
  if (scanning) phase = "scanning";
  else if (current.isPending) phase = "loading";
  else if (summary && !picking) phase = "results";

  const rescan = () => {
    if (summary) scan.mutate({ path: summary.root.path, name: summary.name });
  };

  return (
    <Page className="space-page">
      <PageHeader {...PAGES.space}>
        {phase === "results" && (
          <button type="button" className="ghost-button" onClick={() => setPicking(true)}>
            更换位置
          </button>
        )}
        {phase === "start" && summary && (
          <button
            type="button"
            className="ghost-button"
            onClick={() => {
              setError(null);
              setPicking(false);
            }}
          >
            {`返回「${summary.name}」`}
          </button>
        )}
      </PageHeader>

      {phase === "start" && (
        <StartView
          onScan={(request) => scan.mutate(request)}
          onChoose={() => choose.mutate()}
          choosing={choose.isPending}
          error={error}
        />
      )}
      {phase === "scanning" && scanning && (
        <ScanningView
          name={scanning.name}
          progress={progress}
          onCancel={() => {
            stopped.current = true;
            channel.current?.close();
          }}
        />
      )}
      {phase === "results" && summary && (
        <>
          {error && (
            <p role="alert" className="space-error">
              {error}
            </p>
          )}
          <ResultsView key={summary.scanId} summary={summary} onRescan={rescan} />
          <ActionBar
            label="重新扫描"
            onClick={rescan}
            start={{ label: summary.name, value: formatBytes(summary.root.size) }}
            end={{ label: "上次扫描", value: formatRelativeTime(summary.scannedAt) || "刚刚" }}
          />
        </>
      )}
    </Page>
  );
}
