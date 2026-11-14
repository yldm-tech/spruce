import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  Alert,
  AppSettings,
  Category,
  Cleanup,
  CleanResult,
  Files,
  Health,
  History,
  Permissions,
  Progress,
  Run,
  ScanResult,
  Settings,
  Space,
} from "../../mygo";
import { OverlayHost } from "../common/OverlayHost";
import { ShellBlockProvider } from "../ShellBlock";
import { SmartScanPage } from "./SmartScanPage";

// Only the bindings are faked: the generated client (../../mygo) and the runtime's Channel, which needs a MyGo window.
const fakes = vi.hoisted(() => {
  class FakeChannel<T> {
    onmessage: ((value: T) => void) | null;
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
    Cleanup: {
      scan: vi.fn<typeof Cleanup.scan>(),
      clean: vi.fn<typeof Cleanup.clean>(),
      available: vi.fn<typeof Cleanup.available>(),
    },
    Settings: { get: vi.fn<typeof Settings.get>() },
    Files: { reveal: vi.fn<typeof Files.reveal>(), trashSize: vi.fn<typeof Files.trashSize>() },
    History: { list: vi.fn<typeof History.list>() },
    Permissions: {
      fullDiskAccess: vi.fn<typeof Permissions.fullDiskAccess>(),
      guideFullDiskAccess: vi.fn<typeof Permissions.guideFullDiskAccess>(),
    },
    Space: { locations: vi.fn<typeof Space.locations>() },
    Health: {
      alerts: vi.fn<typeof Health.alerts>(),
      openSystemMonitor: vi.fn<typeof Health.openSystemMonitor>(),
    },
  };
});

vi.mock("../../mygo", () => ({
  Cleanup: fakes.Cleanup,
  Settings: fakes.Settings,
  Files: fakes.Files,
  History: fakes.History,
  Permissions: fakes.Permissions,
  Space: fakes.Space,
  Health: fakes.Health,
  events: {
    settingsChanged: { on: () => () => {} },
    healthAlerts: { on: () => () => {} },
    historyAppended: { on: () => () => {} },
  },
}));

vi.mock("mygo-runtime", async (importOriginal) => ({
  ...(await importOriginal<typeof import("mygo-runtime")>()),
  Channel: fakes.FakeChannel,
}));

type FakeChannel = InstanceType<typeof fakes.FakeChannel<Progress>>;

function settings(health = true): AppSettings {
  return {
    version: 1,
    health: { cpuThreshold: 40, sustainMinutes: 10, enabled: health },
    cleanup: {
      exclusions: [],
      devProjectRoots: [],
      devArtifactMinAgeDays: 30,
      installerRoots: [],
      largeFileMinMB: 500,
    },
    history: { retentionDays: 180 },
    general: { launchAtLogin: false, confirmBeforeClean: true },
  };
}

function category(
  source: string,
  id: string,
  title: string,
  deletion: Category["deletion"],
  selected: boolean,
  items: [string, number, boolean?][],
  extra: Partial<Category> = {},
): Category {
  return {
    id: `${source}:${id}`,
    title,
    description: `${title}的说明`,
    icon: "cache",
    kind: "cache",
    deletion,
    selected,
    source,
    size: items.reduce((sum, [, size]) => sum + size, 0),
    items: items.map(([name, size, sel]) => ({
      id: name,
      path: `/Users/me/${name}`,
      size,
      modTime: "2026-11-01T00:00:00Z",
      selected: sel,
    })),
    ...extra,
  };
}

const CATEGORIES: Category[] = [
  category("system-junk", "app-caches", "应用缓存", "permanent", true, [
    ["caches-a", 300_000_000],
    ["caches-b", 200_000_000],
  ]),
  category(
    "system-junk",
    "app-caches-running",
    "正在运行的应用的缓存",
    "permanent",
    false,
    [["running", 50_000_000]],
    {
      warning: "这些应用正在运行，建议先退出再清理",
    },
  ),
  category("dev-junk", "tool-npm", "npm 缓存", "permanent", true, [["npm", 100_000_000]], {
    command: "npm cache clean --force",
  }),
  category("dev-junk", "tool-xcode-device-support", "设备支持文件", "permanent", false, [
    ["ios16", 400_000_000, true],
    ["ios17", 600_000_000],
  ]),
];

function scanOf(categories: Category[], extra: Partial<ScanResult> = {}): ScanResult {
  return {
    categories,
    size: categories.reduce((sum, c) => sum + c.size, 0),
    items: categories.reduce((sum, c) => sum + c.items.length, 0),
    denied: 0,
    scannedAt: "2026-11-14T01:00:00Z",
    duration: 2e9,
    sources: [
      { id: "system-junk", status: "done", done: 0, total: 0, bytes: 550_000_000, items: 3, denied: 0 },
      { id: "dev-junk", status: "done", done: 0, total: 0, bytes: 1_100_000_000, items: 3, denied: 0 },
      {
        id: "installers",
        status: "failed",
        done: 0,
        total: 0,
        bytes: 0,
        items: 0,
        denied: 0,
        error: "需要「完全磁盘访问权限」才能查找",
      },
    ],
    ...extra,
  };
}

const RESULT: CleanResult = {
  freed: 900_000_000,
  trashed: 0,
  removed: 4,
  skipped: 0,
  failed: [],
  categories: [
    {
      categoryId: "system-junk:app-caches",
      title: "应用缓存",
      source: "system-junk",
      removed: 2,
      freed: 500_000_000,
      trashed: 0,
      failed: 0,
    },
    {
      categoryId: "dev-junk:tool-npm",
      title: "npm 缓存",
      source: "dev-junk",
      removed: 1,
      freed: 100_000_000,
      trashed: 0,
      failed: 0,
    },
  ],
  partial: false,
  interrupted: false,
  startedAt: "2026-11-14T01:00:00Z",
  duration: 1e9,
};

function run(kind: Run["kind"], endedAt: string): Run {
  return { id: `${kind}-${endedAt}`, kind, startedAt: endedAt, endedAt, results: [] };
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const rootRoute = createRootRoute({
    component: () => (
      <OverlayHost>
        <Outlet />
      </OverlayHost>
    ),
  });
  const stub = (path: string, name: string) =>
    createRoute({ getParentRoute: () => rootRoute, path, component: () => <h1>{name}</h1> });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/smart-scan", component: SmartScanPage }),
      stub("/system-junk", "系统垃圾页"),
      stub("/dev-junk", "开发者垃圾页"),
      stub("/installers", "安装包页"),
      stub("/maintenance", "维护页"),
      stub("/space", "空间透视页"),
      stub("/health", "健康监测页"),
      stub("/settings", "设置页"),
    ]),
    history: createMemoryHistory({ initialEntries: ["/smart-scan"] }),
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <ShellBlockProvider>
        <RouterProvider router={router} />
      </ShellBlockProvider>
    </QueryClientProvider>,
  );
}

function stat(label: string) {
  return screen.getByText(label).nextElementSibling?.textContent;
}

function card(name: string) {
  return screen.getByRole("region", { name });
}

beforeEach(() => {
  fakes.Settings.get.mockResolvedValue(settings());
  fakes.History.list.mockResolvedValue([]);
  fakes.Permissions.fullDiskAccess.mockResolvedValue("denied");
  fakes.Space.locations.mockResolvedValue({
    volumes: [{ path: "/", name: "Macintosh HD", total: 1000e9, free: 120e9, startup: true }],
    targets: [],
  });
  fakes.Health.alerts.mockResolvedValue([]);
  fakes.Cleanup.scan.mockResolvedValue(scanOf(CATEGORIES));
  fakes.Cleanup.clean.mockResolvedValue(RESULT);
});

afterEach(() => {
  vi.resetAllMocks();
});

describe("SmartScanPage idle", () => {
  it("offers the scan and the status cards", async () => {
    renderPage();
    expect((await screen.findByRole("heading", { level: 1 })).textContent).toBe("智能扫描");
    expect(screen.getByText("一次扫描系统垃圾、开发者垃圾和安装包，按推荐勾选可以放心清理的")).toBeTruthy();
    expect(screen.getByRole("button", { name: "扫描" })).toBeTruthy();
    expect(screen.getByText("一次检查系统垃圾、开发者垃圾和安装包")).toBeTruthy();

    expect(await within(card("磁盘空间")).findByText("可用 120 GB")).toBeTruthy();
    expect(within(card("磁盘空间")).getByText("Macintosh HD 共 1 TB")).toBeTruthy();
    expect(within(card("CPU 占用")).getByText("没有长时间高占用的进程")).toBeTruthy();
    expect(await within(card("完全磁盘访问权限")).findByText("未开启")).toBeTruthy();
    expect(within(card("完全磁盘访问权限")).getByRole("button", { name: "去授权" })).toBeTruthy();
    expect(await within(card("维护")).findByText("还没有做过维护")).toBeTruthy();
    expect(within(card("维护")).getByRole("link", { name: "去维护" }).getAttribute("href")).toBe(
      "/maintenance",
    );
  });

  it("shows a sustained CPU alert, a recent maintenance run and the granted permission", async () => {
    const alert: Alert = {
      pid: 1,
      name: "Xcode",
      cpuPercent: 180,
      since: "",
      duration: 12 * 60e9,
      threshold: 40,
    };
    fakes.Health.alerts.mockResolvedValue([alert]);
    fakes.Permissions.fullDiskAccess.mockResolvedValue("granted");
    fakes.History.list.mockResolvedValue([
      run("maintenance", new Date(Date.now() - 3 * 86400e3).toISOString()),
    ]);
    renderPage();
    await screen.findByRole("heading", { level: 1 });
    expect(await within(card("CPU 占用")).findByText("Xcode 已经持续占用 180% CPU 有 12 分钟")).toBeTruthy();
    expect(await within(card("完全磁盘访问权限")).findByText("已开启")).toBeTruthy();
    expect(within(card("完全磁盘访问权限")).queryByRole("button")).toBeNull();
    expect(await within(card("维护")).findByText(/^上次维护：3 ?天前$/)).toBeTruthy();
    expect(within(card("维护")).queryByRole("link", { name: "去维护" })).toBeNull();
  });

  it("offers 维护 when it last ran more than 30 days ago, and says when CPU alerts are off", async () => {
    fakes.Settings.get.mockResolvedValue(settings(false));
    fakes.History.list.mockResolvedValue([run("maintenance", "2026-01-01T00:00:00Z")]);
    renderPage();
    await screen.findByRole("heading", { level: 1 });
    expect(await within(card("维护")).findByRole("link", { name: "去维护" })).toBeTruthy();
    expect(await within(card("CPU 占用")).findByText("高 CPU 提醒已关闭")).toBeTruthy();
  });
});

describe("SmartScanPage scanning", () => {
  it("shows each source's progress and stops", async () => {
    const user = userEvent.setup();
    let channel: FakeChannel | undefined;
    fakes.Cleanup.scan.mockImplementation((_id, ch) => {
      channel = ch as unknown as FakeChannel;
      return new Promise(() => {});
    });
    renderPage();
    await user.click(await screen.findByRole("button", { name: "扫描" }));
    expect(fakes.Cleanup.scan).toHaveBeenCalledWith("smart-scan", expect.any(fakes.FakeChannel));
    expect(await screen.findAllByText("等待开始…")).toHaveLength(3);
    act(() =>
      channel?.send({
        phase: "scanning",
        done: 1500,
        total: 3000,
        bytes: 2_000_000,
        sources: [
          { id: "system-junk", status: "done", done: 5, total: 5, bytes: 2_000_000, items: 4, denied: 0 },
          { id: "dev-junk", status: "scanning", done: 1, total: 2, bytes: 0, items: 0, denied: 0 },
          {
            id: "installers",
            status: "failed",
            done: 0,
            total: 0,
            bytes: 0,
            items: 0,
            denied: 0,
            error: "需要权限",
          },
        ],
      }),
    );
    expect(await screen.findByText("完成，找到 4 项，共 2 MB")).toBeTruthy();
    expect(screen.getByText("正在扫描… 已找到 0 B")).toBeTruthy();
    expect(screen.getByText("需要权限")).toBeTruthy();
    expect(
      screen.getByRole("progressbar", { name: "开发者垃圾扫描进度" }).getAttribute("aria-valuenow"),
    ).toBe("50");
    expect(screen.getByRole("progressbar", { name: "智能扫描进度" }).getAttribute("aria-valuenow")).toBe(
      "50",
    );
    await user.click(screen.getByRole("button", { name: "停止" }));
    expect(channel?.closed).toBe(true);
  });
});

describe("SmartScanPage results", () => {
  it("totals what starts checked, sums up each source, and cleans through the same confirmation", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "扫描" }));
    const total = await screen.findByRole("region", { name: "扫描结果" });
    // 应用缓存 (500 MB), npm (100 MB) and the old device support version (400 MB) start checked.
    expect(within(total).getByText("1 GB")).toBeTruthy();
    expect(within(total).getByText(/已按推荐勾选 4 项/)).toBeTruthy();

    const system = card("系统垃圾");
    expect(within(system).getByText("550 MB")).toBeTruthy();
    expect(within(system).getByText("3 项，推荐清理 2 项（500 MB）")).toBeTruthy();
    expect(within(system).getByRole("link", { name: "查看详情" }).getAttribute("href")).toBe("/system-junk");
    expect(within(card("安装包")).getByText("需要「完全磁盘访问权限」才能查找")).toBeTruthy();

    const list = screen.getByRole("region", { name: "全部项目" });
    expect(within(list).getByRole("region", { name: "开发者垃圾的项目" })).toBeTruthy();
    expect(within(list).getByText("已选 1/2 项")).toBeTruthy();
    expect(stat("已选择")).toBe("4 项");

    // A source's 全不选 leaves the other source's checks.
    await user.click(
      within(within(list).getByRole("region", { name: "系统垃圾的项目" })).getByRole("button", {
        name: "全不选",
      }),
    );
    await waitFor(() => expect(stat("已选择")).toBe("2 项"));

    // Folding a source hides its list.
    const toggle = within(list).getByRole("button", { name: /^系统垃圾/ });
    await user.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(within(list).queryByRole("region", { name: "系统垃圾的项目" })).toBeNull();

    await user.click(screen.getByRole("button", { name: "清理" }));
    const dialog = await screen.findByRole("dialog", { name: "确认清理" });
    const commands = within(dialog).getByRole("region", { name: "运行清理命令" });
    expect(within(commands).getByText("npm cache clean --force")).toBeTruthy();
    expect(
      within(within(dialog).getByRole("region", { name: "永久删除" })).getByText("设备支持文件"),
    ).toBeTruthy();
    await user.click(within(dialog).getByRole("button", { name: "清理" }));

    const done = await screen.findByRole("dialog", { name: "清理完成" });
    expect(fakes.Cleanup.clean).toHaveBeenCalledWith(
      "smart-scan",
      { categoryIds: ["dev-junk:tool-npm", "dev-junk:tool-xcode-device-support"], itemIds: ["npm", "ios16"] },
      expect.any(fakes.FakeChannel),
    );
    await user.click(within(done).getByRole("button", { name: "完成" }));
    expect(await screen.findByRole("button", { name: "扫描" })).toBeTruthy();
  });

  it("links a source's card to its page", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "扫描" }));
    await user.click(
      within(await screen.findByRole("region", { name: "开发者垃圾" })).getByRole("link", {
        name: "查看详情",
      }),
    );
    expect(await screen.findByRole("heading", { name: "开发者垃圾页" })).toBeTruthy();
  });

  it("says when nothing was found and keeps the status cards", async () => {
    const user = userEvent.setup();
    fakes.Cleanup.scan.mockResolvedValue(scanOf([]));
    renderPage();
    await user.click(await screen.findByRole("button", { name: "扫描" }));
    expect(await screen.findByText("没有找到需要清理的内容")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "清理" })).toBeNull();
    expect(card("磁盘空间")).toBeTruthy();
  });

  it("shows a scan that failed and lets the user try again", async () => {
    const user = userEvent.setup();
    fakes.Cleanup.scan.mockRejectedValue(new Error("扫描没能完成，详情已记录到日志"));
    renderPage();
    await user.click(await screen.findByRole("button", { name: "扫描" }));
    expect((await screen.findByRole("alert")).textContent).toBe("扫描没能完成，详情已记录到日志");
    expect(screen.getByRole("button", { name: "扫描" })).toBeTruthy();
  });
});
