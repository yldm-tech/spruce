import type { ReactNode } from "react";

import type { RunSummary, TaskView } from "../../mygo";
import { DoneDialog } from "../common/DoneDialog";
import { completedLines, doneTitle, formatIssueLine, issueLines, okCount } from "./summary";

interface DoneOverlayProps {
  /** Shown as the card's lines: the run's summary, or the reason it could not start. */
  outcome: { kind: "summary"; summary: RunSummary } | { kind: "error"; message: string };
  catalog: TaskView[];
  /** The 去授权 action, when a Full Disk Access task was left undone. */
  permissionAction?: ReactNode;
  onClose: () => void;
}

/** DoneOverlay is the modal card at the end of a run. Its big number counts the tasks that completed; the lines below give every task's own message, the ones that need attention (not done, or done only in part) first. It makes no claim of its own such as "nothing needs a restart": only the tasks know that. 完成 returns the page to idle. */
export function DoneOverlay({ outcome, catalog, permissionAction, onClose }: DoneOverlayProps) {
  if (outcome.kind === "error") {
    return (
      <DoneDialog
        title="维护没有运行"
        big="0 项"
        lines={[outcome.message]}
        succeeded={false}
        onClose={onClose}
      >
        {permissionAction}
      </DoneDialog>
    );
  }
  const { summary } = outcome;
  const lines = [...issueLines(summary.results, catalog), ...completedLines(summary.results, catalog)].map(
    formatIssueLine,
  );
  if (summary.interrupted) lines.push("维护在中途停止，没有运行的任务不会改动系统");
  return (
    <DoneDialog
      title={doneTitle(summary)}
      big={`${okCount(summary)} 项`}
      lines={lines}
      succeeded={okCount(summary) > 0}
      onClose={onClose}
    >
      {permissionAction}
    </DoneDialog>
  );
}
