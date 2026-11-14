import { describe, expect, it } from "vite-plus/test";

import type { SystemInfo, Vitals } from "../../mygo";
import {
  appendSample,
  batteryStateText,
  batteryTimeText,
  chartData,
  coresText,
  formatBootTime,
  formatLoad,
  formatMinutes,
  formatPercent,
  formatProcessCPU,
  formatRate,
  formatUptime,
  HISTORY_LENGTH,
  osText,
  pressureText,
} from "./format";

function vitals(at: string, extra: Partial<Vitals> = {}): Vitals {
  return {
    at,
    cpu: null,
    memory: null,
    disk: null,
    diskIo: null,
    network: null,
    battery: null,
    processes: null,
    ...extra,
  };
}

function info(extra: Partial<SystemInfo> = {}): SystemInfo {
  return {
    osName: "macOS",
    osVersion: "26.1",
    osBuild: "25B78",
    model: "Mac16,8",
    cpuModel: "Apple M4 Max",
    physicalCores: 16,
    logicalCores: 16,
    ...extra,
  };
}

describe("number formatting", () => {
  it("rounds percentages and keeps a decimal for small process CPU figures", () => {
    expect(formatPercent(23.4)).toBe("23%");
    expect(formatPercent(-1)).toBe("0%");
    expect(formatProcessCPU(0.44)).toBe("0.4%");
    expect(formatProcessCPU(9.96)).toBe("10.0%");
    expect(formatProcessCPU(12.6)).toBe("13%");
    // One logical CPU is 100 %, so a multi-threaded process passes it.
    expect(formatProcessCPU(250)).toBe("250%");
  });

  it("renders throughput with the file manager's units", () => {
    expect(formatRate(0)).toBe("0 B/s");
    expect(formatRate(1_234_567)).toBe("1.2 MB/s");
  });

  it("renders the load average", () => {
    expect(formatLoad({ one: 2.314, five: 1.98, fifteen: 1.5 })).toBe("2.31 / 1.98 / 1.50");
  });
});

describe("time formatting", () => {
  it("renders battery estimates", () => {
    expect(formatMinutes(45)).toBe("45 分钟");
    expect(formatMinutes(125)).toBe("2 小时 5 分钟");
    expect(formatMinutes(180)).toBe("3 小时");
  });

  it("renders uptime from minutes to days", () => {
    expect(formatUptime(30_000)).toBe("不到 1 分钟");
    expect(formatUptime(12 * 60_000)).toBe("12 分钟");
    expect(formatUptime((5 * 60 + 3) * 60_000)).toBe("5 小时 3 分钟");
    expect(formatUptime(2 * 3600_000)).toBe("2 小时");
    expect(formatUptime((3 * 24 + 4) * 3600_000 + 59 * 60_000)).toBe("3 天 4 小时");
    expect(formatUptime(2 * 24 * 3600_000)).toBe("2 天");
  });

  it("renders the boot time and ignores unknown ones", () => {
    expect(formatBootTime(new Date(2026, 10, 11, 9, 5).toISOString())).toBe("11月11日 09:05");
    expect(formatBootTime(undefined)).toBe("");
    expect(formatBootTime("0001-01-01T00:00:00Z")).toBe("");
    expect(formatBootTime("garbage")).toBe("");
  });
});

describe("battery and memory wording", () => {
  it("describes each battery state", () => {
    expect(batteryStateText("charging")).toBe("正在充电");
    expect(batteryStateText("discharging")).toBe("正在使用电池");
    expect(batteryStateText("full")).toBe("已充满");
    expect(batteryStateText("notCharging")).toBe("已接通电源，未在充电");
    expect(batteryStateText("unknown")).toBe("");
  });

  it("states the time estimate only where it means something", () => {
    expect(batteryTimeText("discharging", 185)).toBe("剩余约 3 小时 5 分钟");
    expect(batteryTimeText("charging", 38)).toBe("充满还需 38 分钟");
    expect(batteryTimeText("charging", undefined)).toBe("");
    expect(batteryTimeText("full", 30)).toBe("");
  });

  it("names memory pressure levels", () => {
    expect(pressureText("normal")).toBe("正常");
    expect(pressureText("warning")).toBe("偏高");
    expect(pressureText("critical")).toBe("紧张");
  });
});

describe("system wording", () => {
  it("describes cores", () => {
    expect(
      coresText(info({ physicalCores: 12, logicalCores: 12, performanceCores: 8, efficiencyCores: 4 })),
    ).toBe("12 核（8 性能核 + 4 能效核）");
    expect(coresText(info({ physicalCores: 8, logicalCores: 16 }))).toBe("8 核 16 线程");
    expect(coresText(info({ physicalCores: 0, logicalCores: 4 }))).toBe("4 线程");
    expect(coresText(info({ physicalCores: 0, logicalCores: 0 }))).toBe("");
  });

  it("names the OS with version and build", () => {
    expect(osText(info())).toBe("macOS 26.1");
    expect(osText(info({ osName: "Windows 11 Pro", osVersion: "24H2", osBuild: "26100.2314" }))).toBe(
      "Windows 11 Pro 24H2",
    );
    expect(osText(info({ osBuild: "" }))).toBe("macOS 26.1");
    expect(osText(info({ osName: "", osVersion: "", osBuild: "" }))).toBe("");
  });
});

describe("chartData", () => {
  it("turns samples into uPlot columns with nulls for missing values", () => {
    const samples = [
      vitals("2026-11-14T09:00:00Z", { cpu: { percent: 10, perCore: [] } }),
      vitals("2026-11-14T09:00:02Z"),
      vitals("2026-11-14T09:00:04Z", { cpu: { percent: 30, perCore: [] } }),
    ];
    const t = Date.parse("2026-11-14T09:00:00Z") / 1000;
    expect(chartData(samples, [(v) => v.cpu?.percent])).toEqual([
      [t, t + 2, t + 4],
      [10, null, 30],
    ]);
  });

  it("breaks the line across a gap in sampling", () => {
    const samples = [
      vitals("2026-11-14T09:00:00Z", { cpu: { percent: 10, perCore: [] } }),
      vitals("2026-11-14T09:01:00Z", { cpu: { percent: 20, perCore: [] } }),
      vitals("2026-11-14T09:01:02Z", { cpu: { percent: 30, perCore: [] } }),
    ];
    const t = Date.parse("2026-11-14T09:00:00Z") / 1000;
    const [xs, ys] = chartData(samples, [(v) => v.cpu?.percent]);
    expect(xs).toEqual([t, t + 30, t + 60, t + 62]);
    expect(ys).toEqual([10, null, 20, 30]);
  });

  it("keeps one column per picker", () => {
    const [xs, read, write] = chartData(
      [vitals("2026-11-14T09:00:00Z", { diskIo: { readPerSec: 5, writePerSec: 7 } })],
      [(v) => v.diskIo?.readPerSec, (v) => v.diskIo?.writePerSec],
    );
    expect([xs?.length, read, write]).toEqual([1, [5], [7]]);
  });
});

describe("appendSample", () => {
  it("drops samples it already has, as when Go resends its history", () => {
    const a = vitals("2026-11-14T09:00:00Z");
    const b = vitals("2026-11-14T09:00:02Z");
    const list = appendSample(appendSample([], a), b);
    expect(appendSample(list, a)).toBe(list);
    expect(appendSample(list, vitals("2026-11-14T09:00:02Z"))).toBe(list);
    expect(appendSample(list, vitals("2026-11-14T09:00:04Z")).map((v) => v.at)).toEqual([
      "2026-11-14T09:00:00Z",
      "2026-11-14T09:00:02Z",
      "2026-11-14T09:00:04Z",
    ]);
  });

  it("keeps the newest two minutes", () => {
    let list: Vitals[] = [];
    const start = Date.parse("2026-11-14T09:00:00Z");
    for (let i = 0; i < HISTORY_LENGTH + 5; i++) {
      list = appendSample(list, vitals(new Date(start + i * 2000).toISOString()));
    }
    expect(list).toHaveLength(HISTORY_LENGTH);
    expect(list[0]?.at).toBe(new Date(start + 5 * 2000).toISOString());
  });
});
