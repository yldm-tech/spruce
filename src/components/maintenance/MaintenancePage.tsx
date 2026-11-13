import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Channel } from "mygo-runtime";
import { useState } from "react";

import { Maintenance, Permissions, type ProgressEvent, type RunSummary, type TaskView } from "../../mygo";
import { AlertBanner } from "./AlertBanner";
import { DoneOverlay } from "./DoneOverlay";
import { catalogQueryOptions, fullDiskAccessQueryOptions } from "./queries";
import { RunOverlay, type RunProgress } from "./RunOverlay";
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
    setProgress({ ...IDLE_PROGRESS, total: selected.length });
    setRunTasks(selected);
    run.mutate(selected.map((t) => t.id));
  };

  const reset = () => {
    run.reset();
    setProgress(IDLE_PROGRESS);
    setRunTasks([]);
  };

  const phase = run.isPending ? "running" : run.isSuccess || run.isError ? "done" : "idle";
  const permissionMissing = needsFullDiskAccess(fullDiskAccess.state);
  const summary: RunSummary | undefined = run.data;

  return (
    <>
      <div className="maint-page" inert={phase !== "idle"}>
        <header className="maint-header">
          <div className="maint-header-title">
            <span aria-hidden="true" className="maint-header-icon">
              <svg
                width="24"
                height="24"
                viewBox="0 0 24 24"
                fill="none"
                stroke="#FFFFFF"
                strokeWidth="2"
                strokeLinejoin="round"
              >
                <path d="M13 3L5 14h6l-1 7 8-11h-6z" />
              </svg>
            </span>
            <div className="maint-header-text">
              <h1>维护</h1>
              <span>给系统做一次轻量保养，不会动你的文档和照片</span>
            </div>
          </div>
        </header>

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

        <footer className="maint-footer">
          <div className="maint-stat maint-stat--end">
            <span className="maint-stat-label">已选择</span>
            <span className="maint-stat-value">{`${selected.length} 项`}</span>
          </div>
          <button type="button" className="run-button" disabled={selected.length === 0} onClick={start}>
            运行
          </button>
          <div className="maint-stat">
            <span className="maint-stat-label">预计用时</span>
            <span className="maint-stat-value">{formatEstimate(estimate)}</span>
          </div>
        </footer>
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
