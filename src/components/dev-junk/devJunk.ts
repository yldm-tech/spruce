import type { Category, DevJunkCommand } from "../../mygo";
import { formatBytes, trashName } from "../common/format";
import { selectedTotals, type Selection } from "../common/selection";

/** The page's two sections: tool caches (Category.kind devCache) and the build output found in projects (projectArtifact). */
export function splitGroups(categories: readonly Category[]): { tools: Category[]; projects: Category[] } {
  return {
    tools: categories.filter((c) => c.kind !== "projectArtifact"),
    projects: categories.filter((c) => c.kind === "projectArtifact"),
  };
}

/** selectionFromIds checks exactly the items whose IDs are in ids (DevJunk.details().defaultItems), which may check a category in part. */
export function selectionFromIds(categories: readonly Category[], ids: readonly string[]): Selection {
  const wanted = new Set(ids);
  const sel = new Map<string, ReadonlySet<string>>();
  for (const c of categories) {
    const picked = c.items.filter((i) => wanted.has(i.id)).map((i) => i.id);
    if (picked.length > 0) sel.set(c.id, new Set(picked));
  }
  return sel;
}

/** withinGroup applies next, a selection a section's CategoryList made, to the section's categories only, keeping the other section's checks: each list's 全选 and 全不选 act on its own section. */
export function withinGroup(all: Selection, group: readonly Category[], next: Selection): Selection {
  const ids = new Set(group.map((c) => c.id));
  const out = new Map<string, ReadonlySet<string>>();
  for (const [id, items] of all) if (!ids.has(id)) out.set(id, items);
  for (const [id, items] of next) if (ids.has(id)) out.set(id, items);
  return out;
}

/** What a clean of the selection will do, counted the way the confirmation tells it. */
export interface CleanPlan {
  count: number;
  bytes: number;
  /** The commands that will run, as typed in a terminal. */
  commands: string[];
  /** Items deleted for good. */
  permanent: number;
  /** Items moved to the Trash. */
  trashed: number;
}

/** cleanPlan splits the selection into commands to run, items deleted for good and items moved to the Trash. */
export function cleanPlan(
  categories: readonly Category[],
  selection: Selection,
  commands: readonly DevJunkCommand[],
): CleanPlan {
  const commandOf = new Map(commands.map((c) => [c.categoryId, c.command]));
  const plan: CleanPlan = {
    ...selectedTotals(categories, selection),
    commands: [],
    permanent: 0,
    trashed: 0,
  };
  for (const c of categories) {
    const ids = selection.get(c.id);
    if (!ids) continue;
    const n = c.items.filter((i) => ids.has(i.id)).length;
    if (n === 0) continue;
    const command = commandOf.get(c.id);
    if (command !== undefined) plan.commands.push(command);
    else if (c.deletion === "trash") plan.trashed += n;
    else plan.permanent += n;
  }
  return plan;
}

/** confirmationLines words a plan for the confirmation dialog: the total, then one line per way things go. danger is set when anything cannot be undone. */
export function confirmationLines(plan: CleanPlan): { summary: string; lines: string[]; danger: boolean } {
  const bin = trashName();
  const lines: string[] = [];
  if (plan.commands.length > 0)
    lines.push(`运行 ${plan.commands.length} 个清理命令：${plan.commands.join("、")}`);
  if (plan.permanent > 0) lines.push(`永久删除 ${plan.permanent} 项，无法恢复`);
  if (plan.trashed > 0) lines.push(`${plan.trashed} 项移到${bin}，可以放回`);
  return {
    summary: `将清理 ${plan.count} 项，共 ${formatBytes(plan.bytes)}。`,
    lines,
    danger: plan.permanent > 0 || plan.commands.length > 0,
  };
}
