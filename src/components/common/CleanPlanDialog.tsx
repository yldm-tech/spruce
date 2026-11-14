import type { Category } from "../../mygo";
import { cleanConfirmation } from "./cleanSummary";
import { cleanPlan, type PlanLine } from "./cleanPlan";
import { ConfirmDialog } from "./ConfirmDialog";
import { formatBytes, trashName } from "./format";
import type { Selection } from "./selection";

import "./cleanerView.css";

interface CleanPlanDialogProps {
  categories: readonly Category[];
  selection: Selection;
  onConfirm: () => void;
  onCancel: () => void;
}

/** CleanPlanDialog asks before a clean like CleanConfirmDialog, and lists what is deleted for good and what goes to the Trash, category by category. It is the danger kind when anything is deleted for good. */
export function CleanPlanDialog({ categories, selection, onConfirm, onCancel }: CleanPlanDialogProps) {
  const { message, danger } = cleanConfirmation(categories, selection);
  const plan = cleanPlan(categories, selection);
  return (
    <ConfirmDialog
      title="确认清理"
      message={
        <>
          <p className="clean-plan-message">{message}</p>
          <PlanGroup title="永久删除" lines={plan.permanent} />
          <PlanGroup title={`移到${trashName()}`} lines={plan.trash} />
        </>
      }
      confirmLabel="清理"
      tone={danger ? "danger" : "default"}
      onConfirm={onConfirm}
      onCancel={onCancel}
    />
  );
}

function PlanGroup({ title, lines }: { title: string; lines: PlanLine[] }) {
  if (lines.length === 0) return null;
  return (
    <section aria-label={title} className="clean-plan-group">
      <span className="clean-plan-title">{title}</span>
      <ul className="clean-plan-list">
        {lines.map((l) => (
          <li key={l.id} className="clean-plan-line">
            <span className="clean-plan-name">{l.title}</span>
            <span className="clean-plan-figure">{`${l.count} 项 · ${formatBytes(l.bytes)}`}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
