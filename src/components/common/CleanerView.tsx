import { useEffect, useRef, type ReactNode } from "react";

import { ActionBar } from "./ActionBar";
import { CategoryList } from "./CategoryList";
import { setInUseAside } from "./cleanPlan";
import { CleanPlanDialog } from "./CleanPlanDialog";
import type { CleanerFlow } from "./cleaner";
import { EmptyState } from "./EmptyState";
import { formatBytes } from "./format";
import { Page, PageHeader } from "./Page";
import { PAGES, type PageId } from "./pages";
import { CleanDoneDialog, CleaningOverlay, ScanIdle, ScanProgress } from "./ScanState";

import "./cleanerView.css";

interface CleanerViewProps {
  /** The page, for its header and tile. */
  page: PageId;
  /** The page's useCleaner. */
  flow: CleanerFlow;
  /** What the idle page offers to do, e.g. "扫描系统垃圾". */
  idleTitle: string;
  /** What the scan looks at. */
  idleDescription: string;
  /** Lines under the description before a scan, such as the last clean. */
  idleDetails?: ReactNode;
  /** Disables 扫描, e.g. when there is nowhere to search. */
  scanDisabled?: boolean;
  /** The result list's accessible name. */
  listLabel: string;
  /** A line above the results, such as where the items go. */
  notice?: string;
  /** The label of the checked size beside 清理: "可释放" when it is freed at once, otherwise what it is. */
  sizeLabel: string;
  /** The page when the scan found nothing. */
  emptyTitle: string;
  emptyDescription: string;
}

/** CleanerView is the body of a cleaner page, driven by useCleaner: the idle page with 扫描, the scan's progress, the results in a CategoryList with 清理 and the checked total, the confirmation that lists what is deleted for good and what goes to the Trash, the cleaning overlay and the done card. Files another program held open are summed up in one line on the done card rather than listed as failures. */
export function CleanerView({
  page,
  flow,
  idleTitle,
  idleDescription,
  idleDetails,
  scanDisabled,
  listLabel,
  notice,
  sizeLabel,
  emptyTitle,
  emptyDescription,
}: CleanerViewProps) {
  const meta = PAGES[page];
  const { phase, scan } = flow;
  const categories = scan?.categories ?? [];
  const idleRef = useRef<HTMLDivElement>(null);
  // Set by 完成, so focus goes to 扫描 once the page is no longer inert.
  const restoreFocus = useRef(false);
  useEffect(() => {
    if (phase === "idle" && restoreFocus.current) {
      restoreFocus.current = false;
      idleRef.current?.querySelector<HTMLButtonElement>("button.action-button")?.focus();
    }
  }, [phase]);

  const finish = () => {
    restoreFocus.current = true;
    flow.finish();
  };
  const blocked = phase === "cleaning" || phase === "done" || flow.confirming;
  const showResults = phase === "results" || phase === "cleaning" || phase === "done";
  const outcome = flow.result ? setInUseAside(flow.result) : null;

  let liveText = "";
  if (phase === "scanning") liveText = "正在扫描…";
  else if (phase === "results" && scan) liveText = `扫描完成，找到 ${formatBytes(scan.size)}`;
  else if (phase === "cleaning") liveText = "正在清理…";

  return (
    <>
      <Page blocked={blocked}>
        <PageHeader {...meta}>
          {phase === "results" && categories.length > 0 && (
            <button type="button" className="ghost-button" onClick={flow.startScan}>
              重新扫描
            </button>
          )}
        </PageHeader>

        {phase === "idle" && (
          <div ref={idleRef} className="cleaner-idle">
            <ScanIdle
              tone={meta.tone}
              icon={meta.icon}
              title={idleTitle}
              description={idleDescription}
              onStart={flow.startScan}
              disabled={scanDisabled}
              error={flow.scanError}
            >
              {idleDetails && <div className="cleaner-idle-details">{idleDetails}</div>}
            </ScanIdle>
          </div>
        )}

        {phase === "scanning" && <ScanProgress progress={flow.progress} onCancel={flow.cancel} />}

        {showResults && categories.length === 0 && (
          <EmptyState
            title={emptyTitle}
            description={emptyDescription}
            tone={meta.tone}
            icon={meta.icon}
            action={
              <button type="button" className="ghost-button" onClick={flow.startScan}>
                重新扫描
              </button>
            }
          />
        )}

        {showResults && categories.length > 0 && (
          <>
            {notice && <p className="cleaner-notice">{notice}</p>}
            {scan && scan.denied > 0 && (
              <p className="cleaner-notice cleaner-notice--warning">{`有 ${scan.denied} 个位置没有读取权限，结果可能不完整`}</p>
            )}
            <CategoryList
              categories={categories}
              selection={flow.selection}
              onSelectionChange={flow.setSelection}
              label={listLabel}
              disabled={phase !== "results"}
            />
            <ActionBar
              label="清理"
              onClick={flow.requestClean}
              disabled={phase !== "results" || flow.totals.count === 0}
              start={{ label: "已选择", value: `${flow.totals.count} 项` }}
              end={{ label: sizeLabel, value: formatBytes(flow.totals.bytes) }}
            />
          </>
        )}
      </Page>

      {/* Mounted from the first render and outside the inert page, so screen readers announce each phase. */}
      <div role="status" aria-live="polite" className="visually-hidden">
        {liveText}
      </div>
      {phase === "results" && flow.confirming && (
        <CleanPlanDialog
          categories={categories}
          selection={flow.selection}
          onConfirm={flow.confirmClean}
          onCancel={flow.dismissConfirm}
        />
      )}
      {phase === "cleaning" && (
        <CleaningOverlay sub={`共 ${flow.totals.count} 项`} progress={flow.progress} onCancel={flow.cancel} />
      )}
      {phase === "done" && (
        <CleanDoneDialog
          outcome={outcome ? { result: outcome.result } : { error: flow.cleanError ?? "清理没有完成" }}
          onClose={finish}
        >
          {outcome && outcome.inUse > 0 && (
            <span className="cleaner-done-note">{`${outcome.inUse} 项里有文件正被其他程序使用，已跳过这些文件，下次可以再试`}</span>
          )}
        </CleanDoneDialog>
      )}
    </>
  );
}
