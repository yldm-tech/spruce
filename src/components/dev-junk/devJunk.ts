import type { Category } from "../../mygo";
import { formatBytes, trashName } from "../common/format";
import { selectedTotals, type Selection } from "../common/selection";

/** The page's two sections: tool caches (Category.kind devCache) and the build output found in projects (projectArtifact). */
export function splitGroups(categories: readonly Category[]): { tools: Category[]; projects: Category[] } {
  return {
    tools: categories.filter((c) => c.kind !== "projectArtifact"),
    projects: categories.filter((c) => c.kind === "projectArtifact"),
  };
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

/** cleanPlan splits the selection into commands to run (categories with a command), items deleted for good and items moved to the Trash. */
export function cleanPlan(categories: readonly Category[], selection: Selection): CleanPlan {
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
    if (c.command) plan.commands.push(c.command);
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
