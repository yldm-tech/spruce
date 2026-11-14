import type { Category, CleanResult } from "../../mygo";
import type { Selection } from "./selection";

/** One category's share of a clean: how many of its items are checked and their size. */
export interface PlanLine {
  id: string;
  title: string;
  count: number;
  bytes: number;
  /** The tool command that cleans the category (Category.command), for the commands group. */
  command?: string;
}

/** cleanPlan splits the checked items by what happens to them: cleaned by the tool's own command, deleted for good, or moved to the Trash, one line per category in list order. */
export function cleanPlan(
  categories: readonly Category[],
  selection: Selection,
): { permanent: PlanLine[]; trash: PlanLine[]; commands: PlanLine[] } {
  const permanent: PlanLine[] = [];
  const trash: PlanLine[] = [];
  const commands: PlanLine[] = [];
  for (const c of categories) {
    const ids = selection.get(c.id);
    if (!ids) continue;
    const picked = c.items.filter((i) => ids.has(i.id));
    if (picked.length === 0) continue;
    const line = {
      id: c.id,
      title: c.title,
      count: picked.length,
      bytes: picked.reduce((sum, i) => sum + i.size, 0),
    };
    if (c.command) commands.push({ ...line, command: c.command });
    else (c.deletion === "permanent" ? permanent : trash).push(line);
  }
  return { permanent, trash, commands };
}

/** setInUseAside takes the items another program held open out of a clean's failures: they are not errors the user must act on but files to try again later, counted on their own. The categories' failure counts drop to match. */
export function setInUseAside(result: CleanResult): { result: CleanResult; inUse: number } {
  const inUse = result.failed.filter((f) => f.inUse);
  if (inUse.length === 0) return { result, inUse: 0 };
  const byCategory = new Map<string, number>();
  for (const f of inUse) byCategory.set(f.categoryId, (byCategory.get(f.categoryId) ?? 0) + 1);
  return {
    result: {
      ...result,
      failed: result.failed.filter((f) => !f.inUse),
      categories: result.categories.map((c) => ({
        ...c,
        failed: Math.max(0, c.failed - (byCategory.get(c.categoryId) ?? 0)),
      })),
    },
    inUse: inUse.length,
  };
}
