import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type { AppSettings, History, Maintenance, Run, Settings, TaskView } from "../../mygo";
import { HistoryPage } from "./HistoryPage";

const fakes = vi.hoisted(() => ({
  History: {
    list: vi.fn<typeof History.list>(),
    clear: vi.fn<typeof History.clear>(),
  },
  Maintenance: { catalog: vi.fn<typeof Maintenance.catalog>() },
  Settings: { get: vi.fn<typeof Settings.get>() },
  appended: new Set<(run: Run) => void>(),
}));

vi.mock("../../mygo", () => ({
  History: fakes.History,
  Maintenance: fakes.Maintenance,
  Settings: fakes.Settings,
  events: {
    historyAppended: {
      on: (fn: (run: Run) => void) => {
        fakes.appended.add(fn);
        return () => fakes.appended.delete(fn);
      },
    },
    settingsChanged: { on: () => () => {} },
  },
}));

const CATALOG: TaskView[] = [
  { id: "dns", title: "刷新 DNS 缓存", description: "", icon: "network", defaultOn: true, needsAdmin: true, estimatedSeconds: 5 },
  { id: "databases", title: "优化应用数据库", description: "", icon: "database", defaultOn: false, needsAdmin: false, estimatedSeconds: 45 },
];

const SETTINGS: AppSettings = {
  version: 1,
  health: { cpuThreshold: 40, sustainMinutes: 10, enabled: true },
  cleanup: { exclusions: [], devProjectRoots: [], devArtifactMinAgeDays: 30, installerRoots: [], largeFileMinMB: 500 },
  history: { retentionDays: 180 },
  general: { launchAtLogin: false, confirmBeforeClean: true },
};

/** A local time today (or days ago), so grouping does not depend on the time zone the tests run in. */
function daysAgo(days: number, h: number, m: number, s = 0): Date {
  const now = new Date();
  return new Date(now.getFullYear(), now.getMonth(), now.getDate() - days, h, m, s);
}

function iso(d: Date, plusSeconds = 0): string {
  return new Date(d.getTime() + plusSeconds * 1000).toISOString();
}

const MAINTENANCE: Run = {
  id: "m1",
  kind: "maintenance",
  startedAt: iso(daysAgo(0, 10, 15, 3)),
  endedAt: iso(daysAgo(0, 10, 15, 3), 28),
  results: [
    { taskId: "dns", outcome: "applied", message: "已刷新 DNS 缓存", duration: 0 },
    { taskId: "databases", outcome: "skipped", message: "Safari 仍在运行，已跳过", duration: 0 },
  ],
};

const JUNK: Run = {
  id: "j1",
  kind: "systemJunk",
  startedAt: iso(daysAgo(0, 9, 0)),
  endedAt: iso(daysAgo(0, 9, 0), 5),
  results: [],
  summary: { freed: 1_200_000_000, items: 34, failed: 1 },
  details: ["系统缓存：已清理 34 项", "~/Library/Caches/x：文件正在使用，已跳过"],
};

const UNINSTALL: Run = {
  id: "u1",
  kind: "uninstall",
  startedAt: iso(daysAgo(1, 20, 45)),
  endedAt: iso(daysAgo(1, 20, 45), 3),
  results: [],
  summary: { freed: 300_000_000, trashed: 2_000_000_000, items: 3, failed: 0 },
  details: ["Xcode：已移到废纸篓 3 项"],
};

const RUNS = [MAINTENANCE, JUNK, UNINSTALL];

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <HistoryPage />
    </QueryClientProvider>,
  );
}

function row(id: string): HTMLElement {
  const el = document.querySelector<HTMLElement>(`[data-run-id="${id}"]`);
  if (!el) throw new Error(`no row ${id}`);
  return el;
}

beforeEach(() => {
  fakes.History.list.mockResolvedValue(RUNS);
  fakes.History.clear.mockResolvedValue(undefined);
  fakes.Maintenance.catalog.mockResolvedValue(CATALOG);
  fakes.Settings.get.mockResolvedValue(SETTINGS);
});

afterEach(() => {
  vi.resetAllMocks();
  fakes.appended.clear();
});

describe("HistoryPage list", () => {
  it("groups the runs by day with a title, summary and time per row, and totals the freed space", async () => {
    renderPage();
    const today = await screen.findByRole("region", { name: "今天" });
    const yesterday = screen.getByRole("region", { name: "昨天" });
    expect(fakes.History.list).toHaveBeenCalledWith(500);

    const todayRows = within(today).getAllByRole("listitem");
    expect(todayRows.map((r) => r.getAttribute("data-run-id"))).toEqual(["m1", "j1"]);
    expect(within(row("m1")).getByText("维护")).toBeTruthy();
    expect(within(row("m1")).getByText("完成 1 项，1 项未完成")).toBeTruthy();
    expect(within(row("m1")).getByText("10:15")).toBeTruthy();
    expect(within(row("j1")).getByText("系统垃圾")).toBeTruthy();
    expect(within(row("j1")).getByText("释放 1.2 GB，34 项，1 项未能处理")).toBeTruthy();
    expect(within(yesterday).getByText("应用卸载")).toBeTruthy();
    expect(within(row("u1")).getByText("释放 300 MB，移到废纸篓 2 GB，3 项")).toBeTruthy();

    // 1.2 GB + 300 MB freed; what went to the Trash is not counted.
    expect(screen.getByText("1.5 GB").parentElement?.textContent).toBe("累计释放 1.5 GB");
    expect(await screen.findByText("共 3 条记录，保留最近 180 天")).toBeTruthy();
  });

  it("expands a maintenance run into its tasks and a clean-up into its detail lines", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText("完成 1 项，1 项未完成");
    const head = within(row("m1")).getByRole("button");
    expect(head.getAttribute("aria-expanded")).toBe("false");
    await user.click(head);
    expect(head.getAttribute("aria-expanded")).toBe("true");
    const details = document.getElementById(head.getAttribute("aria-controls") ?? "") as HTMLElement;
    const tasks = within(details).getAllByRole("listitem").map((li) => li.textContent);
    // The skipped task first, as the 维护 done card lists it, then the completed one.
    expect(tasks).toEqual(["优化应用数据库已跳过Safari 仍在运行，已跳过", "刷新 DNS 缓存已完成已刷新 DNS 缓存"]);
    expect(within(details).getByText("开始 10:15:03 · 结束 10:15:31 · 用时 28 秒")).toBeTruthy();
    await user.click(head);
    expect(details.hidden).toBe(true);

    await user.click(within(row("j1")).getByRole("button"));
    expect(within(row("j1")).getByText("系统缓存：已清理 34 项")).toBeTruthy();
    expect(within(row("j1")).getByText("~/Library/Caches/x：文件正在使用，已跳过")).toBeTruthy();
    expect(within(row("j1")).getByText("开始 09:00:00 · 结束 09:00:05 · 用时 5 秒")).toBeTruthy();
  });

  it("filters by the kinds that occur", async () => {
    const user = userEvent.setup();
    renderPage();
    const group = await screen.findByRole("group", { name: "按类型筛选" });
    const chips = within(group).getAllByRole("button");
    expect(chips.map((c) => c.textContent)).toEqual(["全部", "系统垃圾", "应用卸载", "维护"]);
    expect(chips[0]?.getAttribute("aria-pressed")).toBe("true");

    await user.click(within(group).getByRole("button", { name: "应用卸载" }));
    expect(within(group).getByRole("button", { name: "应用卸载" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.queryByRole("region", { name: "今天" })).toBeNull();
    expect(document.querySelectorAll("[data-run-id]")).toHaveLength(1);
    // The total stays over every clean-up.
    expect(screen.getByText("1.5 GB")).toBeTruthy();

    await user.click(within(group).getByRole("button", { name: "全部" }));
    expect(document.querySelectorAll("[data-run-id]")).toHaveLength(3);
  });

  it("refreshes when a run is recorded anywhere", async () => {
    renderPage();
    await screen.findByRole("region", { name: "今天" });
    await waitFor(() => expect(fakes.appended.size).toBe(1));
    const fresh: Run = { ...JUNK, id: "j2", kind: "devJunk", startedAt: iso(daysAgo(0, 11, 0)), endedAt: iso(daysAgo(0, 11, 0), 1) };
    fakes.History.list.mockResolvedValue([fresh, ...RUNS]);
    act(() => {
      for (const fn of fakes.appended) fn(fresh);
    });
    await waitFor(() => expect(document.querySelector('[data-run-id="j2"]')).not.toBeNull());
    expect(within(row("j2")).getByText("开发者垃圾")).toBeTruthy();
    expect(fakes.History.list).toHaveBeenCalledTimes(2);
  });

  it("shows the empty state without the clear button when nothing is recorded", async () => {
    fakes.History.list.mockResolvedValue([]);
    renderPage();
    expect(await screen.findByText("还没有操作记录")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "清空历史" })).toBeNull();
    expect(screen.queryByRole("group", { name: "按类型筛选" })).toBeNull();
  });

  it("offers 重试 when the history cannot be read", async () => {
    const user = userEvent.setup();
    fakes.History.list.mockRejectedValueOnce(new Error("读取失败"));
    renderPage();
    expect((await screen.findByRole("alert")).textContent).toContain("无法读取操作历史：读取失败");
    await user.click(screen.getByRole("button", { name: "重试" }));
    expect(await screen.findByRole("region", { name: "今天" })).toBeTruthy();
  });
});

describe("HistoryPage clear", () => {
  it("asks first, clears and shows the empty state", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "清空历史" }));
    const dialog = screen.getByRole("dialog", { name: "清空操作历史？" });
    expect(dialog.textContent).toContain("将删除全部 3 条操作记录，无法恢复");
    // A danger dialog starts on 取消.
    expect(document.activeElement).toBe(within(dialog).getByRole("button", { name: "取消" }));
    await user.click(within(dialog).getByRole("button", { name: "取消" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(fakes.History.clear).not.toHaveBeenCalled();

    fakes.History.list.mockResolvedValue([]);
    await user.click(screen.getByRole("button", { name: "清空历史" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "清空" }));
    expect(await screen.findByText("还没有操作记录")).toBeTruthy();
    expect(fakes.History.clear).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps the dialog open with the reason when clearing fails", async () => {
    const user = userEvent.setup();
    fakes.History.clear.mockRejectedValue(new Error("无法清空操作历史"));
    renderPage();
    await user.click(await screen.findByRole("button", { name: "清空历史" }));
    const dialog = screen.getByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "清空" }));
    expect((await within(dialog).findByRole("alert")).textContent).toBe("无法清空操作历史");
    expect(document.querySelectorAll("[data-run-id]")).toHaveLength(3);
  });
});
