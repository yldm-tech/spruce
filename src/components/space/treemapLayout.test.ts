import { describe, expect, it } from "vite-plus/test";

import { cellShowsLabel, layoutTreemap, TREEMAP_GAP } from "./treemapLayout";

interface Item {
  name: string;
  size: number;
}

const size = (i: Item) => i.size;

function overlaps(a: { x: number; y: number; width: number; height: number }, b: typeof a): boolean {
  return a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
}

describe("layoutTreemap", () => {
  it("gives each sized item one cell, largest first, inside the area and without overlaps", () => {
    const items: Item[] = [
      { name: "c", size: 10 },
      { name: "a", size: 60 },
      { name: "b", size: 30 },
    ];
    const cells = layoutTreemap(items, size, 400, 300);
    expect(cells.map((c) => c.item.name)).toEqual(["a", "b", "c"]);
    for (const c of cells) {
      expect(c.x).toBeGreaterThanOrEqual(0);
      expect(c.y).toBeGreaterThanOrEqual(0);
      expect(c.x + c.width).toBeLessThanOrEqual(400);
      expect(c.y + c.height).toBeLessThanOrEqual(300);
      expect(Number.isInteger(c.x) && Number.isInteger(c.width)).toBe(true);
    }
    for (let i = 0; i < cells.length; i++) {
      for (let j = i + 1; j < cells.length; j++) {
        expect(overlaps(cells[i]!, cells[j]!)).toBe(false);
      }
    }
  });

  it("keeps the areas in proportion to the sizes", () => {
    const cells = layoutTreemap(
      [
        { name: "big", size: 75 },
        { name: "small", size: 25 },
      ],
      size,
      400,
      200,
    );
    const area = (name: string) => {
      const c = cells.find((cell) => cell.item.name === name)!;
      return c.width * c.height;
    };
    // The gap takes a little of the area; the ratio stays close to 3.
    expect(area("big") / area("small")).toBeGreaterThan(2.7);
    expect(area("big") / area("small")).toBeLessThan(3.3);
    // Two cells side by side leave exactly one gap between them.
    const [first, second] = [...cells].sort((a, b) => a.x - b.x || a.y - b.y);
    const gap =
      first!.y === second!.y ? second!.x - (first!.x + first!.width) : second!.y - (first!.y + first!.height);
    expect(gap).toBe(TREEMAP_GAP);
  });

  it("draws nothing for items of no size or an empty area", () => {
    expect(layoutTreemap([{ name: "zero", size: 0 }], size, 400, 300)).toEqual([]);
    expect(layoutTreemap([{ name: "a", size: 5 }], size, 0, 300)).toEqual([]);
    expect(layoutTreemap([], size, 400, 300)).toEqual([]);
    const cells = layoutTreemap(
      [
        { name: "a", size: 5 },
        { name: "none", size: 0 },
      ],
      size,
      100,
      100,
    );
    expect(cells).toHaveLength(1);
    expect(cells[0]).toMatchObject({ x: 0, y: 0, width: 100, height: 100 });
  });

  it("labels only cells with room for a name and a size", () => {
    expect(cellShowsLabel({ width: 120, height: 60 })).toBe(true);
    expect(cellShowsLabel({ width: 40, height: 60 })).toBe(false);
    expect(cellShowsLabel({ width: 120, height: 20 })).toBe(false);
  });
});
