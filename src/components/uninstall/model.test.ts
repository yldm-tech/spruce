import { describe, expect, it } from "vite-plus/test";

import type { CleanResult, UninstallApp, UninstallLeftover } from "../../mygo";
import {
  checkedTotals,
  confirmMessage,
  defaultChecked,
  mergeDetails,
  removeSummary,
  sizeText,
  uninstalledSummary,
  usageText,
  visibleApps,
  type AppDetails,
} from "./model";

const NOW = new Date();
const daysAgo = (n: number) => new Date(NOW.getTime() - n * 86400e3).toISOString();

function app(id: string, name: string, extra: Partial<UninstallApp> = {}): UninstallApp {
  return { id, name, appStore: false, running: false, ...extra };
}

const APPS = [
  app("a", "Zed", { bundleId: "dev.zed.Zed" }),
  app("b", "Arc", { bundleId: "company.thebrowser.Browser", installDate: "2024-01-01T00:00:00Z" }),
  app("c", "微信", {
    bundleId: "com.tencent.xinWeChat",
    publisher: "Tencent",
    installDate: "2025-06-01T00:00:00Z",
  }),
  app("d", "Blender", { bundleId: "org.blenderfoundation.blender" }),
];

const DETAILS = new Map<string, AppDetails>([
  ["a", { size: 300, usage: "used", lastUsed: daysAgo(2) }],
  ["b", { size: 900, usage: "never" }],
  ["c", { usage: "used", lastUsed: daysAgo(30) }],
  ["d", { size: 300, usage: "unknown" }],
]);

describe("visibleApps", () => {
  const ids = (apps: UninstallApp[]) => apps.map((a) => a.id);

  it("sorts by name, size and last use, unknowns last", () => {
    expect(ids(visibleApps(APPS, DETAILS, "", "name", false))).toEqual(["c", "b", "d", "a"]);
    expect(ids(visibleApps(APPS, DETAILS, "", "size", false))).toEqual(["b", "d", "a", "c"]);
    expect(ids(visibleApps(APPS, DETAILS, "", "recent", false))).toEqual(["a", "c", "b", "d"]);
  });

  it("orders by install date on Windows", () => {
    expect(ids(visibleApps(APPS, DETAILS, "", "recent", true))).toEqual(["c", "b", "d", "a"]);
  });

  it("searches names, bundle IDs and publishers, ignoring case", () => {
    expect(ids(visibleApps(APPS, DETAILS, " ZED ", "name", false))).toEqual(["a"]);
    expect(ids(visibleApps(APPS, DETAILS, "thebrowser", "name", false))).toEqual(["b"]);
    expect(ids(visibleApps(APPS, DETAILS, "tencent", "name", false))).toEqual(["c"]);
    expect(visibleApps(APPS, DETAILS, "nothing", "name", false)).toEqual([]);
  });

  it("does not reorder its input", () => {
    const input = [...APPS];
    visibleApps(input, DETAILS, "", "size", false);
    expect(input).toEqual(APPS);
  });
});

describe("details", () => {
  it("merges streamed updates field by field", () => {
    let m = mergeDetails(new Map(), [{ id: "a", usage: "never" }]);
    m = mergeDetails(m, [
      { id: "a", icon: "data:x" },
      { id: "a", size: 0 },
    ]);
    expect(m.get("a")).toEqual({ usage: "never", lastUsed: undefined, icon: "data:x", size: 0 });
  });

  it("words the last use, the install date and the size", () => {
    expect(usageText(APPS[0]!, DETAILS.get("a"), false)).toMatch(/使用过$/);
    expect(usageText(APPS[1]!, DETAILS.get("b"), false)).toBe("从未使用");
    expect(usageText(APPS[3]!, DETAILS.get("d"), false)).toBe("");
    expect(usageText(APPS[3]!, undefined, false)).toBe("");
    expect(usageText(APPS[1]!, undefined, true)).toMatch(/^安装于 /);
    expect(usageText(APPS[0]!, undefined, true)).toBe("");
    expect(sizeText({ size: 0 }, true)).toBe("0 B");
    expect(sizeText(undefined, true)).toBe("正在计算…");
    expect(sizeText(undefined, false)).toBe("大小未知");
  });
});

function leftover(id: string, size: number, selected: boolean): UninstallLeftover {
  return { id, path: `/Users/me/Library/Caches/${id}`, kind: "caches", size, dir: true, selected };
}

describe("leftovers", () => {
  it("starts from the selected ones and totals the checked", () => {
    const ls = [leftover("x", 1000, true), leftover("y", 2000, false), leftover("z", 3000, true)];
    const checked = defaultChecked(ls);
    expect([...checked]).toEqual(["x", "z"]);
    expect(checkedTotals(ls, checked)).toEqual({ count: 2, bytes: 4000 });
    expect(confirmMessage("Foo", 2, 1_500_000)).toBe(
      "「Foo」和勾选的 2 个相关文件（共 1.5 MB）会移到废纸篓，可以随时放回。",
    );
    expect(confirmMessage("Foo", 0, 1000)).toBe("「Foo」（共 1 KB）会移到废纸篓，可以随时放回。");
  });
});

function result(extra: Partial<CleanResult> = {}): CleanResult {
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

describe("summaries", () => {
  it("reports the app and its leftovers", () => {
    const s = removeSummary(
      {
        app: "Foo",
        bundleRemoved: true,
        result: result({
          trashed: 5_000_000,
          removed: 3,
          categories: [
            { categoryId: "app", title: "app", removed: 1, freed: 0, trashed: 4_000_000, failed: 0 },
            { categoryId: "caches", title: "caches", removed: 2, freed: 0, trashed: 1_000_000, failed: 1 },
          ],
          failed: [{ itemId: "f", categoryId: "caches", path: "/x/Caches/com.foo", reason: "没有权限删除" }],
        }),
      },
      false,
    );
    expect(s.title).toBe("已卸载「Foo」");
    expect(s.big).toBe("5 MB");
    expect(s.succeeded).toBe(true);
    expect(s.lines).toEqual([
      "「Foo」已移到废纸篓",
      "相关文件：2 项已移到废纸篓",
      "相关文件：1 项未能处理",
      "com.foo：没有权限删除",
      "移到废纸篓的项目在清空废纸篓后才会释放空间",
    ]);
  });

  it("explains an app that stayed", () => {
    const s = removeSummary(
      { app: "Foo", bundleRemoved: false, bundleFailure: "需要管理员权限。", result: result() },
      false,
    );
    expect(s).toEqual({
      title: "没能卸载「Foo」",
      big: "未卸载",
      lines: ["需要管理员权限。", "它的相关文件也没有改动。"],
      succeeded: false,
    });
  });

  it("says when a removal stopped midway", () => {
    const s = removeSummary(
      { app: "Foo", bundleRemoved: true, result: result({ interrupted: true }) },
      false,
    );
    expect(s.title).toBe("卸载已中断");
    expect(s.lines).toContain("卸载在中途停止，其余项目没有改动");
  });

  it("reports a Windows program and what it left", () => {
    const s = removeSummary(
      {
        app: "Foo",
        bundleRemoved: false,
        result: result({
          trashed: 2000,
          removed: 1,
          categories: [
            { categoryId: "appData", title: "appData", removed: 1, freed: 0, trashed: 2000, failed: 0 },
          ],
        }),
      },
      true,
    );
    expect(s.lines[0]).toBe("卸载程序已移除「Foo」");
    expect(s.lines[1]).toBe("相关文件：1 项已移到废纸篓");
    expect(uninstalledSummary("Foo", 0).lines).toEqual(["卸载程序已移除「Foo」", "没有找到它留下的文件"]);
    expect(uninstalledSummary("Foo", 3).lines[1]).toBe("留下的 3 个相关文件没有改动");
  });
});
