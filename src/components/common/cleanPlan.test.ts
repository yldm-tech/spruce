import { describe, expect, it } from "vite-plus/test";

import type { Category, CleanResult, Run } from "../../mygo";
import { cleanPlan, setInUseAside } from "./cleanPlan";
import { lastCleanText } from "./lastClean";

function category(id: string, deletion: Category["deletion"], sizes: number[]): Category {
  return {
    id,
    title: `${id} 标题`,
    description: "",
    icon: "cache",
    kind: "cache",
    deletion,
    selected: true,
    size: sizes.reduce((a, b) => a + b, 0),
    items: sizes.map((size, i) => ({ id: `${id}-${i}`, path: `/x/${id}/${i}`, size, modTime: "" })),
  };
}

describe("cleanPlan", () => {
  it("splits the checked items by deletion mode, in list order", () => {
    const cats = [
      category("a", "permanent", [10, 20]),
      category("b", "trash", [5]),
      category("c", "permanent", [1]),
    ];
    const sel = new Map([
      ["a", new Set(["a-1"])],
      ["b", new Set(["b-0"])],
      ["c", new Set<string>()],
    ]);
    expect(cleanPlan(cats, sel)).toEqual({
      permanent: [{ id: "a", title: "a 标题", count: 1, bytes: 20 }],
      trash: [{ id: "b", title: "b 标题", count: 1, bytes: 5 }],
      commands: [],
    });
  });

  it("puts a category its tool's command cleans apart", () => {
    const cats = [{ ...category("npm", "permanent", [7]), command: "npm cache clean --force" }];
    expect(cleanPlan(cats, new Map([["npm", new Set(["npm-0"])]]))).toEqual({
      permanent: [],
      trash: [],
      commands: [{ id: "npm", title: "npm 标题", count: 1, bytes: 7, command: "npm cache clean --force" }],
    });
  });
});

describe("setInUseAside", () => {
  const base: CleanResult = {
    freed: 100,
    trashed: 0,
    removed: 1,
    skipped: 0,
    failed: [
      {
        itemId: "1",
        categoryId: "temp",
        path: "/t/1",
        reason: "有 2 个文件未能删除，可能正在使用",
        inUse: true,
      },
      { itemId: "2", categoryId: "temp", path: "/t/2", reason: "没有权限删除" },
    ],
    categories: [{ categoryId: "temp", title: "临时文件", removed: 1, freed: 100, trashed: 0, failed: 2 }],
    partial: true,
    interrupted: false,
    startedAt: "2026-11-14T01:00:00Z",
    duration: 1e9,
  };

  it("counts the in-use items and leaves the other failures", () => {
    const { result, inUse } = setInUseAside(base);
    expect(inUse).toBe(1);
    expect(result.failed.map((f) => f.itemId)).toEqual(["2"]);
    expect(result.categories[0]?.failed).toBe(1);
  });

  it("returns the result as it is without in-use items", () => {
    const plain = { ...base, failed: base.failed.slice(1) };
    expect(setInUseAside(plain)).toEqual({ result: plain, inUse: 0 });
  });
});

describe("lastCleanText", () => {
  const run = (summary: Run["summary"]): Run => ({
    id: "r",
    kind: "systemJunk",
    startedAt: "2026-11-11T01:00:00Z",
    endedAt: "2026-11-11T01:00:05Z",
    results: [],
    summary,
  });
  const now = new Date("2026-11-14T03:00:00Z");

  it("says when and how much", () => {
    expect(lastCleanText(run({ freed: 1_200_000_000, items: 3, failed: 0 }), now)).toBe(
      "上次清理：3天前，释放 1.2 GB",
    );
    expect(lastCleanText(run({ freed: 0, trashed: 5_000_000, items: 1, failed: 0 }), now)).toBe(
      "上次清理：3天前，移到废纸篓 5 MB",
    );
  });
});
