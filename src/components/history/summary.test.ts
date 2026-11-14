import { describe, expect, it } from "vite-plus/test";

import type { Result, Run, TaskView } from "../../mygo";
import {
  dayLabel,
  formatClock,
  groupByDay,
  kindMeta,
  kindsIn,
  runDuration,
  runSummary,
  taskLines,
  totalFreed,
} from "./summary";

function at(y: number, m: number, d: number, h = 9, min = 30): string {
  return new Date(y, m - 1, d, h, min).toISOString();
}

function run(id: string, kind: Run["kind"], startedAt: string, extra: Partial<Run> = {}): Run {
  return { id, kind, startedAt, endedAt: startedAt, results: [], ...extra };
}

function result(taskId: string, outcome: Result["outcome"], message: string, partial?: boolean): Result {
  return { taskId, outcome, message, duration: 0, ...(partial ? { partial } : {}) };
}

const NOW = new Date(2026, 10, 14, 15, 0);

describe("groupByDay", () => {
  it("groups by local day as 今天, 昨天 and the date, keeping the order", () => {
    const runs = [
      run("a", "maintenance", at(2026, 11, 14, 14)),
      run("b", "systemJunk", at(2026, 11, 14, 0, 5)),
      run("c", "devJunk", at(2026, 11, 13, 23, 50)),
      run("d", "installers", at(2026, 11, 2)),
      run("e", "space", at(2025, 11, 2)),
    ];
    const groups = groupByDay(runs, NOW);
    expect(groups.map((g) => [g.label, g.runs.map((r) => r.id).join("")])).toEqual([
      ["今天", "ab"],
      ["昨天", "c"],
      ["11月2日", "d"],
      ["2025年11月2日", "e"],
    ]);
    expect(new Set(groups.map((g) => g.key)).size).toBe(4);
  });

  it("knows yesterday across a month boundary", () => {
    expect(dayLabel(new Date(2026, 10, 30, 22), new Date(2026, 11, 1, 1))).toBe("昨天");
    expect(dayLabel(new Date(2026, 10, 29, 22), new Date(2026, 11, 1, 1))).toBe("11月29日");
  });
});

describe("runSummary", () => {
  it("counts completed and unfinished maintenance tasks", () => {
    const results = [
      result("dns", "applied", "已刷新 DNS 缓存"),
      result("icons", "applied", "需要重新登录", true),
      result("quicklook", "unchanged", "无需处理"),
      result("databases", "skipped", "Safari 仍在运行，已跳过"),
      result("spotlight", "failed", "失败了"),
    ];
    expect(runSummary(run("m", "maintenance", at(2026, 11, 14), { results }))).toBe("完成 3 项，2 项未完成");
    expect(runSummary(run("m", "maintenance", at(2026, 11, 14), { results: results.slice(0, 3) }))).toBe(
      "完成 3 项",
    );
  });

  it("reports freed space, the Trash and failures of a clean-up", () => {
    const t = at(2026, 11, 14);
    expect(runSummary(run("s", "systemJunk", t, { summary: { freed: 1_234_000_000, items: 34, failed: 2 } }))).toBe(
      "释放 1.2 GB，34 项，2 项未能处理",
    );
    expect(runSummary(run("u", "uninstall", t, { summary: { freed: 0, trashed: 300_000_000, items: 3, failed: 0 } }))).toBe(
      "移到废纸篓 300 MB，3 项",
    );
    expect(
      runSummary(run("x", "smartScan", t, { summary: { freed: 5_000_000, trashed: 2_000_000, items: 9, failed: 0 } })),
    ).toBe("释放 5 MB，移到废纸篓 2 MB，9 项");
    expect(runSummary(run("z", "space", t, { summary: { freed: 0, items: 0, failed: 1 } }))).toBe(
      "释放 0 B，0 项，1 项未能处理",
    );
  });
});

describe("totals and kinds", () => {
  it("adds up the space clean-ups freed, not what went to the Trash", () => {
    const t = at(2026, 11, 14);
    const runs = [
      run("m", "maintenance", t),
      run("a", "systemJunk", t, { summary: { freed: 1_000_000, items: 1, failed: 0 } }),
      run("b", "uninstall", t, { summary: { freed: 0, trashed: 9_000_000, items: 1, failed: 0 } }),
      run("c", "devJunk", t, { summary: { freed: 2_000_000, items: 1, failed: 0 } }),
    ];
    expect(totalFreed(runs)).toEqual({ bytes: 3_000_000, cleanups: 3 });
    expect(totalFreed([runs[0] as Run])).toEqual({ bytes: 0, cleanups: 0 });
  });

  it("lists the kinds present in the sidebar's order", () => {
    const t = at(2026, 11, 14);
    expect(kindsIn([run("1", "maintenance", t), run("2", "space", t), run("3", "smartScan", t), run("4", "space", t)])).toEqual([
      "smartScan",
      "space",
      "maintenance",
    ]);
  });

  it("names kinds after their pages and copes with unknown ones", () => {
    expect(kindMeta("devJunk").title).toBe("开发者垃圾");
    expect(kindMeta("maintenance").title).toBe("维护");
    expect(kindMeta("future" as Run["kind"]).title).toBe("其他操作");
  });
});

describe("taskLines", () => {
  const catalog: TaskView[] = [
    { id: "dns", title: "刷新 DNS 缓存", description: "", icon: "network", defaultOn: true, needsAdmin: true, estimatedSeconds: 5 },
    { id: "icons", title: "重建图标缓存", description: "", icon: "icons", defaultOn: true, needsAdmin: true, estimatedSeconds: 5 },
    { id: "databases", title: "优化应用数据库", description: "", icon: "database", defaultOn: false, needsAdmin: false, estimatedSeconds: 5 },
  ];

  it("puts the tasks needing attention first, then catalog order, with each task's status and message", () => {
    const lines = taskLines(
      [
        result("dns", "applied", "已刷新 DNS 缓存"),
        result("icons", "applied", "注销后生效", true),
        result("databases", "skipped", "Safari 仍在运行，已跳过"),
        result("gone", "unavailable", "这台电脑上没有这项"),
      ],
      catalog,
    );
    expect(lines.map((l) => [l.title, l.status, l.message, l.attention])).toEqual([
      ["重建图标缓存", "部分完成", "注销后生效", true],
      ["优化应用数据库", "已跳过", "Safari 仍在运行，已跳过", true],
      ["gone", "不可用", "这台电脑上没有这项", true],
      ["刷新 DNS 缓存", "已完成", "已刷新 DNS 缓存", false],
    ]);
  });
});

describe("times", () => {
  it("formats the clock and the duration", () => {
    const start = new Date(2026, 10, 14, 9, 5, 7);
    const r = run("a", "maintenance", start.toISOString(), {
      endedAt: new Date(start.getTime() + 28_000).toISOString(),
    });
    expect(formatClock(r.startedAt)).toBe("09:05");
    expect(formatClock(r.startedAt, true)).toBe("09:05:07");
    expect(runDuration(r)).toBe("28 秒");
  });
});
