import { Link } from "@tanstack/react-router";

import type { Category } from "../../mygo";
import { ActionBar } from "../common/ActionBar";
import { CategoryList } from "../common/CategoryList";
import { useCleaner } from "../common/cleaner";
import { ConfirmDialog } from "../common/ConfirmDialog";
import { EmptyState } from "../common/EmptyState";
import { formatBytes, truncateMiddle } from "../common/format";
import { Page, PageHeader } from "../common/Page";
import { PAGES } from "../common/pages";
import { CleanDoneDialog, CleaningOverlay, ScanIdle, ScanProgress } from "../common/ScanState";
import { withinGroup, type Selection } from "../common/selection";
import { useSettings } from "../common/settings";
import { cleanPlan, confirmationLines, splitGroups } from "./devJunk";

import "./devJunk.css";

const PAGE = PAGES["dev-junk"];

/** ProjectRoots lists the folders the scan searches for projects, with a link to 设置 to change them. */
function ProjectRoots({ roots }: { roots: readonly string[] | undefined }) {
  if (roots === undefined) return null;
  if (roots.length === 0) {
    return (
      <div className="dev-junk-roots">
        <span className="dev-junk-roots-label">还没有设置项目文件夹，这次只扫描开发工具的缓存。</span>
        <Link to="/settings" className="link-button">
          去设置
        </Link>
      </div>
    );
  }
  return (
    <div className="dev-junk-roots">
      <span className="dev-junk-roots-label">会在这些文件夹里查找项目：</span>
      <ul className="dev-junk-roots-list" aria-label="项目文件夹">
        {roots.map((r) => (
          <li key={r} title={r}>
            {truncateMiddle(r, 64)}
          </li>
        ))}
      </ul>
      <Link to="/settings" className="link-button">
        更改文件夹
      </Link>
    </div>
  );
}

interface SectionProps {
  title: string;
  categories: readonly Category[];
  selection: Selection;
  setSelection: (next: Selection) => void;
}

/** Section is one of the two groups of results, with its heading and its own CategoryList (a region named after the heading, whose 全选 and 全不选 act on the section only). */
function Section({ title, categories, selection, setSelection }: SectionProps) {
  return (
    <div className="dev-junk-section">
      <h2 className="dev-junk-section-title">{title}</h2>
      <CategoryList
        label={title}
        categories={categories}
        selection={selection}
        onSelectionChange={(next) => setSelection(withinGroup(selection, categories, next))}
      />
    </div>
  );
}

/** DevJunkPage is the 开发者垃圾 page: before a scan the folders it will search; while scanning the progress; then the tool caches and the projects' build output as two lists, the 清理 button, its confirmation (commands, permanent deletions and the Trash counted apart), the cleaning overlay and the done card. */
export function DevJunkPage() {
  const flow = useCleaner("dev-junk");
  const settings = useSettings();
  const roots = settings.data?.cleanup.devProjectRoots;

  const categories = flow.scan?.categories ?? [];
  const { tools, projects } = splitGroups(categories);
  const plan = cleanPlan(categories, flow.selection);
  const confirm = confirmationLines(plan);

  return (
    <>
      <Page>
        <PageHeader {...PAGE}>
          {flow.phase === "results" && (
            <button type="button" className="ghost-button" onClick={flow.startScan}>
              重新扫描
            </button>
          )}
        </PageHeader>

        {flow.phase === "idle" && (
          <ScanIdle
            tone={PAGE.tone}
            icon={PAGE.icon}
            title="查找开发者垃圾"
            description="扫描开发工具的缓存，以及项目文件夹里可以重新生成的构建产物。扫描只读取，不会删除任何东西。"
            onStart={flow.startScan}
            error={flow.scanError}
          >
            <ProjectRoots roots={roots} />
          </ScanIdle>
        )}

        {flow.phase === "scanning" && <ScanProgress progress={flow.progress} onCancel={flow.cancel} />}

        {flow.phase !== "idle" && flow.phase !== "scanning" && flow.scan && (
          <>
            {flow.scan.denied > 0 && (
              <p className="dev-junk-warning">{`有 ${flow.scan.denied} 个位置没有权限读取，结果可能不完整。`}</p>
            )}
            {categories.length === 0 ? (
              <EmptyState
                title="没有找到开发者垃圾"
                description="开发工具的缓存和项目里的构建产物都已经很干净了。"
                tone={PAGE.tone}
                icon={PAGE.icon}
              />
            ) : (
              <>
                {tools.length > 0 && (
                  <Section
                    title="工具缓存"
                    categories={tools}
                    selection={flow.selection}
                    setSelection={flow.setSelection}
                  />
                )}
                {projects.length > 0 ? (
                  <Section
                    title="项目构建产物"
                    categories={projects}
                    selection={flow.selection}
                    setSelection={flow.setSelection}
                  />
                ) : (
                  <p className="dev-junk-notice">
                    {roots && roots.length === 0
                      ? "没有设置项目文件夹，所以没有查找项目的构建产物。"
                      : "项目文件夹里没有找到构建产物。"}
                  </p>
                )}
                <ActionBar
                  label="清理"
                  onClick={flow.requestClean}
                  disabled={flow.totals.count === 0}
                  start={{ label: "已选择", value: `${flow.totals.count} 项` }}
                  end={{ label: "已选大小", value: formatBytes(flow.totals.bytes) }}
                />
              </>
            )}
          </>
        )}
      </Page>

      {flow.confirming && (
        <ConfirmDialog
          title="确认清理"
          message={
            <>
              <p className="dev-junk-confirm-summary">{confirm.summary}</p>
              {confirm.lines.length > 0 && (
                <ul className="dev-junk-confirm-lines">
                  {confirm.lines.map((l) => (
                    <li key={l}>{l}</li>
                  ))}
                </ul>
              )}
            </>
          }
          confirmLabel="清理"
          tone={confirm.danger ? "danger" : "default"}
          onConfirm={flow.confirmClean}
          onCancel={flow.dismissConfirm}
        />
      )}
      {flow.phase === "cleaning" && (
        <CleaningOverlay
          progress={flow.progress}
          sub={
            plan.commands.length > 0
              ? `共 ${plan.count} 项；停止后，正在运行的命令会先完成`
              : `共 ${plan.count} 项`
          }
          onCancel={flow.cancel}
        />
      )}
      {flow.phase === "done" && (
        <CleanDoneDialog
          outcome={flow.result ? { result: flow.result } : { error: flow.cleanError ?? "清理没有进行" }}
          onClose={flow.finish}
        />
      )}
    </>
  );
}
