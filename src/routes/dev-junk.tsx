import { createFileRoute } from "@tanstack/react-router";

import { DevJunkPage } from "../components/dev-junk/DevJunkPage";

export const Route = createFileRoute("/dev-junk")({ component: DevJunkPage });
