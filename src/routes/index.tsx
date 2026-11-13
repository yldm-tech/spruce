import { createFileRoute, redirect } from "@tanstack/react-router";

// Only 维护 exists in this milestone, so the app opens on it.
export const Route = createFileRoute("/")({
  beforeLoad: () => {
    throw redirect({ to: "/maintenance", replace: true });
  },
});
