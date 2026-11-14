import { createFileRoute, redirect } from "@tanstack/react-router";

// The app opens on 智能扫描, its home page; scripts/e2e-smoke.sh checks it there before it goes to 维护.
export const Route = createFileRoute("/")({
  beforeLoad: () => {
    throw redirect({ to: "/smart-scan", replace: true });
  },
});
