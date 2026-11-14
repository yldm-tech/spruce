import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  AppSettings,
  Category,
  Cleanup,
  CleanResult,
  Files,
  History,
  Progress,
  Run,
  ScanResult,
  Settings,
} from "../../mygo";
import { ShellBlockProvider, useShellBlocked } from "../ShellBlock";
import { SystemJunkPage } from "./SystemJunkPage";

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
      this.onclose?.();
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
    Files: {
      reveal: vi.fn<typeof Files.reveal>(),
      trashSize: vi.fn<typeof Files.trashSize>(),
      emptyTrash: vi.fn<typeof Files.emptyTrash>(),
    },
    History: { list: vi.fn<typeof History.list>(), clear: vi.fn<typeof History.clear>() },
    isMyGo: vi.fn<() => boolean>(),
    runtime: vi.fn<typeof import("mygo-runtime").runtime>(),
  };
});

vi.mock("../../mygo", () => ({
  Cleanup: fakes.Cleanup,
  Settings: fakes.Settings,
  Files: fakes.Files,
  History: fakes.History,
  events: { settingsChanged: { on: () => () => {} } },
}));

vi.mock("mygo-runtime", async (importOriginal) => ({
  ...(await importOriginal<typeof import("mygo-runtime")>()),
  Channel: fakes.FakeChannel,
  isMyGo: fakes.isMyGo,
  runtime: fakes.runtime,
}));

type FakeChannel = InstanceType<typeof fakes.FakeChannel<Progress>>;

function settings(confirm = true): AppSettings {
  return {
    version: 1,
    health: { cpuThreshold: 40, sustainMinutes: 10, enabled: true },
    cleanup: {
      exclusions: [],
      devProjectRoots: [],
      devArtifactMinAgeDays: 30,
      installerRoots: [],
      largeFileMinMB: 500,
    },
    history: { retentionDays: 180 },
    general: { launchAtLogin: false, confirmBeforeClean: confirm },
  };
}

function category(
  id: string,
  title: string,
  deletion: Category["deletion"],
  selected: boolean,
  items: [string, number][],
  extra: Partial<Category> = {},
): Category {
  return {
    id,
    title,
    description: `${title}的说明`,
    icon: "cache",
    kind: "cache",
    deletion,
    selected,
    size: items.reduce((sum, [, size]) => sum + size, 0),
    items: items.map(([path, size]) => ({
      id: `${id}:${path}`,
      path,
      size,
      modTime: "2026-11-01T00:00:00Z",
    })),
    ...extra,
  };
}

const CATEGORIES: Category[] = [
  category("app-caches", "应用缓存", "permanent", true, [
    ["/Users/me/Library/Caches/com.example.app", 300_000_000],
    ["/Users/me/Library/Caches/com.other.app", 200_000_000],
  ]),
  category(
    "app-caches-running",
    "正在运行的应用的缓存",
    "permanent",
    false,
    [["/Users/me/Library/Caches/com.running.app", 50_000_000]],
    { warning: "这些应用正在运行，建议先退出再清理" },
  ),
  category("incomplete-downloads", "未完成的下载", "trash", true, [
    ["/Users/me/Downloads/a.crdownload", 10_000_000],
  ]),
  category("trash", "废纸篓", "permanent", false, [["/Users/me/.Trash", 1_000_000_000]], {
    kind: "trash",
    icon: "trash",
    warning: "清空后无法恢复，默认不选",
  }),
];

function scanOf(categories: Category[], extra: Partial<ScanResult> = {}): ScanResult {
  return {
    categories,
    size: categories.reduce((sum, c) => sum + c.size, 0),
    items: categories.reduce((sum, c) => sum + c.items.length, 0),
    denied: 0,
    scannedAt: "2026-11-14T01:00:00Z",
    duration: 2e9,
    ...extra,
  };
}

const RESULT: CleanResult = {
  freed: 500_000_000,
  trashed: 10_000_000,
  removed: 2,
  skipped: 0,
  failed: [
    {
      itemId: "app-caches:/Users/me/Library/Caches/com.other.app",
      categoryId: "app-caches",
      path: "/Users/me/Library/Caches/com.other.app",
      reason: "有 1 个文件未能删除，可能正在使用",
      inUse: true,
    },
  ],
  categories: [
    { categoryId: "app-caches", title: "应用缓存", removed: 1, freed: 500_000_000, trashed: 0, failed: 1 },
    {
      categoryId: "incomplete-downloads",
      title: "未完成的下载",
      removed: 1,
      freed: 0,
      trashed: 10_000_000,
      failed: 0,
    },
  ],
  partial: true,
  interrupted: false,
  startedAt: "2026-11-14T01:00:00Z",
  duration: 1e9,
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

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
        <SystemJunkPage />
      </ShellBlockProvider>
    </QueryClientProvider>,
  );
}

function stat(label: string) {
  return screen.getByText(label).nextElementSibling?.textContent;
}

function doneLines(dialog: HTMLElement): string[] {
  const [, linesId] = (dialog.getAttribute("aria-describedby") ?? "").split(" ");
  return (document.getElementById(linesId ?? "")?.textContent ?? "").split("\n");
}

async function scanned(user: ReturnType<typeof userEvent.setup>, categories = CATEGORIES) {
  fakes.Cleanup.scan.mockResolvedValue(scanOf(categories));
  await user.click(await screen.findByRole("button", { name: "扫描" }));
  await screen.findByRole("region", { name: "可以清理的系统垃圾" });
}

beforeEach(() => {
  fakes.isMyGo.mockReturnValue(false);
  fakes.Settings.get.mockResolvedValue(settings());
  fakes.History.list.mockResolvedValue([]);
  fakes.Files.reveal.mockResolvedValue(undefined);
});

afterEach(() => {
  vi.resetAllMocks();
});

describe("SystemJunkPage idle", () => {
  it("says what it scans and when it last cleaned", async () => {
    const run: Run = {
      id: "r",
      kind: "systemJunk",
      startedAt: "2026-11-01T01:00:00Z",
      endedAt: "2026-11-01T01:00:05Z",
      results: [],
      summary: { freed: 1_200_000_000, items: 4, failed: 0 },
    };
    fakes.History.list.mockResolvedValue([{ ...run, id: "m", kind: "maintenance" }, run]);
    renderPage();
    expect(screen.getByRole("heading", { name: "系统垃圾" })).toBeTruthy();
    expect(screen.getByText(/检查应用缓存、浏览器缓存、日志、崩溃报告、未完成的下载和废纸篓/)).toBeTruthy();
    expect(await screen.findByText(/^上次清理：.*，释放 1.2 GB$/)).toBeTruthy();
    expect(fakes.History.list).toHaveBeenCalledWith(100);
  });

  it("shows why the last scan failed", async () => {
    const user = userEvent.setup();
    fakes.Cleanup.scan.mockRejectedValue(new Error("扫描没能完成，详情已记录到日志"));
    renderPage();
    await user.click(screen.getByRole("button", { name: "扫描" }));
    expect((await screen.findByRole("alert")).textContent).toBe("扫描没能完成，详情已记录到日志");
  });

  it("uses the Windows wording in a Windows window", () => {
    fakes.isMyGo.mockReturnValue(true);
    fakes.runtime.mockReturnValue({ platform: "win32", windowId: 1, version: "test" } as ReturnType<
      typeof fakes.runtime
    >);
    renderPage();
    expect(
      screen.getByText(/检查临时文件、应用缓存、浏览器缓存、错误报告、未完成的下载和回收站/),
    ).toBeTruthy();
  });
});

describe("SystemJunkPage scan and clean", () => {
  it("shows progress, lists the categories with their defaults, confirms by deletion mode and cleans", async () => {
    const user = userEvent.setup();
    const pendingScan = deferred<ScanResult>();
    let channel: FakeChannel | undefined;
    fakes.Cleanup.scan.mockImplementation((_id, ch) => {
      channel = ch as unknown as FakeChannel;
      return pendingScan.promise;
    });
    renderPage();
    await user.click(screen.getByRole("button", { name: "扫描" }));
    expect(fakes.Cleanup.scan).toHaveBeenCalledWith("system-junk", expect.any(fakes.FakeChannel));
    act(() =>
      channel?.send({
        phase: "scanning",
        path: "/Users/me/Library/Caches",
        done: 1,
        total: 4,
        bytes: 2_000_000,
      }),
    );
    expect(screen.getByText("已找到 2 MB")).toBeTruthy();
    expect(screen.getByText("25%")).toBeTruthy();
    await act(async () => pendingScan.resolve(scanOf(CATEGORIES)));

    const list = await screen.findByRole("region", { name: "可以清理的系统垃圾" });
    // The defaults: the app caches and the downloads, not the running app's caches or the Trash.
    expect(within(list).getByRole("checkbox", { name: "应用缓存" })).toHaveProperty("checked", true);
    expect(within(list).getByRole("checkbox", { name: "未完成的下载" })).toHaveProperty("checked", true);
    expect(within(list).getByRole("checkbox", { name: "正在运行的应用的缓存" })).toHaveProperty(
      "checked",
      false,
    );
    expect(within(list).getByRole("checkbox", { name: "废纸篓" })).toHaveProperty("checked", false);
    expect(within(list).getByText("清空后无法恢复，默认不选")).toBeTruthy();
    expect(stat("已选择")).toBe("3 项");
    expect(stat("可释放")).toBe("510 MB");
    expect(screen.getByRole("status").textContent).toBe("扫描完成，找到 1.6 GB");

    // Checking the Trash adds a permanent line to the confirmation.
    await user.click(within(list).getByRole("checkbox", { name: "废纸篓" }));
    expect(stat("已选择")).toBe("4 项");
    await user.click(screen.getByRole("button", { name: "清理" }));
    const confirm = screen.getByRole("dialog", { name: "确认清理" });
    const permanent = within(confirm).getByRole("region", { name: "永久删除" });
    expect(
      within(permanent)
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual(["应用缓存2 项 · 500 MB", "废纸篓1 项 · 1 GB"]);
    const trash = within(confirm).getByRole("region", { name: "移到废纸篓" });
    expect(
      within(trash)
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual(["未完成的下载1 项 · 10 MB"]);
    expect(within(confirm).getByText(/其中 3 项会永久删除，无法恢复；其余移到废纸篓。/)).toBeTruthy();
    // Deleting for good focuses 取消 first.
    expect(document.activeElement).toBe(within(confirm).getByRole("button", { name: "取消" }));

    const pendingClean = deferred<CleanResult>();
    let cleanChannel: FakeChannel | undefined;
    fakes.Cleanup.clean.mockImplementation((_id, _req, ch) => {
      cleanChannel = ch as unknown as FakeChannel;
      return pendingClean.promise;
    });
    await user.click(within(confirm).getByRole("button", { name: "清理" }));
    expect(fakes.Cleanup.clean).toHaveBeenCalledWith(
      "system-junk",
      { categoryIds: ["app-caches", "incomplete-downloads", "trash"] },
      expect.any(fakes.FakeChannel),
    );
    const overlay = screen.getByRole("group", { name: "正在清理…" });
    expect(within(overlay).getByText("共 4 项")).toBeTruthy();
    act(() =>
      cleanChannel?.send({
        phase: "cleaning",
        path: "/Users/me/.Trash",
        done: 1,
        total: 4,
        bytes: 1_000_000_000,
      }),
    );
    expect(within(overlay).getByText("已处理 1/4 · 已释放 1 GB")).toBeTruthy();

    await act(async () => pendingClean.resolve(RESULT));
    const done = await screen.findByRole("dialog", { name: "清理完成" });
    expect(within(done).getByText("510 MB")).toBeTruthy();
    // The in-use item is one gentle line, not a failure.
    expect(doneLines(done)).toEqual([
      "应用缓存：已清理 1 项，释放 500 MB",
      "未完成的下载：1 项已移到废纸篓（10 MB）",
      "移到废纸篓的项目在清空废纸篓后才会释放空间",
    ]);
    expect(within(done).getByText("1 项里有文件正被其他程序使用，已跳过这些文件，下次可以再试")).toBeTruthy();

    await user.click(within(done).getByRole("button", { name: "完成" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    const scanButton = screen.getByRole("button", { name: "扫描" });
    expect(document.activeElement).toBe(scanButton);
    expect(screen.getByText("shell").getAttribute("data-shell-blocked")).toBe("false");
  });

  it("cleans part of a category without asking when confirmation is off", async () => {
    const user = userEvent.setup();
    fakes.Settings.get.mockResolvedValue(settings(false));
    fakes.Cleanup.clean.mockResolvedValue({ ...RESULT, failed: [], partial: false });
    renderPage();
    await waitFor(() => expect(fakes.Settings.get).toHaveBeenCalled());
    await scanned(user);
    await user.click(screen.getByRole("button", { name: "展开应用缓存" }));
    await user.click(screen.getByRole("checkbox", { name: "com.other.app" }));
    await user.click(screen.getByRole("checkbox", { name: "未完成的下载" }));
    expect(stat("可释放")).toBe("300 MB");
    await user.click(screen.getByRole("button", { name: "清理" }));
    expect(screen.queryByRole("dialog", { name: "确认清理" })).toBeNull();
    expect(fakes.Cleanup.clean).toHaveBeenCalledWith(
      "system-junk",
      { categoryIds: ["app-caches"], itemIds: ["app-caches:/Users/me/Library/Caches/com.example.app"] },
      expect.any(fakes.FakeChannel),
    );
  });

  it("reveals an item in Finder", async () => {
    const user = userEvent.setup();
    renderPage();
    await scanned(user);
    await user.click(screen.getByRole("button", { name: "展开未完成的下载" }));
    await user.click(screen.getByRole("button", { name: "在访达中显示 a.crdownload" }));
    expect(fakes.Files.reveal).toHaveBeenCalledWith("/Users/me/Downloads/a.crdownload");
  });

  it("disables 清理 when nothing is checked, and rescans from the header", async () => {
    const user = userEvent.setup();
    renderPage();
    await scanned(user);
    await user.click(screen.getByRole("button", { name: "全不选" }));
    expect(screen.getByRole("button", { name: "清理" })).toHaveProperty("disabled", true);
    await user.click(screen.getByRole("button", { name: "重新扫描" }));
    await waitFor(() => expect(fakes.Cleanup.scan).toHaveBeenCalledTimes(2));
  });

  it("explains the Trash it cannot read and a partial scan", async () => {
    const user = userEvent.setup();
    const noAccess = category("trash", "废纸篓", "permanent", false, [], {
      warning: "需要为 Spruce 开启「完全磁盘访问权限」才能读取废纸篓，开启后重新扫描",
    });
    fakes.Cleanup.scan.mockResolvedValue(scanOf([...CATEGORIES.slice(0, 1), noAccess], { denied: 2 }));
    renderPage();
    await user.click(screen.getByRole("button", { name: "扫描" }));
    expect(await screen.findByText(/完全磁盘访问权限/)).toBeTruthy();
    expect(screen.getByRole("checkbox", { name: "废纸篓" })).toHaveProperty("disabled", true);
    expect(screen.getByText("有 2 个位置没有读取权限，结果可能不完整")).toBeTruthy();
  });

  it("says so when there is nothing to clean", async () => {
    const user = userEvent.setup();
    fakes.Cleanup.scan.mockResolvedValue(scanOf([]));
    renderPage();
    await user.click(screen.getByRole("button", { name: "扫描" }));
    expect(await screen.findByText("没有找到系统垃圾")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "清理" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "重新扫描" }));
    expect(fakes.Cleanup.scan).toHaveBeenCalledTimes(2);
  });

  it("explains a clean that could not start", async () => {
    const user = userEvent.setup();
    fakes.Cleanup.clean.mockRejectedValue(new Error("扫描结果已过期，请重新扫描"));
    renderPage();
    await scanned(user);
    await user.click(screen.getByRole("button", { name: "清理" }));
    await user.click(
      within(screen.getByRole("dialog", { name: "确认清理" })).getByRole("button", { name: "清理" }),
    );
    const done = await screen.findByRole("dialog", { name: "清理没有进行" });
    expect(doneLines(done)).toEqual(["扫描结果已过期，请重新扫描"]);
  });

  it("stops a scan and returns to idle without an error", async () => {
    const user = userEvent.setup();
    let channel: FakeChannel | undefined;
    fakes.Cleanup.scan.mockImplementation((_id, ch) => {
      const fake = ch as unknown as FakeChannel;
      channel = fake;
      // Closing the channel cancels the Go call, which then fails.
      return new Promise((_resolve, reject) => {
        fake.onclose = () => reject(new Error("已取消"));
      });
    });
    renderPage();
    await user.click(screen.getByRole("button", { name: "扫描" }));
    await user.click(screen.getByRole("button", { name: "停止" }));
    expect(channel?.closed).toBe(true);
    expect(await screen.findByRole("button", { name: "扫描" })).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
