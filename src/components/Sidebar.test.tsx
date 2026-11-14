import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it } from "vite-plus/test";

import { ShellBlockProvider, useBlockShell } from "./ShellBlock";
import { Sidebar } from "./Sidebar";

/** A page that keeps a modal overlay up, as the 维护 page does during and after a run. */
function BlockingPage() {
  useBlockShell(true);
  return null;
}

function renderAt(path: string, page?: () => ReactNode) {
  const rootRoute = createRootRoute({
    component: () => (
      <ShellBlockProvider>
        <Sidebar />
        <Outlet />
      </ShellBlockProvider>
    ),
  });
  const maintenance = createRoute({ getParentRoute: () => rootRoute, path: "/maintenance", component: page });
  const router = createRouter({
    routeTree: rootRoute.addChildren([maintenance]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  return render(<RouterProvider router={router} />);
}

describe("Sidebar", () => {
  it("links 维护 and marks it current on its page", async () => {
    renderAt("/maintenance");
    const link = await screen.findByRole("link", { name: "维护" });
    expect(link.getAttribute("href")).toBe("/maintenance");
    expect(link.getAttribute("aria-current")).toBe("page");
  });

  it("links every page in the designed order", async () => {
    renderAt("/maintenance");
    await screen.findByRole("link", { name: "维护" });
    const links = screen.getAllByRole("link");
    expect(links.map((l) => [l.textContent, l.getAttribute("href")])).toEqual([
      ["智能扫描", "/smart-scan"],
      ["系统垃圾", "/system-junk"],
      ["开发者垃圾", "/dev-junk"],
      ["安装包", "/installers"],
      ["应用卸载", "/uninstall"],
      ["空间透视", "/space"],
      ["维护", "/maintenance"],
      ["健康监测", "/health"],
      ["操作历史", "/history"],
      ["设置", "/settings"],
    ]);
    // Only the current page is marked, and nothing is disabled any more.
    expect(links.filter((l) => l.getAttribute("aria-current") === "page").map((l) => l.textContent)).toEqual([
      "维护",
    ]);
    expect(links.some((l) => l.hasAttribute("aria-disabled"))).toBe(false);
  });

  it("is inert while a page's modal overlay is up", async () => {
    renderAt("/maintenance", BlockingPage);
    const nav = await screen.findByRole("navigation", { name: "Spruce", hidden: true });
    expect(nav.hasAttribute("inert")).toBe(true);
  });

  it("is usable otherwise", async () => {
    renderAt("/maintenance");
    expect((await screen.findByRole("navigation", { name: "Spruce" })).hasAttribute("inert")).toBe(false);
  });
});
