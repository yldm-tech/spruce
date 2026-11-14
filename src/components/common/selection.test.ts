import { describe, expect, it } from "vite-plus/test";

import type { Category, Item } from "../../mygo";
import {
  categoryState,
  defaultSelection,
  NOTHING_SELECTED,
  selectAll,
  selectedTotals,
  setCategory,
  setItem,
  toCleanRequest,
} from "./selection";

function item(id: string, size: number): Item {
  return { id, path: `/x/${id}`, size, modTime: "2026-11-01T00:00:00Z" };
}

function category(id: string, selected: boolean, items: Item[]): Category {
  return {
    id,
    title: id,
    description: "",
    icon: "cache",
    kind: "cache",
    deletion: "permanent",
    items,
    size: items.reduce((s, i) => s + i.size, 0),
    selected,
  };
}

const cats = [
  category("caches", true, [item("a", 100), item("b", 50)]),
  category("logs", false, [item("c", 10)]),
  category("empty", true, []),
];

describe("selection", () => {
  it("starts from the scan's defaults", () => {
    const sel = defaultSelection(cats);
    expect(categoryState(cats[0]!, sel)).toBe("all");
    expect(categoryState(cats[1]!, sel)).toBe("none");
    expect(categoryState(cats[2]!, sel)).toBe("none");
    expect(selectedTotals(cats, sel)).toEqual({ count: 2, bytes: 150 });
  });

  it("checks the items the scan marked on their own, a category in part", () => {
    const part = category("support", false, [{ ...item("old", 30), selected: true }, item("newest", 40)]);
    const sel = defaultSelection([part]);
    expect(categoryState(part, sel)).toBe("some");
    expect([...(sel.get("support") ?? [])]).toEqual(["old"]);
  });

  it("toggles items and categories", () => {
    let sel = setItem(defaultSelection(cats), cats[0]!, "b", false);
    expect(categoryState(cats[0]!, sel)).toBe("some");
    sel = setItem(sel, cats[0]!, "a", false);
    expect(sel.has("caches")).toBe(false);
    sel = setCategory(sel, cats[1]!, true);
    expect(selectedTotals(cats, sel)).toEqual({ count: 1, bytes: 10 });
    expect(categoryState(cats[1]!, setCategory(sel, cats[1]!, false))).toBe("none");
    expect(selectedTotals(cats, selectAll(cats))).toEqual({ count: 3, bytes: 160 });
    expect(selectedTotals(cats, NOTHING_SELECTED)).toEqual({ count: 0, bytes: 0 });
  });

  it("builds the clean request with item IDs only for a partial selection", () => {
    expect(toCleanRequest(cats, selectAll(cats))).toEqual({ categoryIds: ["caches", "logs"] });
    const partial = setItem(selectAll(cats), cats[0]!, "a", false);
    expect(toCleanRequest(cats, partial)).toEqual({ categoryIds: ["caches", "logs"], itemIds: ["b", "c"] });
    // A stale ID from an older scan is dropped.
    const stale = new Map([["caches", new Set(["gone"])]]);
    expect(toCleanRequest(cats, stale)).toEqual({ categoryIds: [] });
  });
});
