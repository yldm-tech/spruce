import { useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";

import type { Kind, Run, TaskView } from "../../mygo";
import { EmptyState } from "../common/EmptyState";
import { formatBytes } from "../common/format";
import { Glyph, IconTile } from "../common/Glyph";
import { Page, PageHeader } from "../common/Page";
import { PAGES } from "../common/pages";
import { useSettings } from "../common/settings";
import { catalogQueryOptions } from "../maintenance/queries";
import { ClearHistoryDialog } from "./ClearHistoryDialog";
import { useHistory } from "./queries";
import {
  formatClock,
  groupByDay,
  kindMeta,
  kindsIn,
  runDuration,
  runSummary,
  taskLines,
  totalFreed,
} from "./summary";

import "./history.css";

// A Go method's error arrives as a CallError, an Error whose message is the error's text.
function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** HistoryPage is the 操作历史 page: every recorded run, newest first and grouped by day, with filter chips by kind, the space the clean-ups freed, each run's details on demand, and 清空历史. */
export function HistoryPage() {
  const history = useHistory();
  const settings = useSettings();
  // Titles of the maintenance tasks; a run of a task this build does not know shows its ID.
  const catalog = useQuery(catalogQueryOptions);
  const [filter, setFilter] = useState<Kind | null>(null);
  const [clearing, setClearing] = useState(false);

  const runs = history.data ?? [];
  const kinds = kindsIn(runs);
  // A filter whose kind is gone (after 清空历史) shows everything again.
  const active = filter !== null && kinds.includes(filter) ? filter : null;
  const shown = active === null ? runs : runs.filter((r) => r.kind === active);
  const groups = groupByDay(shown);
  const freed = totalFreed(runs);
  const retention = settings.data?.history.retentionDays;

  return (
    <>
      <Page>
        <PageHeader {...PAGES.history}>
          {runs.length > 0 && (
            <button type="button" className="ghost-button" onClick={() => setClearing(true)}>
              清空历史
            </button>
          )}
        </PageHeader>

        {history.isError && (
          <div role="alert" className="history-error">
            <span>{`无法读取操作历史：${errorMessage(history.error)}`}</span>
            <button type="button" className="ghost-button" onClick={() => void history.refetch()}>
              重试
            </button>
          </div>
        )}

        {history.isSuccess && runs.length === 0 && (
          <EmptyState
            title="还没有操作记录"
            description="运行维护或清理后，每一次的结果都会记在这里。"
            tone={PAGES.history.tone}
            icon={PAGES.history.icon}
          />
        )}

        {runs.length > 0 && (
          <>
            <div className="history-overview">
              {freed.cleanups > 0 && (
                <span className="history-total">
                  累计释放 <strong>{formatBytes(freed.bytes)}</strong>
                </span>
              )}
              <span className="history-count">
                {retention === undefined
                  ? `共 ${runs.length} 条记录`
                  : `共 ${runs.length} 条记录，保留最近 ${retention} 天`}
              </span>
            </div>

            {kinds.length > 1 && (
              <div role="group" aria-label="按类型筛选" className="history-filters">
                <FilterChip label="全部" pressed={active === null} onClick={() => setFilter(null)} />
                {kinds.map((k) => (
                  <FilterChip
                    key={k}
                    label={kindMeta(k).title}
                    pressed={active === k}
                    onClick={() => setFilter(k)}
                  />
                ))}
              </div>
            )}

            {groups.map((g) => (
              <DaySection key={g.key} label={g.label} runs={g.runs} catalog={catalog.data ?? []} />
            ))}
          </>
        )}
      </Page>
      {clearing && <ClearHistoryDialog count={runs.length} onClose={() => setClearing(false)} />}
    </>
  );
}

function FilterChip({ label, pressed, onClick }: { label: string; pressed: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      className={pressed ? "history-chip history-chip--on" : "history-chip"}
      aria-pressed={pressed}
      onClick={onClick}
    >
      {label}
    </button>
  );
}

function DaySection({ label, runs, catalog }: { label: string; runs: Run[]; catalog: TaskView[] }) {
  const headingId = useId();
  return (
    <section aria-labelledby={headingId} className="history-day">
      <h2 id={headingId} className="history-day-title">
        {label}
      </h2>
      <ul className="history-list">
        {runs.map((r) => (
          <RunRow key={r.id} run={r} catalog={catalog} />
        ))}
      </ul>
    </section>
  );
}

function RunRow({ run, catalog }: { run: Run; catalog: TaskView[] }) {
  const [open, setOpen] = useState(false);
  const detailsId = useId();
  const meta = kindMeta(run.kind);
  return (
    <li className="history-row" data-run-id={run.id}>
      <button
        type="button"
        className="history-row-head"
        aria-expanded={open}
        aria-controls={detailsId}
        onClick={() => setOpen((v) => !v)}
      >
        <IconTile tone={meta.tone} size={38} glyphSize={19} className="category-icon">
          {meta.icon}
        </IconTile>
        <span className="history-row-text">
          <span className="history-row-title">{meta.title}</span>
          <span className="history-row-summary">{runSummary(run)}</span>
        </span>
        <time className="history-row-time" dateTime={run.startedAt}>
          {formatClock(run.startedAt)}
        </time>
        <span className={open ? "history-chevron history-chevron--open" : "history-chevron"}>
          <Glyph size={16} color="currentColor">
            <path d="M9 6l6 6-6 6" />
          </Glyph>
        </span>
      </button>
      <div id={detailsId} className="history-details" hidden={!open}>
        {open && <RunDetails run={run} catalog={catalog} />}
      </div>
    </li>
  );
}

function RunDetails({ run, catalog }: { run: Run; catalog: TaskView[] }) {
  const lines = run.kind === "maintenance" ? null : (run.details ?? []);
  return (
    <>
      {lines === null ? (
        <ul className="history-tasks">
          {taskLines(run.results, catalog).map((t) => (
            <li key={t.taskId} className="history-task">
              <span className="history-task-title">{t.title}</span>
              <span className={t.attention ? "history-status history-status--attention" : "history-status"}>
                {t.status}
              </span>
              <span className="history-task-message">{t.message}</span>
            </li>
          ))}
        </ul>
      ) : lines.length > 0 ? (
        <ul className="history-lines">
          {lines.map((line, i) => (
            // Lines may repeat (two items with the same reason); their position is their identity.
            <li key={i}>{line}</li>
          ))}
        </ul>
      ) : null}
      <p className="history-times">
        {`开始 ${formatClock(run.startedAt, true)} · 结束 ${formatClock(run.endedAt, true)} · 用时 ${runDuration(run)}`}
      </p>
    </>
  );
}
