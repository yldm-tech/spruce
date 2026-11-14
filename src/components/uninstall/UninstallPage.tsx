import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Channel } from "mygo-runtime";
import { useEffect, useId, useMemo, useRef, useState } from "react";

import {
  Uninstall,
  type Progress,
  type UninstallApp,
  type UninstallerStatus,
  type UninstallPlan,
  type UninstallRequest,
} from "../../mygo";
import { isWindows } from "../../platform";
import { useBlockShell } from "../ShellBlock";
import { ActionBar } from "../common/ActionBar";
import { ConfirmDialog } from "../common/ConfirmDialog";
import { DoneDialog } from "../common/DoneDialog";
import { EmptyState } from "../common/EmptyState";
import { HISTORY_QUERY_KEY, trashSizeQueryOptions, useReveal } from "../common/files";
import { fileManagerName, formatBytes, trashName } from "../common/format";
import { GlassDialog } from "../common/GlassDialog";
import { InOverlayHost } from "../common/OverlayHost";
import { Page, PageHeader } from "../common/Page";
import { PAGES } from "../common/pages";
import { ProgressRing } from "../common/ProgressRing";
import { CleaningOverlay } from "../common/ScanState";
import { AppDetail } from "./AppDetail";
import { AppList } from "./AppList";
import { LeftoverList } from "./LeftoverList";
import {
  checkedTotals,
  confirmMessage,
  defaultChecked,
  removeSummary,
  uninstalledSummary,
  visibleApps,
  type DoneSummary,
  type SortKey,
} from "./model";
import { appsQueryOptions, planQueryOptions, useAppDetails } from "./queries";

import "./uninstall.css";

// A Go method's error arrives as a CallError, an Error whose message is the sentence to show.
function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** Where the page is in an uninstall. */
type Flow =
  | { step: "idle" }
  | { step: "confirm"; app: UninstallApp }
  | { step: "removing"; app: UninstallApp }
  | { step: "waiting"; app: UninstallApp; running: boolean }
  | { step: "notRemoved"; app: UninstallApp; waited: boolean; checking: boolean; error: string | null }
  | { step: "leftovers"; app: UninstallApp; plan: UninstallPlan; checked: ReadonlySet<string> }
  | { step: "done"; summary: DoneSummary };

const IDLE: Flow = { step: "idle" };

/** UninstallPage is the 应用卸载 page: the installed apps with their sizes, icons and last use, the picked app's details and leftovers, and the uninstall itself. On macOS the app and the checked leftovers go to the Trash; on Windows the program's own uninstaller runs, then the page offers what it left in the user's folders. */
export function UninstallPage() {
  const windows = isWindows();
  const queryClient = useQueryClient();
  const apps = useQuery(appsQueryOptions);
  const { details, measuring } = useAppDetails(apps.isSuccess ? apps.dataUpdatedAt : 0);
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<SortKey>("name");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [flow, setFlow] = useState<Flow>(IDLE);
  const [progress, setProgress] = useState<Progress | null>(null);
  const channel = useRef<{ close: () => void } | null>(null);
  const reveal = useReveal();

  const list = useMemo(() => apps.data ?? [], [apps.data]);
  const visible = useMemo(
    () => visibleApps(list, details, query, sort, windows),
    [list, details, query, sort, windows],
  );
  const app = list.find((a) => a.id === selectedId) ?? null;

  const plan = useQuery({ ...planQueryOptions(app?.id ?? ""), enabled: !windows && app !== null });
  const planKey = plan.data ? `${plan.data.appId}:${plan.dataUpdatedAt}` : "";
  const [checkedState, setCheckedState] = useState<{ key: string; ids: ReadonlySet<string> }>({
    key: "",
    ids: new Set(),
  });
  const leftovers = plan.data?.leftovers ?? [];
  const checked = checkedState.key === planKey ? checkedState.ids : defaultChecked(leftovers);
  const totals = checkedTotals(leftovers, checked);
  const running = plan.data?.running ?? app?.running ?? false;

  useEffect(() => () => channel.current?.close(), []);

  const refreshAfter = () => {
    void queryClient.invalidateQueries({ queryKey: HISTORY_QUERY_KEY });
    void queryClient.invalidateQueries({ queryKey: trashSizeQueryOptions.queryKey });
  };
  const finish = (summary: DoneSummary) => setFlow({ step: "done", summary });

  const remove = async (target: UninstallApp, req: UninstallRequest) => {
    setProgress(null);
    setFlow({ step: "removing", app: target });
    const ch = new Channel<Progress>((p) => setProgress(p));
    channel.current = ch;
    try {
      finish(removeSummary(await Uninstall.remove(req, ch), windows));
    } catch (err) {
      finish({ title: "卸载没有进行", big: "未卸载", lines: [errorMessage(err)], succeeded: false });
    } finally {
      channel.current = null;
      refreshAfter();
    }
  };

  const offerLeftovers = async (target: UninstallApp) => {
    try {
      const found = await Uninstall.leftovers(target.id);
      if (found.leftovers.length === 0) finish(uninstalledSummary(target.name, 0));
      else setFlow({ step: "leftovers", app: target, plan: found, checked: defaultChecked(found.leftovers) });
    } catch (err) {
      const s = uninstalledSummary(target.name, 0);
      finish({ ...s, lines: [s.lines[0] ?? "", `没能查找它留下的文件：${errorMessage(err)}`] });
    }
  };

  const runUninstaller = async (target: UninstallApp) => {
    setFlow({ step: "waiting", app: target, running: false });
    const ch = new Channel<UninstallerStatus>(() =>
      setFlow((f) => (f.step === "waiting" ? { ...f, running: true } : f)),
    );
    channel.current = ch;
    try {
      const out = await Uninstall.runUninstaller(target.id, ch);
      channel.current = null;
      if (out.removed) await offerLeftovers(target);
      else setFlow({ step: "notRemoved", app: target, waited: out.waited, checking: false, error: null });
    } catch (err) {
      finish({ title: "卸载没有进行", big: "未卸载", lines: [errorMessage(err)], succeeded: false });
    } finally {
      channel.current = null;
      refreshAfter();
    }
  };

  const recheck = async (target: UninstallApp, waited: boolean) => {
    setFlow({ step: "notRemoved", app: target, waited, checking: true, error: null });
    try {
      const out = await Uninstall.checkRemoved(target.id);
      if (out.removed) await offerLeftovers(target);
      else
        setFlow({
          step: "notRemoved",
          app: target,
          waited,
          checking: false,
          error: `「${target.name}」仍在已安装的程序中`,
        });
    } catch (err) {
      setFlow({ step: "notRemoved", app: target, waited, checking: false, error: errorMessage(err) });
    }
  };

  const start = () => {
    if (!app) return;
    if (!windows && (!plan.data || running)) return;
    setFlow({ step: "confirm", app });
  };

  const confirm = (target: UninstallApp) => {
    if (windows) void runUninstaller(target);
    else void remove(target, { appId: target.id, leftoverIds: [...checked] });
  };

  const close = () => {
    setFlow(IDLE);
    setSelectedId(null);
    void queryClient.invalidateQueries({ queryKey: appsQueryOptions.queryKey });
  };

  const refresh = () => {
    void apps.refetch();
    if (app && !windows) void plan.refetch();
  };

  const bin = trashName();
  const actionDisabled = !app || (!windows && (!plan.data || plan.isFetching || running));
  const liveText =
    flow.step === "removing"
      ? `正在卸载「${flow.app.name}」…`
      : flow.step === "waiting"
        ? `正在等待「${flow.app.name}」的卸载程序…`
        : "";

  return (
    <>
      <Page className="uninstall-page">
        <PageHeader {...PAGES.uninstall}>
          <button type="button" className="ghost-button" disabled={apps.isFetching} onClick={refresh}>
            {apps.isFetching && apps.data ? "正在刷新…" : "刷新"}
          </button>
        </PageHeader>

        {apps.isPending ? (
          <section aria-label="已安装的应用" aria-busy="true" className="scan-state">
            <ProgressRing size={64} label="正在读取已安装的应用" />
            <span className="scan-state-sub">正在读取已安装的应用…</span>
          </section>
        ) : apps.isError ? (
          <EmptyState
            title="无法读取已安装的应用"
            description={errorMessage(apps.error)}
            tone="red"
            icon={<path d="M12 7v6M12 17v.5" />}
            action={
              <button type="button" className="ghost-button" onClick={() => void apps.refetch()}>
                重试
              </button>
            }
          />
        ) : list.length === 0 ? (
          <EmptyState
            title="没有找到可以卸载的应用"
            description={
              windows
                ? "已安装的程序列表里没有可以卸载的桌面程序。"
                : "应用程序文件夹里没有系统自带应用以外的应用。"
            }
            tone="blue"
            icon={PAGES.uninstall.icon}
          />
        ) : (
          <div className="uninstall-body">
            <AppList
              apps={visible}
              total={list.length}
              details={details}
              measuring={measuring}
              selectedId={selectedId}
              onSelect={setSelectedId}
              query={query}
              onQueryChange={setQuery}
              sort={sort}
              onSortChange={setSort}
              windows={windows}
            />
            <AppDetail
              app={app}
              details={app ? details.get(app.id) : undefined}
              measuring={measuring}
              windows={windows}
              plan={
                windows
                  ? undefined
                  : {
                      plan: plan.data,
                      loading: plan.isFetching,
                      error: plan.isError ? errorMessage(plan.error) : null,
                      retry: () => void plan.refetch(),
                    }
              }
              checked={checked}
              onCheckedChange={(ids) => setCheckedState({ key: planKey, ids })}
              onReveal={(path) => reveal.mutate(path)}
              revealError={
                reveal.isError
                  ? reveal.error instanceof Error
                    ? reveal.error.message
                    : `无法在${fileManagerName()}中显示`
                  : null
              }
            />
          </div>
        )}

        <p className="uninstall-footnote">
          {windows
            ? "这里列出通过安装程序安装的桌面程序；Microsoft Store 应用请在 Windows「设置 → 应用」中卸载。卸载时会运行程序自带的卸载程序，Spruce 只把它留下的文件移到回收站。"
            : `系统自带的应用不会列出。卸载会把应用和勾选的相关文件移到${bin}，可以随时放回。`}
        </p>

        <ActionBar
          label="卸载"
          disabled={actionDisabled}
          onClick={start}
          start={{
            label: "已选择",
            value: !app ? "0 项" : windows ? "1 个程序" : `${1 + totals.count} 项`,
          }}
          end={
            windows
              ? { label: "卸载方式", value: "自带卸载程序" }
              : { label: `将移到${bin}`, value: formatBytes((plan.data?.bundle?.size ?? 0) + totals.bytes) }
          }
        />
      </Page>

      <div role="status" aria-live="polite" className="visually-hidden">
        {liveText}
      </div>

      {flow.step === "confirm" && (
        <ConfirmDialog
          title={`卸载「${flow.app.name}」`}
          message={
            windows
              ? `将打开「${flow.app.name}」自带的卸载程序，请按它的提示完成卸载。它结束后，Spruce 会查找它留下的文件，由你决定是否移到${bin}。`
              : confirmMessage(flow.app.name, totals.count, (plan.data?.bundle?.size ?? 0) + totals.bytes)
          }
          confirmLabel={windows ? "打开卸载程序" : "卸载"}
          onConfirm={() => confirm(flow.app)}
          onCancel={() => setFlow(IDLE)}
        />
      )}
      {flow.step === "removing" && (
        <CleaningOverlay
          title={`正在卸载「${flow.app.name}」…`}
          sub={`移到${bin}，可以随时放回`}
          progress={progress}
          onCancel={() => channel.current?.close()}
        />
      )}
      {flow.step === "waiting" && (
        <WaitingOverlay app={flow.app} running={flow.running} onStop={() => channel.current?.close()} />
      )}
      {flow.step === "notRemoved" && (
        <DoneDialog
          title="卸载没有完成"
          big="未卸载"
          lines={[
            flow.waited
              ? `卸载程序已退出，但「${flow.app.name}」仍在已安装的程序中。`
              : `已不再等待卸载程序，「${flow.app.name}」仍在已安装的程序中。`,
            "如果卸载窗口还开着，请在里面完成卸载，再点「再次检查」。",
            ...(flow.error ? [flow.error] : []),
          ]}
          succeeded={false}
          onClose={close}
        >
          <button
            type="button"
            className="ghost-button"
            disabled={flow.checking}
            onClick={() => void recheck(flow.app, flow.waited)}
          >
            {flow.checking ? "正在检查…" : "再次检查"}
          </button>
        </DoneDialog>
      )}
      {flow.step === "leftovers" && (
        <LeftoversDialog
          app={flow.app}
          plan={flow.plan}
          checked={flow.checked}
          onChange={(ids) => setFlow({ ...flow, checked: ids })}
          onReveal={(path) => reveal.mutate(path)}
          onKeep={() => finish(uninstalledSummary(flow.app.name, flow.plan.leftovers.length))}
          onRemove={() => void remove(flow.app, { appId: flow.app.id, leftoverIds: [...flow.checked] })}
        />
      )}
      {flow.step === "done" && (
        <DoneDialog
          title={flow.summary.title}
          big={flow.summary.big}
          lines={flow.summary.lines}
          succeeded={flow.summary.succeeded}
          onClose={close}
        />
      )}
    </>
  );
}

/** WaitingOverlay covers the page while a Windows uninstaller runs. 不再等待 stops the wait, never the uninstaller. */
function WaitingOverlay({
  app,
  running,
  onStop,
}: {
  app: UninstallApp;
  running: boolean;
  onStop: () => void;
}) {
  useBlockShell(true);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    ref.current?.focus();
  }, []);
  const title = running ? "正在等待卸载程序…" : "正在打开卸载程序…";
  return (
    <InOverlayHost>
      <div ref={ref} tabIndex={-1} role="group" aria-label={title} className="overlay overlay--run">
        <ProgressRing label={title} />
        <span className="overlay-title">{title}</span>
        <span className="overlay-sub">{`请在「${app.name}」的卸载窗口中完成操作`}</span>
        {!running && <span className="overlay-note">如果系统请求管理员授权，请在弹出的窗口中确认</span>}
        <button type="button" className="ghost-button" onClick={onStop}>
          不再等待
        </button>
      </div>
    </InOverlayHost>
  );
}

interface LeftoversDialogProps {
  app: UninstallApp;
  plan: UninstallPlan;
  checked: ReadonlySet<string>;
  onChange: (next: ReadonlySet<string>) => void;
  onReveal: (path: string) => void;
  onKeep: () => void;
  onRemove: () => void;
}

/** LeftoversDialog offers what a Windows program left in the user's folders once its uninstaller removed it. */
function LeftoversDialog({ app, plan, checked, onChange, onReveal, onKeep, onRemove }: LeftoversDialogProps) {
  const titleId = useId();
  const textId = useId();
  const totals = checkedTotals(plan.leftovers, checked);
  const bin = trashName();
  return (
    <GlassDialog
      labelledBy={titleId}
      describedBy={textId}
      onClose={onKeep}
      className="uninstall-leftovers-card"
    >
      <span id={titleId} className="done-title">
        {`「${app.name}」留下的文件`}
      </span>
      <span id={textId} className="confirm-message">
        {`卸载程序已移除「${app.name}」。下面是它在你的用户文件夹里留下的文件，勾选的会移到${bin}，可以随时放回。`}
      </span>
      <LeftoverList leftovers={plan.leftovers} checked={checked} onChange={onChange} onReveal={onReveal} />
      <div className="confirm-buttons">
        <button type="button" className="ghost-button" onClick={onKeep}>
          保留
        </button>
        <button type="button" className="done-button" disabled={totals.count === 0} onClick={onRemove}>
          {`移到${bin}（${formatBytes(totals.bytes)}）`}
        </button>
      </div>
    </GlassDialog>
  );
}
