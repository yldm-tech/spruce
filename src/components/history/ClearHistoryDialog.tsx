import { ConfirmDialog } from "../common/ConfirmDialog";
import { useClearHistory } from "./queries";

import "./history.css";

interface ClearHistoryDialogProps {
  /** How many runs there are, when the caller knows. */
  count?: number;
  /** Called when the dialog closes: after the history was cleared, or on 取消. */
  onClose: () => void;
}

// A Go method's error arrives as a CallError, an Error whose message is the error's text.
function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** ClearHistoryDialog asks before History.clear, which cannot be undone, and clears on 清空. 操作历史 and 设置 both open it. An error keeps it open with the reason. */
export function ClearHistoryDialog({ count, onClose }: ClearHistoryDialogProps) {
  const clear = useClearHistory();
  const what = count === undefined ? "全部操作记录" : `全部 ${count} 条操作记录`;
  return (
    <ConfirmDialog
      title="清空操作历史？"
      message={
        <>
          {`将删除${what}，无法恢复。只删除记录，不会撤销或改动已经做过的清理和维护。`}
          {clear.isError && (
            <span role="alert" className="history-confirm-error">
              {errorMessage(clear.error)}
            </span>
          )}
        </>
      }
      confirmLabel="清空"
      tone="danger"
      busy={clear.isPending}
      onConfirm={() => clear.mutate(undefined, { onSuccess: onClose })}
      onCancel={onClose}
    />
  );
}
