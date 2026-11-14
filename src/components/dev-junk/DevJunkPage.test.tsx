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
  AppSettings,
  Category,
  CleanResult,
  Cleanup,
  DevJunk,
  DevJunkDetails,
  Files,
  Progress,
  ScanResult,
  Settings,
} from "../../mygo";
import { OverlayHost } from "../common/OverlayHost";
import { ShellBlockProvider } from "../ShellBlock";
import { DevJunkPage } from "./DevJunkPage";

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
    DevJunk: { details: vi.fn<typeof DevJunk.details>() },
    Settings: { get: vi.fn<typeof Settings.get>() },
    Files: { reveal: vi.fn<typeof Files.reveal>() },
  };
});

vi.mock("../../mygo", () => ({
  Cleanup: fakes.Cleanup,
  DevJunk: fakes.DevJunk,
  Settings: fakes.Settings,
  Files: fakes.Files,
  events: { settingsChanged: { on: () => () => {} } },
}));

vi.mock("mygo-runtime", async (importOriginal) => ({
  ...(await importOriginal<typeof import("mygo-runtime")>()),
  Channel: fakes.FakeChannel,
}));

type FakeChannel = InstanceType<typeof fakes.FakeChannel<Progress>>;

function category(
  id: string,
  title: string,
  kind: Category["kind"],
  deletion: Category["deletion"],
  items: [string, number, string?][],
  extra: Partial<Category> = {},
): Category {
  return {
    id,
    title,
    description: `${title}的说明`,
    icon: "cache",
    kind,
    deletion,
    selected: false,
    size: items.reduce((sum, [, size]) => sum + size, 0),
    items: items.map(([name, size, note]) => ({
      id: name,
      path: `/Users/u/x/${name}`,
      size,
      modTime: "2026-01-01T00:00:00Z",
      note,
    })),
    ...extra,
  };
}

const SCAN: ScanResult = {
  categories: [
    category("tool-npm", "npm 缓存", "devCache", "permanent", [["npm", 1000]], {
      selected: true,
      description: "运行 npm cache clean --force，清空 npm 下载的包",
    }),
    category("tool-xcode-device-support", "设备支持文件", "devCache", "permanent", [
      ["ios17", 3000, "这个平台最新的版本，默认保留"],
      ["ios16", 2000],
    ]),
    category("tool-xcode-archives", "Xcode 归档", "devCache", "trash", [["arch", 500]], {
      warning: "包含已发布版本的符号文件",
    }),
    category("artifact-node-modules", "node_modules", "projectArtifact", "permanent", [
      ["nm-new", 5000, "new · 最近 30 天内有改动"],
      ["nm-old", 4000, "old"],
    ]),
  ],
  size: 15500,
  items: 6,
  denied: 0,
  scannedAt: "2026-11-14T01:00:00Z",
  duration: 2e9,
};

const DETAILS: DevJunkDetails = {
  defaultItems: ["npm", "ios16", "nm-old"],
  commands: [{ categoryId: "tool-npm", command: "npm cache clean --force" }],
};

const RESULT: CleanResult = {
  freed: 7000,
  trashed: 0,
  removed: 3,
  skipped: 0,
  failed: [],
  categories: [
    { categoryId: "tool-npm", title: "npm 缓存", removed: 1, freed: 1000, trashed: 0, failed: 0 },
    {
      categoryId: "tool-xcode-device-support",
      title: "设备支持文件",
      removed: 1,
      freed: 2000,
      trashed: 0,
      failed: 0,
    },
    {
      categoryId: "artifact-node-modules",
      title: "node_modules",
      removed: 1,
      freed: 4000,
      trashed: 0,
      failed: 0,
    },
  ],
  partial: false,
  interrupted: false,
  startedAt: "2026-11-14T01:01:00Z",
  duration: 3e9,
};

function settings(roots: string[], confirm = true): AppSettings {
  return {
    version: 1,
    health: { cpuThreshold: 40, sustainMinutes: 10, enabled: true },
    cleanup: {
      exclusions: [],
      devProjectRoots: roots,
      devArtifactMinAgeDays: 30,
      installerRoots: [],
      largeFileMinMB: 500,
    },
    history: { retentionDays: 180 },
    general: { launchAtLogin: false, confirmBeforeClean: confirm },
  };
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const rootRoute = createRootRoute({
    component: () => (
      <ShellBlockProvider>
        <OverlayHost>
          <Outlet />
        </OverlayHost>
      </ShellBlockProvider>
    ),
  });
  const page = createRoute({ getParentRoute: () => rootRoute, path: "/dev-junk", component: DevJunkPage });
  const settingsPage = createRoute({
    getParentRoute: () => rootRoute,
    path: "/settings",
    component: () => <h1>设置页</h1>,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([page, settingsPage]),
    history: createMemoryHistory({ initialEntries: ["/dev-junk"] }),
  });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

function stat(label: string) {
  return screen.getByText(label).nextElementSibling?.textContent;
}

async function scanToResults(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: "扫描" }));
  await screen.findByRole("region", { name: "工具缓存" });
  // The default selection arrives with DevJunk.details: some categories are checked in part.
  await waitFor(() => expect(stat("已选择")).toBe("3 项"));
}

beforeEach(() => {
  fakes.Settings.get.mockResolvedValue(settings(["/Users/u/Workspace", "/Users/u/Projects"]));
  fakes.Cleanup.scan.mockResolvedValue(SCAN);
  fakes.Cleanup.clean.mockResolvedValue(RESULT);
  fakes.DevJunk.details.mockResolvedValue(DETAILS);
  fakes.Files.reveal.mockResolvedValue(undefined);
});

afterEach(() => {
  vi.resetAllMocks();
});

describe("DevJunkPage", () => {
  it("lists the project folders it will search before a scan, with a link to 设置", async () => {
    const user = userEvent.setup();
    renderPage();
    expect((await screen.findByRole("heading", { level: 1 })).textContent).toBe("开发者垃圾");
    const list = await screen.findByRole("list", { name: "项目文件夹" });
    expect(
      within(list)
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual(["/Users/u/Workspace", "/Users/u/Projects"]);
    await user.click(screen.getByRole("link", { name: "更改文件夹" }));
    expect(await screen.findByRole("heading", { name: "设置页" })).toBeTruthy();
  });

  it("says only tool caches are scanned when no project folder is set", async () => {
    fakes.Settings.get.mockResolvedValue(settings([]));
    renderPage();
    expect(await screen.findByText("还没有设置项目文件夹，这次只扫描开发工具的缓存。")).toBeTruthy();
    expect(screen.getByRole("link", { name: "去设置" }).getAttribute("href")).toBe("/settings");
  });

  it("shows scan progress and stops the scan", async () => {
    const user = userEvent.setup();
    let channel: FakeChannel | undefined;
    fakes.Cleanup.scan.mockImplementation((_id, ch) => {
      channel = ch as unknown as FakeChannel;
      return new Promise(() => {});
    });
    renderPage();
    await user.click(await screen.findByRole("button", { name: "扫描" }));
    await waitFor(() => expect(channel).toBeDefined());
    act(() =>
      channel?.send({ phase: "scanning", path: "/Users/u/Workspace/app", done: 3, total: 10, bytes: 2000 }),
    );
    expect(await screen.findByText("/Users/u/Workspace/app")).toBeTruthy();
    expect(screen.getByText("已找到 2 KB")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "停止" }));
    expect(channel?.closed).toBe(true);
  });

  it("shows the two sections with the scan's default selection, confirms commands, deletions and the Trash apart, and cleans", async () => {
    const user = userEvent.setup();
    renderPage();
    await scanToResults(user);
    expect(fakes.Cleanup.scan).toHaveBeenCalledWith("dev-junk", expect.any(fakes.FakeChannel));

    const tools = screen.getByRole("region", { name: "工具缓存" });
    const projects = screen.getByRole("region", { name: "项目构建产物" });
    expect(screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent)).toEqual([
      "工具缓存",
      "项目构建产物",
    ]);
    expect(within(tools).getByText("已选 1/2 项")).toBeTruthy();
    expect(within(tools).getByText("包含已发布版本的符号文件")).toBeTruthy();
    expect(within(projects).getByText("已选 1/2 项")).toBeTruthy();
    expect(stat("已选大小")).toBe("7 KB");

    // Expanding node_modules shows each project with its note; the recent one is not checked.
    await user.click(within(projects).getByRole("button", { name: "展开node_modules" }));
    expect(within(projects).getByText(/最近 30 天内有改动/)).toBeTruthy();
    expect((within(projects).getByRole("checkbox", { name: "nm-new" }) as HTMLInputElement).checked).toBe(
      false,
    );
    expect((within(projects).getByRole("checkbox", { name: "nm-old" }) as HTMLInputElement).checked).toBe(
      true,
    );

    await user.click(screen.getByRole("button", { name: "清理" }));
    const dialog = await screen.findByRole("dialog", { name: "确认清理" });
    expect(within(dialog).getByText("将清理 3 项，共 7 KB。")).toBeTruthy();
    expect(
      within(dialog)
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual(["运行 1 个清理命令：npm cache clean --force", "永久删除 2 项，无法恢复"]);
    expect(fakes.Cleanup.clean).not.toHaveBeenCalled();
    await user.click(within(dialog).getByRole("button", { name: "清理" }));

    expect(await screen.findByRole("dialog", { name: "清理完成" })).toBeTruthy();
    expect(fakes.Cleanup.clean).toHaveBeenCalledWith(
      "dev-junk",
      {
        categoryIds: ["tool-npm", "tool-xcode-device-support", "artifact-node-modules"],
        itemIds: ["npm", "ios16", "nm-old"],
      },
      expect.any(fakes.FakeChannel),
    );
    await user.click(screen.getByRole("button", { name: "完成" }));
    expect(await screen.findByRole("button", { name: "扫描" })).toBeTruthy();
  });

  it("selects within one section only", async () => {
    const user = userEvent.setup();
    renderPage();
    await scanToResults(user);
    const projects = screen.getByRole("region", { name: "项目构建产物" });
    await user.click(within(projects).getByRole("button", { name: "全不选" }));
    await waitFor(() => expect(stat("已选择")).toBe("2 项"));
    const tools = screen.getByRole("region", { name: "工具缓存" });
    await user.click(within(tools).getByRole("button", { name: "全选" }));
    await waitFor(() => expect(stat("已选择")).toBe("4 项"));
    expect(within(projects).getByText("2 项")).toBeTruthy();
  });

  it("says when nothing was found", async () => {
    const user = userEvent.setup();
    fakes.Cleanup.scan.mockResolvedValue({ ...SCAN, categories: [], size: 0, items: 0 });
    fakes.DevJunk.details.mockResolvedValue({ defaultItems: [], commands: [] });
    renderPage();
    await user.click(await screen.findByRole("button", { name: "扫描" }));
    expect(await screen.findByText("没有找到开发者垃圾")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "清理" })).toBeNull();
  });

  it("shows a scan that failed and lets the user try again", async () => {
    const user = userEvent.setup();
    fakes.Cleanup.scan.mockRejectedValue(new Error("扫描没能完成，详情已记录到日志"));
    renderPage();
    await user.click(await screen.findByRole("button", { name: "扫描" }));
    expect((await screen.findByRole("alert")).textContent).toBe("扫描没能完成，详情已记录到日志");
    expect(screen.getByRole("button", { name: "扫描" })).toBeTruthy();
  });

  it("notes when the projects held no build output", async () => {
    const user = userEvent.setup();
    fakes.Cleanup.scan.mockResolvedValue({ ...SCAN, categories: SCAN.categories.slice(0, 1), denied: 2 });
    renderPage();
    await user.click(await screen.findByRole("button", { name: "扫描" }));
    expect(await screen.findByText("项目文件夹里没有找到构建产物。")).toBeTruthy();
    expect(screen.getByText("有 2 个位置没有权限读取，结果可能不完整。")).toBeTruthy();
  });
});
