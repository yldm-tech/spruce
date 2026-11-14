import { describe, expect, it } from "vite-plus/test";

import type { Category, Progress, Run, ScanResult } from "../../mygo";
import { diskLevel, lastMaintenance, maintenanceDue } from "./StatusCards";
import { overallFraction, recommendedTotals, sourceRows, summarize } from "./sources";

function category(
  id: string,
  source: string,
  selected: boolean,
  items: [string, number, boolean?][],
): Category {
  return {
    id: `${source}:${id}`,
    title: id,
    description: "",
    icon: "cache",
    kind: "cache",
    deletion: "permanent",
    selected,
    source,
    size: items.reduce((sum, [, size]) => sum + size, 0),
    items: items.map(([name, size, sel]) => ({
      id: name,
      path: `/x/${name}`,
      size,
      modTime: "",
      selected: sel,
    })),
  };
}

const SCAN: ScanResult = {
  categories: [
    category("caches", "system-junk", true, [
      ["a", 100],
      ["b", 50],
    ]),
    category("running", "system-junk", false, [["c", 30]]),
    category("support", "dev-junk", false, [
      ["old", 200, true],
      ["new", 300],
    ]),
  ],
  size: 680,
  items: 5,
  denied: 0,
  scannedAt: "2026-11-14T01:00:00Z",
  duration: 1e9,
  sources: [
    { id: "system-junk", status: "done", done: 0, total: 0, bytes: 180, items: 3, denied: 0 },
    { id: "dev-junk", status: "done", done: 0, total: 0, bytes: 500, items: 2, denied: 0 },
    {
      id: "installers",
      status: "failed",
      done: 0,
      total: 0,
      bytes: 0,
      items: 0,
      denied: 0,
      error: "没能扫描完成",
    },
  ],
};

describe("summarize", () => {
  it("splits the scan by source, in scan order, with what starts checked", () => {
    const s = summarize(SCAN);
    expect(s.map((x) => [x.id, x.items, x.bytes, x.recommendedItems, x.recommendedBytes])).toEqual([
      ["system-junk", 3, 180, 2, 150],
      ["dev-junk", 2, 500, 1, 200],
      ["installers", 0, 0, 0, 0],
    ]);
    expect(s[2]?.state?.status).toBe("failed");
    expect(recommendedTotals(SCAN)).toEqual({ count: 3, bytes: 350 });
  });
});

describe("sourceRows and overallFraction", () => {
  it("waits before the first event", () => {
    expect(sourceRows(null).map((r) => r.status)).toEqual(["waiting", "waiting", "waiting"]);
    expect(overallFraction(null)).toBeUndefined();
  });

  it("reads each source's state from the event", () => {
    const p: Progress = {
      phase: "scanning",
      done: 1250,
      total: 3000,
      bytes: 10,
      sources: [
        { id: "system-junk", status: "done", done: 4, total: 4, bytes: 10, items: 2, denied: 0 },
        { id: "dev-junk", status: "scanning", done: 1, total: 4, bytes: 0, items: 0, denied: 0 },
        { id: "installers", status: "scanning", done: 0, total: 0, bytes: 0, items: 0, denied: 0 },
      ],
    };
    expect(sourceRows(p).map((r) => [r.id, r.status, r.fraction])).toEqual([
      ["system-junk", "done", 1],
      ["dev-junk", "scanning", 0.25],
      ["installers", "scanning", undefined],
    ]);
    expect(overallFraction(p)).toBeCloseTo(1250 / 3000);
  });
});

describe("status helpers", () => {
  it("warns about a disk with little space left", () => {
    expect(diskLevel({ free: 200e9, total: 1000e9 })).toBe("ok");
    expect(diskLevel({ free: 50e9, total: 1000e9 })).toBe("low");
    expect(diskLevel({ free: 15e9, total: 100e9 })).toBe("low");
    expect(diskLevel({ free: 0, total: 0 })).toBe("ok");
  });

  it("offers 维护 when it never ran or ran more than 30 days ago", () => {
    const now = new Date("2026-11-14T00:00:00Z");
    const run = (kind: Run["kind"], endedAt: string): Run => ({
      id: endedAt,
      kind,
      startedAt: endedAt,
      endedAt,
      results: [],
    });
    const runs = [run("systemJunk", "2026-11-13T00:00:00Z"), run("maintenance", "2026-11-01T00:00:00Z")];
    expect(lastMaintenance(runs)?.endedAt).toBe("2026-11-01T00:00:00Z");
    expect(maintenanceDue(lastMaintenance(runs), now)).toBe(false);
    expect(maintenanceDue(run("maintenance", "2026-10-01T00:00:00Z"), now)).toBe(true);
    expect(maintenanceDue(null, now)).toBe(true);
  });
});
