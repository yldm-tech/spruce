import { createFileRoute } from "@tanstack/react-router";

import { SmartScanPage } from "../components/smart-scan/SmartScanPage";

export const Route = createFileRoute("/smart-scan")({ component: SmartScanPage });
