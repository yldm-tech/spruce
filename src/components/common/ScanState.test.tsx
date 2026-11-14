import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vite-plus/test";

import type { Category, CleanResult } from "../../mygo";
import { ShellBlockProvider, useShellBlocked } from "../ShellBlock";
import { PAGES } from "./pages";
import { CleanConfirmDialog, CleanDoneDialog, CleaningOverlay, ScanIdle, ScanProgress } from "./ScanState";

function Shell() {
  return <span data-testid="shell">{String(useShellBlocked())}</span>;
}

describe("ScanIdle", () => {
  it("offers the scan and shows the last error", async () => {
    const user = userEvent.setup();
    const onStart = vi.fn<() => void>();
    const meta = PAGES["system-junk"];
    render(
      <ScanIdle
        tone={meta.tone}
        icon={meta.icon}
        title="扫描系统垃圾"
        description="查找缓存和日志"
        onStart={onStart}
        error="扫描没能完成"
      />,
    );
    expect(screen.getByRole("alert").textContent).toBe("扫描没能完成");
    await user.click(screen.getByRole("button", { name: "扫描" }));
    expect(onStart).toHaveBeenCalledTimes(1);
  });
});

describe("ScanProgress", () => {
  it("fills the ring once the total is known and stops on request", async () => {
    const user = userEvent.setup();
    const onCancel = vi.fn<() => void>();
    const { rerender } = render(<ScanProgress progress={null} onCancel={onCancel} />);
    expect(screen.getByRole("progressbar", { name: "正在扫描…" }).hasAttribute("aria-valuenow")).toBe(false);
    expect(screen.getByText("正在准备…")).toBeTruthy();
    rerender(
      <ScanProgress
        progress={{
          phase: "scanning",
          path: "/Users/me/Library/Caches",
          done: 3,
          total: 4,
          bytes: 2_500_000,
        }}
        onCancel={onCancel}
      />,
    );
    expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("75");
    expect(screen.getByText("75%")).toBeTruthy();
    expect(screen.getByText("已找到 2.5 MB")).toBeTruthy();
    expect(screen.getByText("/Users/me/Library/Caches")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "停止" }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});

describe("CleaningOverlay", () => {
  it("blocks the shell, takes focus and counts what it freed", () => {
    render(
      <ShellBlockProvider>
        <Shell />
        <CleaningOverlay
          sub="共 4 项"
          progress={{ phase: "cleaning", path: "/x/a", done: 1, total: 4, bytes: 1_000_000 }}
        />
      </ShellBlockProvider>,
    );
    const overlay = screen.getByRole("group", { name: "正在清理…" });
    expect(document.activeElement).toBe(overlay);
    expect(screen.getByTestId("shell").textContent).toBe("true");
    expect(screen.getByText("已处理 1/4 · 已释放 1 MB")).toBeTruthy();
  });
});

describe("CleanDoneDialog and CleanConfirmDialog", () => {
  it("summarises a clean", () => {
    const result: CleanResult = {
      freed: 2_000_000,
      trashed: 0,
      removed: 2,
      skipped: 0,
      failed: [],
      categories: [
        { categoryId: "c", title: "应用缓存", removed: 2, freed: 2_000_000, trashed: 0, failed: 0 },
      ],
      partial: false,
      interrupted: false,
      startedAt: "",
      duration: 0,
    };
    render(<CleanDoneDialog outcome={{ result }} onClose={() => {}} />);
    const dialog = screen.getByRole("dialog", { name: "清理完成" });
    expect(dialog.textContent).toContain("2 MB");
    expect(dialog.textContent).toContain("应用缓存：已清理 2 项，释放 2 MB");
  });

  it("asks with the danger tone before deleting for good", () => {
    const category: Category = {
      id: "c",
      title: "应用缓存",
      description: "",
      icon: "cache",
      kind: "cache",
      deletion: "permanent",
      selected: true,
      size: 1_000_000,
      items: [{ id: "i", path: "/x/i", size: 1_000_000, modTime: "" }],
    };
    render(
      <CleanConfirmDialog
        categories={[category]}
        selection={new Map([["c", new Set(["i"])]])}
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );
    expect(screen.getByRole("dialog", { name: "确认清理" }).textContent).toContain("删除后无法恢复");
    // Danger: 取消 has focus first.
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "取消" }));
  });
});
