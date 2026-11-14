import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  Files,
  Permissions,
  Space,
  SpaceEntry,
  SpaceListing,
  SpaceLocations,
  SpaceProgress,
  SpaceSummary,
} from "../../mygo";
import { ShellBlockProvider } from "../ShellBlock";
import { SpacePage } from "./SpacePage";

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
    Space: {
      locations: vi.fn<typeof Space.locations>(),
      chooseFolder: vi.fn<typeof Space.chooseFolder>(),
      scan: vi.fn<typeof Space.scan>(),
      current: vi.fn<typeof Space.current>(),
      children: vi.fn<typeof Space.children>(),
      largeFiles: vi.fn<typeof Space.largeFiles>(),
      trash: vi.fn<typeof Space.trash>(),
    },
    Permissions: {
      fullDiskAccess: vi.fn<typeof Permissions.fullDiskAccess>(),
      guideFullDiskAccess: vi.fn<typeof Permissions.guideFullDiskAccess>(),
    },
    Files: {
      reveal: vi.fn<typeof Files.reveal>(),
      trashSize: vi.fn<typeof Files.trashSize>(),
      emptyTrash: vi.fn<typeof Files.emptyTrash>(),
    },
    isMyGo: vi.fn<() => boolean>(),
    runtime: vi.fn<typeof import("mygo-runtime").runtime>(),
  };
});

vi.mock("../../mygo", () => ({
  Space: fakes.Space,
  Permissions: fakes.Permissions,
  Files: fakes.Files,
}));

vi.mock("mygo-runtime", async (importOriginal) => ({
  ...(await importOriginal<typeof import("mygo-runtime")>()),
  Channel: fakes.FakeChannel,
  isMyGo: fakes.isMyGo,
  runtime: fakes.runtime,
}));

type FakeChannel = InstanceType<typeof fakes.FakeChannel<SpaceProgress>>;

const HOME = "/Users/me";

const LOCATIONS: SpaceLocations = {
  volumes: [
    { path: "/", name: "Macintosh HD", total: 500e9, free: 120e9, startup: true },
    { path: "/Volumes/Archive", name: "Archive", total: 2e12, free: 1.9e12, startup: false },
  ],
  targets: [
    { id: "home", name: "个人文件夹", path: HOME },
    { id: "downloads", name: "下载", path: `${HOME}/Downloads` },
    { id: "applications", name: "应用程序", path: "/Applications" },
  ],
};

function entry(
  id: number,
  kind: SpaceEntry["kind"],
  name: string,
  size: number,
  extra: Partial<SpaceEntry> = {},
): SpaceEntry {
  const path = kind === "dir" || kind === "file" ? (id === 0 ? HOME : `${HOME}/${name}`) : "";
  return { id, kind, name, path, size, files: 1, ...extra };
}

const ROOT = entry(0, "dir", "个人文件夹", 10e9, { files: 5000 });
const PROJECTS = entry(1, "dir", "Projects", 6e9, { files: 4000 });
const ISO = entry(2, "file", "big.iso", 3e9, { modTime: "2026-11-10T08:00:00Z" });
const MOVIES = entry(5, "dir", "Movies", 0.5e9, { locked: "个人文件夹和磁盘最上一层的项目请在访达中处理" });
const OTHER = entry(3, "other", "", 0.3e9, { count: 4 });
const SMALL = entry(4, "files", "", 0.2e9, { count: 30 });
const APP = entry(6, "dir", "app", 4e9, { path: `${HOME}/Projects/app` });
const DATA = entry(7, "file", "data.bin", 2e9, { path: `${HOME}/Projects/data.bin` });

const SUMMARY: SpaceSummary = {
  scanId: "scan-1",
  name: "个人文件夹",
  root: ROOT,
  files: 5000,
  dirs: 700,
  denied: 0,
  deniedPaths: [],
  errors: 0,
  scannedAt: new Date().toISOString(),
  duration: 3e9,
  largeFileMin: 500e6,
};

const LISTINGS: Record<number, SpaceListing> = {
  0: { entry: ROOT, ancestors: [ROOT], children: [PROJECTS, ISO, MOVIES, OTHER, SMALL] },
  1: { entry: PROJECTS, ancestors: [ROOT, PROJECTS], children: [APP, DATA] },
};

/** deferred returns a promise and the functions that settle it, for calls the test finishes by hand. */
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (err: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <ShellBlockProvider>
        <SpacePage />
      </ShellBlockProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

/** actionStat is the value under label beside the round button. */
function actionStat(label: string) {
  return screen.getByText(label, { selector: ".action-stat-label" }).nextElementSibling?.textContent;
}

beforeAll(() => {
  // jsdom lays nothing out; the treemap measures its box and follows its size.
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(600);
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(360);
});

beforeEach(() => {
  fakes.isMyGo.mockReturnValue(false);
  fakes.Space.locations.mockResolvedValue(LOCATIONS);
  fakes.Space.current.mockResolvedValue(null);
  fakes.Space.children.mockImplementation(async (_scan, id) => {
    const listing = LISTINGS[id];
    if (!listing) throw new Error("这个文件夹已经不在扫描结果里了");
    return listing;
  });
  fakes.Space.largeFiles.mockResolvedValue([ISO, DATA]);
  fakes.Permissions.fullDiskAccess.mockResolvedValue("granted");
  fakes.Files.reveal.mockResolvedValue(undefined);
  fakes.Files.trashSize.mockResolvedValue({ bytes: 0, items: 0 });
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("SpacePage start screen", () => {
  it("lists the disks with their capacity and the quick folders", async () => {
    renderPage();
    const disk = await screen.findByRole("button", { name: /Macintosh HD/ });
    expect(within(disk).getByText("可用 120 GB，共 500 GB")).toBeTruthy();
    expect(within(disk).getByText("已用 76%")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Archive/ })).toBeTruthy();
    for (const name of ["个人文件夹", "下载", "应用程序"]) {
      expect(screen.getByRole("button", { name: new RegExp(`^${name}`) })).toBeTruthy();
    }
    expect(screen.getByRole("button", { name: "选择其他文件夹…" })).toBeTruthy();
  });

  it("scans the chosen folder from the system's picker", async () => {
    const user = userEvent.setup();
    fakes.Space.chooseFolder.mockResolvedValue("/Users/me/Music");
    fakes.Space.scan.mockReturnValue(new Promise(() => {}));
    renderPage();
    await user.click(await screen.findByRole("button", { name: "选择其他文件夹…" }));
    await waitFor(() =>
      expect(fakes.Space.scan).toHaveBeenCalledWith("/Users/me/Music", expect.any(fakes.FakeChannel)),
    );
    expect(screen.getByText("正在分析「Music」…")).toBeTruthy();
  });

  it("stays put when the picker is cancelled", async () => {
    const user = userEvent.setup();
    fakes.Space.chooseFolder.mockResolvedValue("");
    renderPage();
    await user.click(await screen.findByRole("button", { name: "选择其他文件夹…" }));
    await waitFor(() => expect(fakes.Space.chooseFolder).toHaveBeenCalled());
    expect(fakes.Space.scan).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: /Macintosh HD/ })).toBeTruthy();
  });
});

describe("SpacePage scan", () => {
  it("shows the progress from the channel, then the results", async () => {
    const user = userEvent.setup();
    const pending = deferred<SpaceSummary>();
    let channel: FakeChannel | undefined;
    fakes.Space.scan.mockImplementation((_path, ch) => {
      channel = ch as unknown as FakeChannel;
      return pending.promise;
    });
    renderPage();
    await user.click(await screen.findByRole("button", { name: /^个人文件夹/ }));
    expect(fakes.Space.scan).toHaveBeenCalledWith(HOME, expect.any(fakes.FakeChannel));
    expect(screen.getByText("正在分析「个人文件夹」…")).toBeTruthy();
    expect(screen.getByText("正在准备…")).toBeTruthy();

    act(() => channel?.send({ files: 1204, dirs: 80, bytes: 2.5e9, path: `${HOME}/Library/Caches` }));
    expect(screen.getByText("已扫描 1,204 个文件 · 2.5 GB")).toBeTruthy();
    expect(screen.getByText(`${HOME}/Library/Caches`)).toBeTruthy();

    await act(async () => pending.resolve(SUMMARY));
    const list = await screen.findByRole("list", { name: "个人文件夹 的内容" });
    expect(within(list).getByRole("button", { name: "Projects" })).toBeTruthy();
    expect(within(list).getByText("其他 4 项")).toBeTruthy();
    expect(within(list).getByText("30 个小文件")).toBeTruthy();
    expect(within(list).getByText("60% · 4,000 个文件")).toBeTruthy();
    // The bottom bar: the scanned size, 重新扫描 and when.
    expect(screen.getByRole("button", { name: "重新扫描" })).toBeTruthy();
    expect(actionStat("个人文件夹")).toBe("10 GB");
    expect(actionStat("上次扫描")).toBe("刚刚");
  });

  it("goes back without an error when the user stops the scan", async () => {
    const user = userEvent.setup();
    const pending = deferred<SpaceSummary>();
    let channel: FakeChannel | undefined;
    fakes.Space.scan.mockImplementation((_path, ch) => {
      channel = ch as unknown as FakeChannel;
      return pending.promise;
    });
    renderPage();
    await user.click(await screen.findByRole("button", { name: /^下载/ }));
    await user.click(screen.getByRole("button", { name: "停止" }));
    expect(channel?.closed).toBe(true);
    await act(async () => pending.reject(new Error("已取消")));
    expect(await screen.findByRole("button", { name: /Macintosh HD/ })).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("explains a scan that failed", async () => {
    const user = userEvent.setup();
    fakes.Space.scan.mockRejectedValue(new Error("Spruce 没有读取这个文件夹的权限"));
    renderPage();
    await user.click(await screen.findByRole("button", { name: /^应用程序/ }));
    expect((await screen.findByRole("alert")).textContent).toBe("Spruce 没有读取这个文件夹的权限");
  });
});

describe("SpacePage results", () => {
  beforeEach(() => {
    fakes.Space.current.mockResolvedValue(SUMMARY);
  });

  it("shows the last scan at once and opens folders from the list, the map and the path", async () => {
    const user = userEvent.setup();
    renderPage();
    const list = await screen.findByRole("list", { name: "个人文件夹 的内容" });
    expect(fakes.Space.scan).not.toHaveBeenCalled();
    expect(fakes.Space.children).toHaveBeenCalledWith("scan-1", 0);

    // The treemap draws one cell per child with a size; folders are buttons.
    const map = screen.getByRole("group", { name: "个人文件夹 的空间分布图" });
    expect(
      within(map)
        .getAllByRole("button")
        .map((b) => b.getAttribute("aria-label")),
    ).toEqual(["打开 Projects，6 GB", "打开 Movies，500 MB"]);

    await user.click(within(list).getByRole("button", { name: "Projects" }));
    expect(await screen.findByRole("list", { name: "Projects 的内容" })).toBeTruthy();
    expect(fakes.Space.children).toHaveBeenLastCalledWith("scan-1", 1);
    const path = screen.getByRole("navigation", { name: "位置" });
    expect(within(path).getByText("Projects").getAttribute("aria-current")).toBe("location");

    await user.click(within(path).getByRole("button", { name: "个人文件夹" }));
    expect(await screen.findByRole("list", { name: "个人文件夹 的内容" })).toBeTruthy();

    await user.click(
      within(screen.getByRole("group", { name: "个人文件夹 的空间分布图" })).getByRole("button", {
        name: "打开 Projects，6 GB",
      }),
    );
    expect(await screen.findByRole("list", { name: "Projects 的内容" })).toBeTruthy();
  });

  it("moves an entry to the Trash after confirming and shows the corrected size", async () => {
    const user = userEvent.setup();
    fakes.Space.trash.mockResolvedValue({ trashed: 3e9, items: 1, failed: [], root: { ...ROOT, size: 7e9 } });
    renderPage();
    const list = await screen.findByRole("list", { name: "个人文件夹 的内容" });
    await user.click(within(list).getByRole("button", { name: "将 big.iso 移到废纸篓" }));
    const dialog = screen.getByRole("dialog", { name: "移到废纸篓" });
    expect(
      within(dialog).getByText("将把「big.iso」（3 GB）移到废纸篓。需要时可以从废纸篓放回原处。"),
    ).toBeTruthy();
    await user.click(within(dialog).getByRole("button", { name: "移到废纸篓" }));

    expect(fakes.Space.trash).toHaveBeenCalledWith("scan-1", [ISO.path]);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.getByRole("status").textContent).toBe("已把「big.iso」移到废纸篓，清倒废纸篓后可释放 3 GB");
    expect(actionStat("个人文件夹")).toBe("7 GB");
    // The folder is asked for again, so its sizes follow.
    await waitFor(() => expect(fakes.Space.children.mock.calls.filter(([, id]) => id === 0).length).toBe(2));
  });

  it("says why an entry cannot be moved instead of asking", async () => {
    const user = userEvent.setup();
    renderPage();
    const list = await screen.findByRole("list", { name: "个人文件夹 的内容" });
    const trash = within(list).getByRole("button", { name: "将 Movies 移到废纸篓" });
    expect(trash.getAttribute("aria-disabled")).toBe("true");
    await user.click(trash);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("alert").textContent).toBe(
      "「Movies」不能移走：个人文件夹和磁盘最上一层的项目请在访达中处理",
    );
    expect(fakes.Space.trash).not.toHaveBeenCalled();
    // Folded groups have no actions at all.
    expect(within(list).queryByRole("button", { name: /其他 4 项/ })).toBeNull();
  });

  it("shows why the service left an entry in place", async () => {
    const user = userEvent.setup();
    fakes.Space.trash.mockResolvedValue({
      trashed: 0,
      items: 0,
      failed: [{ path: ISO.path, reason: "没有权限移动这个项目" }],
      root: ROOT,
    });
    renderPage();
    const list = await screen.findByRole("list", { name: "个人文件夹 的内容" });
    await user.click(within(list).getByRole("button", { name: "将 big.iso 移到废纸篓" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "移到废纸篓" }));
    expect((await screen.findByRole("alert")).textContent).toBe("「big.iso」没有移走：没有权限移动这个项目");
  });

  it("reveals an entry in Finder", async () => {
    const user = userEvent.setup();
    renderPage();
    const list = await screen.findByRole("list", { name: "个人文件夹 的内容" });
    await user.click(within(list).getByRole("button", { name: "在访达中显示 big.iso" }));
    expect(fakes.Files.reveal).toHaveBeenCalledWith(ISO.path);
  });

  it("lists the large files in their own tab", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("list", { name: "个人文件夹 的内容" });
    expect(fakes.Space.largeFiles).not.toHaveBeenCalled();
    await user.click(screen.getByRole("tab", { name: "大文件" }));
    expect(screen.getByRole("tab", { name: "大文件" }).getAttribute("aria-selected")).toBe("true");
    const list = await screen.findByRole("list", { name: "大文件" });
    expect(fakes.Space.largeFiles).toHaveBeenCalledWith("scan-1");
    expect(within(list).getByText("big.iso")).toBeTruthy();
    expect(within(list).getByText(`${HOME}/Projects`, { exact: false })).toBeTruthy();
    expect(
      screen.getByText("「个人文件夹」中 500 MB 以上的文件，最多列出 200 个。可以在「设置」中调整大小。"),
    ).toBeTruthy();
  });

  it("offers 去授权 for folders it could not read without Full Disk Access", async () => {
    const user = userEvent.setup();
    fakes.Space.current.mockResolvedValue({ ...SUMMARY, denied: 3, deniedPaths: [`${HOME}/Library/Mail`] });
    fakes.Permissions.fullDiskAccess.mockResolvedValue("denied");
    fakes.Permissions.guideFullDiskAccess.mockResolvedValue("granted");
    renderPage();
    expect(await screen.findByText("3 个文件夹无权读取")).toBeTruthy();
    expect(screen.getByText(`例如 ${HOME}/Library/Mail`)).toBeTruthy();
    await user.click(await screen.findByRole("button", { name: "去授权" }));
    expect(fakes.Permissions.guideFullDiskAccess).toHaveBeenCalled();
    expect(await screen.findByText("已获得完全磁盘访问权限，重新扫描即可算进这些文件夹")).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "重新扫描" })).toHaveLength(2);
  });

  it("goes back to the start screen and returns to the results", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("list", { name: "个人文件夹 的内容" });
    await user.click(screen.getByRole("button", { name: "更换位置" }));
    expect(await screen.findByRole("button", { name: /Macintosh HD/ })).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "返回「个人文件夹」" }));
    expect(await screen.findByRole("list", { name: "个人文件夹 的内容" })).toBeTruthy();
  });

  it("scans the same location again", async () => {
    const user = userEvent.setup();
    fakes.Space.scan.mockReturnValue(new Promise(() => {}));
    renderPage();
    await screen.findByRole("list", { name: "个人文件夹 的内容" });
    await user.click(screen.getByRole("button", { name: "重新扫描" }));
    expect(fakes.Space.scan).toHaveBeenCalledWith(HOME, expect.any(fakes.FakeChannel));
    expect(screen.getByText("正在分析「个人文件夹」…")).toBeTruthy();
  });
});
