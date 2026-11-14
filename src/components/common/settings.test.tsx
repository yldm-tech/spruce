import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import type { AppSettings, Settings } from "../../mygo";
import { useSettings, useUpdateSettings } from "./settings";

const fakes = vi.hoisted(() => ({
  Settings: {
    get: vi.fn<typeof Settings.get>(),
    update: vi.fn<typeof Settings.update>(),
    reset: vi.fn<typeof Settings.reset>(),
  },
  listeners: new Set<(s: AppSettings) => void>(),
}));

vi.mock("../../mygo", () => ({
  Settings: fakes.Settings,
  events: {
    settingsChanged: {
      on: (fn: (s: AppSettings) => void) => {
        fakes.listeners.add(fn);
        return () => fakes.listeners.delete(fn);
      },
    },
  },
}));

function settings(threshold: number): AppSettings {
  return {
    version: 1,
    health: { cpuThreshold: threshold, sustainMinutes: 10, enabled: true },
    cleanup: {
      exclusions: [],
      devProjectRoots: [],
      devArtifactMinAgeDays: 30,
      installerRoots: [],
      largeFileMinMB: 500,
    },
    history: { retentionDays: 180 },
    general: { launchAtLogin: false, confirmBeforeClean: true },
  };
}

function Probe() {
  const s = useSettings();
  const update = useUpdateSettings();
  return (
    <button
      type="button"
      onClick={() => s.data && update.mutate({ ...s.data, health: { ...s.data.health, cpuThreshold: 5 } })}
    >
      {s.data ? `阈值 ${s.data.health.cpuThreshold}` : "加载中"}
    </button>
  );
}

afterEach(() => {
  vi.resetAllMocks();
  fakes.listeners.clear();
});

describe("settings hooks", () => {
  it("loads once, follows settings:changed and stores what Go returns", async () => {
    const user = userEvent.setup();
    fakes.Settings.get.mockResolvedValue(settings(40));
    // Go clamps 5 to the minimum.
    fakes.Settings.update.mockResolvedValue(settings(20));
    render(
      <QueryClientProvider client={new QueryClient()}>
        <Probe />
      </QueryClientProvider>,
    );
    expect(await screen.findByRole("button", { name: "阈值 40" })).toBeTruthy();
    await waitFor(() => expect(fakes.listeners.size).toBe(1));

    act(() => {
      for (const fn of fakes.listeners) fn(settings(75));
    });
    // The query cache notifies on the next task.
    expect(await screen.findByRole("button", { name: "阈值 75" })).toBeTruthy();

    await user.click(screen.getByRole("button"));
    expect(fakes.Settings.update.mock.calls[0]?.[0].health.cpuThreshold).toBe(5);
    expect(await screen.findByRole("button", { name: "阈值 20" })).toBeTruthy();
    expect(fakes.Settings.get).toHaveBeenCalledTimes(1);
  });
});
