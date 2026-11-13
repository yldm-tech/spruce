import { useEffect, useId, useRef, type ReactNode } from "react";

import type { RunSummary, TaskView } from "../../mygo";
import { doneTitle, formatIssueLine, issueLines, okCount } from "./summary";

interface DoneOverlayProps {
  /** Shown as the card's lines: the run's summary, or the reason it could not start. */
  outcome: { kind: "summary"; summary: RunSummary } | { kind: "error"; message: string };
  catalog: TaskView[];
  /** The 去授权 action, when a Full Disk Access task was left undone. */
  permissionAction?: ReactNode;
  onClose: () => void;
}

/** DoneOverlay is the modal card at the end of a run. Its big number counts the tasks that completed; the lines below explain the rest with each task's own message. 完成 returns the page to idle. */
export function DoneOverlay({ outcome, catalog, permissionAction, onClose }: DoneOverlayProps) {
  const titleId = useId();
  const closeRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    closeRef.current?.focus();
  }, []);

  let title: string;
  let big: string;
  let lines: string[];
  let succeeded: boolean;
  if (outcome.kind === "error") {
    title = "维护没有运行";
    big = "0 项";
    lines = [outcome.message];
    succeeded = false;
  } else {
    const { summary } = outcome;
    title = doneTitle(summary);
    big = `${okCount(summary)} 项`;
    succeeded = okCount(summary) > 0;
    const issues = issueLines(summary.results, catalog).map(formatIssueLine);
    lines = issues.length === 0 ? ["全部已应用，无需重启"] : issues;
    if (summary.interrupted) lines.push("维护在中途停止，没有运行的任务不会改动系统");
  }

  return (
    <div className="overlay overlay--done">
      <div role="dialog" aria-modal="true" aria-labelledby={titleId} className="done-card">
        <span aria-hidden="true" className="done-badge">
          <svg
            width="44"
            height="44"
            viewBox="0 0 24 24"
            fill="none"
            stroke="#FFFFFF"
            strokeWidth="2.6"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            {succeeded ? <path d="M5 12.5l4.5 4.5L19 7.5" /> : <path d="M12 6.5v7M12 17.5v.5" />}
          </svg>
        </span>
        <span id={titleId} className="done-title">
          {title}
        </span>
        <span className="done-big">{big}</span>
        <span className="done-sub">{lines.join("\n")}</span>
        {permissionAction}
        <button ref={closeRef} type="button" className="done-button" onClick={onClose}>
          完成
        </button>
      </div>
    </div>
  );
}
