import { describe, expect, it } from "vite-plus/test";

import type { Result, TaskView } from "../../mygo";
import {
  completedLines,
  formatAlertTitle,
  formatEstimate,
  fullDiskAccessBlocked,
  issueLines,
} from "./summary";

function r(taskId: string, outcome: Result["outcome"], message: string, extra: Partial<Result> = {}): Result {
  return { taskId, outcome, message, duration: 0, ...extra };
}

const catalog = [
  { id: "a", title: "甲" },
  { id: "b", title: "乙" },
  { id: "c", title: "丙" },
] as TaskView[];

describe("formatEstimate", () => {
  it("rounds to minutes and says 不到 1 分钟 below one", () => {
    expect(formatEstimate(0)).toBe("0 分钟");
    expect(formatEstimate(59)).toBe("不到 1 分钟");
    expect(formatEstimate(60)).toBe("约 1 分钟");
    expect(formatEstimate(89)).toBe("约 1 分钟");
    expect(formatEstimate(90)).toBe("约 2 分钟");
  });
});

describe("formatAlertTitle", () => {
  it("rounds the percent and floors the minutes", () => {
    expect(
      formatAlertTitle({
        pid: 1,
        name: "Xcode",
        cpuPercent: 47.6,
        since: "",
        duration: 12 * 60e9 + 59e9,
        threshold: 40,
      }),
    ).toBe("Xcode 已经持续占用 48% CPU 有 12 分钟");
  });
});

describe("issueLines", () => {
  it("skips completed tasks and groups the rest by message in catalog order", () => {
    expect(
      issueLines(
        [
          r("a", "skipped", "取消"),
          r("b", "applied", "好"),
          r("c", "skipped", "取消"),
          r("x", "failed", "坏"),
        ],
        catalog,
      ),
    ).toEqual([
      { titles: ["甲", "丙"], message: "取消" },
      { titles: ["x"], message: "坏" },
    ]);
  });
});

describe("issueLines and completedLines", () => {
  it("lists a partly applied result as an issue and every other completed result with its message", () => {
    const results = [
      r("c", "unchanged", "无需处理"),
      r("b", "applied", "已清除，另有 2 个未能清除", { partial: true }),
      r("a", "applied", "已完成；注销后生效"),
    ];
    expect(issueLines(results, catalog)).toEqual([{ titles: ["乙"], message: "已清除，另有 2 个未能清除" }]);
    // Catalog order, whatever order the results came in.
    expect(completedLines(results, catalog)).toEqual([
      { titles: ["甲"], message: "已完成；注销后生效" },
      { titles: ["丙"], message: "无需处理" },
    ]);
  });

  it("groups completed results that share a message", () => {
    expect(
      completedLines([r("a", "applied", "好"), r("b", "skipped", "取消"), r("c", "applied", "好")], catalog),
    ).toEqual([{ titles: ["甲", "丙"], message: "好" }]);
  });
});

describe("fullDiskAccessBlocked", () => {
  it("is true only for a result skipped because Full Disk Access is missing", () => {
    expect(fullDiskAccessBlocked([r("databases", "skipped", "", { reason: "fullDiskAccess" })])).toBe(true);
    // Safari still running, a busy database or low disk space are not about the permission.
    expect(fullDiskAccessBlocked([r("databases", "skipped", "Safari 仍在运行，已跳过")])).toBe(false);
    expect(fullDiskAccessBlocked([r("databases", "unavailable", "")])).toBe(false);
    expect(fullDiskAccessBlocked([r("databases", "failed", "")])).toBe(false);
    expect(fullDiskAccessBlocked([r("dns", "skipped", "")])).toBe(false);
  });
});
