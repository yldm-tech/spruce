import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type {
  CleanResult,
  Files,
  Progress,
  Uninstall,
  UninstallApp,
  UninstallAppDetails,
  UninstallerStatus,
  UninstallLeftover,
  UninstallOutcome,
  UninstallPlan,
} from "../../mygo";
import { ShellBlockProvider } from "../ShellBlock";
import { UninstallPage } from "./UninstallPage";

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
    Uninstall: {
      apps: vi.fn<typeof Uninstall.apps>(),
      measure: vi.fn<typeof Uninstall.measure>(),
      leftovers: vi.fn<typeof Uninstall.leftovers>(),
      remove: vi.fn<typeof Uninstall.remove>(),
      runUninstaller: vi.fn<typeof Uninstall.runUninstaller>(),
      checkRemoved: vi.fn<typeof Uninstall.checkRemoved>(),
    },
    Files: { reveal: vi.fn<typeof Files.reveal>() },
    isMyGo: vi.fn<() => boolean>(),
    runtime: vi.fn<typeof import("mygo-runtime").runtime>(),
  };
});

vi.mock("../../mygo", () => ({ Uninstall: fakes.Uninstall, Files: fakes.Files }));

vi.mock("mygo-runtime", async (importOriginal) => ({
  ...(await importOriginal<typeof import("mygo-runtime")>()),
  Channel: fakes.FakeChannel,
  isMyGo: fakes.isMyGo,
  runtime: fakes.runtime,
}));

type Chan<T> = InstanceType<typeof fakes.FakeChannel<T>>;

const ICON = "data:image/png;base64,iVBORw0KGgo=";

const APPS: UninstallApp[] = [
  {
    id: "foo",
    name: "Foo",
    version: "2.1",
    bundleId: "com.example.foo",
    path: "/Applications/Foo.app",
    appStore: true,
    running: false,
  },
  {
    id: "bar",
    name: "Bar",
    version: "1.0",
    bundleId: "com.example.bar",
    path: "/Applications/Bar.app",
    appStore: false,
    running: false,
  },
  {
    id: "run",
    name: "Runner",
    bundleId: "com.example.run",
    path: "/Applications/Runner.app",
    appStore: false,
    running: true,
  },
];

function leftover(
  id: string,
  kind: UninstallLeftover["kind"],
  size: number,
  selected = true,
): UninstallLeftover {
  return { id, path: `/Users/me/Library/${kind}/${id}`, kind, size, dir: true, selected };
}

const FOO_PLAN: UninstallPlan = {
  appId: "foo",
  bundle: {
    id: "foo",
    path: "/Applications/Foo.app",
    kind: "app",
    size: 100_000_000,
    dir: true,
    selected: true,
  },
  leftovers: [
    leftover("sup", "support", 20_000_000),
    leftover("cache", "caches", 5_000_000),
    leftover("agent", "launchAgents", 1000, false),
  ],
  denied: 0,
  running: false,
};

function cleanResult(extra: Partial<CleanResult> = {}): CleanResult {
  return {
    freed: 0,
    trashed: 0,
    removed: 0,
    skipped: 0,
    failed: [],
    categories: [],
    partial: false,
    interrupted: false,
    startedAt: "2026-11-14T01:00:00Z",
    duration: 1e9,
    ...extra,
  };
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <ShellBlockProvider>
        <UninstallPage />
      </ShellBlockProvider>
    </QueryClientProvider>,
  );
}

function doneLines(dialog: HTMLElement): string[] {
  const linesId = (dialog.getAttribute("aria-describedby") ?? "").split(" ")[1] ?? "";
  return (document.getElementById(linesId)?.textContent ?? "").split("\n");
}

/** stat reads a figure of the action bar by its label. */
function stat(label: string) {
  const bar = document.querySelector<HTMLElement>("footer.action-bar");
  if (!bar) throw new Error("no action bar");
  return within(bar).getByText(label).nextElementSibling?.textContent;
}

beforeEach(() => {
  fakes.isMyGo.mockReturnValue(false);
  fakes.Uninstall.apps.mockResolvedValue(APPS);
  fakes.Uninstall.measure.mockImplementation((ch) => {
    const c = ch as unknown as Chan<UninstallAppDetails>;
    c.send({ id: "foo", size: 120_000_000, usage: "never", icon: ICON });
    c.send({ id: "bar", size: 900_000_000, usage: "used", lastUsed: new Date().toISOString() });
    return Promise.resolve();
  });
  fakes.Uninstall.leftovers.mockResolvedValue(FOO_PLAN);
  fakes.Files.reveal.mockResolvedValue(undefined);
});

afterEach(() => {
  vi.resetAllMocks();
});

describe("UninstallPage list", () => {
  it("lists the apps with their streamed details, searches and sorts", async () => {
    const user = userEvent.setup();
    renderPage();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("应用卸载");
    expect(screen.getByText("连同应用留下的缓存和设置一起移除")).toBeTruthy();
    const list = await screen.findByRole("listbox", { name: "应用" });
    expect(
      within(list)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual([
      expect.stringContaining("Bar"),
      expect.stringContaining("Foo"),
      expect.stringContaining("Runner"),
    ]);
    await waitFor(() =>
      expect(within(list).getByRole("option", { name: /Foo/ }).textContent).toContain("120 MB"),
    );
    const foo = within(list).getByRole("option", { name: /Foo/ });
    expect(foo.textContent).toContain("从未使用");
    expect(foo.textContent).toContain("App Store");
    expect(foo.querySelector("img")?.getAttribute("src")).toBe(ICON);
    expect(within(list).getByRole("option", { name: /Runner/ }).textContent).toContain("运行中");
    // Runner's size never came: once measuring ends it is unknown.
    expect(within(list).getByRole("option", { name: /Runner/ }).textContent).toContain("大小未知");

    await user.click(screen.getByRole("button", { name: "大小" }));
    expect(
      within(list)
        .getAllByRole("option")
        .map((o) => o.id && o.textContent?.slice(0, 3)),
    ).toEqual(["Bar", "Foo", "Run"]);
    await user.type(screen.getByRole("searchbox", { name: "搜索应用" }), "example.f");
    expect(within(list).getAllByRole("option")).toHaveLength(1);
    expect(screen.getByText("找到 1 个，共 3 个应用")).toBeTruthy();
    await user.clear(screen.getByRole("searchbox", { name: "搜索应用" }));
    await user.type(screen.getByRole("searchbox", { name: "搜索应用" }), "zzz");
    expect(screen.getByText("没有符合搜索的应用")).toBeTruthy();
  });

  it("moves the selection with the arrow keys", async () => {
    const user = userEvent.setup();
    renderPage();
    const list = await screen.findByRole("listbox", { name: "应用" });
    list.focus();
    await user.keyboard("{ArrowDown}");
    expect(within(list).getByRole("option", { name: /Bar/ }).getAttribute("aria-selected")).toBe("true");
    await user.keyboard("{ArrowDown}");
    expect(list.getAttribute("aria-activedescendant")).toBe(
      within(list).getByRole("option", { name: /Foo/ }).id,
    );
    await user.keyboard("{End}");
    expect(
      within(list)
        .getByRole("option", { name: /Runner/ })
        .getAttribute("aria-selected"),
    ).toBe("true");
  });

  it("shows the error with a retry", async () => {
    const user = userEvent.setup();
    fakes.Uninstall.apps.mockRejectedValueOnce(new Error("无法读取已安装的应用，详情已记录到日志"));
    renderPage();
    expect(await screen.findByText("无法读取已安装的应用，详情已记录到日志")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "重试" }));
    expect(await screen.findByRole("listbox", { name: "应用" })).toBeTruthy();
  });

  it("says when there is nothing to uninstall", async () => {
    fakes.Uninstall.apps.mockResolvedValue([]);
    renderPage();
    expect(await screen.findByText("没有找到可以卸载的应用")).toBeTruthy();
  });
});

describe("UninstallPage on macOS", () => {
  it("uninstalls the app with the checked leftovers", async () => {
    const user = userEvent.setup();
    let progress: Chan<Progress> | undefined;
    let finishRemove!: (o: UninstallOutcome) => void;
    fakes.Uninstall.remove.mockImplementation((_req, ch) => {
      progress = ch as unknown as Chan<Progress>;
      return new Promise((resolve) => (finishRemove = resolve));
    });
    renderPage();
    const list = await screen.findByRole("listbox", { name: "应用" });
    expect(screen.getByRole("button", { name: "卸载" })).toHaveProperty("disabled", true);
    await user.click(within(list).getByRole("option", { name: /Foo/ }));

    const detail = await screen.findByRole("region", { name: "Foo 的详情" });
    const files = await within(detail).findByRole("list", { name: "相关文件" });
    const boxes = within(files).getAllByRole("checkbox");
    expect(boxes.map((b) => (b as HTMLInputElement).checked)).toEqual([true, true, false]);
    expect(stat("已选择")).toBe("3 项");
    expect(stat("将移到废纸篓")).toBe("125 MB");
    await user.click(within(files).getByRole("checkbox", { name: "缓存：cache" }));
    expect(stat("已选择")).toBe("2 项");
    expect(stat("将移到废纸篓")).toBe("120 MB");

    await user.click(screen.getByRole("button", { name: "卸载" }));
    const confirm = screen.getByRole("dialog", { name: "卸载「Foo」" });
    expect(confirm.textContent).toContain(
      "「Foo」和勾选的 1 个相关文件（共 120 MB）会移到废纸篓，可以随时放回。",
    );
    await user.click(within(confirm).getByRole("button", { name: "卸载" }));
    expect(fakes.Uninstall.remove).toHaveBeenCalledWith(
      { appId: "foo", leftoverIds: ["sup"] },
      expect.anything(),
    );
    expect(await screen.findByRole("group", { name: "正在卸载「Foo」…" })).toBeTruthy();
    progress?.send({
      phase: "cleaning",
      path: "/Applications/Foo.app",
      done: 1,
      total: 2,
      bytes: 100_000_000,
    });
    expect(await screen.findByText("已处理 1/2 · 已释放 100 MB")).toBeTruthy();

    finishRemove({
      app: "Foo",
      bundleRemoved: true,
      result: cleanResult({
        trashed: 120_000_000,
        removed: 2,
        categories: [
          { categoryId: "app", title: "app", removed: 1, freed: 0, trashed: 100_000_000, failed: 0 },
          { categoryId: "support", title: "support", removed: 1, freed: 0, trashed: 20_000_000, failed: 0 },
        ],
      }),
    });
    const done = await screen.findByRole("dialog", { name: "已卸载「Foo」" });
    expect(doneLines(done)).toEqual([
      "「Foo」已移到废纸篓",
      "相关文件：1 项已移到废纸篓",
      "移到废纸篓的项目在清空废纸篓后才会释放空间",
    ]);
    expect(fakes.Uninstall.apps).toHaveBeenCalledTimes(1);
    await user.click(within(done).getByRole("button", { name: "完成" }));
    await waitFor(() => expect(fakes.Uninstall.apps).toHaveBeenCalledTimes(2));
    expect(screen.getByText("选择一个应用")).toBeTruthy();
  });

  it("refuses a running app until it quits", async () => {
    const user = userEvent.setup();
    fakes.Uninstall.leftovers.mockResolvedValue({ ...FOO_PLAN, appId: "run", running: true });
    renderPage();
    await user.click(
      within(await screen.findByRole("listbox", { name: "应用" })).getByRole("option", { name: /Runner/ }),
    );
    expect(await screen.findByText("请先退出「Runner」再卸载")).toBeTruthy();
    expect(screen.getByRole("button", { name: "卸载" })).toHaveProperty("disabled", true);
    fakes.Uninstall.leftovers.mockResolvedValue({ ...FOO_PLAN, appId: "run", running: false });
    await user.click(screen.getByRole("button", { name: "重新检查" }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "卸载" })).toHaveProperty("disabled", false),
    );
    expect(screen.queryByText("请先退出「Runner」再卸载")).toBeNull();
  });

  it("explains an app that needs an administrator", async () => {
    const user = userEvent.setup();
    const failure =
      "需要管理员权限才能把「Foo」移到废纸篓。请在访达中把它拖到废纸篓，系统会请你输入管理员密码。";
    fakes.Uninstall.remove.mockResolvedValue({
      app: "Foo",
      bundleRemoved: false,
      bundleFailure: failure,
      result: cleanResult(),
    });
    renderPage();
    await user.click(
      within(await screen.findByRole("listbox", { name: "应用" })).getByRole("option", { name: /Foo/ }),
    );
    await screen.findByRole("list", { name: "相关文件" });
    await user.click(screen.getByRole("button", { name: "卸载" }));
    await user.click(
      within(screen.getByRole("dialog", { name: "卸载「Foo」" })).getByRole("button", { name: "卸载" }),
    );
    const done = await screen.findByRole("dialog", { name: "没能卸载「Foo」" });
    expect(doneLines(done)).toEqual([failure, "它的相关文件也没有改动。"]);
  });

  it("shows why the leftovers could not be found", async () => {
    const user = userEvent.setup();
    fakes.Uninstall.leftovers.mockRejectedValue(new Error("没能查找相关文件，详情已记录到日志"));
    renderPage();
    await user.click(
      within(await screen.findByRole("listbox", { name: "应用" })).getByRole("option", { name: /Bar/ }),
    );
    expect(await screen.findByText("没能查找相关文件，详情已记录到日志")).toBeTruthy();
    expect(screen.getByRole("button", { name: "卸载" })).toHaveProperty("disabled", true);
  });
});

describe("UninstallPage on Windows", () => {
  beforeEach(() => {
    fakes.isMyGo.mockReturnValue(true);
    fakes.runtime.mockReturnValue({ platform: "win32" } as ReturnType<typeof import("mygo-runtime").runtime>);
    fakes.Uninstall.apps.mockResolvedValue([
      {
        id: "foo",
        name: "Foo",
        version: "2.1",
        publisher: "Foo Software",
        appStore: false,
        running: false,
        installDate: "2025-03-04T00:00:00Z",
      },
    ]);
  });

  it("runs the program's uninstaller, then offers what it left", async () => {
    const user = userEvent.setup();
    let status: Chan<UninstallerStatus> | undefined;
    let finishRun!: (o: { removed: boolean; waited: boolean }) => void;
    fakes.Uninstall.runUninstaller.mockImplementation((_id, ch) => {
      status = ch as unknown as Chan<UninstallerStatus>;
      return new Promise((resolve) => (finishRun = resolve));
    });
    fakes.Uninstall.leftovers.mockResolvedValue({
      appId: "foo",
      bundle: null,
      leftovers: [leftover("roaming", "appData", 3000), leftover("local", "localAppData", 4000)],
      denied: 0,
      running: false,
    });
    fakes.Uninstall.remove.mockResolvedValue({
      app: "Foo",
      bundleRemoved: false,
      result: cleanResult({
        trashed: 3000,
        removed: 1,
        categories: [
          { categoryId: "appData", title: "appData", removed: 1, freed: 0, trashed: 3000, failed: 0 },
        ],
      }),
    });
    renderPage();
    const list = await screen.findByRole("listbox", { name: "应用" });
    expect(screen.getByRole("button", { name: "安装时间" })).toBeTruthy();
    expect(screen.getByText(/Microsoft Store 应用/)).toBeTruthy();
    await user.click(within(list).getByRole("option", { name: /Foo/ }));
    // The leftovers are looked up only after the uninstaller.
    expect(fakes.Uninstall.leftovers).not.toHaveBeenCalled();
    expect(stat("卸载方式")).toBe("自带卸载程序");

    await user.click(screen.getByRole("button", { name: "卸载" }));
    await user.click(
      within(screen.getByRole("dialog", { name: "卸载「Foo」" })).getByRole("button", {
        name: "打开卸载程序",
      }),
    );
    expect(await screen.findByText("正在打开卸载程序…")).toBeTruthy();
    status?.send({ running: true });
    expect(await screen.findByText("正在等待卸载程序…")).toBeTruthy();
    finishRun({ removed: true, waited: true });

    const dialog = await screen.findByRole("dialog", { name: "「Foo」留下的文件" });
    await user.click(within(dialog).getByRole("checkbox", { name: "本地数据（Local）：local" }));
    await user.click(within(dialog).getByRole("button", { name: /移到回收站/ }));
    expect(fakes.Uninstall.remove).toHaveBeenCalledWith(
      { appId: "foo", leftoverIds: ["roaming"] },
      expect.anything(),
    );
    const done = await screen.findByRole("dialog", { name: "已卸载「Foo」" });
    expect(doneLines(done)[0]).toBe("卸载程序已移除「Foo」");
    expect(doneLines(done)[1]).toBe("相关文件：1 项已移到回收站");
  });

  it("checks again when the uninstaller went on in another process", async () => {
    const user = userEvent.setup();
    fakes.Uninstall.runUninstaller.mockResolvedValue({ removed: false, waited: true });
    fakes.Uninstall.checkRemoved.mockResolvedValueOnce({ removed: false, waited: true });
    fakes.Uninstall.checkRemoved.mockResolvedValueOnce({ removed: true, waited: true });
    fakes.Uninstall.leftovers.mockResolvedValue({
      appId: "foo",
      bundle: null,
      leftovers: [],
      denied: 0,
      running: false,
    });
    renderPage();
    await user.click(
      within(await screen.findByRole("listbox", { name: "应用" })).getByRole("option", { name: /Foo/ }),
    );
    await user.click(screen.getByRole("button", { name: "卸载" }));
    await user.click(
      within(screen.getByRole("dialog", { name: "卸载「Foo」" })).getByRole("button", {
        name: "打开卸载程序",
      }),
    );
    const notDone = await screen.findByRole("dialog", { name: "卸载没有完成" });
    expect(doneLines(notDone)[0]).toBe("卸载程序已退出，但「Foo」仍在已安装的程序中。");
    await user.click(within(notDone).getByRole("button", { name: "再次检查" }));
    await waitFor(() =>
      expect(doneLines(screen.getByRole("dialog", { name: "卸载没有完成" }))).toContain(
        "「Foo」仍在已安装的程序中",
      ),
    );
    await user.click(
      within(screen.getByRole("dialog", { name: "卸载没有完成" })).getByRole("button", { name: "再次检查" }),
    );
    const done = await screen.findByRole("dialog", { name: "已卸载「Foo」" });
    expect(doneLines(done)).toEqual(["卸载程序已移除「Foo」", "没有找到它留下的文件"]);
  });

  it("shows a declined administrator prompt", async () => {
    const user = userEvent.setup();
    fakes.Uninstall.runUninstaller.mockRejectedValue(new Error("没有获得管理员授权，卸载程序没有运行"));
    renderPage();
    await user.click(
      within(await screen.findByRole("listbox", { name: "应用" })).getByRole("option", { name: /Foo/ }),
    );
    await user.click(screen.getByRole("button", { name: "卸载" }));
    await user.click(
      within(screen.getByRole("dialog", { name: "卸载「Foo」" })).getByRole("button", {
        name: "打开卸载程序",
      }),
    );
    const done = await screen.findByRole("dialog", { name: "卸载没有进行" });
    expect(doneLines(done)).toEqual(["没有获得管理员授权，卸载程序没有运行"]);
  });
});
