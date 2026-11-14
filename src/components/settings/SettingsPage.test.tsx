import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type { AppSettings, Files, History, Permissions, Settings, SettingsLimits } from "../../mygo";
import { SettingsPage } from "./SettingsPage";

const fakes = vi.hoisted(() => ({
  Settings: {
    get: vi.fn<typeof Settings.get>(),
    update: vi.fn<typeof Settings.update>(),
    reset: vi.fn<typeof Settings.reset>(),
    limits: vi.fn<typeof Settings.limits>(),
    chooseFolder: vi.fn<typeof Settings.chooseFolder>(),
    about: vi.fn<typeof Settings.about>(),
  },
  Permissions: {
    fullDiskAccess: vi.fn<typeof Permissions.fullDiskAccess>(),
    guideFullDiskAccess: vi.fn<typeof Permissions.guideFullDiskAccess>(),
  },
  Files: { reveal: vi.fn<typeof Files.reveal>() },
  History: { clear: vi.fn<typeof History.clear>() },
  isMyGo: vi.fn<() => boolean>(),
  runtime: vi.fn<typeof import("mygo-runtime").runtime>(),
}));

vi.mock("../../mygo", () => ({
  Settings: fakes.Settings,
  Permissions: fakes.Permissions,
  Files: fakes.Files,
  History: fakes.History,
  events: {
    settingsChanged: { on: () => () => {} },
    historyAppended: { on: () => () => {} },
  },
}));

vi.mock("mygo-runtime", async (importOriginal) => ({
  ...(await importOriginal<typeof import("mygo-runtime")>()),
  isMyGo: fakes.isMyGo,
  runtime: fakes.runtime,
}));

const LIMITS: SettingsLimits = {
  cpuThreshold: { min: 20, max: 800 },
  sustainMinutes: { min: 1, max: 120 },
  devArtifactMinAgeDays: { min: 0, max: 3650 },
  largeFileMinMB: { min: 10, max: 1048576 },
  retentionDays: { min: 7, max: 3650 },
  maxPaths: 3,
};

function defaults(): AppSettings {
  return {
    version: 1,
    health: { cpuThreshold: 40, sustainMinutes: 10, enabled: true },
    cleanup: {
      exclusions: ["/Users/me/Keep"],
      devProjectRoots: ["/Users/me/Projects"],
      devArtifactMinAgeDays: 30,
      installerRoots: ["/Users/me/Downloads", "/Users/me/Desktop"],
      largeFileMinMB: 500,
    },
    history: { retentionDays: 180 },
    general: { launchAtLogin: false, confirmBeforeClean: true },
  };
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <SettingsPage />
    </QueryClientProvider>,
  );
}

function lastUpdate(): AppSettings {
  const calls = fakes.Settings.update.mock.calls;
  const last = calls[calls.length - 1];
  if (!last) throw new Error("Settings.update was not called");
  return last[0];
}

function section(name: string): HTMLElement {
  return screen.getByRole("region", { name });
}

beforeEach(() => {
  fakes.isMyGo.mockReturnValue(false);
  fakes.Settings.get.mockResolvedValue(defaults());
  fakes.Settings.update.mockImplementation(async (next) => next);
  fakes.Settings.limits.mockResolvedValue(LIMITS);
  fakes.Settings.about.mockResolvedValue({
    version: "0.1.0",
    dataDir: "/Users/me/Library/Application Support/Spruce",
  });
  fakes.Settings.chooseFolder.mockResolvedValue("");
  fakes.Permissions.fullDiskAccess.mockResolvedValue("granted");
  fakes.Files.reveal.mockResolvedValue(undefined);
  fakes.History.clear.mockResolvedValue(undefined);
});

afterEach(() => {
  vi.resetAllMocks();
});

describe("SettingsPage switches", () => {
  it("saves each switch into its own field at once", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("switch", { name: "登录时启动 Spruce" }));
    expect(lastUpdate()).toEqual({
      ...defaults(),
      general: { launchAtLogin: true, confirmBeforeClean: true },
    });

    await user.click(screen.getByRole("switch", { name: "清理前确认" }));
    expect(lastUpdate().general).toEqual({ launchAtLogin: true, confirmBeforeClean: false });

    await user.click(screen.getByRole("switch", { name: "启用高 CPU 提醒" }));
    expect(lastUpdate().health).toEqual({ cpuThreshold: 40, sustainMinutes: 10, enabled: false });
    expect(screen.getByRole("switch", { name: "启用高 CPU 提醒" })).toHaveProperty("checked", false);
    expect(fakes.Settings.update).toHaveBeenCalledTimes(3);
  });

  it("shows the reason inline and the stored value when Go refuses", async () => {
    const user = userEvent.setup();
    fakes.Settings.update.mockRejectedValue(new Error("无法更改「登录时启动」，其他设置没有保存"));
    renderPage();
    const toggle = await screen.findByRole("switch", { name: "登录时启动 Spruce" });
    await user.click(toggle);
    const general = section("通用");
    expect((await within(general).findByRole("alert")).textContent).toBe(
      "无法更改「登录时启动」，其他设置没有保存",
    );
    // The stored settings come back.
    await waitFor(() => expect(toggle).toHaveProperty("checked", false));
    expect(fakes.Settings.get.mock.calls.length).toBeGreaterThan(1);
  });
});

describe("SettingsPage health", () => {
  it("draws the sliders over the ranges of the settings package and saves once the user stops", async () => {
    renderPage();
    const threshold = await screen.findByRole("slider", { name: "提醒阈值" });
    expect([
      threshold.getAttribute("min"),
      threshold.getAttribute("max"),
      threshold.getAttribute("aria-valuetext"),
    ]).toEqual(["20", "800", "40%"]);
    fireEvent.change(threshold, { target: { value: "60" } });
    fireEvent.change(threshold, { target: { value: "75" } });
    expect(threshold.getAttribute("aria-valuetext")).toBe("75%");
    expect(fakes.Settings.update).not.toHaveBeenCalled();
    await waitFor(() => expect(fakes.Settings.update).toHaveBeenCalledTimes(1));
    expect(lastUpdate().health).toEqual({ cpuThreshold: 75, sustainMinutes: 10, enabled: true });

    const sustain = screen.getByRole("slider", { name: "持续时间" });
    expect([sustain.getAttribute("min"), sustain.getAttribute("max")]).toEqual(["1", "120"]);
    fireEvent.change(sustain, { target: { value: "3" } });
    await waitFor(() => expect(fakes.Settings.update).toHaveBeenCalledTimes(2));
    expect(lastUpdate().health).toEqual({ cpuThreshold: 75, sustainMinutes: 3, enabled: true });
    expect(sustain.getAttribute("aria-valuetext")).toBe("3 分钟");
  });

  it("says so when Go clamps a value", async () => {
    fakes.Settings.update.mockImplementation(async (next) => ({
      ...next,
      health: { ...next.health, cpuThreshold: 800 },
    }));
    renderPage();
    const threshold = await screen.findByRole("slider", { name: "提醒阈值" });
    fireEvent.change(threshold, { target: { value: "795" } });
    expect(await within(section("健康监测")).findByText("已调整为 800%")).toBeTruthy();
    expect(threshold.getAttribute("aria-valuetext")).toBe("800%");
  });
});

describe("SettingsPage numbers", () => {
  it("validates whole numbers in range before saving them", async () => {
    const user = userEvent.setup();
    renderPage();
    const retention = await screen.findByRole("textbox", { name: "保留天数" });
    expect(retention).toHaveProperty("value", "180");
    await user.clear(retention);
    await user.type(retention, "3天");
    const historySection = section("历史记录");
    expect(within(historySection).getByRole("alert").textContent).toBe("请输入 7 到 3650 之间的整数");
    expect(retention.getAttribute("aria-invalid")).toBe("true");
    await user.clear(retention);
    await user.type(retention, "30");
    expect(within(historySection).queryByRole("alert")).toBeNull();
    await waitFor(() => expect(fakes.Settings.update).toHaveBeenCalledTimes(1));
    expect(lastUpdate()).toEqual({ ...defaults(), history: { retentionDays: 30 } });

    const large = screen.getByRole("textbox", { name: "大文件阈值" });
    await user.clear(large);
    await user.type(large, "2000");
    await user.tab();
    await waitFor(() => expect(lastUpdate().cleanup.largeFileMinMB).toBe(2000));
    expect(large).toHaveProperty("value", "2000");

    const age = screen.getByRole("textbox", { name: "项目构建产物最少闲置天数" });
    await user.clear(age);
    await user.type(age, "0");
    await waitFor(() => expect(lastUpdate().cleanup.devArtifactMinAgeDays).toBe(0));
    expect(lastUpdate().cleanup.largeFileMinMB).toBe(2000);
    expect(fakes.Settings.update).toHaveBeenCalledTimes(3);
  });
});

describe("SettingsPage folders", () => {
  it("adds a folder from the picker, refuses a duplicate, ignores a cancel and removes one", async () => {
    const user = userEvent.setup();
    renderPage();
    const exclusions = (await screen.findByText("排除的文件夹")).closest(".setting-row") as HTMLElement;
    expect(within(exclusions).getByText("/Users/me/Keep")).toBeTruthy();

    fakes.Settings.chooseFolder.mockResolvedValueOnce("/Users/me/Movies");
    await user.click(within(exclusions).getByRole("button", { name: "添加…" }));
    expect(fakes.Settings.chooseFolder).toHaveBeenLastCalledWith("选择要排除的文件夹");
    await waitFor(() =>
      expect(lastUpdate().cleanup.exclusions).toEqual(["/Users/me/Keep", "/Users/me/Movies"]),
    );
    expect(await within(exclusions).findByText("/Users/me/Movies")).toBeTruthy();

    fakes.Settings.chooseFolder.mockResolvedValueOnce("/users/ME/keep");
    await user.click(within(exclusions).getByRole("button", { name: "添加…" }));
    expect((await within(exclusions).findByRole("alert")).textContent).toBe("这个文件夹已经在列表中了");
    expect(fakes.Settings.update).toHaveBeenCalledTimes(1);

    fakes.Settings.chooseFolder.mockResolvedValueOnce("");
    await user.click(within(exclusions).getByRole("button", { name: "添加…" }));
    await waitFor(() => expect(fakes.Settings.chooseFolder).toHaveBeenCalledTimes(3));
    expect(fakes.Settings.update).toHaveBeenCalledTimes(1);

    await user.click(within(exclusions).getByRole("button", { name: "移除 /Users/me/Keep" }));
    await waitFor(() => expect(lastUpdate().cleanup.exclusions).toEqual(["/Users/me/Movies"]));
    expect(within(exclusions).queryByText("/Users/me/Keep")).toBeNull();
  });

  it("writes each list into its own field and stops at the maximum", async () => {
    const user = userEvent.setup();
    renderPage();
    const dev = (await screen.findByText("开发项目所在文件夹")).closest(".setting-row") as HTMLElement;
    fakes.Settings.chooseFolder.mockResolvedValueOnce("/Users/me/code");
    await user.click(within(dev).getByRole("button", { name: "添加…" }));
    await waitFor(() =>
      expect(lastUpdate().cleanup.devProjectRoots).toEqual(["/Users/me/Projects", "/Users/me/code"]),
    );
    expect(lastUpdate().cleanup.exclusions).toEqual(["/Users/me/Keep"]);

    const installers = screen.getByText("安装包查找位置").closest(".setting-row") as HTMLElement;
    fakes.Settings.chooseFolder.mockResolvedValueOnce("/Volumes/Data/Installers");
    await user.click(within(installers).getByRole("button", { name: "添加…" }));
    await waitFor(() => expect(lastUpdate().cleanup.installerRoots).toHaveLength(3));
    // maxPaths is 3 in these limits.
    expect(within(installers).getByRole("button", { name: "添加…" })).toHaveProperty("disabled", true);
    expect(within(installers).getByText("最多 3 项")).toBeTruthy();
  });

  it("shows the picker's failure", async () => {
    const user = userEvent.setup();
    fakes.Settings.chooseFolder.mockRejectedValue(new Error("无法打开选择文件夹的窗口"));
    renderPage();
    const exclusions = (await screen.findByText("排除的文件夹")).closest(".setting-row") as HTMLElement;
    await user.click(within(exclusions).getByRole("button", { name: "添加…" }));
    expect((await within(exclusions).findByRole("alert")).textContent).toBe("无法打开选择文件夹的窗口");
  });
});

describe("SettingsPage history, permissions and about", () => {
  it("clears the history after confirming", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "清空历史…" }));
    const dialog = screen.getByRole("dialog", { name: "清空操作历史？" });
    await user.click(within(dialog).getByRole("button", { name: "清空" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(fakes.History.clear).toHaveBeenCalledTimes(1);
  });

  it("offers 去授权 for Full Disk Access on macOS", async () => {
    const user = userEvent.setup();
    fakes.Permissions.fullDiskAccess.mockResolvedValue("denied");
    fakes.Permissions.guideFullDiskAccess.mockResolvedValue("granted");
    renderPage();
    const permissions = await screen.findByRole("region", { name: "权限" });
    expect(await within(permissions).findByText("未授权")).toBeTruthy();
    expect(
      within(permissions).getByText(/Safari 和邮件的数据库、读取废纸篓和查找部分应用的残留文件/),
    ).toBeTruthy();
    await user.click(within(permissions).getByRole("button", { name: "去授权" }));
    expect(await within(permissions).findByText("已授权")).toBeTruthy();
    expect(within(permissions).queryByRole("button", { name: "去授权" })).toBeNull();
  });

  it("has no 权限 section on Windows", async () => {
    fakes.isMyGo.mockReturnValue(true);
    fakes.runtime.mockReturnValue({ platform: "win32" } as ReturnType<typeof import("mygo-runtime").runtime>);
    fakes.Permissions.fullDiskAccess.mockResolvedValue("notRequired");
    renderPage();
    await screen.findByRole("region", { name: "关于" });
    expect(screen.queryByRole("region", { name: "权限" })).toBeNull();
    expect(screen.getByRole("button", { name: "在资源管理器中显示" })).toBeTruthy();
  });

  it("shows the version and reveals the data folder", async () => {
    const user = userEvent.setup();
    renderPage();
    const about = await screen.findByRole("region", { name: "关于" });
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("设置");
    expect(await within(about).findByText("版本 0.1.0")).toBeTruthy();
    expect(within(about).getByText("/Users/me/Library/Application Support/Spruce")).toBeTruthy();
    expect(within(about).getByText("MyGo")).toBeTruthy();
    await user.click(within(about).getByRole("button", { name: "在访达中显示" }));
    expect(fakes.Files.reveal).toHaveBeenCalledWith("/Users/me/Library/Application Support/Spruce");
  });
});

describe("SettingsPage reset", () => {
  it("restores the defaults after confirming", async () => {
    const user = userEvent.setup();
    const changed = defaults();
    changed.history.retentionDays = 30;
    changed.health.cpuThreshold = 90;
    fakes.Settings.get.mockResolvedValue(changed);
    fakes.Settings.reset.mockResolvedValue(defaults());
    renderPage();
    expect(await screen.findByRole("textbox", { name: "保留天数" })).toHaveProperty("value", "30");

    await user.click(screen.getByRole("button", { name: "恢复默认设置" }));
    const dialog = screen.getByRole("dialog", { name: "恢复默认设置？" });
    await user.click(within(dialog).getByRole("button", { name: "取消" }));
    expect(fakes.Settings.reset).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "恢复默认设置" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "恢复默认" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(fakes.Settings.reset).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("textbox", { name: "保留天数" })).toHaveProperty("value", "180");
    expect(screen.getByRole("slider", { name: "提醒阈值" }).getAttribute("aria-valuetext")).toBe("40%");
    expect(fakes.Settings.update).not.toHaveBeenCalled();
  });
});
