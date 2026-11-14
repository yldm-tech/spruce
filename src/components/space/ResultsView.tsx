import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useRef, useState, type KeyboardEvent } from "react";

import { Permissions, Space, type SpaceEntry, type SpaceSummary } from "../../mygo";
import { ConfirmDialog } from "../common/ConfirmDialog";
import { EmptyState } from "../common/EmptyState";
import { HISTORY_QUERY_KEY, trashSizeQueryOptions, useReveal } from "../common/files";
import { fileManagerName, formatBytes, trashName, truncateMiddle } from "../common/format";
import { EntryList } from "./EntryList";
import { entryLabel } from "./labels";
import {
  childrenQueryOptions,
  currentScanQueryOptions,
  fullDiskAccessQueryOptions,
  largeFilesQueryOptions,
  SPACE_QUERY_KEY,
} from "./queries";
import { Treemap } from "./Treemap";

// A Go method's error arrives as a CallError, an Error whose message is the sentence to show.
function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

type Tab = "map" | "large";

interface Notice {
  kind: "done" | "error";
  text: string;
}

interface ResultsViewProps {
  summary: SpaceSummary;
  onRescan: () => void;
}

/** ResultsView is the page after a scan: the 空间分布 tab (the path, the treemap of the open folder and the same children as a list) and the 大文件 tab, with 在访达中显示 and 移到废纸篓 on every entry. The service keeps the tree, so opening a folder asks for that one folder only; a move to the Trash corrects the sizes without a new scan. */
export function ResultsView({ summary, onRescan }: ResultsViewProps) {
  const queryClient = useQueryClient();
  const [folderId, setFolderId] = useState(summary.root.id);
  const [tab, setTab] = useState<Tab>("map");
  const [confirming, setConfirming] = useState<SpaceEntry | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const reveal = useReveal();
  const listing = useQuery({
    ...childrenQueryOptions(summary.scanId, folderId),
    placeholderData: keepPreviousData,
  });
  const large = useQuery({ ...largeFilesQueryOptions(summary.scanId), enabled: tab === "large" });

  const trash = useMutation({
    mutationFn: (entry: SpaceEntry) => Space.trash(summary.scanId, [entry.path]),
    onSuccess: (res, entry) => {
      queryClient.setQueryData(currentScanQueryOptions.queryKey, (prev) =>
        prev && prev.scanId === summary.scanId ? { ...prev, root: res.root } : prev,
      );
      void queryClient.invalidateQueries({ queryKey: [...SPACE_QUERY_KEY, "children", summary.scanId] });
      void queryClient.invalidateQueries({ queryKey: [...SPACE_QUERY_KEY, "large", summary.scanId] });
      void queryClient.invalidateQueries({ queryKey: HISTORY_QUERY_KEY });
      void queryClient.invalidateQueries({ queryKey: trashSizeQueryOptions.queryKey });
      const name = entryLabel(entry);
      setNotice(
        res.items > 0
          ? {
              kind: "done",
              text: `已把「${name}」移到${trashName()}，清倒${trashName()}后可释放 ${formatBytes(res.trashed)}`,
            }
          : { kind: "error", text: `「${name}」没有移走：${res.failed[0]?.reason ?? "原因不明"}` },
      );
    },
    onError: (err) => setNotice({ kind: "error", text: errorMessage(err) }),
    onSettled: () => setConfirming(null),
  });

  const actions = {
    onOpen: (entry: SpaceEntry) => {
      setNotice(null);
      setTab("map");
      setFolderId(entry.id);
    },
    onReveal: (entry: SpaceEntry) => reveal.mutate(entry.path),
    onTrash: (entry: SpaceEntry) => {
      if (entry.locked) {
        setNotice({ kind: "error", text: `「${entryLabel(entry)}」不能移走：${entry.locked}` });
        return;
      }
      setNotice(null);
      setConfirming(entry);
    },
  };

  const folder = listing.data;
  const tabIds = { map: useId(), large: useId() };
  const panelIds = { map: useId(), large: useId() };
  const tabRefs = useRef<Record<Tab, HTMLButtonElement | null>>({ map: null, large: null });
  const onTabKey = (e: KeyboardEvent) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    const next: Tab = tab === "map" ? "large" : "map";
    setTab(next);
    tabRefs.current[next]?.focus();
  };

  return (
    <div className="space-results">
      {summary.denied > 0 && <DeniedBanner summary={summary} onRescan={onRescan} />}

      <div role="tablist" aria-label="查看方式" className="space-tabs">
        {(["map", "large"] as const).map((t) => (
          <button
            key={t}
            ref={(el) => {
              tabRefs.current[t] = el;
            }}
            type="button"
            role="tab"
            id={tabIds[t]}
            aria-selected={tab === t}
            aria-controls={panelIds[t]}
            tabIndex={tab === t ? 0 : -1}
            className="space-tab"
            onClick={() => setTab(t)}
            onKeyDown={onTabKey}
          >
            {t === "map" ? "空间分布" : "大文件"}
          </button>
        ))}
      </div>

      <div role="status" aria-live="polite" className="space-notice">
        {notice?.kind === "done" ? notice.text : ""}
      </div>
      {notice?.kind === "error" && (
        <p role="alert" className="space-error">
          {notice.text}
        </p>
      )}
      {reveal.isError && (
        <p role="alert" className="space-error">
          {reveal.error instanceof Error ? reveal.error.message : `无法在${fileManagerName()}中显示`}
        </p>
      )}

      <div
        role="tabpanel"
        id={panelIds.map}
        aria-labelledby={tabIds.map}
        hidden={tab !== "map"}
        className="space-panel"
      >
        {folder && (
          <nav aria-label="位置" className="space-path">
            <ol>
              {folder.ancestors.map((a, i) => {
                const last = i === folder.ancestors.length - 1;
                return (
                  <li key={a.id}>
                    {last ? (
                      <span aria-current="location" className="space-path-current">
                        {a.name}
                      </span>
                    ) : (
                      <button type="button" className="link-button" onClick={() => actions.onOpen(a)}>
                        {a.name}
                      </button>
                    )}
                  </li>
                );
              })}
            </ol>
            <span className="space-path-size">{formatBytes(folder.entry.size)}</span>
          </nav>
        )}
        {listing.isError && (
          <div role="alert" className="space-error space-error--row">
            <span>{errorMessage(listing.error)}</span>
            <button type="button" className="ghost-button" onClick={onRescan}>
              重新扫描
            </button>
          </div>
        )}
        {folder && folder.children.length === 0 && (
          <EmptyState
            title={folder.entry.denied ? "无权读取这个文件夹" : "这个文件夹是空的"}
            description={folder.entry.denied ? "Spruce 没有读取它的权限，它的大小没有算进结果" : undefined}
            tone="cyan"
          />
        )}
        {folder && folder.children.length > 0 && (
          <div className="space-browser" aria-busy={listing.isFetching}>
            <Treemap
              entries={folder.children}
              total={folder.entry.size}
              label={folder.entry.name}
              onOpen={actions.onOpen}
            />
            <div className="space-browser-list">
              <EntryList
                label={`${folder.entry.name} 的内容`}
                entries={folder.children}
                detail="share"
                total={folder.entry.size}
                {...actions}
              />
            </div>
          </div>
        )}
      </div>

      <div
        role="tabpanel"
        id={panelIds.large}
        aria-labelledby={tabIds.large}
        hidden={tab !== "large"}
        className="space-panel"
      >
        <p className="space-large-note">
          {`「${summary.name}」中 ${formatBytes(summary.largeFileMin)} 以上的文件，最多列出 200 个。可以在「设置」中调整大小。`}
        </p>
        {large.isError && (
          <p role="alert" className="space-error">
            {errorMessage(large.error)}
          </p>
        )}
        {large.data && large.data.length === 0 && (
          <EmptyState title="没有找到大文件" description="这里没有达到这个大小的文件" tone="cyan" />
        )}
        {large.data && large.data.length > 0 && (
          <div className="space-large-list">
            <EntryList label="大文件" entries={large.data} detail="location" {...actions} />
          </div>
        )}
      </div>

      {confirming && (
        <ConfirmDialog
          title={`移到${trashName()}`}
          message={`将把「${entryLabel(confirming)}」（${formatBytes(confirming.size)}）移到${trashName()}。需要时可以从${trashName()}放回原处。`}
          confirmLabel={`移到${trashName()}`}
          busy={trash.isPending}
          onConfirm={() => trash.mutate(confirming)}
          onCancel={() => setConfirming(null)}
        />
      )}
    </div>
  );
}

/** DeniedBanner says how many folders the scan could not read and, on macOS without Full Disk Access, offers 去授权 (the same guide as the 维护 page). */
function DeniedBanner({ summary, onRescan }: { summary: SpaceSummary; onRescan: () => void }) {
  const queryClient = useQueryClient();
  const access = useQuery(fullDiskAccessQueryOptions);
  const guide = useMutation({
    mutationFn: () => Permissions.guideFullDiskAccess(),
    onSuccess: (next) => queryClient.setQueryData(fullDiskAccessQueryOptions.queryKey, next),
  });
  const first = summary.deniedPaths[0];
  const granted = guide.isSuccess && access.data === "granted";
  let sub = "这些文件夹的大小没有算进结果";
  if (access.data === "denied") sub = "开启「完全磁盘访问权限」后重新扫描，就能算进这些文件夹";
  if (granted) sub = "已获得完全磁盘访问权限，重新扫描即可算进这些文件夹";
  return (
    <div className="space-banner">
      <div className="space-banner-text">
        <span className="space-banner-title">{`${summary.denied.toLocaleString("zh-CN")} 个文件夹无权读取`}</span>
        <span className="space-banner-sub">{sub}</span>
        {first && (
          <span className="space-banner-path" title={summary.deniedPaths.join("\n")}>
            {`例如 ${truncateMiddle(first, 60)}`}
          </span>
        )}
      </div>
      {access.data === "denied" && (
        <button
          type="button"
          className="space-banner-button"
          disabled={guide.isPending}
          onClick={() => guide.mutate()}
        >
          {guide.isPending ? "等待授权…" : "去授权"}
        </button>
      )}
      {granted && (
        <button type="button" className="ghost-button" onClick={onRescan}>
          重新扫描
        </button>
      )}
      {guide.isError && (
        <span role="alert" className="space-error">
          无法打开系统设置
        </span>
      )}
    </div>
  );
}
