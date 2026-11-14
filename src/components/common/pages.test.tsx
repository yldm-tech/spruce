import { render, screen } from "@testing-library/react";
import type { ComponentType } from "react";
import { describe, expect, it } from "vite-plus/test";

import { DevJunkPage } from "../dev-junk/DevJunkPage";
import { HealthPage } from "../health/HealthPage";
import { HistoryPage } from "../history/HistoryPage";
import { InstallersPage } from "../installers/InstallersPage";
import { SettingsPage } from "../settings/SettingsPage";
import { SmartScanPage } from "../smart-scan/SmartScanPage";
import { SystemJunkPage } from "../system-junk/SystemJunkPage";
import { UninstallPage } from "../uninstall/UninstallPage";
import { PAGES, type PageId } from "./pages";

const SHELLS: [PageId, ComponentType][] = [
  ["smart-scan", SmartScanPage],
  ["system-junk", SystemJunkPage],
  ["dev-junk", DevJunkPage],
  ["installers", InstallersPage],
  ["uninstall", UninstallPage],
  ["health", HealthPage],
  ["history", HistoryPage],
  ["settings", SettingsPage],
];

describe("page shells", () => {
  it.each(SHELLS)("%s shows its header", (id, Page) => {
    render(<Page />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(PAGES[id].title);
    expect(screen.getByText(PAGES[id].subtitle)).toBeTruthy();
  });
});
