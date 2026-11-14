import { useEffect, useRef, type ReactNode } from "react";

import type { Category, CleanResult, Progress } from "../../mygo";
import { useBlockShell } from "../ShellBlock";
import { ActionButton } from "./ActionBar";
import { cleanConfirmation, cleanSummary } from "./cleanSummary";
import { ConfirmDialog } from "./ConfirmDialog";
import { DoneDialog } from "./DoneDialog";
import { formatBytes, truncateMiddle } from "./format";
import { IconTile, type Tone } from "./Glyph";
import { InOverlayHost } from "./OverlayHost";
import { ProgressRing } from "./ProgressRing";
import type { Selection } from "./selection";

import "./common.css";

/** fractionOf is how far a progress event got, or undefined while its total is unknown. */
function fractionOf(progress: Progress | null): number | undefined {
  if (!progress || progress.total <= 0) return undefined;
  return progress.done / progress.total;
}

interface ScanIdleProps {
  tone: Tone;
  /** The glyph's paths, as in PAGES. */
  icon: ReactNode;
  title: string;
  description: string;
  actionLabel?: string;
  onStart: () => void;
  disabled?: boolean;
  /** Why the last scan failed, shown in the warning colour. */
  error?: string | null;
  /** Extra content under the description, such as the size of the Trash. */
  children?: ReactNode;
}

/** ScanIdle is a cleaner page before a scan: the page's tile, what the scan looks at and the round 扫描 button, centred in the page. */
export function ScanIdle({
  tone,
  icon,
  title,
  description,
  actionLabel = "扫描",
  onStart,
  disabled,
  error,
  children,
}: ScanIdleProps) {
  return (
    <section aria-label={title} className="scan-state">
      <IconTile tone={tone} size={72} glyphSize={34} className="scan-state-tile" glow>
        {icon}
      </IconTile>
      <span className="scan-state-title">{title}</span>
      <span className="scan-state-sub">{description}</span>
      {children}
      {error && (
        <span role="alert" className="scan-state-error">
          {error}
        </span>
      )}
      <div className="scan-state-action">
        <ActionButton label={actionLabel} onClick={onStart} disabled={disabled} />
      </div>
    </section>
  );
}

interface ScanProgressProps {
  title?: string;
  progress: Progress | null;
  onCancel: () => void;
  cancelLabel?: string;
}

/** ScanProgress is a cleaner page while it scans, in place of its content: the ring (filling once the scanner knows how many steps there are), the space found so far, the folder being read and 停止. */
export function ScanProgress({
  title = "正在扫描…",
  progress,
  onCancel,
  cancelLabel = "停止",
}: ScanProgressProps) {
  const fraction = fractionOf(progress);
  return (
    <section aria-label={title} aria-busy="true" className="scan-state">
      <ProgressRing value={fraction} size={120} label={title}>
        {fraction !== undefined ? `${Math.round(fraction * 100)}%` : undefined}
      </ProgressRing>
      <span className="scan-state-title">{title}</span>
      <span className="scan-state-sub">{`已找到 ${formatBytes(progress?.bytes ?? 0)}`}</span>
      <span className="scan-state-path" title={progress?.path}>
        {progress?.path ? truncateMiddle(progress.path, 64) : "正在准备…"}
      </span>
      <button type="button" className="ghost-button" onClick={onCancel}>
        {cancelLabel}
      </button>
    </section>
  );
}

interface CleaningOverlayProps {
  title?: string;
  /** The line under the title, e.g. "共 34 项，不会动你的文档和照片". */
  sub?: string;
  progress: Progress | null;
  /** Shows 停止, which ends the clean after the current item. */
  onCancel?: () => void;
}

/** CleaningOverlay covers the main area while a clean runs, as the 维护 run overlay: the ring, the item being removed, and 已释放 so far. It blocks the shell and takes focus. */
export function CleaningOverlay({ title = "正在清理…", sub, progress, onCancel }: CleaningOverlayProps) {
  useBlockShell(true);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    ref.current?.focus();
  }, []);
  const fraction = fractionOf(progress);
  return (
    <InOverlayHost>
      <div ref={ref} tabIndex={-1} role="group" aria-label={title} className="overlay overlay--run">
        <ProgressRing value={fraction} label={title} />
        <span className="overlay-title">{title}</span>
        {sub && <span className="overlay-sub">{sub}</span>}
        <span className="overlay-progress">
          {progress && progress.total > 0
            ? `已处理 ${progress.done}/${progress.total} · 已释放 ${formatBytes(progress.bytes)}`
            : "正在准备…"}
        </span>
        {progress?.path && (
          <span className="scan-state-path" title={progress.path}>
            {truncateMiddle(progress.path, 64)}
          </span>
        )}
        {onCancel && (
          <button type="button" className="ghost-button" onClick={onCancel}>
            停止
          </button>
        )}
      </div>
    </InOverlayHost>
  );
}

interface CleanDoneDialogProps {
  /** The clean's result, or the sentence of a clean that could not start. */
  outcome: { result: CleanResult } | { error: string };
  onClose: () => void;
  children?: ReactNode;
}

/** CleanDoneDialog is the done card of a clean: the space given back, one line per category, the items left in place and what the Trash means (cleanSummary). */
export function CleanDoneDialog({ outcome, onClose, children }: CleanDoneDialogProps) {
  const s = cleanSummary(outcome);
  return (
    <DoneDialog title={s.title} big={s.big} lines={s.lines} succeeded={s.succeeded} onClose={onClose}>
      {children}
    </DoneDialog>
  );
}

interface CleanConfirmDialogProps {
  categories: readonly Category[];
  selection: Selection;
  onConfirm: () => void;
  onCancel: () => void;
}

/** CleanConfirmDialog asks before a clean, saying how much goes and whether it can be undone (cleanConfirmation); it is the danger kind when anything is deleted for good. */
export function CleanConfirmDialog({ categories, selection, onConfirm, onCancel }: CleanConfirmDialogProps) {
  const { message, danger } = cleanConfirmation(categories, selection);
  return (
    <ConfirmDialog
      title="确认清理"
      message={message}
      confirmLabel="清理"
      tone={danger ? "danger" : "default"}
      onConfirm={onConfirm}
      onCancel={onCancel}
    />
  );
}
