import { useId, type ReactNode } from "react";

import type { TaskView } from "../../mygo";
import { TaskIcon } from "./TaskIcon";

interface TaskCardProps {
  task: TaskView;
  checked: boolean;
  onToggle: (id: string, checked: boolean) => void;
  /** Extra content under the description, such as the Full Disk Access prompt. */
  children?: ReactNode;
}

/** TaskCard is one maintenance card: icon, switch, title and either the description or, for tasks with a prerequisite, the hint in the warning colour on a dashed card. */
export function TaskCard({ task, checked, onToggle, children }: TaskCardProps) {
  const titleId = useId();
  const textId = useId();
  return (
    <div className={task.hint ? "task-card task-card--hint" : "task-card"} data-task-id={task.id}>
      <div className="task-card-top">
        <TaskIcon icon={task.icon} />
        <span className="switch">
          <input
            type="checkbox"
            role="switch"
            checked={checked}
            aria-checked={checked}
            onChange={(e) => onToggle(task.id, e.currentTarget.checked)}
            aria-labelledby={titleId}
            aria-describedby={textId}
          />
          <span aria-hidden="true" className="switch-track">
            <span className="switch-thumb" />
          </span>
        </span>
      </div>
      <div className="task-card-text">
        <span id={titleId} className="task-card-title">
          {task.title}
        </span>
        <span id={textId} className={task.hint ? "task-card-hint" : "task-card-description"}>
          {task.hint ?? task.description}
        </span>
        {children}
      </div>
    </div>
  );
}
