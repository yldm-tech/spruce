import { createFileRoute } from "@tanstack/react-router";

import { InstallersPage } from "../components/installers/InstallersPage";

export const Route = createFileRoute("/installers")({ component: InstallersPage });
