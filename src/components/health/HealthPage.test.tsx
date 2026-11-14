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

import type { Alert, AppSettings, Health, Settings, Sysinfo, SystemInfo, Vitals } from "../../mygo";
import { ShellBlockProvider } from "../ShellBlock";
import { HealthPage } from "./HealthPage";

// Only the bindings are faked: the generated client (../../mygo), the runtime's Channel, which needs a MyGo window, and the uPlot chart, which needs a canvas.
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
    Sysinfo: {
      info: vi.fn<typeof Sysinfo.info>(),
      watch: vi.fn<typeof Sysinfo.watch>(),
    },
    Health: {
      alerts: vi.fn<typeof Health.alerts>(),
      openSystemMonitor: vi.fn<typeof Health.openSystemMonitor>(),
    },
    Settings: {
      get: vi.fn<typeof Settings.get>(),
    },
    alertListeners: new Set<(alerts: Alert[]) => void>(),
    charts: [] as { series: number; points: number }[],
    isMyGo: vi.fn<() => boolean>(),
    runtime: vi.fn<typeof import("mygo-runtime").runtime>(),
  };
});

vi.mock("../../mygo", () => ({
  Sysinfo: fakes.Sysinfo,
  Health: fakes.Health,
  Settings: fakes.Settings,
  events: {
    healthAlerts: {
      on: (listener: (alerts: Alert[]) => void) => {
        fakes.alertListeners.add(listener);
        return () => fakes.alertListeners.delete(listener);
      },
    },
    settingsChanged: { on: () => () => {} },
  },
}));

vi.mock("mygo-runtime", async (importOriginal) => ({
  ...(await importOriginal<typeof import("mygo-runtime")>()),
  Channel: fakes.FakeChannel,
  isMyGo: fakes.isMyGo,
  runtime: fakes.runtime,
}));

vi.mock("./Sparkline", async () => {
  const { createElement } = await import("react");
  return {
    Sparkline: ({ data }: { data: unknown[][] }) => {
      const points = data[0]?.length ?? 0;
      fakes.charts.push({ series: data.length - 1, points });
      return createElement("div", { "data-testid": "sparkline", "data-points": points });
    },
  };
});

type FakeChannel = InstanceType<typeof fakes.FakeChannel<Vitals>>;

const INFO: SystemInfo = {
  osName: "macOS",
  osVersion: "26.1",
  osBuild: "25B78",
  model: "Mac16,8",
  cpuModel: "Apple M4 Max",
  physicalCores: 12,
  logicalCores: 12,
  performanceCores: 8,
  efficiencyCores: 4,
  bootTime: "2026-11-11T09:00:00Z",
};

function settings(health: Partial<AppSettings["health"]> = {}): AppSettings {
  return {
    version: 1,
    health: { cpuThreshold: 40, sustainMinutes: 10, enabled: true, ...health },
    cleanup: {
      exclusions: [],
      devProjectRoots: [],
      devArtifactMinAgeDays: 14,
      installerRoots: [],
      largeFileMinMB: 500,
    },
    history: { retentionDays: 90 },
    general: { launchAtLogin: false, confirmBeforeClean: true },
  };
}

function sample(at: string, extra: Partial<Vitals> = {}): Vitals {
  return {
    at,
    cpu: { percent: 23.4, perCore: [10, 40, 5, 38.6], load: { one: 2.31, five: 1.98, fifteen: 1.75 } },
    memory: {
      total: 36e9,
      used: 24.5e9,
      percent: 68,
      swap: { total: 2e9, used: 1.2e9 },
      pressure: "warning",
    },
    disk: { total: 494e9, used: 249e9, free: 245e9 },
    diskIo: { readPerSec: 1.2e6, writePerSec: 300e3 },
    network: { downloadPerSec: 2.5e6, uploadPerSec: 120e3 },
    battery: {
      present: true,
      percent: 87,
      state: "charging",
      minutesRemaining: 38,
      cycleCount: 213,
      healthPercent: 89.2,
    },
    processes: {
      topCpu: [
        { pid: 812, name: "Xcode", cpuPercent: 148.2, memory: 3.1e9 },
        { pid: 90, name: "Safari", cpuPercent: 4.26, memory: 1e9 },
      ],
      topMemory: [
        { pid: 812, name: "Xcode", cpuPercent: 148.2, memory: 3.1e9 },
        { pid: 300, name: "launchd-helper", cpuPercent: null, memory: 2e9 },
      ],
    },
    ...extra,
  };
}

let channels: FakeChannel[] = [];

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const rootRoute = createRootRoute({ component: Outlet });
  const health = createRoute({ getParentRoute: () => rootRoute, path: "/health", component: HealthPage });
  const settingsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/settings",
    component: () => <h1>设置页</h1>,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([health, settingsRoute]),
    history: createMemoryHistory({ initialEntries: ["/health"] }),
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <ShellBlockProvider>
        <RouterProvider router={router} />
      </ShellBlockProvider>
    </QueryClientProvider>,
  );
}

/** send delivers samples through the page's latest channel. */
function send(...samples: Vitals[]) {
  const channel = channels.at(-1);
  if (!channel) throw new Error("the page did not call Sysinfo.watch");
  act(() => {
    for (const s of samples) channel.send(s);
  });
}

function card(title: string) {
  return screen.getByRole("region", { name: title });
}

beforeEach(() => {
  channels = [];
  fakes.charts.length = 0;
  fakes.isMyGo.mockReturnValue(false);
  fakes.Sysinfo.info.mockResolvedValue(INFO);
  // Watch runs until the page closes the channel.
  fakes.Sysinfo.watch.mockImplementation((ch) => {
    channels.push(ch as unknown as FakeChannel);
    return new Promise(() => {});
  });
  fakes.Health.alerts.mockResolvedValue([]);
  fakes.Health.openSystemMonitor.mockResolvedValue(undefined);
  fakes.Settings.get.mockResolvedValue(settings());
});

afterEach(() => {
  vi.resetAllMocks();
  fakes.alertListeners.clear();
});

describe("HealthPage overview", () => {
  it("says it is reading until the first sample arrives", async () => {
    renderPage();
    expect((await screen.findByRole("heading", { level: 1 })).textContent).toBe("健康监测");
    expect(screen.getByText("实时查看 CPU、内存、磁盘、网络和电池，找出最占资源的进程")).toBeTruthy();
    expect(within(card("CPU")).getByText("正在读取…")).toBeTruthy();
    expect(within(card("网络")).getByText("正在读取…")).toBeTruthy();
    expect(screen.queryByRole("region", { name: "电池" })).toBeNull();
    expect(fakes.Sysinfo.watch).toHaveBeenCalledTimes(1);
  });

  it("shows every card from a sample", async () => {
    renderPage();
    await screen.findByRole("heading", { name: "健康监测" });
    send(sample("2026-11-14T09:00:00Z"));

    const cpu = card("CPU");
    expect(within(cpu).getByText("23%")).toBeTruthy();
    expect(within(cpu).getByText("负载 2.31 / 1.98 / 1.75")).toBeTruthy();
    expect(within(cpu).getByRole("img", { name: "各核心占用：10%、40%、5%、39%" })).toBeTruthy();
    expect(await within(cpu).findByText("Apple M4 Max · 12 核（8 性能核 + 4 能效核）")).toBeTruthy();

    const memory = card("内存");
    expect(within(memory).getByText("68%")).toBeTruthy();
    expect(within(memory).getByText("已用 24.5 GB，共 36 GB")).toBeTruthy();
    expect(within(memory).getByText("已用 1.2 GB")).toBeTruthy();
    expect(within(memory).getByText("偏高")).toBeTruthy();
    expect(within(memory).getByRole("meter", { name: "内存已用" }).getAttribute("aria-valuenow")).toBe("68");

    const disk = card("磁盘");
    expect(within(disk).getByText("245 GB 可用")).toBeTruthy();
    expect(within(disk).getByText("读取 1.2 MB/s")).toBeTruthy();
    expect(within(disk).getByText("写入 300 KB/s")).toBeTruthy();

    const network = card("网络");
    expect(within(network).getByText("↓ 2.5 MB/s")).toBeTruthy();
    expect(within(network).getByText("↑ 120 KB/s")).toBeTruthy();

    const battery = card("电池");
    expect(within(battery).getByText("87%")).toBeTruthy();
    expect(within(battery).getByText("正在充电 · 充满还需 38 分钟")).toBeTruthy();
    expect(within(battery).getByText("213 次")).toBeTruthy();
    expect(within(battery).getByText("89%（相对设计容量）")).toBeTruthy();

    const system = card("系统");
    expect(within(system).getByText("macOS 26.1")).toBeTruthy();
    expect(within(system).getByText("25B78")).toBeTruthy();
    expect(within(system).getByText("Mac16,8")).toBeTruthy();
    // Uptime is measured to the sample's time: 3 days from boot.
    expect(within(system).getByText("3 天")).toBeTruthy();
  });

  it("draws the history Go sends first, and then every new sample", async () => {
    renderPage();
    await screen.findByRole("heading", { name: "健康监测" });
    send(sample("2026-11-14T09:00:00Z"), sample("2026-11-14T09:00:02Z"), sample("2026-11-14T09:00:04Z"));
    const points = () => screen.getAllByTestId("sparkline").map((el) => el.getAttribute("data-points"));
    expect(points()).toEqual(["3", "3", "3", "3"]);
    send(sample("2026-11-14T09:00:06Z"));
    expect(points()).toEqual(["4", "4", "4", "4"]);
    // Disk and network draw two series each.
    expect(fakes.charts.slice(-4).map((c) => c.series)).toEqual([1, 1, 2, 2]);
  });

  it("shows 无法读取 for a metric that failed and keeps the others", async () => {
    renderPage();
    await screen.findByRole("heading", { name: "健康监测" });
    send(sample("2026-11-14T09:00:00Z", { network: null, disk: null, battery: null, processes: null }));
    expect(within(card("网络")).getByText("无法读取")).toBeTruthy();
    expect(within(card("磁盘")).getByText("无法读取")).toBeTruthy();
    // The battery could not be read: its card says so rather than disappearing.
    expect(within(card("电池")).getByText("无法读取")).toBeTruthy();
    expect(within(card("CPU")).getByText("23%")).toBeTruthy();
    expect(within(screen.getByRole("region", { name: "CPU 占用最高" })).getByText("无法读取")).toBeTruthy();
  });

  it("has no battery card on a machine without one", async () => {
    renderPage();
    await screen.findByRole("heading", { name: "健康监测" });
    send(sample("2026-11-14T09:00:00Z", { battery: { present: false, percent: 0, state: "unknown" } }));
    expect(screen.queryByRole("region", { name: "电池" })).toBeNull();
  });

  it("closes the channel when the page goes away, which stops sampling", async () => {
    const { unmount } = renderPage();
    await screen.findByRole("heading", { name: "健康监测" });
    const channel = channels[0];
    expect(channel?.closed).toBe(false);
    unmount();
    expect(channel?.closed).toBe(true);
  });

  it("offers a retry when watching fails", async () => {
    const user = userEvent.setup();
    fakes.Sysinfo.watch.mockRejectedValueOnce(new Error("boom"));
    renderPage();
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("无法读取系统状态");
    await user.click(within(alert).getByRole("button", { name: "重试" }));
    expect(fakes.Sysinfo.watch).toHaveBeenCalledTimes(2);
    await waitFor(() => expect(screen.queryByText("无法读取系统状态")).toBeNull());
    send(sample("2026-11-14T09:00:00Z"));
    expect(within(card("CPU")).getByText("23%")).toBeTruthy();
  });
});

describe("HealthPage processes", () => {
  it("lists the top processes by CPU and memory", async () => {
    renderPage();
    await screen.findByRole("heading", { name: "健康监测" });
    send(sample("2026-11-14T09:00:00Z"));
    const byCPU = within(screen.getByRole("region", { name: "CPU 占用最高" }));
    const rows = byCPU.getAllByRole("row").slice(1);
    expect(rows.map((r) => r.textContent)).toEqual(["XcodePID 812148%3.1 GB", "SafariPID 904.3%1 GB"]);
    const byMemory = within(screen.getByRole("region", { name: "内存占用最高" }));
    expect(byMemory.getByRole("rowheader", { name: /launchd-helper/ }).closest("tr")?.textContent).toBe(
      "launchd-helperPID 300—2 GB",
    );
    expect(screen.getByText(/CPU 按单个处理器核心计算，100% 即占满一个核心，与活动监视器一致/)).toBeTruthy();
    expect(screen.getByText(/Spruce 只显示进程，不会结束进程/)).toBeTruthy();
  });

  it("opens Activity Monitor and never offers to end a process", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "健康监测" });
    send(sample("2026-11-14T09:00:00Z"));
    await user.click(screen.getByRole("button", { name: "在活动监视器中打开" }));
    expect(fakes.Health.openSystemMonitor).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("button", { name: /结束|退出|强制/ })).toBeNull();
  });

  it("uses Task Manager's name and unit note on Windows", async () => {
    fakes.isMyGo.mockReturnValue(true);
    fakes.runtime.mockReturnValue({ platform: "win32" } as ReturnType<typeof import("mygo-runtime").runtime>);
    fakes.Health.openSystemMonitor.mockRejectedValue(new Error("boom"));
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { name: "健康监测" });
    send(
      sample("2026-11-14T09:00:00Z", {
        memory: { total: 36e9, used: 24.5e9, percent: 68, swap: { total: 2e9, used: 1.2e9 } },
      }),
    );
    expect(screen.getByText(/任务管理器按全部核心显示，数值会更低/)).toBeTruthy();
    expect(within(card("内存")).getByText("页面文件")).toBeTruthy();
    expect(within(card("内存")).queryByText("内存压力")).toBeNull();
    await user.click(screen.getByRole("button", { name: "在任务管理器中打开" }));
    expect(await screen.findByText("无法打开任务管理器")).toBeTruthy();
  });
});

describe("HealthPage alerts", () => {
  it("shows the sustained CPU alert as the 维护 page does", async () => {
    const alert: Alert = {
      pid: 812,
      name: "Xcode",
      cpuPercent: 148,
      since: "2026-11-14T08:48:00Z",
      duration: 12 * 60e9,
      threshold: 40,
    };
    fakes.Health.alerts.mockResolvedValue([alert]);
    renderPage();
    const banner = await screen.findByRole("region", { name: "CPU 占用提醒" });
    expect(within(banner).getByText("Xcode 已经持续占用 148% CPU 有 12 分钟")).toBeTruthy();
    act(() => {
      for (const listener of fakes.alertListeners) listener([]);
    });
    expect(screen.queryByRole("region", { name: "CPU 占用提醒" })).toBeNull();
  });

  it("states the alert settings and links to 设置", async () => {
    const user = userEvent.setup();
    renderPage();
    const settingsCard = await screen.findByRole("region", { name: "高 CPU 提醒设置" });
    expect(
      await within(settingsCard).findByText("高 CPU 提醒：单个进程持续 10 分钟占用超过 40% CPU 时提醒"),
    ).toBeTruthy();
    await user.click(within(settingsCard).getByRole("link", { name: "调整阈值" }));
    expect(await screen.findByRole("heading", { name: "设置页" })).toBeTruthy();
  });

  it("says when the alert is off", async () => {
    fakes.Settings.get.mockResolvedValue(settings({ enabled: false }));
    renderPage();
    const settingsCard = await screen.findByRole("region", { name: "高 CPU 提醒设置" });
    expect(await within(settingsCard).findByText("高 CPU 提醒已关闭")).toBeTruthy();
    expect(within(settingsCard).getByRole("link", { name: "去设置开启" }).getAttribute("href")).toBe(
      "/settings",
    );
  });
});
