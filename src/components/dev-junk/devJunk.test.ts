import { describe, expect, it } from "vite-plus/test";

import type { Category } from "../../mygo";
import { cleanPlan, confirmationLines, selectionFromIds, splitGroups, withinGroup } from "./devJunk";

function category(
  id: string,
  kind: Category["kind"],
  deletion: Category["deletion"],
  items: string[],
): Category {
  return {
    id,
    title: id,
    description: "",
    icon: "cache",
    kind,
    deletion,
    selected: false,
    size: items.length * 100,
    items: items.map((i) => ({ id: i, path: `/x/${i}`, size: 100, modTime: "" })),
  };
}

const CATEGORIES = [
  category("tool-npm", "devCache", "permanent", ["npm"]),
  category("tool-xcode-device-support", "devCache", "permanent", ["ios16", "ios17"]),
  category("tool-xcode-archives", "devCache", "trash", ["arch"]),
  category("artifact-node-modules", "projectArtifact", "permanent", ["nm-old", "nm-new"]),
  category("artifact-node-modules-trash", "projectArtifact", "trash", ["nm-docs"]),
];

describe("splitGroups", () => {
  it("puts tool caches and project build output apart", () => {
    const { tools, projects } = splitGroups(CATEGORIES);
    expect(tools.map((c) => c.id)).toEqual(["tool-npm", "tool-xcode-device-support", "tool-xcode-archives"]);
    expect(projects.map((c) => c.id)).toEqual(["artifact-node-modules", "artifact-node-modules-trash"]);
  });
});

describe("selectionFromIds", () => {
  it("checks exactly the listed items, categories in part", () => {
    const sel = selectionFromIds(CATEGORIES, ["npm", "ios16", "nm-old", "gone"]);
    expect([...sel.keys()]).toEqual(["tool-npm", "tool-xcode-device-support", "artifact-node-modules"]);
    expect([...(sel.get("tool-xcode-device-support") ?? [])]).toEqual(["ios16"]);
    expect([...(sel.get("artifact-node-modules") ?? [])]).toEqual(["nm-old"]);
  });
});

describe("withinGroup", () => {
  it("changes one section and keeps the other's checks", () => {
    const { tools, projects } = splitGroups(CATEGORIES);
    const all = selectionFromIds(CATEGORIES, ["npm", "nm-old"]);
    // 全不选 in the projects list sends the empty selection.
    const next = withinGroup(all, projects, new Map());
    expect([...next.keys()]).toEqual(["tool-npm"]);
    // 全选 in the tools list sends only the tools.
    const everyTool = new Map(tools.map((c) => [c.id, new Set(c.items.map((i) => i.id))]));
    const again = withinGroup(all, tools, everyTool);
    expect([...again.keys()].sort()).toEqual(
      ["artifact-node-modules", "tool-npm", "tool-xcode-archives", "tool-xcode-device-support"].sort(),
    );
    expect([...(again.get("artifact-node-modules") ?? [])]).toEqual(["nm-old"]);
  });
});

describe("cleanPlan and confirmationLines", () => {
  it("counts commands, permanent deletions and the Trash apart", () => {
    const sel = selectionFromIds(CATEGORIES, ["npm", "ios16", "arch", "nm-old", "nm-new", "nm-docs"]);
    const plan = cleanPlan(CATEGORIES, sel, [{ categoryId: "tool-npm", command: "npm cache clean --force" }]);
    expect(plan).toEqual({
      count: 6,
      bytes: 600,
      commands: ["npm cache clean --force"],
      permanent: 3,
      trashed: 2,
    });
    const text = confirmationLines(plan);
    expect(text.summary).toBe("将清理 6 项，共 600 B。");
    expect(text.lines).toEqual([
      "运行 1 个清理命令：npm cache clean --force",
      "永久删除 3 项，无法恢复",
      "2 项移到废纸篓，可以放回",
    ]);
    expect(text.danger).toBe(true);
  });

  it("is no danger when everything goes to the Trash", () => {
    const sel = selectionFromIds(CATEGORIES, ["arch"]);
    const text = confirmationLines(cleanPlan(CATEGORIES, sel, []));
    expect(text.lines).toEqual(["1 项移到废纸篓，可以放回"]);
    expect(text.danger).toBe(false);
  });

  it("treats a command category as a command, not a deletion", () => {
    const sel = selectionFromIds(CATEGORIES, ["npm"]);
    const plan = cleanPlan(CATEGORIES, sel, [{ categoryId: "tool-npm", command: "npm cache clean --force" }]);
    expect([plan.permanent, plan.trashed, plan.commands.length]).toEqual([0, 0, 1]);
    expect(confirmationLines(plan).danger).toBe(true);
  });
});
