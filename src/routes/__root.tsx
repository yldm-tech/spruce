import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";

import { ShellBlockProvider } from "../components/ShellBlock";
import { Sidebar } from "../components/Sidebar";

/** What every route can reach through its context. */
export interface RouterContext {
  queryClient: QueryClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
});

function RootLayout() {
  return (
    <ShellBlockProvider>
      <div className="app-shell">
        <Sidebar />
        <main className="app-main">
          <Outlet />
        </main>
      </div>
    </ShellBlockProvider>
  );
}
