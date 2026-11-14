import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vite-plus/test";

import { ShellBlockProvider, useShellBlocked } from "../ShellBlock";
import { ConfirmDialog } from "./ConfirmDialog";
import { DoneDialog } from "./DoneDialog";
import { OverlayHost } from "./OverlayHost";
import { Page } from "./Page";
import { ProgressRing } from "./ProgressRing";

function Shell() {
  return <span data-testid="shell">{String(useShellBlocked())}</span>;
}

/** A page with a button that opens a confirmation, drawn inside the page but portaled to the overlay host. */
function Harness({ onConfirm }: { onConfirm: () => void }) {
  const [open, setOpen] = useState(false);
  return (
    <Page>
      <button type="button" onClick={() => setOpen(true)}>
        清理
      </button>
      {open && (
        <ConfirmDialog
          title="确认清理"
          message="将清理 3 项，共 1 MB。删除后无法恢复。"
          confirmLabel="清理"
          tone="danger"
          onConfirm={() => {
            onConfirm();
            setOpen(false);
          }}
          onCancel={() => setOpen(false)}
        />
      )}
    </Page>
  );
}

function renderHarness(onConfirm = vi.fn<() => void>()) {
  render(
    <ShellBlockProvider>
      <Shell />
      <OverlayHost>
        <Harness onConfirm={onConfirm} />
      </OverlayHost>
    </ShellBlockProvider>,
  );
  return onConfirm;
}

describe("ConfirmDialog", () => {
  it("opens over an inert page, focuses 取消 for a danger action and closes on Escape with focus back", async () => {
    const user = userEvent.setup();
    renderHarness();
    const opener = screen.getByRole("button", { name: "清理" });
    await user.click(opener);
    const dialog = screen.getByRole("dialog", { name: "确认清理" });
    expect(dialog.getAttribute("aria-modal")).toBe("true");
    expect(screen.getByText("将清理 3 项，共 1 MB。删除后无法恢复。").id).toBe(
      dialog.getAttribute("aria-describedby"),
    );
    // Drawn in the overlay host, outside the page, which is inert like the shell.
    expect(dialog.closest(".overlay-host")).toBeTruthy();
    expect(opener.closest(".page")?.hasAttribute("inert")).toBe(true);
    expect(screen.getByTestId("shell").textContent).toBe("true");
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "取消" }));

    // Tab cycles inside the dialog.
    await user.tab();
    expect(document.activeElement).toBe(within(dialog).getByRole("button", { name: "清理" }));
    await user.tab();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "取消" }));

    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByTestId("shell").textContent).toBe("false");
    await waitFor(() => expect(document.activeElement).toBe(opener));
  });

  it("confirms", async () => {
    const user = userEvent.setup();
    const onConfirm = renderHarness();
    await user.click(screen.getByRole("button", { name: "清理" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "清理" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("DoneDialog", () => {
  it("is described by the figure and the lines and focuses 完成", () => {
    const onClose = vi.fn<() => void>();
    render(
      <DoneDialog
        title="清理完成"
        big="1.2 GB"
        lines={["系统缓存：已清理 3 项", "移到废纸篓的项目在清空废纸篓后才会释放空间"]}
        succeeded
        onClose={onClose}
      />,
    );
    const dialog = screen.getByRole("dialog", { name: "清理完成" });
    const [bigId, linesId] = (dialog.getAttribute("aria-describedby") ?? "").split(" ");
    expect(document.getElementById(bigId ?? "")?.textContent).toBe("1.2 GB");
    expect(document.getElementById(linesId ?? "")?.textContent).toBe(
      "系统缓存：已清理 3 项\n移到废纸篓的项目在清空废纸篓后才会释放空间",
    );
    const done = screen.getByRole("button", { name: "完成" });
    expect(document.activeElement).toBe(done);
    act(() => done.click());
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

describe("ProgressRing", () => {
  it("reports a known fraction and spins without one", () => {
    const { rerender } = render(<ProgressRing value={0.426} label="正在清理" />);
    const ring = screen.getByRole("progressbar", { name: "正在清理" });
    expect(ring.getAttribute("aria-valuenow")).toBe("43");
    rerender(<ProgressRing value={3} label="正在清理" />);
    expect(ring.getAttribute("aria-valuenow")).toBe("100");
    rerender(<ProgressRing label="正在清理" />);
    expect(ring.hasAttribute("aria-valuenow")).toBe(false);
    expect(ring.querySelector(".run-spinner")).toBeTruthy();
  });
});
