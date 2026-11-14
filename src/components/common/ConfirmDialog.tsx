import { useId, useRef, type ReactNode } from "react";

import { GlassDialog } from "./GlassDialog";

interface ConfirmDialogProps {
  title: string;
  /** What will happen, e.g. "将永久删除 34 项，共 1.2 GB，无法恢复。" */
  message: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  /** danger draws the confirm button red and focuses 取消 first, for actions that cannot be undone. */
  tone?: "default" | "danger";
  /** Disables both buttons while the action starts. */
  busy?: boolean;
  onConfirm: () => void;
  /** Also called on Escape. */
  onCancel: () => void;
}

/** ConfirmDialog asks before an action: a GlassDialog with a title, the message and two buttons. */
export function ConfirmDialog({
  title,
  message,
  confirmLabel = "确定",
  cancelLabel = "取消",
  tone = "default",
  busy = false,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const titleId = useId();
  const messageId = useId();
  const cancelRef = useRef<HTMLButtonElement>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);
  return (
    <GlassDialog
      labelledBy={titleId}
      describedBy={messageId}
      onClose={busy ? undefined : onCancel}
      initialFocus={tone === "danger" ? cancelRef : confirmRef}
      className="confirm-card"
    >
      <span id={titleId} className="done-title">
        {title}
      </span>
      <div id={messageId} className="confirm-message">
        {message}
      </div>
      <div className="confirm-buttons">
        <button ref={cancelRef} type="button" className="ghost-button" disabled={busy} onClick={onCancel}>
          {cancelLabel}
        </button>
        <button
          ref={confirmRef}
          type="button"
          className={tone === "danger" ? "done-button done-button--danger" : "done-button"}
          disabled={busy}
          onClick={onConfirm}
        >
          {confirmLabel}
        </button>
      </div>
    </GlassDialog>
  );
}
