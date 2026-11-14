import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  AppSettings,
  Category,
  Cleanup,
  CleanResult,
  Files,
  History,
  Run,
  ScanResult,
  Settings,
} from "../../mygo";
import { ShellBlockProvider } from "../ShellBlock";
import { InstallersPage } from "./InstallersPage";

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

function settings(roots: string[]): AppSettings {
  return {
    version: 1,
    health: { cpuThreshold: 40, sustainMinutes: 10, enabled: true },
    cleanup: {
      exclusions: [],
      devProjectRoots: [],
      devArtifactMinAgeDays: 30,
      installerRoots: roots,
      largeFileMinMB: 500,
    },
    history: { retentionDays: 180 },
    general: { launchAtLogin: false, confirmBeforeClean: true },
  };
}

const ROOTS = ["/Users/me/Downloads", "/Users/me/Desktop"];

function category(
  id: string,
  title: string,
  selected: boolean,
  items: { path: string; size: number; modTime: string; note?: string }[],
): Category {
  return {
    id,
    title,
    description: `${title}的说明`,
    icon: "installer",
    kind: "installer",
    deletion: "trash",
    selected,
    size: items.reduce((sum, i) => sum + i.size, 0),
    items: items.map((i) => ({ id: `${id}:${i.path}`, ...i })),
  };
}

const SCAN: ScanResult = {
  categories: [
    category("disk-images", "磁盘映像", true, [
      { path: "/Users/me/Downloads/Firefox 130.0.dmg", size: 140_000_000, modTime: "2026-09-01T00:00:00Z" },
    ]),
    category("installer-packages", "安装器", true, [
      { path: "/Users/me/Desktop/node-v22.11.0.pkg", size: 80_000_000, modTime: "2026-08-01T00:00:00Z" },
    ]),
    category("recent-installers", "最近下载或正在使用的安装包", false, [
      {
        path: "/Users/me/Downloads/Slack.dmg",
        size: 200_000_000,
        modTime: "2026-11-13T00:00:00Z",
        note: "磁盘映像 · 已挂载",
      },
    ]),
  ],
  size: 420_000_000,
  items: 3,
  denied: 0,
  scannedAt: "2026-11-14T01:00:00Z",
  duration: 1e9,
};

const RESULT: CleanResult = {
  freed: 0,
  trashed: 220_000_000,
  removed: 2,
  skipped: 0,
  failed: [],
  categories: [
    { categoryId: "disk-images", title: "磁盘映像", removed: 1, freed: 0, trashed: 140_000_000, failed: 0 },
    {
      categoryId: "installer-packages",
      title: "安装器",
      removed: 1,
      freed: 0,
      trashed: 80_000_000,
      failed: 0,
    },
  ],
  partial: false,
  interrupted: false,
  startedAt: "2026-11-14T01:00:00Z",
  duration: 1e9,
};

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <ShellBlockProvider>
        <InstallersPage />
      </ShellBlockProvider>
    </QueryClientProvider>,
  );
}

function stat(label: string) {
  return screen.getByText(label).nextElementSibling?.textContent;
}

beforeEach(() => {
  fakes.isMyGo.mockReturnValue(false);
  fakes.Settings.get.mockResolvedValue(settings(ROOTS));
  fakes.History.list.mockResolvedValue([]);
});

afterEach(() => {
  vi.resetAllMocks();
});

describe("InstallersPage idle", () => {
  it("lists the folders it searches, says where installers go and when it last cleaned", async () => {
    const run: Run = {
      id: "r",
      kind: "installers",
      startedAt: "2026-11-01T01:00:00Z",
      endedAt: "2026-11-01T01:00:05Z",
      results: [],
      summary: { freed: 0, trashed: 2_100_000_000, items: 3, failed: 0 },
    };
    fakes.History.list.mockResolvedValue([run]);
    renderPage();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("安装包");
    expect(screen.getByText("找出装完就没再用的安装包和磁盘映像")).toBeTruthy();
    const folders = await screen.findByRole("list", { name: "查找的文件夹" });
    expect(
      within(folders)
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual(ROOTS);
    expect(screen.getByText("清理的安装包会移到废纸篓，可以随时放回。")).toBeTruthy();
    expect(screen.getByText(/\.dmg、\.pkg、\.iso、\.xip/)).toBeTruthy();
    expect(await screen.findByText(/^上次清理：.*，移到废纸篓 2.1 GB$/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "扫描" })).toHaveProperty("disabled", false);
  });

  it("cannot scan without folders to search", async () => {
    fakes.Settings.get.mockResolvedValue(settings([]));
    renderPage();
    expect(await screen.findByText("还没有要查找的文件夹，可以在「设置」里添加。")).toBeTruthy();
    expect(screen.getByRole("button", { name: "扫描" })).toHaveProperty("disabled", true);
  });

  it("uses the Windows wording in a Windows window", async () => {
    fakes.isMyGo.mockReturnValue(true);
    fakes.runtime.mockReturnValue({ platform: "win32", windowId: 1, version: "test" } as ReturnType<
      typeof fakes.runtime
    >);
    renderPage();
    expect(await screen.findByText("清理的安装包会移到回收站，可以随时放回。")).toBeTruthy();
    expect(screen.getByText(/\.exe、\.msi、\.msix、\.appx/)).toBeTruthy();
  });
});

describe("InstallersPage scan and clean", () => {
  it("leaves recent and attached installers unchecked and moves the rest to the Trash", async () => {
    const user = userEvent.setup();
    fakes.Cleanup.scan.mockResolvedValue(SCAN);
    fakes.Cleanup.clean.mockResolvedValue(RESULT);
    renderPage();
    await user.click(await screen.findByRole("button", { name: "扫描" }));
    expect(fakes.Cleanup.scan).toHaveBeenCalledWith("installers", expect.any(fakes.FakeChannel));
    const list = await screen.findByRole("region", { name: "找到的安装包" });
    expect(
      screen.getByText("清理的安装包会移到废纸篓，可以随时放回。最近 7 天内下载的和仍在挂载的默认不选。"),
    ).toBeTruthy();
    expect(within(list).getByRole("checkbox", { name: "磁盘映像" })).toHaveProperty("checked", true);
    expect(within(list).getByRole("checkbox", { name: "最近下载或正在使用的安装包" })).toHaveProperty(
      "checked",
      false,
    );
    await user.click(within(list).getByRole("button", { name: "展开最近下载或正在使用的安装包" }));
    expect(within(list).getByText(/磁盘映像 · 已挂载/)).toBeTruthy();
    expect(stat("已选择")).toBe("2 项");
    expect(stat("已选大小")).toBe("220 MB");

    await user.click(screen.getByRole("button", { name: "清理" }));
    const confirm = screen.getByRole("dialog", { name: "确认清理" });
    expect(
      within(confirm).getByText("将清理 2 项，共 220 MB。它们会移到废纸篓，可以随时放回。"),
    ).toBeTruthy();
    expect(within(confirm).queryByRole("region", { name: "永久删除" })).toBeNull();
    expect(
      within(within(confirm).getByRole("region", { name: "移到废纸篓" }))
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual(["磁盘映像1 项 · 140 MB", "安装器1 项 · 80 MB"]);
    // Nothing is deleted for good, so 清理 has focus.
    expect(document.activeElement).toBe(within(confirm).getByRole("button", { name: "清理" }));
    await user.click(within(confirm).getByRole("button", { name: "清理" }));
    expect(fakes.Cleanup.clean).toHaveBeenCalledWith(
      "installers",
      { categoryIds: ["disk-images", "installer-packages"] },
      expect.any(fakes.FakeChannel),
    );
    const done = await screen.findByRole("dialog", { name: "清理完成" });
    expect(within(done).getByText("220 MB")).toBeTruthy();
    await user.click(within(done).getByRole("button", { name: "完成" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "扫描" })).toBeTruthy());
  });

  it("says so when there are no installers", async () => {
    const user = userEvent.setup();
    fakes.Cleanup.scan.mockResolvedValue({ ...SCAN, categories: [], size: 0, items: 0 });
    renderPage();
    await user.click(await screen.findByRole("button", { name: "扫描" }));
    expect(await screen.findByText("没有找到安装包")).toBeTruthy();
  });
});
