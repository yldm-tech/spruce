import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vite-plus/test";

import { Sidebar } from "./Sidebar";

function renderAt(path: string) {
  const rootRoute = createRootRoute({
    component: () => (
      <>
        <Sidebar />
        <Outlet />
      </>
    ),
  });
  const maintenance = createRoute({ getParentRoute: () => rootRoute, path: "/maintenance" });
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
    expect(link.getAttribute("aria-disabled")).toBeNull();
  });

  it("renders every other entry disabled with the coming-soon tooltip", async () => {
    renderAt("/maintenance");
    await screen.findByRole("link", { name: "维护" });
    for (const name of [
      "智能扫描",
      "系统垃圾",
      "开发者垃圾",
      "安装包",
      "应用卸载",
      "空间透视",
      "健康监测",
      "操作历史",
      "设置",
    ]) {
      const entry = screen.getByRole("link", { name });
      expect(entry.getAttribute("aria-disabled")).toBe("true");
      expect(entry.getAttribute("title")).toBe("即将推出");
      expect(entry.hasAttribute("href")).toBe(false);
    }
  });
});
