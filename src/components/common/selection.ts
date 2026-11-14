import type { Category, CleanRequest } from "../../mygo";

/** Which items are checked: category ID → IDs of its checked items. A category without an entry has none checked. */
export type Selection = ReadonlyMap<string, ReadonlySet<string>>;

/** How much of a category is checked, for its tri-state checkbox. */
export type CheckState = "none" | "some" | "all";

/** defaultSelection checks what the scan says starts checked: every item of a category marked selected, and each item marked selected on its own (a category may start checked in part). */
export function defaultSelection(categories: readonly Category[]): Selection {
  const sel = new Map<string, ReadonlySet<string>>();
  for (const c of categories) {
    const picked = c.items.filter((i) => c.selected || i.selected).map((i) => i.id);
    if (picked.length > 0) sel.set(c.id, new Set(picked));
  }
  return sel;
}

/** categoryState says whether none, some or all of a category's items are checked; an empty category is "none". */
export function categoryState(category: Category, sel: Selection): CheckState {
  const n = sel.get(category.id)?.size ?? 0;
  if (n === 0 || category.items.length === 0) return "none";
  return n >= category.items.length ? "all" : "some";
}

/** setCategory checks or unchecks every item of a category. */
export function setCategory(sel: Selection, category: Category, on: boolean): Selection {
  const next = new Map(sel);
  if (on && category.items.length > 0) next.set(category.id, new Set(category.items.map((i) => i.id)));
  else next.delete(category.id);
  return next;
}

/** setItem checks or unchecks one item of a category. */
export function setItem(sel: Selection, category: Category, itemId: string, on: boolean): Selection {
  const items = new Set(sel.get(category.id));
  if (on) items.add(itemId);
  else items.delete(itemId);
  const next = new Map(sel);
  if (items.size === 0) next.delete(category.id);
  else next.set(category.id, items);
  return next;
}

/** selectAll checks every item of every category. */
export function selectAll(categories: readonly Category[]): Selection {
  return defaultSelection(categories.map((c) => ({ ...c, selected: true })));
}

/** withinGroup applies next, a selection a CategoryList over group made, to group's categories only, keeping the checks of every other category: a list that shows part of a scan has 全选 and 全不选 act on its own part. */
export function withinGroup(all: Selection, group: readonly Category[], next: Selection): Selection {
  const ids = new Set(group.map((c) => c.id));
  const out = new Map<string, ReadonlySet<string>>();
  for (const [id, items] of all) if (!ids.has(id)) out.set(id, items);
  for (const [id, items] of next) if (ids.has(id)) out.set(id, items);
  return out;
}

/** The empty selection. */
export const NOTHING_SELECTED: Selection = new Map();

/** selectedTotals sums the checked items' count and size. */
export function selectedTotals(
  categories: readonly Category[],
  sel: Selection,
): { count: number; bytes: number } {
  let count = 0;
  let bytes = 0;
  for (const c of categories) {
    const ids = sel.get(c.id);
    if (!ids) continue;
    for (const item of c.items) {
      if (ids.has(item.id)) {
        count++;
        bytes += item.size;
      }
    }
  }
  return { count, bytes };
}

/** toCleanRequest turns a selection into the request Cleanup.clean takes: every category with a checked item, and the item IDs only when some category is checked in part (the Go side then deletes exactly those items). IDs no longer in the categories are left out. */
export function toCleanRequest(categories: readonly Category[], sel: Selection): CleanRequest {
  const categoryIds: string[] = [];
  const itemIds: string[] = [];
  let partial = false;
  for (const c of categories) {
    const ids = sel.get(c.id);
    if (!ids) continue;
    const picked = c.items.filter((i) => ids.has(i.id)).map((i) => i.id);
    if (picked.length === 0) continue;
    categoryIds.push(c.id);
    itemIds.push(...picked);
    if (picked.length < c.items.length) partial = true;
  }
  return partial ? { categoryIds, itemIds } : { categoryIds };
}
