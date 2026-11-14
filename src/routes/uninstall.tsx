import { createFileRoute } from "@tanstack/react-router";

import { UninstallPage } from "../components/uninstall/UninstallPage";

export const Route = createFileRoute("/uninstall")({ component: UninstallPage });
