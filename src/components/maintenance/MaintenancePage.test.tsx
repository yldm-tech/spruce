import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  Alert,
  Health,
  Maintenance,
  Permissions,
  PermissionState,
  ProgressEvent,
  Result,
  RunSummary,
  TaskView,
} from "../../mygo";
import { ShellBlockProvider, useShellBlocked } from "../ShellBlock";
import { MaintenancePage } from "./MaintenancePage";

// Only the bindings are faked: the generated client (../../mygo) and the runtime's Channel, which needs a MyGo window.
const fakes = vi.hoisted(() => {
  class FakeChannel<T> {
    onmessage: ((value: T) => void) | null;
    onclose: (() => void) | null = null;
    closed = false;
    constructor(onmessage?: (value: T) => void) {
      this.onmessage = onmessage ?? null;
    }
    close() {
      this.closed = true;
    }
    send(value: T) {
      this.onmessage?.(value);
    }
  }
  return {
    FakeChannel,
    Maintenance: {
      catalog: vi.fn<typeof Maintenance.catalog>(),
      run: vi.fn<typeof Maintenance.run>(),
    },
    Health: {
      alerts: vi.fn<typeof Health.alerts>(),
      openSystemMonitor: vi.fn<typeof Health.openSystemMonitor>(),
    },
    Permissions: {
      fullDiskAccess: vi.fn<typeof Permissions.fullDiskAccess>(),
      guideFullDiskAccess: vi.fn<typeof Permissions.guideFullDiskAccess>(),
    },
    alertListeners: new Set<(alerts: Alert[]) => void>(),
    isMyGo: vi.fn<() => boolean>(),
    runtime: vi.fn<typeof import("mygo-runtime").runtime>(),
  };
});

vi.mock("../../mygo", () => ({
  Maintenance: fakes.Maintenance,
  Health: fakes.Health,
  Permissions: fakes.Permissions,
  events: {
    healthAlerts: {
      on: (listener: (alerts: Alert[]) => void) => {
        fakes.alertListeners.add(listener);
        return () => fakes.alertListeners.delete(listener);
      },
    },
  },
}));

vi.mock("mygo-runtime", async (importOriginal) => ({
  ...(await importOriginal<typeof import("mygo-runtime")>()),
  Channel: fakes.FakeChannel,
  isMyGo: fakes.isMyGo,
  runtime: fakes.runtime,
}));

type FakeChannel = InstanceType<typeof fakes.FakeChannel<ProgressEvent>>;

const CATALOG: TaskView[] = [
  task("dns", "刷新 DNS 缓存", "network", true, { needsAdmin: true, estimatedSeconds: 5 }),
  task("spotlight", "校验 Spotlight 索引", "search", false, { needsAdmin: true, estimatedSeconds: 30 }),
  task("quicklook", "刷新 QuickLook 预览", "preview", true, { estimatedSeconds: 5 }),
  task("icons", "重建图标缓存", "icons", true, { needsAdmin: true, estimatedSeconds: 20 }),
  task("launchservices", "修复「打开方式」", "open-with", true, { estimatedSeconds: 30 }),
  task("databases", "优化应用数据库", "database", false, {
    hint: "需要先退出 Safari 和邮件",
    estimatedSeconds: 45,
  }),
];

function task(
  id: string,
  title: string,
  icon: TaskView["icon"],
  defaultOn: boolean,
  extra: Partial<TaskView> = {},
): TaskView {
  return {
    id,
    title,
    description: `${title}的说明`,
    icon,
    defaultOn,
    needsAdmin: false,
    estimatedSeconds: 10,
    ...extra,
  };
}

function result(
  taskId: string,
  outcome: Result["outcome"],
  message: string,
  extra: Partial<Result> = {},
): Result {
  return { taskId, outcome, message, duration: 1_000_000, ...extra };
}

function summaryOf(results: Result[], interrupted = false): RunSummary {
  const count = (o: Result["outcome"]) => results.filter((r) => r.outcome === o).length;
  return {
    runId: "20261114T010412Z-3f9a0c1d",
    startedAt: "2026-11-14T01:04:12Z",
    endedAt: "2026-11-14T01:04:40Z",
    results,
    applied: count("applied"),
    unchanged: count("unchanged"),
    skipped: count("skipped"),
    unavailable: count("unavailable"),
    failed: count("failed"),
    interrupted,
  };
}

/** deferred returns a promise and the function that resolves it, for runs the test finishes by hand. */
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (err: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

/** ShellProbe stands in for the sidebar: it shows whether the page asked the shell to be inert. */
function ShellProbe() {
  return <span data-shell-blocked={String(useShellBlocked())}>shell</span>;
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <ShellBlockProvider>
        <ShellProbe />
        <MaintenancePage />
      </ShellBlockProvider>
    </QueryClientProvider>,
  );
}

function shellBlocked() {
  return screen.getByText("shell").getAttribute("data-shell-blocked");
}

/** doneLines returns the lines of the done dialog, read through its aria-describedby (the big number, then the lines). */
function doneLines(dialog: HTMLElement): string[] {
  const [bigId, linesId, ...rest] = (dialog.getAttribute("aria-describedby") ?? "").split(" ");
  expect([bigId, rest]).toEqual([expect.any(String), []]);
  return (document.getElementById(linesId ?? "")?.textContent ?? "").split("\n");
}

function emitAlerts(alerts: Alert[]) {
  act(() => {
    for (const listener of fakes.alertListeners) listener(alerts);
  });
}

function selectedCount() {
  return screen.getByText("已选择").nextElementSibling?.textContent;
}

function estimate() {
  return screen.getByText("预计用时").nextElementSibling?.textContent;
}

beforeEach(() => {
  // Outside a MyGo window by default, as under the dev server: the macOS wording.
  fakes.isMyGo.mockReturnValue(false);
  fakes.Maintenance.catalog.mockResolvedValue(CATALOG);
  fakes.Health.alerts.mockResolvedValue([]);
  fakes.Health.openSystemMonitor.mockResolvedValue(undefined);
  fakes.Permissions.fullDiskAccess.mockResolvedValue("granted");
});

afterEach(() => {
  vi.resetAllMocks();
  fakes.alertListeners.clear();
});

describe("MaintenancePage idle", () => {
  it("renders one card per catalog task with its default switch state", async () => {
    renderPage();
    const switches = await screen.findAllByRole("switch");
    expect(switches).toHaveLength(6);
    expect(switches.map((s) => s.getAttribute("aria-labelledby") !== null)).toEqual(Array(6).fill(true));
    expect(screen.getByRole("switch", { name: "刷新 DNS 缓存" })).toHaveProperty("checked", true);
    expect(screen.getByRole("switch", { name: "校验 Spotlight 索引" })).toHaveProperty("checked", false);
    expect(screen.getByRole("switch", { name: "优化应用数据库" })).toHaveProperty("checked", false);
    // A task with a hint shows the hint instead of its description.
    expect(screen.getByText("需要先退出 Safari 和邮件")).toBeTruthy();
    expect(screen.queryByText("优化应用数据库的说明")).toBeNull();
    expect(screen.getByText("给系统做一次轻量保养，不会动你的文档和照片")).toBeTruthy();
    expect(selectedCount()).toBe("4 项");
    // 5 + 5 + 20 + 30 seconds.
    expect(estimate()).toBe("约 1 分钟");
  });

  it("counts the switches the user turns on and off", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("switch", { name: "校验 Spotlight 索引" }));
    expect(selectedCount()).toBe("5 项");
    await user.click(screen.getByRole("switch", { name: "修复「打开方式」" }));
    await user.click(screen.getByRole("switch", { name: "刷新 DNS 缓存" }));
    expect(selectedCount()).toBe("3 项");
    // 30 + 5 + 20 seconds.
    expect(estimate()).toBe("不到 1 分钟");
  });

  it("disables 运行 when nothing is selected", async () => {
    const user = userEvent.setup();
    renderPage();
    for (const s of await screen.findAllByRole("switch")) {
      if ((s as HTMLInputElement).checked) await user.click(s);
    }
    expect(selectedCount()).toBe("0 项");
    expect(estimate()).toBe("0 分钟");
    const run = screen.getByRole("button", { name: "运行" });
    expect(run).toHaveProperty("disabled", true);
    await user.click(run);
    expect(fakes.Maintenance.run).not.toHaveBeenCalled();
  });
});

describe("MaintenancePage run", () => {
  it("shows progress from the channel, then the summary with the skipped task, and resets on 完成", async () => {
    const user = userEvent.setup();
    const pending = deferred<RunSummary>();
    let channel: FakeChannel | undefined;
    fakes.Maintenance.run.mockImplementation((_ids, ch) => {
      channel = ch as unknown as FakeChannel;
      return pending.promise;
    });
    renderPage();
    await user.click(await screen.findByRole("switch", { name: "优化应用数据库" }));
    await user.click(screen.getByRole("button", { name: "运行" }));

    expect(fakes.Maintenance.run).toHaveBeenCalledWith(
      ["dns", "quicklook", "icons", "launchservices", "databases"],
      expect.any(fakes.FakeChannel),
    );
    const overlay = screen.getByRole("group", { name: "正在维护系统" });
    // The page under the overlay is inert, so focus moves to the overlay instead of falling to the body; the sidebar is inert too.
    expect(document.activeElement).toBe(overlay);
    expect(shellBlocked()).toBe("true");
    expect(within(overlay).getByText("正在维护系统…")).toBeTruthy();
    expect(within(overlay).getByText("共 5 项任务，不会动你的文档和照片")).toBeTruthy();
    expect(within(overlay).getByText("正在准备…")).toBeTruthy();
    expect(within(overlay).getByText("部分任务需要管理员权限，请在系统弹出的授权窗口中确认")).toBeTruthy();
    // The live region was mounted before the run, so its first text is announced.
    const status = screen.getByRole("status");
    expect(status.textContent).toBe("正在维护系统…，共 5 项任务");

    act(() => {
      channel?.send({ kind: "started", taskId: "dns", done: 0, total: 5 });
    });
    expect(within(overlay).getByText("刷新 DNS 缓存 · 已完成 0/5")).toBeTruthy();
    expect(status.textContent).toBe("刷新 DNS 缓存 · 已完成 0/5");
    act(() => {
      channel?.send({
        kind: "finished",
        taskId: "dns",
        result: result("dns", "applied", "已刷新 DNS 缓存"),
        done: 1,
        total: 5,
      });
      channel?.send({ kind: "started", taskId: "icons", done: 1, total: 5 });
      channel?.send({
        kind: "finished",
        taskId: "icons",
        result: result("icons", "applied", "已重建图标缓存"),
        done: 2,
        total: 5,
      });
      channel?.send({ kind: "started", taskId: "quicklook", done: 2, total: 5 });
    });
    expect(within(overlay).getByText("刷新 QuickLook 预览 · 已完成 2/5")).toBeTruthy();
    // Every admin task finished, so the prompt note is gone.
    expect(within(overlay).queryByText(/管理员权限/)).toBeNull();

    await act(async () => {
      pending.resolve(
        summaryOf([
          result("dns", "applied", "已刷新 DNS 缓存"),
          result("quicklook", "applied", "已刷新 QuickLook"),
          result("icons", "applied", "已重建图标缓存"),
          result("launchservices", "unchanged", "没有需要清理的登记"),
          result("databases", "skipped", "Safari 仍在运行，已跳过"),
        ]),
      );
    });

    const dialog = await screen.findByRole("dialog", { name: "维护完成" });
    expect(within(dialog).getByText("4 项")).toBeTruthy();
    // The skipped task first, then every completed task with its own message, in catalog order.
    expect(doneLines(dialog)).toEqual([
      "优化应用数据库：Safari 仍在运行，已跳过",
      "刷新 DNS 缓存：已刷新 DNS 缓存",
      "刷新 QuickLook 预览：已刷新 QuickLook",
      "重建图标缓存：已重建图标缓存",
      "修复「打开方式」：没有需要清理的登记",
    ]);
    expect(status.textContent).toBe("");
    // Full Disk Access is granted, so the skip was not about the permission.
    expect(within(dialog).queryByRole("button", { name: "去授权" })).toBeNull();
    const done = within(dialog).getByRole("button", { name: "完成" });
    expect(document.activeElement).toBe(done);
    expect(shellBlocked()).toBe("true");

    await user.click(done);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByRole("group", { name: "正在维护系统" })).toBeNull();
    expect(selectedCount()).toBe("5 项");
    // Focus goes back to 运行 instead of the body, and the sidebar is usable again.
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "运行" }));
    expect(shellBlocked()).toBe("false");
  });

  it("lists every completed task's own message and never claims 无需重启 on its own", async () => {
    const user = userEvent.setup();
    fakes.Maintenance.run.mockResolvedValue(
      summaryOf([
        result("dns", "applied", "已刷新 DNS 缓存"),
        result("quicklook", "applied", "已刷新 QuickLook"),
        result("icons", "applied", "已重建图标缓存并刷新程序坞；如果访达里仍有旧图标，注销并重新登录即可"),
        result("launchservices", "unchanged", "没有需要清理的登记"),
      ]),
    );
    renderPage();
    await user.click(await screen.findByRole("button", { name: "运行" }));
    const dialog = await screen.findByRole("dialog", { name: "维护完成" });
    expect(within(dialog).getByText("4 项")).toBeTruthy();
    expect(doneLines(dialog)).toContain(
      "重建图标缓存：已重建图标缓存并刷新程序坞；如果访达里仍有旧图标，注销并重新登录即可",
    );
    expect(within(dialog).queryByText(/无需重启/)).toBeNull();
  });

  it("puts a partly applied task first", async () => {
    const user = userEvent.setup();
    fakes.Maintenance.run.mockResolvedValue(
      summaryOf([
        result("dns", "applied", "已刷新 DNS 缓存"),
        result("icons", "applied", "已清除图标缓存，但未能重启图标服务和程序坞；注销并重新登录后生效", {
          partial: true,
        }),
      ]),
    );
    renderPage();
    await user.click(await screen.findByRole("button", { name: "运行" }));
    const dialog = await screen.findByRole("dialog", { name: "维护完成" });
    // A partial task still did work, so it counts.
    expect(within(dialog).getByText("2 项")).toBeTruthy();
    expect(doneLines(dialog)).toEqual([
      "重建图标缓存：已清除图标缓存，但未能重启图标服务和程序坞；注销并重新登录后生效",
      "刷新 DNS 缓存：已刷新 DNS 缓存",
    ]);
  });

  it("groups the admin tasks of a declined prompt into one line", async () => {
    const user = userEvent.setup();
    const cancelled = "已取消管理员授权，未执行";
    fakes.Maintenance.run.mockResolvedValue(
      summaryOf([
        result("dns", "skipped", cancelled),
        result("quicklook", "applied", "已刷新 QuickLook"),
        result("icons", "skipped", cancelled),
        result("launchservices", "failed", "lsregister 运行失败"),
      ]),
    );
    renderPage();
    await user.click(await screen.findByRole("button", { name: "运行" }));
    const dialog = await screen.findByRole("dialog", { name: "维护完成" });
    expect(within(dialog).getByText("1 项")).toBeTruthy();
    // One line per message, separated by the newline that white-space: pre-line shows.
    expect(within(dialog).getByText(/^刷新 DNS 缓存、重建图标缓存/).textContent).toBe(
      "刷新 DNS 缓存、重建图标缓存：已取消管理员授权，未执行\n修复「打开方式」：lsregister 运行失败\n刷新 QuickLook 预览：已刷新 QuickLook",
    );
  });

  it("explains a run that could not start", async () => {
    const user = userEvent.setup();
    // The services layer turns the engine's errors into sentences for this card.
    fakes.Maintenance.run.mockRejectedValue(new Error("已有维护正在进行，请等它结束后再试"));
    renderPage();
    await user.click(await screen.findByRole("button", { name: "运行" }));
    const dialog = await screen.findByRole("dialog", { name: "维护没有运行" });
    expect(within(dialog).getByText("已有维护正在进行，请等它结束后再试")).toBeTruthy();
  });
});

/** selectOnlyDatabases turns 优化应用数据库 on and every other switch off. */
async function selectOnlyDatabases(user: ReturnType<typeof userEvent.setup>) {
  for (const s of await screen.findAllByRole("switch")) {
    const on = (s as HTMLInputElement).checked;
    const isDatabases = s.getAttribute("aria-labelledby") === screen.getByText("优化应用数据库").id;
    if (on !== isDatabases) await user.click(s);
  }
}

describe("MaintenancePage Full Disk Access", () => {
  it("offers 去授权 on the database card while the permission is missing", async () => {
    const user = userEvent.setup();
    fakes.Permissions.fullDiskAccess.mockResolvedValue("denied");
    const guide = deferred<PermissionState>();
    fakes.Permissions.guideFullDiskAccess.mockReturnValue(guide.promise);
    renderPage();
    await user.click(await screen.findByRole("switch", { name: "优化应用数据库" }));
    const action = await screen.findByRole("button", { name: "去授权" });
    await user.click(action);
    expect(fakes.Permissions.guideFullDiskAccess).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: "等待授权…" })).toHaveProperty("disabled", true);
    await act(async () => {
      guide.resolve("granted");
    });
    await waitFor(() => expect(screen.queryByText(/完全磁盘访问权限/)).toBeNull());
  });

  it("does not ask for the permission while the database card is off", async () => {
    fakes.Permissions.fullDiskAccess.mockResolvedValue("denied");
    renderPage();
    await screen.findAllByRole("switch");
    await waitFor(() => expect(fakes.Permissions.fullDiskAccess).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: "去授权" })).toBeNull();
  });

  it("offers 去授权 in the summary when the database task was skipped for the permission", async () => {
    const user = userEvent.setup();
    fakes.Permissions.fullDiskAccess.mockResolvedValue("denied");
    fakes.Maintenance.run.mockResolvedValue(
      summaryOf([
        result("databases", "skipped", "需要为 Spruce 开启「完全磁盘访问权限」，开启后再运行一次", {
          reason: "fullDiskAccess",
        }),
      ]),
    );
    fakes.Permissions.guideFullDiskAccess.mockResolvedValue("granted");
    renderPage();
    await selectOnlyDatabases(user);
    await user.click(screen.getByRole("button", { name: "运行" }));
    const dialog = await screen.findByRole("dialog", { name: "维护未完成" });
    await user.click(await within(dialog).findByRole("button", { name: "去授权" }));
    expect(fakes.Permissions.guideFullDiskAccess).toHaveBeenCalledTimes(1);
    // Granted on this card, after the run: now running again helps.
    expect(await within(dialog).findByText("已获得完全磁盘访问权限，再运行一次即可优化数据库")).toBeTruthy();
  });

  it("does not blame the permission for a skip with another reason, even after a grant before the run", async () => {
    const user = userEvent.setup();
    fakes.Permissions.fullDiskAccess.mockResolvedValue("denied");
    fakes.Permissions.guideFullDiskAccess.mockResolvedValue("granted");
    fakes.Maintenance.run.mockResolvedValue(
      summaryOf([result("databases", "skipped", "Safari 仍在运行，已跳过；退出后再运行一次")]),
    );
    renderPage();
    await selectOnlyDatabases(user);
    await user.click(await screen.findByRole("button", { name: "去授权" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "去授权" })).toBeNull());
    fakes.Permissions.fullDiskAccess.mockResolvedValue("granted");
    await user.click(screen.getByRole("button", { name: "运行" }));
    const dialog = await screen.findByRole("dialog", { name: "维护未完成" });
    expect(doneLines(dialog)).toEqual(["优化应用数据库：Safari 仍在运行，已跳过；退出后再运行一次"]);
    expect(within(dialog).queryByText(/完全磁盘访问权限/)).toBeNull();
    expect(within(dialog).queryByRole("button", { name: "去授权" })).toBeNull();
  });
});

describe("AlertBanner", () => {
  const xcode: Alert = {
    pid: 4242,
    name: "Xcode",
    cpuPercent: 48.3,
    since: "2026-11-14T00:52:00Z",
    duration: 12 * 60e9 + 20e9,
    threshold: 40,
  };

  it("follows the health stream and opens the system monitor", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findAllByRole("switch");
    await waitFor(() => expect(fakes.alertListeners.size).toBe(1));
    expect(screen.queryByRole("region", { name: "CPU 占用提醒" })).toBeNull();

    emitAlerts([xcode]);
    const banner = screen.getByRole("region", { name: "CPU 占用提醒" });
    expect(within(banner).getByText("Xcode 已经持续占用 48% CPU 有 12 分钟")).toBeTruthy();
    expect(within(banner).getByText(/Spruce 不会结束进程，只提醒你。/)).toBeTruthy();
    await user.click(within(banner).getByRole("button", { name: "查看详情" }));
    expect(fakes.Health.openSystemMonitor).toHaveBeenCalledTimes(1);

    emitAlerts([]);
    expect(screen.queryByRole("region", { name: "CPU 占用提醒" })).toBeNull();
  });

  it("shows an alert that is already active when the page opens", async () => {
    fakes.Health.alerts.mockResolvedValue([xcode, { ...xcode, pid: 7, name: "node", cpuPercent: 41 }]);
    renderPage();
    const banner = await screen.findByRole("region", { name: "CPU 占用提醒" });
    expect(within(banner).getByText("Xcode 已经持续占用 48% CPU 有 12 分钟")).toBeTruthy();
    expect(within(banner).getByText(/另有 1 个进程也在高占用。/)).toBeTruthy();
  });
});

/** The Windows catalog's shape: five cards and no Full Disk Access task. Titles and switch defaults come from the Go catalog and are tested there. */
const WINDOWS_CATALOG: TaskView[] = [
  task("dns", "刷新 DNS 缓存", "network", true),
  task("search", "检查 Windows 搜索", "search", false, { needsAdmin: true }),
  task("thumbnails", "清除缩略图缓存", "preview", true),
  task("icons", "刷新图标缓存", "icons", true),
  task("associations", "清理失效的「打开方式」", "open-with", true),
];

describe("MaintenancePage on Windows", () => {
  const xcode: Alert = {
    pid: 4242,
    name: "devenv",
    cpuPercent: 48.3,
    since: "2026-11-14T00:52:00Z",
    duration: 12 * 60e9,
    threshold: 40,
  };

  beforeEach(() => {
    fakes.isMyGo.mockReturnValue(true);
    fakes.runtime.mockReturnValue({ platform: "win32", windowId: 1, version: "test" } as ReturnType<
      typeof fakes.runtime
    >);
    fakes.Maintenance.catalog.mockResolvedValue(WINDOWS_CATALOG);
    fakes.Permissions.fullDiskAccess.mockResolvedValue("notRequired");
  });

  it("renders the Windows cards without any Full Disk Access UI", async () => {
    renderPage();
    const switches = await screen.findAllByRole("switch");
    expect(switches).toHaveLength(5);
    // Every card draws its glyph.
    for (const s of switches) {
      expect(s.closest(".task-card")?.querySelector("svg")).toBeTruthy();
    }
    await waitFor(() => expect(fakes.Permissions.fullDiskAccess).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: "去授权" })).toBeNull();
    expect(screen.queryByText(/完全磁盘访问权限/)).toBeNull();
  });

  it("names Task Manager and explains its CPU unit in the banner", async () => {
    const user = userEvent.setup();
    fakes.Health.openSystemMonitor.mockRejectedValue(new Error("taskmgr failed"));
    renderPage();
    await screen.findAllByRole("switch");
    await waitFor(() => expect(fakes.alertListeners.size).toBe(1));
    emitAlerts([xcode]);
    const banner = screen.getByRole("region", { name: "CPU 占用提醒" });
    expect(within(banner).getByText(/可以在任务管理器里看看/)).toBeTruthy();
    expect(within(banner).getByText(/任务管理器按全部核心显示/)).toBeTruthy();
    await user.click(within(banner).getByRole("button", { name: "查看详情" }));
    expect(await within(banner).findByText("无法打开任务管理器")).toBeTruthy();
  });

  it("names Activity Monitor without a unit note in a macOS window", async () => {
    fakes.runtime.mockReturnValue({ platform: "darwin", windowId: 1, version: "test" } as ReturnType<
      typeof fakes.runtime
    >);
    renderPage();
    await screen.findAllByRole("switch");
    await waitFor(() => expect(fakes.alertListeners.size).toBe(1));
    emitAlerts([xcode]);
    const banner = screen.getByRole("region", { name: "CPU 占用提醒" });
    expect(within(banner).getByText(/可以在活动监视器里看看/)).toBeTruthy();
    expect(within(banner).queryByText(/按全部核心显示/)).toBeNull();
  });
});
