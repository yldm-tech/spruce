import { useId, useRef, type ReactNode } from "react";

import { Glyph } from "./Glyph";
import { GlassDialog } from "./GlassDialog";

interface DoneDialogProps {
  title: string;
  /** The big figure, e.g. "4 项" or "1.2 GB". */
  big: string;
  /** One line each, shown in order; they scroll when they outgrow the window. */
  lines: string[];
  /** Draws the check mark; otherwise the exclamation mark. */
  succeeded: boolean;
  /** Extra content above the button, such as a permission prompt. */
  children?: ReactNode;
  closeLabel?: string;
  onClose: () => void;
}

/** DoneDialog is the card at the end of a run or a clean, as the 维护 design draws it: the badge, the title, the big figure, the lines and 完成. Its description is the figure and then the lines. */
export function DoneDialog({
  title,
  big,
  lines,
  succeeded,
  children,
  closeLabel = "完成",
  onClose,
}: DoneDialogProps) {
  const titleId = useId();
  const bigId = useId();
  const linesId = useId();
  const closeRef = useRef<HTMLButtonElement>(null);
  return (
    <GlassDialog
      labelledBy={titleId}
      describedBy={`${bigId} ${linesId}`}
      initialFocus={closeRef}
      className="done-card"
    >
      <span aria-hidden="true" className="done-badge">
        <Glyph size={44} strokeWidth={2.6}>
          {succeeded ? <path d="M5 12.5l4.5 4.5L19 7.5" /> : <path d="M12 6.5v7M12 17.5v.5" />}
        </Glyph>
      </span>
      <span id={titleId} className="done-title">
        {title}
      </span>
      <span id={bigId} className="done-big">
        {big}
      </span>
      <span id={linesId} className="done-sub">
        {lines.join("\n")}
      </span>
      {children}
      <button ref={closeRef} type="button" className="done-button" onClick={onClose}>
        {closeLabel}
      </button>
    </GlassDialog>
  );
}
