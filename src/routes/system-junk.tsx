import { createFileRoute } from "@tanstack/react-router";

import { SystemJunkPage } from "../components/system-junk/SystemJunkPage";

export const Route = createFileRoute("/system-junk")({ component: SystemJunkPage });
