import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Channel } from "mygo-runtime";
import { useEffect, useRef, useState } from "react";

import { Maintenance, Permissions, type ProgressEvent, type RunSummary, type TaskView } from "../../mygo";
import { ActionBar } from "../common/ActionBar";
import { Page, PageHeader } from "../common/Page";
import { PAGES } from "../common/pages";
import { useBlockShell } from "../ShellBlock";
import { AlertBanner } from "./AlertBanner";
import { DoneOverlay } from "./DoneOverlay";
import { catalogQueryOptions, fullDiskAccessQueryOptions } from "./queries";
import { RunOverlay, runStatusText, type RunProgress } from "./RunOverlay";
import {
  formatEstimate,
  FULL_DISK_ACCESS_TASKS,
  fullDiskAccessBlocked,
  needsFullDiskAccess,
} from "./summary";
import { TaskCard } from "./TaskCard";

import "./maintenance.css";

const IDLE_PROGRESS: RunProgress = { currentTaskId: null, finished: new Set(), done: 0, total: 0 };

function applyProgress(prev: RunProgress, ev: ProgressEvent): RunProgress {
  if (ev.kind === "started") return { ...prev, currentTaskId: ev.taskId, done: ev.done, total: ev.total };
  return { ...prev, finished: new Set(prev.finished).add(ev.taskId), done: ev.done, total: ev.total };
}

// A Go method's error arrives as a CallError, an Error whose message is the error's text.
function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** useFullDiskAccess returns the Full Disk Access state and the guide that opens System Settings at it. */
function useFullDiskAccess() {
  const queryClient = useQueryClient();
  const state = useQuery(fullDiskAccessQueryOptions);
  const guide = useMutation({
    mutationFn: () => Permissions.guideFullDiskAccess(),
    onSuccess: (next) => queryClient.setQueryData(fullDiskAccessQueryOptions.queryKey, next),
  });
  return { state: state.data, guide };
}

function FullDiskAccessAction({ guide }: { guide: ReturnType<typeof useFullDiskAccess>["guide"] }) {
  return (
    <span className="permission-action">
      <span className="permission-action-text">需要「完全磁盘访问权限」才能优化 Safari 和邮件的数据库</span>
      <button
        type="button"
        className="permission-action-button"
        disabled={guide.isPending}
        onClick={() => guide.mutate()}
      >
        {guide.isPending ? "等待授权…" : "去授权"}
      </button>
      {guide.isError && (
        <span role="alert" className="permission-action-error">
          无法打开系统设置
        </span>
      )}
    </span>
  );
}

/** MaintenancePage is the 维护 page: the platform's maintenance cards from Maintenance.catalog, the CPU alert banner, and the run button with its running and done overlays. */
export function MaintenancePage() {
  const queryClient = useQueryClient();
  const catalog = useQuery(catalogQueryOptions);
  const fullDiskAccess = useFullDiskAccess();
  // The user's choices; a task without an entry keeps its DefaultOn.
  const [overrides, setOverrides] = useState<Record<string, boolean>>({});
  const [progress, setProgress] = useState<RunProgress>(IDLE_PROGRESS);
  const [runTasks, setRunTasks] = useState<TaskView[]>([]);
  const runButtonRef = useRef<HTMLButtonElement>(null);
  // Set by 完成, so focus goes back to 运行 once the page is no longer inert.
  const restoreFocus = useRef(false);

  const tasks = catalog.data ?? [];
  const isOn = (t: TaskView) => overrides[t.id] ?? t.defaultOn;
  const selected = tasks.filter(isOn);
  const estimate = selected.reduce((sum, t) => sum + t.estimatedSeconds, 0);

  const run = useMutation({
    mutationFn: (ids: string[]) => {
      const channel = new Channel<ProgressEvent>((ev) => setProgress((prev) => applyProgress(prev, ev)));
      return Maintenance.run(ids, channel);
    },
    // A run can be the moment the user notices a missing permission; check again so the done card's 去授权 reflects the present state.
    onSettled: () => queryClient.invalidateQueries({ queryKey: fullDiskAccessQueryOptions.queryKey }),
  });

  const start = () => {
    if (selected.length === 0) return;
    // A grant made before this run is already in effect for it; only a grant made on the done card may say "run again".
    fullDiskAccess.guide.reset();
    setProgress({ ...IDLE_PROGRESS, total: selected.length });
    setRunTasks(selected);
    run.mutate(selected.map((t) => t.id));
  };

  const reset = () => {
    run.reset();
    fullDiskAccess.guide.reset();
    setProgress(IDLE_PROGRESS);
    setRunTasks([]);
    restoreFocus.current = true;
  };

  const phase = run.isPending ? "running" : run.isSuccess || run.isError ? "done" : "idle";
  useBlockShell(phase !== "idle");
  useEffect(() => {
    if (phase === "idle" && restoreFocus.current) {
      restoreFocus.current = false;
      runButtonRef.current?.focus();
    }
  }, [phase]);
  const liveText =
    phase !== "running"
      ? ""
      : progress.currentTaskId === null
        ? `正在维护系统…，共 ${progress.total || runTasks.length} 项任务`
        : runStatusText(runTasks, progress);
  const permissionMissing = needsFullDiskAccess(fullDiskAccess.state);
  const summary: RunSummary | undefined = run.data;

  return (
    <>
      <Page blocked={phase !== "idle"}>
        <PageHeader {...PAGES.maintenance} />

        <AlertBanner />

        <section aria-label="维护任务" aria-busy={catalog.isPending} className="task-grid">
          {catalog.isError && (
            <div role="alert" className="task-grid-error">
              <span>{`无法读取维护项目：${errorMessage(catalog.error)}`}</span>
              <button type="button" className="alert-banner-button" onClick={() => void catalog.refetch()}>
                重试
              </button>
            </div>
          )}
          {tasks.map((task) => (
            <TaskCard
              key={task.id}
              task={task}
              checked={isOn(task)}
              onToggle={(id, checked) => setOverrides((prev) => ({ ...prev, [id]: checked }))}
            >
              {FULL_DISK_ACCESS_TASKS.has(task.id) && isOn(task) && permissionMissing && (
                <FullDiskAccessAction guide={fullDiskAccess.guide} />
              )}
            </TaskCard>
          ))}
        </section>

        <ActionBar
          ref={runButtonRef}
          label="运行"
          disabled={selected.length === 0}
          onClick={start}
          start={{ label: "已选择", value: `${selected.length} 项` }}
          end={{ label: "预计用时", value: formatEstimate(estimate) }}
        />
      </Page>

      {/* Mounted from the first render and outside the inert page, so screen readers announce the run as soon as it starts. */}
      <div role="status" aria-live="polite" className="visually-hidden">
        {liveText}
      </div>
      {phase === "running" && <RunOverlay tasks={runTasks} progress={progress} />}
      {phase === "done" && (
        <DoneOverlay
          outcome={
            summary ? { kind: "summary", summary } : { kind: "error", message: errorMessage(run.error) }
          }
          catalog={tasks}
          permissionAction={
            summary &&
            fullDiskAccessBlocked(summary.results) &&
            (permissionMissing ? (
              <FullDiskAccessAction guide={fullDiskAccess.guide} />
            ) : (
              fullDiskAccess.state === "granted" &&
              fullDiskAccess.guide.isSuccess && (
                <span className="permission-action-text">
                  已获得完全磁盘访问权限，再运行一次即可优化数据库
                </span>
              )
            ))
          }
          onClose={reset}
        />
      )}
    </>
  );
}
