import { useEffect, useRef } from "react";

import type { TaskView } from "../../mygo";
import { ProgressRing } from "../common/ProgressRing";

/** How far a run got, from the progress channel. */
export interface RunProgress {
  /** The task that started last, or null before the first event. */
  currentTaskId: string | null;
  /** IDs of the tasks that finished. */
  finished: ReadonlySet<string>;
  done: number;
  total: number;
}

interface RunOverlayProps {
  /** The selected tasks, in catalog order. */
  tasks: TaskView[];
  progress: RunProgress;
}

/** runStatusText is the progress line of a run, e.g. "刷新 DNS 缓存 · 已完成 1/5", or "正在准备…" before the first task starts. */
export function runStatusText(tasks: TaskView[], progress: RunProgress): string {
  const current = tasks.find((t) => t.id === progress.currentTaskId);
  const total = progress.total || tasks.length;
  return current ? `${current.title} · 已完成 ${progress.done}/${total}` : "正在准备…";
}

/** RunOverlay covers the page while a run goes on. It takes focus when it appears, since the page under it turns inert; the page's own live region announces the progress, because a live region that is inserted already filled in is often not read. */
export function RunOverlay({ tasks, progress }: RunOverlayProps) {
  const total = progress.total || tasks.length;
  // Admin tasks run first, together behind one system prompt; until they finish the user may have a prompt to answer.
  const awaitingAdmin = tasks.some((t) => t.needsAdmin && !progress.finished.has(t.id));
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    ref.current?.focus();
  }, []);

  return (
    <div ref={ref} tabIndex={-1} role="group" aria-label="正在维护系统" className="overlay overlay--run">
      <ProgressRing label="正在维护系统" />
      <span className="overlay-title">正在维护系统…</span>
      <span className="overlay-sub">{`共 ${total} 项任务，不会动你的文档和照片`}</span>
      <span className="overlay-progress">{runStatusText(tasks, progress)}</span>
      {awaitingAdmin && (
        <span className="overlay-note">部分任务需要管理员权限，请在系统弹出的授权窗口中确认</span>
      )}
    </div>
  );
}
