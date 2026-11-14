import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import type { Category, Files } from "../../mygo";
import { CategoryList } from "./CategoryList";
import { CATEGORY_ICONS } from "./CategoryIcon";
import { defaultSelection, type Selection } from "./selection";

const fakes = vi.hoisted(() => ({ Files: { reveal: vi.fn<typeof Files.reveal>() } }));
vi.mock("../../mygo", () => ({ Files: fakes.Files }));

const CATEGORIES: Category[] = [
  {
    id: "caches",
    title: "应用缓存",
    description: "应用临时保存的数据，删除后会自动重建",
    icon: "cache",
    kind: "cache",
    deletion: "permanent",
    selected: true,
    size: 3_000_000,
    items: [
      {
        id: "a",
        path: "/Users/me/Library/Caches/com.example.a",
        size: 2_000_000,
        modTime: "2026-11-01T00:00:00Z",
        dir: true,
      },
      {
        id: "b",
        path: "/Users/me/Library/Caches/com.example.b",
        size: 1_000_000,
        modTime: "2026-11-01T00:00:00Z",
        note: "3 个月未修改",
      },
    ],
  },
  {
    id: "logs",
    title: "日志",
    description: "应用和系统的运行记录",
    warning: "排查问题时可能用得到",
    icon: "log",
    kind: "log",
    deletion: "permanent",
    selected: false,
    size: 500_000,
    items: [
      { id: "c", path: "/Users/me/Library/Logs/x.log", size: 500_000, modTime: "2026-11-01T00:00:00Z" },
    ],
  },
];

function Harness({ onChange }: { onChange?: (s: Selection) => void }) {
  const [sel, setSel] = useState<Selection>(() => defaultSelection(CATEGORIES));
  return (
    <CategoryList
      label="可以清理的项目"
      categories={CATEGORIES}
      selection={sel}
      onSelectionChange={(next) => {
        setSel(next);
        onChange?.(next);
      }}
    />
  );
}

function renderList() {
  const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <Harness />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.resetAllMocks();
});

describe("CategoryList", () => {
  it("shows each category with its default check, size and warning", () => {
    renderList();
    const caches = screen.getByRole("checkbox", { name: "应用缓存" });
    const logs = screen.getByRole("checkbox", { name: "日志" });
    expect(caches).toHaveProperty("checked", true);
    expect(logs).toHaveProperty("checked", false);
    expect(screen.getByText("排查问题时可能用得到")).toBeTruthy();
    expect(screen.queryByText("应用和系统的运行记录")).toBeNull();
    expect(screen.getByText("3 MB")).toBeTruthy();
    expect(screen.getByText("2 类，共 3.5 MB，已选 2 项")).toBeTruthy();
  });

  it("expands to items, checks them one by one and shows the partial state", async () => {
    const user = userEvent.setup();
    renderList();
    await user.click(screen.getByRole("button", { name: "展开应用缓存" }));
    const list = screen.getByRole("list", { name: "应用缓存" });
    expect(within(list).getAllByRole("listitem")).toHaveLength(2);
    expect(within(list).getByText("3 个月未修改", { exact: false })).toBeTruthy();
    await user.click(within(list).getByRole("checkbox", { name: "com.example.a" }));
    const caches = screen.getByRole("checkbox", { name: "应用缓存" });
    expect(caches.getAttribute("aria-checked")).toBe("mixed");
    expect((caches as HTMLInputElement).indeterminate).toBe(true);
    expect(screen.getByText("已选 1/2 项")).toBeTruthy();
    // Checking the partial category checks all of it.
    await user.click(caches);
    expect(caches).toHaveProperty("checked", true);
    expect(screen.getByRole("button", { name: "收起应用缓存" }).getAttribute("aria-expanded")).toBe("true");
  });

  it("selects all and none", async () => {
    const user = userEvent.setup();
    renderList();
    await user.click(screen.getByRole("button", { name: "全选" }));
    expect(screen.getByText("2 类，共 3.5 MB，已选 3 项")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "全不选" }));
    for (const c of screen.getAllByRole("checkbox")) expect(c).toHaveProperty("checked", false);
  });

  it("reveals an item in the file manager and reports a failure", async () => {
    const user = userEvent.setup();
    fakes.Files.reveal.mockRejectedValue(new Error("这个项目已经不存在了"));
    renderList();
    await user.click(screen.getByRole("button", { name: "展开日志" }));
    await user.click(screen.getByRole("button", { name: "在访达中显示 x.log" }));
    expect(fakes.Files.reveal).toHaveBeenCalledWith("/Users/me/Library/Logs/x.log");
    expect((await screen.findByRole("alert")).textContent).toBe("这个项目已经不存在了");
  });

  it("draws every documented icon key", () => {
    expect(CATEGORY_ICONS).toEqual(
      expect.arrayContaining(["cache", "log", "trash", "node", "xcode", "installer", "leftover", "folder"]),
    );
  });
});
