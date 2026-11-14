import type { Category, CleanResult } from "../../mygo";
import { baseName, formatBytes, trashName } from "./format";
import { selectedTotals, type Selection } from "./selection";

/** How many failed items the done card names one by one. */
const MAX_FAILURE_LINES = 8;

/** What the done card of a clean shows. */
export interface CleanSummary {
  title: string;
  big: string;
  lines: string[];
  succeeded: boolean;
}

/** cleanSummary turns a clean's result, or the sentence of a clean that could not start, into the done card: the space given back, a line per category, the items left in place, and what the Trash means. */
export function cleanSummary(outcome: { result: CleanResult } | { error: string }): CleanSummary {
  if ("error" in outcome) {
    return { title: "清理没有进行", big: formatBytes(0), lines: [outcome.error], succeeded: false };
  }
  const r = outcome.result;
  const bin = trashName();
  const lines: string[] = [];
  for (const c of r.categories) {
    const parts: string[] = [];
    if (c.removed > 0) {
      parts.push(
        c.trashed > 0
          ? `${c.removed} 项已移到${bin}（${formatBytes(c.trashed)}）`
          : `已清理 ${c.removed} 项，释放 ${formatBytes(c.freed)}`,
      );
    }
    if (c.failed > 0) parts.push(`${c.failed} 项未能处理`);
    if (parts.length > 0) lines.push(`${c.title}：${parts.join("，")}`);
  }
  for (const f of r.failed.slice(0, MAX_FAILURE_LINES)) lines.push(`${baseName(f.path)}：${f.reason}`);
  if (r.failed.length > MAX_FAILURE_LINES)
    lines.push(`另有 ${r.failed.length - MAX_FAILURE_LINES} 项未能处理`);
  if (r.skipped > 0) lines.push(`${r.skipped} 项在扫描后已不存在，已跳过`);
  if (r.trashed > 0) lines.push(`移到${bin}的项目在清空${bin}后才会释放空间`);
  if (r.interrupted) lines.push("清理在中途停止，其余项目没有改动");

  let title = "清理完成";
  if (r.interrupted) title = "清理已中断";
  else if (r.removed === 0) title = "清理未完成";
  return { title, big: formatBytes(r.freed + r.trashed), lines, succeeded: r.removed > 0 };
}

/** cleanConfirmation is the question asked before a clean: how much goes, and whether it can be undone. danger is set when anything is deleted for good. */
export function cleanConfirmation(
  categories: readonly Category[],
  selection: Selection,
): { message: string; danger: boolean } {
  const { count, bytes } = selectedTotals(categories, selection);
  let permanent = 0;
  for (const c of categories) {
    if (c.deletion !== "permanent") continue;
    const ids = selection.get(c.id);
    if (ids) permanent += c.items.filter((i) => ids.has(i.id)).length;
  }
  const bin = trashName();
  let tail: string;
  if (permanent === 0) tail = `它们会移到${bin}，可以随时放回。`;
  else if (permanent === count) tail = "删除后无法恢复。";
  else tail = `其中 ${permanent} 项会永久删除，无法恢复；其余移到${bin}。`;
  return { message: `将清理 ${count} 项，共 ${formatBytes(bytes)}。${tail}`, danger: permanent > 0 };
}
