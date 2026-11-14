import type { SpaceProgress } from "../../mygo";
import { formatBytes, truncateMiddle } from "../common/format";
import { ProgressRing } from "../common/ProgressRing";
import { fileCount } from "./labels";

interface ScanningViewProps {
  /** What is being scanned, e.g. "Macintosh HD". */
  name: string;
  progress: SpaceProgress | null;
  onCancel: () => void;
}

/** ScanningView takes the page's place while a scan runs: the spinning ring (a walk cannot know its length in advance), the files and space counted so far, the folder being read and 停止. */
export function ScanningView({ name, progress, onCancel }: ScanningViewProps) {
  const title = `正在分析「${name}」…`;
  return (
    <section aria-label={title} aria-busy="true" className="scan-state">
      <ProgressRing size={120} label={title} />
      <span className="scan-state-title">{title}</span>
      <span className="scan-state-sub space-progress-figures">
        {progress ? `已扫描 ${fileCount(progress.files)} · ${formatBytes(progress.bytes)}` : "正在准备…"}
      </span>
      <span className="scan-state-path" title={progress?.path}>
        {progress?.path ? truncateMiddle(progress.path, 64) : " "}
      </span>
      <button type="button" className="ghost-button" onClick={onCancel}>
        停止
      </button>
    </section>
  );
}
