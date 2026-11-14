import type { Ref } from "react";

import "./common.css";

/** A figure beside the action button, e.g. 已选择 / 4 项. */
export interface Stat {
  label: string;
  value: string;
}

interface ActionButtonProps {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  ref?: Ref<HTMLButtonElement>;
}

/** ActionButton is the design's round 92px button with its glowing rings, the one main action of a page (运行, 扫描, 清理). */
export function ActionButton({ label, onClick, disabled, ref }: ActionButtonProps) {
  return (
    <button ref={ref} type="button" className="action-button" disabled={disabled} onClick={onClick}>
      {label}
    </button>
  );
}

interface ActionBarProps extends ActionButtonProps {
  /** The figure left of the button, right-aligned. */
  start?: Stat;
  /** The figure right of the button. */
  end?: Stat;
}

/** ActionBar is the bottom of a page: the round action button between two figures, as on the 维护 page. It sits at the bottom of the page's column. */
export function ActionBar({ start, end, ...button }: ActionBarProps) {
  return (
    <footer className="action-bar">
      <StatView stat={start} align="end" />
      <ActionButton {...button} />
      <StatView stat={end} align="start" />
    </footer>
  );
}

function StatView({ stat, align }: { stat: Stat | undefined; align: "start" | "end" }) {
  return (
    <div className={align === "end" ? "action-stat action-stat--end" : "action-stat"}>
      {stat && (
        <>
          <span className="action-stat-label">{stat.label}</span>
          <span className="action-stat-value">{stat.value}</span>
        </>
      )}
    </div>
  );
}
