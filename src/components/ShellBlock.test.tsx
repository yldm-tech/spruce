import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vite-plus/test";

import { ShellBlockProvider, useBlockShell, useShellBlocked } from "./ShellBlock";

function Blocker({ active }: { active: boolean }) {
  useBlockShell(active);
  return null;
}

function Probe() {
  return <span>{String(useShellBlocked())}</span>;
}

function tree(a: boolean, b: boolean, mountB = true) {
  return (
    <ShellBlockProvider>
      <Blocker active={a} />
      {mountB && <Blocker active={b} />}
      <Probe />
    </ShellBlockProvider>
  );
}

describe("ShellBlock", () => {
  it("stays blocked while any overlay holds it", () => {
    const { rerender } = render(tree(true, true));
    expect(screen.getByText("true")).toBeTruthy();
    // One overlay closes; the other still holds the shell.
    rerender(tree(false, true));
    expect(screen.getByText("true")).toBeTruthy();
    rerender(tree(false, true, false));
    expect(screen.getByText("false")).toBeTruthy();
    rerender(tree(false, false));
    expect(screen.getByText("false")).toBeTruthy();
  });
});
