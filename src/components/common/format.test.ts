import { describe, expect, it } from "vite-plus/test";

import {
  baseName,
  formatBytes,
  formatDuration,
  formatRelativeTime,
  parentPath,
  truncateMiddle,
} from "./format";

describe("formatBytes", () => {
  it("counts in 1000s like Finder by default outside Windows", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(999)).toBe("999 B");
    expect(formatBytes(1500)).toBe("2 KB");
    expect(formatBytes(1_234_567)).toBe("1.2 MB");
    expect(formatBytes(4_800_000_000)).toBe("4.8 GB");
    expect(formatBytes(-5)).toBe("0 B");
    expect(formatBytes(Number.NaN)).toBe("0 B");
  });

  it("counts in 1024s like Explorer when binary", () => {
    expect(formatBytes(1024, { binary: true })).toBe("1 KB");
    expect(formatBytes(1024 ** 3 * 1.5, { binary: true })).toBe("1.5 GB");
  });
});

describe("formatDuration", () => {
  it("renders nanoseconds in Chinese", () => {
    expect(formatDuration(0)).toBe("不到 1 秒");
    expect(formatDuration(12e9)).toBe("12 秒");
    expect(formatDuration(200e9)).toBe("3 分 20 秒");
    expect(formatDuration(180e9)).toBe("3 分钟");
    expect(formatDuration(725e9)).toBe("12 分钟");
    expect(formatDuration(3900e9)).toBe("1 小时 5 分");
    expect(formatDuration(7200e9)).toBe("2 小时");
  });
});

describe("formatRelativeTime", () => {
  const now = new Date(2026, 10, 14, 12, 0, 0);
  const ago = (seconds: number) => new Date(now.getTime() - seconds * 1000);

  it("says how long ago", () => {
    expect(formatRelativeTime(ago(10), now)).toBe("刚刚");
    expect(formatRelativeTime(ago(5 * 60), now)).toBe("5分钟前");
    expect(formatRelativeTime(ago(3 * 3600), now)).toBe("3小时前");
    expect(formatRelativeTime(new Date(2026, 10, 13, 9, 0), now)).toBe("昨天");
    expect(formatRelativeTime(new Date(2026, 10, 11, 9, 0), now)).toBe("3天前");
  });

  it("falls back to the date", () => {
    expect(formatRelativeTime(new Date(2026, 9, 2), now)).toBe("10月2日");
    expect(formatRelativeTime(new Date(2025, 10, 2), now)).toBe("2025年11月2日");
  });

  it("ignores Go's zero time and invalid input", () => {
    expect(formatRelativeTime("0001-01-01T00:00:00Z", now)).toBe("");
    expect(formatRelativeTime("not a time", now)).toBe("");
  });
});

describe("paths", () => {
  it("truncates the middle and splits names", () => {
    expect(truncateMiddle("short", 10)).toBe("short");
    const t = truncateMiddle("/Users/me/Library/Caches/com.example.app/data.bin", 24);
    expect(t).toHaveLength(24);
    expect(t.endsWith("data.bin")).toBe(true);
    expect(baseName("/a/b/c.txt")).toBe("c.txt");
    expect(baseName("C:\\a\\b\\")).toBe("b");
    expect(parentPath("/a/b/c.txt")).toBe("/a/b");
    expect(parentPath("/a")).toBe("/");
    expect(parentPath("C:\\a\\b")).toBe("C:\\a");
  });
});
