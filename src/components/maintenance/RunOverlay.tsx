import type { TaskView } from "../../mygo";

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

/** RunOverlay covers the page while a run goes on. It is a polite live region: the current task and the count are announced as they change. */
export function RunOverlay({ tasks, progress }: RunOverlayProps) {
  const current = tasks.find((t) => t.id === progress.currentTaskId);
  const total = progress.total || tasks.length;
  // Admin tasks run first, together behind one system prompt; until they finish the user may have a prompt to answer.
  const awaitingAdmin = tasks.some((t) => t.needsAdmin && !progress.finished.has(t.id));

  return (
    <div role="status" aria-live="polite" className="overlay overlay--run">
      <svg width="96" height="96" viewBox="0 0 96 96" aria-hidden="true" className="run-spinner">
        <circle className="run-spinner-track" cx="48" cy="48" r="40" fill="none" strokeWidth="8" />
        <path
          className="run-spinner-arc"
          d="M48 8 A40 40 0 0 1 88 48"
          fill="none"
          strokeWidth="8"
          strokeLinecap="round"
        />
      </svg>
      <span className="overlay-title">正在维护系统…</span>
      <span className="overlay-sub">{`共 ${total} 项任务，不会动你的文档和照片`}</span>
      <span className="overlay-progress">
        {current ? `${current.title} · 已完成 ${progress.done}/${total}` : "正在准备…"}
      </span>
      {awaitingAdmin && (
        <span className="overlay-note">部分任务需要管理员权限，请在系统弹出的授权窗口中确认</span>
      )}
    </div>
  );
}
