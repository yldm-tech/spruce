import { createFileRoute, redirect } from "@tanstack/react-router";

// The app opens on 维护, the first page that was built; scripts/e2e-smoke.sh expects it there.
export const Route = createFileRoute("/")({
  beforeLoad: () => {
    throw redirect({ to: "/maintenance", replace: true });
  },
});
