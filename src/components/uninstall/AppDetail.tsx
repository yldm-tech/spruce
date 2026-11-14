import type { UninstallApp, UninstallPlan } from "../../mygo";
import { EmptyState } from "../common/EmptyState";
import { fileManagerName, formatBytes, trashName, truncateMiddle } from "../common/format";
import { Glyph } from "../common/Glyph";
import { ProgressRing } from "../common/ProgressRing";
import { AppIcon } from "./AppIcon";
import { LeftoverList } from "./LeftoverList";
import { sizeText, usageText, type AppDetails } from "./model";

/** The state of the leftovers lookup of the selected app (macOS). */
export interface PlanState {
  plan: UninstallPlan | undefined;
  loading: boolean;
  error: string | null;
  retry: () => void;
}

interface AppDetailProps {
  app: UninstallApp | null;
  details: AppDetails | undefined;
  measuring: boolean;
  windows: boolean;
  /** The leftovers lookup; undefined on Windows, where it happens after the uninstaller. */
  plan?: PlanState;
  checked: ReadonlySet<string>;
  onCheckedChange: (next: ReadonlySet<string>) => void;
  onReveal: (path: string) => void;
  /** Why 在访达中显示 failed, if it did. */
  revealError: string | null;
}

/** AppDetail is the right column: the picked app's facts and, on macOS, the files it would take along, with their checkboxes. */
export function AppDetail({
  app,
  details,
  measuring,
  windows,
  plan,
  checked,
  onCheckedChange,
  onReveal,
  revealError,
}: AppDetailProps) {
  if (!app) {
    return (
      <section aria-label="应用详情" className="uninstall-detail-card uninstall-detail-card--empty">
        <EmptyState
          title="选择一个应用"
          description="在左侧列表中选择要卸载的应用，这里会列出它和它留下的文件。"
          tone="blue"
          icon={
            <>
              <rect x="5" y="5" width="14" height="14" rx="4" />
              <path d="M9 12h6" />
            </>
          }
        />
      </section>
    );
  }
  const running = plan?.plan?.running ?? app.running;
  const used = usageText(app, details, windows);
  const identity = windows ? app.publisher : app.bundleId;
  return (
    <section aria-label={`${app.name} 的详情`} className="uninstall-detail-card">
      <div className="uninstall-detail-head">
        <AppIcon icon={details?.icon} size={56} />
        <div className="uninstall-detail-title">
          <h2>{app.name}</h2>
          <span>{[app.version && `版本 ${app.version}`, identity].filter(Boolean).join(" · ")}</span>
        </div>
        {app.appStore && <span className="uninstall-badge">App Store</span>}
        {running && <span className="uninstall-badge uninstall-badge--running">运行中</span>}
      </div>

      <dl className="uninstall-facts">
        <div>
          <dt>大小</dt>
          <dd>{sizeText(details, measuring)}</dd>
        </div>
        <div>
          <dt>{windows ? "安装时间" : "最近使用"}</dt>
          <dd>{used || "未知"}</dd>
        </div>
        {app.path && (
          <div className="uninstall-fact-path">
            <dt>位置</dt>
            <dd title={app.path}>
              <span>{truncateMiddle(app.path, 56)}</span>
              <button
                type="button"
                className="icon-button"
                aria-label={`在${fileManagerName()}中显示 ${app.name}`}
                title={`在${fileManagerName()}中显示`}
                onClick={() => onReveal(app.path ?? "")}
              >
                <Glyph size={16} color="currentColor">
                  <circle cx="11" cy="11" r="7" />
                  <path d="M20 20l-4-4" />
                </Glyph>
              </button>
            </dd>
          </div>
        )}
      </dl>
      {revealError && (
        <span role="alert" className="uninstall-note uninstall-note--warning">
          {revealError}
        </span>
      )}

      {windows ? (
        <WindowsNote app={app} />
      ) : (
        plan && (
          <MacLeftovers
            app={app}
            plan={plan}
            running={running}
            checked={checked}
            onCheckedChange={onCheckedChange}
            onReveal={onReveal}
          />
        )
      )}
    </section>
  );
}

function WindowsNote({ app }: { app: UninstallApp }) {
  return (
    <div className="uninstall-section">
      <span className="uninstall-section-title">卸载方式</span>
      <p className="uninstall-note">
        {`卸载时会打开「${app.name}」自带的卸载程序，请按它的提示完成。它结束后，Spruce 会查找它在你的用户文件夹里留下的文件，由你决定是否移到${trashName()}。`}
      </p>
    </div>
  );
}

interface MacLeftoversProps {
  app: UninstallApp;
  plan: PlanState;
  running: boolean;
  checked: ReadonlySet<string>;
  onCheckedChange: (next: ReadonlySet<string>) => void;
  onReveal: (path: string) => void;
}

function MacLeftovers({ app, plan, running, checked, onCheckedChange, onReveal }: MacLeftoversProps) {
  return (
    <div className="uninstall-section">
      {running && (
        <div role="alert" className="uninstall-running">
          <span>{`请先退出「${app.name}」再卸载`}</span>
          <button type="button" className="ghost-button" disabled={plan.loading} onClick={plan.retry}>
            重新检查
          </button>
        </div>
      )}
      <span className="uninstall-section-title">相关文件</span>
      {plan.loading && !plan.plan ? (
        <div className="uninstall-loading" aria-busy="true">
          <ProgressRing size={28} label="正在查找相关文件" />
          <span>正在查找相关文件…</span>
        </div>
      ) : plan.error ? (
        <div role="alert" className="uninstall-running">
          <span>{plan.error}</span>
          <button type="button" className="ghost-button" onClick={plan.retry}>
            重试
          </button>
        </div>
      ) : plan.plan && plan.plan.leftovers.length === 0 ? (
        <p className="uninstall-note">没有找到它留下的文件，卸载时只会移除应用本身。</p>
      ) : (
        plan.plan && (
          <LeftoverList
            leftovers={plan.plan.leftovers}
            checked={checked}
            onChange={onCheckedChange}
            onReveal={onReveal}
          />
        )
      )}
      {plan.plan?.sharedWith && (
        <p className="uninstall-note uninstall-note--warning">
          {`另有一份「${app.name}」（${truncateMiddle(plan.plan.sharedWith, 48)}）可能也在使用这些文件，所以默认都不勾选。`}
        </p>
      )}
      {(plan.plan?.denied ?? 0) > 0 && (
        <p className="uninstall-note">部分位置没有权限读取，列出的文件和大小可能不完整。</p>
      )}
      {plan.plan?.bundle && (
        <p className="uninstall-note">{`应用本身 ${formatBytes(plan.plan.bundle.size)}，会和勾选的文件一起移到${trashName()}。`}</p>
      )}
    </div>
  );
}
