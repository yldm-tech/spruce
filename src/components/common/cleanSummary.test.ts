import { describe, expect, it } from "vite-plus/test";

import type { Category, CleanResult } from "../../mygo";
import { cleanConfirmation, cleanSummary } from "./cleanSummary";

function result(extra: Partial<CleanResult>): CleanResult {
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

describe("cleanSummary", () => {
  it("totals the space and lists categories, failures and the Trash note", () => {
    const s = cleanSummary({
      result: result({
        freed: 1_200_000_000,
        trashed: 300_000_000,
        removed: 35,
        skipped: 2,
        partial: true,
        failed: Array.from({ length: 10 }, (_, i) => ({
          itemId: `f${i}`,
          categoryId: "caches",
          path: `/Users/me/Library/Caches/app${i}`,
          reason: "文件正在使用，已跳过",
        })),
        categories: [
          {
            categoryId: "caches",
            title: "系统缓存",
            removed: 34,
            freed: 1_200_000_000,
            trashed: 0,
            failed: 10,
          },
          { categoryId: "dmg", title: "安装包", removed: 1, freed: 0, trashed: 300_000_000, failed: 0 },
        ],
      }),
    });
    expect(s.title).toBe("清理完成");
    expect(s.big).toBe("1.5 GB");
    expect(s.succeeded).toBe(true);
    expect(s.lines).toEqual([
      "系统缓存：已清理 34 项，释放 1.2 GB，10 项未能处理",
      "安装包：1 项已移到废纸篓（300 MB）",
      ...Array.from({ length: 8 }, (_, i) => `app${i}：文件正在使用，已跳过`),
      "另有 2 项未能处理",
      "2 项在扫描后已不存在，已跳过",
      "移到废纸篓的项目在清空废纸篓后才会释放空间",
    ]);
  });

  it("names interrupted, empty and failed cleans", () => {
    expect(cleanSummary({ result: result({ interrupted: true, removed: 1, partial: true }) }).title).toBe(
      "清理已中断",
    );
    expect(cleanSummary({ result: result({ skipped: 3 }) })).toMatchObject({
      title: "清理未完成",
      succeeded: false,
    });
    expect(cleanSummary({ error: "扫描结果已过期，请重新扫描" })).toEqual({
      title: "清理没有进行",
      big: "0 B",
      lines: ["扫描结果已过期，请重新扫描"],
      succeeded: false,
    });
  });
});

describe("cleanConfirmation", () => {
  const cat = (id: string, deletion: Category["deletion"]): Category => ({
    id,
    title: id,
    description: "",
    icon: "cache",
    kind: "cache",
    deletion,
    items: [{ id: `${id}-1`, path: `/x/${id}`, size: 1_000_000, modTime: "" }],
    size: 1_000_000,
    selected: true,
  });
  const cats = [cat("a", "permanent"), cat("b", "trash")];

  it("says whether the clean can be undone", () => {
    expect(cleanConfirmation(cats, new Map([["b", new Set(["b-1"])]]))).toEqual({
      message: "将清理 1 项，共 1 MB。它们会移到废纸篓，可以随时放回。",
      danger: false,
    });
    expect(cleanConfirmation(cats, new Map([["a", new Set(["a-1"])]])).message).toBe(
      "将清理 1 项，共 1 MB。删除后无法恢复。",
    );
    const both = new Map([
      ["a", new Set(["a-1"])],
      ["b", new Set(["b-1"])],
    ]);
    expect(cleanConfirmation(cats, both)).toEqual({
      message: "将清理 2 项，共 2 MB。其中 1 项会永久删除，无法恢复；其余移到废纸篓。",
      danger: true,
    });
  });
});
