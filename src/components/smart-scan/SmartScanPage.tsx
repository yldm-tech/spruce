import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useEffect, useId, useRef, useState } from "react";

import { ActionBar } from "../common/ActionBar";
import { CategoryList } from "../common/CategoryList";
import { useCleaner, type CleanerFlow } from "../common/cleaner";
import { setInUseAside } from "../common/cleanPlan";
import { CleanPlanDialog } from "../common/CleanPlanDialog";
import { EmptyState } from "../common/EmptyState";
import { formatBytes } from "../common/format";
import { Glyph, IconTile } from "../common/Glyph";
import { lastCleanText, useLastClean } from "../common/lastClean";
import { Page, PageHeader } from "../common/Page";
import { PAGES, type PageId } from "../common/pages";
import { ProgressRing } from "../common/ProgressRing";
import { CleanDoneDialog, CleaningOverlay } from "../common/ScanState";
import { withinGroup, type Selection } from "../common/selection";
import { locationsQueryOptions } from "../space/queries";
import {
  overallFraction,
  recommendedTotals,
  SOURCE_SCOPE,
  sourceRows,
  summarize,
  type SourceRow,
  type SourceSummary,
} from "./sources";
import { StatusCards } from "./StatusCards";

import "../common/cleanerView.css";
import "./smartScan.css";

const PAGE = PAGES["smart-scan"];

type PagePath = `/${PageId}`;

/** Hero is the page before a scan: what 智能扫描 does, the big round 扫描 button and the last smart clean. */
function Hero({ flow, last }: { flow: CleanerFlow; last: string | null }) {
  const titleId = useId();
  return (
    <section aria-labelledby={titleId} className="smart-hero">
      <button type="button" className="smart-hero-button" onClick={flow.startScan} aria-describedby={titleId}>
        <Glyph size={40} strokeWidth={2.2} color="currentColor">
          {PAGE.icon}
        </Glyph>
        <span>扫描</span>
      </button>
      <span id={titleId} className="smart-hero-title">
        一次检查系统垃圾、开发者垃圾和安装包
      </span>
      <span className="smart-hero-sub">
        扫描只读取，不会删除任何东西。结果会按推荐勾选可以放心清理的项目，你确认后才会清理。
      </span>
      {last && <span className="smart-hero-sub">{last}</span>}
      {flow.scanError && (
        <span role="alert" className="scan-state-error">
          {flow.scanError}
        </span>
      )}
    </section>
  );
}

function rowStatus(row: SourceRow): string {
  switch (row.status) {
    case "waiting":
      return "等待开始…";
    case "scanning":
      return `正在扫描… 已找到 ${formatBytes(row.bytes)}`;
    case "done":
      return row.items > 0
        ? `完成，找到 ${row.items} 项，共 ${formatBytes(row.bytes)}`
        : "完成，没有找到可以清理的内容";
    case "failed":
      return row.error ?? "没能扫描完成";
  }
}

/** Scanning shows the overall ring and one row per source, each with its own progress bar and state, and 停止. */
function Scanning({ flow }: { flow: CleanerFlow }) {
  const fraction = overallFraction(flow.progress);
  const rows = sourceRows(flow.progress);
  return (
    <section aria-label="正在扫描" aria-busy="true" className="smart-scanning">
      <ProgressRing value={fraction} size={120} label="智能扫描进度">
        {fraction !== undefined ? `${Math.round(fraction * 100)}%` : undefined}
      </ProgressRing>
      <span className="scan-state-title">正在扫描…</span>
      <ul className="smart-scan-rows" aria-label="扫描进度">
        {rows.map((row) => {
          const meta = PAGES[row.id];
          return (
            <li key={row.id} className="smart-scan-row" data-status={row.status}>
              <IconTile tone={meta.tone} size={36} glyphSize={18} className="smart-scan-row-icon">
                {meta.icon}
              </IconTile>
              <div className="smart-scan-row-text">
                <span className="smart-scan-row-title">{meta.title}</span>
                <span className="smart-scan-row-scope">{SOURCE_SCOPE[row.id]}</span>
                <div
                  role="progressbar"
                  aria-label={`${meta.title}扫描进度`}
                  aria-valuemin={row.fraction !== undefined ? 0 : undefined}
                  aria-valuemax={row.fraction !== undefined ? 100 : undefined}
                  aria-valuenow={row.fraction !== undefined ? Math.round(row.fraction * 100) : undefined}
                  className={row.fraction === undefined ? "smart-bar smart-bar--busy" : "smart-bar"}
                >
                  <span className="smart-bar-fill" style={{ width: `${(row.fraction ?? 0) * 100}%` }} />
                </div>
              </div>
              <span
                className={
                  row.status === "failed"
                    ? "smart-scan-row-status smart-scan-row-status--failed"
                    : "smart-scan-row-status"
                }
              >
                {rowStatus(row)}
              </span>
            </li>
          );
        })}
      </ul>
      <button type="button" className="ghost-button" onClick={flow.cancel}>
        停止
      </button>
    </section>
  );
}

/** SourceCard sums up one source's share of the results, with 查看详情 to its page. */
function SourceCard({ summary }: { summary: SourceSummary }) {
  const meta = PAGES[summary.id];
  const to: PagePath = `/${summary.id}`;
  const failed = summary.state?.status === "failed";
  return (
    <section aria-label={meta.title} className="smart-source-card">
      <div className="smart-source-head">
        <IconTile tone={meta.tone} size={36} glyphSize={18} className="smart-source-icon">
          {meta.icon}
        </IconTile>
        <span className="smart-source-title">{meta.title}</span>
      </div>
      {failed ? (
        <span className="smart-source-error">{summary.state?.error ?? "没能扫描完成"}</span>
      ) : (
        <>
          <span className="smart-source-size">{formatBytes(summary.bytes)}</span>
          <span className="smart-source-note">
            {summary.items > 0
              ? `${summary.items} 项，推荐清理 ${summary.recommendedItems} 项（${formatBytes(summary.recommendedBytes)}）`
              : "没有找到可以清理的内容"}
          </span>
        </>
      )}
      <Link to={to} className="link-button smart-source-link">
        查看详情
      </Link>
    </section>
  );
}

interface SourceSectionProps {
  summary: SourceSummary;
  selection: Selection;
  setSelection: (next: Selection) => void;
  disabled: boolean;
}

/** SourceSection is one source's categories in the full list, under a heading that folds them away; its 全选 and 全不选 act on the source only. */
function SourceSection({ summary, selection, setSelection, disabled }: SourceSectionProps) {
  const [open, setOpen] = useState(true);
  const listId = useId();
  const meta = PAGES[summary.id];
  return (
    <div className="smart-list-section">
      <h3 className="smart-list-heading">
        <button
          type="button"
          className="smart-list-toggle"
          aria-expanded={open}
          aria-controls={listId}
          onClick={() => setOpen((v) => !v)}
        >
          <Glyph size={16} color="currentColor">
            <path d={open ? "M6 9l6 6 6-6" : "M9 6l6 6-6 6"} />
          </Glyph>
          <span>{meta.title}</span>
          <span className="smart-list-figure">{`${summary.items} 项 · ${formatBytes(summary.bytes)}`}</span>
        </button>
      </h3>
      <div id={listId} hidden={!open}>
        {open && (
          <CategoryList
            label={`${meta.title}的项目`}
            categories={summary.categories}
            selection={selection}
            onSelectionChange={(next) => setSelection(withinGroup(selection, summary.categories, next))}
            disabled={disabled}
          />
        )}
      </div>
    </div>
  );
}

/** Results is the scan's outcome: the 「可清理」 total of what starts checked, a card per source, the other checks, and every category grouped by source, ready to adjust. */
function Results({ flow }: { flow: CleanerFlow }) {
  const scan = flow.scan;
  if (!scan) return null;
  const summaries = summarize(scan);
  const recommended = recommendedTotals(scan);
  const found = scan.items > 0;
  const disabled = flow.phase !== "results";
  return (
    <>
      {found ? (
        <section aria-label="扫描结果" className="smart-total">
          <span className="smart-total-label">可清理</span>
          <span className="smart-total-value">{formatBytes(recommended.bytes)}</span>
          <span className="smart-total-sub">
            {`已按推荐勾选 ${recommended.count} 项。一共找到 ${scan.items} 项（${formatBytes(scan.size)}），默认不选的可能还要用，可以在下面逐项查看。`}
          </span>
        </section>
      ) : (
        <EmptyState
          title="没有找到需要清理的内容"
          description="系统垃圾、开发者垃圾和安装包都很干净，过段时间再来看看。"
          tone={PAGE.tone}
          icon={PAGE.icon}
        />
      )}
      <div className="smart-source-grid">
        {summaries.map((s) => (
          <SourceCard key={s.id} summary={s} />
        ))}
      </div>
      {scan.denied > 0 && (
        <p className="cleaner-notice cleaner-notice--warning">{`有 ${scan.denied} 个位置没有读取权限，结果可能不完整`}</p>
      )}
      <StatusCards />
      {found && (
        <section aria-label="全部项目" className="smart-list">
          <h2 className="smart-section-title">全部项目</h2>
          {summaries
            .filter((s) => s.items > 0)
            .map((s) => (
              <SourceSection
                key={s.id}
                summary={s}
                selection={flow.selection}
                setSelection={flow.setSelection}
                disabled={disabled}
              />
            ))}
          <ActionBar
            label="清理"
            onClick={flow.requestClean}
            disabled={disabled || flow.totals.count === 0}
            start={{ label: "已选择", value: `${flow.totals.count} 项` }}
            end={{ label: "已选大小", value: formatBytes(flow.totals.bytes) }}
          />
        </section>
      )}
    </>
  );
}

/** SmartScanPage is the 智能扫描 page, the app's home: one scan over 系统垃圾, 开发者垃圾 and 安装包 (Cleanup.scan("smart-scan")) with the recommended items checked, cleaned through the same confirmation, overlay and done card as the cleaner pages, beside informational cards about the disk, the CPU, Full Disk Access and 维护. */
export function SmartScanPage() {
  const flow = useCleaner("smart-scan");
  const last = useLastClean("smartScan");
  const queryClient = useQueryClient();
  const { phase, scan } = flow;
  const categories = scan?.categories ?? [];
  const blocked = phase === "cleaning" || phase === "done" || flow.confirming;
  const outcome = flow.result ? setInUseAside(flow.result) : null;

  const heroRef = useRef<HTMLDivElement>(null);
  // Set by 完成, so focus goes to 扫描 once the page is no longer inert.
  const restoreFocus = useRef(false);
  useEffect(() => {
    if (phase === "idle" && restoreFocus.current) {
      restoreFocus.current = false;
      heroRef.current?.querySelector<HTMLButtonElement>("button.smart-hero-button")?.focus();
    }
  }, [phase]);
  const finish = () => {
    restoreFocus.current = true;
    flow.finish();
    void queryClient.invalidateQueries({ queryKey: locationsQueryOptions.queryKey });
  };

  let liveText = "";
  if (phase === "scanning") liveText = "正在扫描…";
  else if (phase === "results" && scan)
    liveText = `扫描完成，可清理 ${formatBytes(recommendedTotals(scan).bytes)}`;
  else if (phase === "cleaning") liveText = "正在清理…";

  return (
    <>
      <Page blocked={blocked} className="smart-page">
        <PageHeader {...PAGE}>
          {phase === "results" && (
            <button type="button" className="ghost-button" onClick={flow.startScan}>
              重新扫描
            </button>
          )}
        </PageHeader>

        {phase === "idle" && (
          <div ref={heroRef} className="smart-idle">
            <Hero flow={flow} last={last.data ? lastCleanText(last.data) : null} />
            <StatusCards />
          </div>
        )}

        {phase === "scanning" && <Scanning flow={flow} />}

        {(phase === "results" || phase === "cleaning" || phase === "done") && <Results flow={flow} />}
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
