import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useEffect } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";

import type { AppSettings, CleanResult, Cleanup, Progress, ScanResult, Settings } from "../../mygo";
import { useCleaner, type CleanerFlow } from "./cleaner";

const fakes = vi.hoisted(() => {
  class FakeChannel<T> {
    onmessage: ((value: T) => void) | null;
    closed = false;
    constructor(onmessage?: (value: T) => void) {
      this.onmessage = onmessage ?? null;
    }
    close() {
      this.closed = true;
    }
    send(value: T) {
      this.onmessage?.(value);
    }
  }
  return {
    FakeChannel,
    Cleanup: {
      scan: vi.fn<typeof Cleanup.scan>(),
      clean: vi.fn<typeof Cleanup.clean>(),
      available: vi.fn<typeof Cleanup.available>(),
    },
    Settings: { get: vi.fn<typeof Settings.get>() },
  };
});

vi.mock("../../mygo", () => ({
  Cleanup: fakes.Cleanup,
  Settings: fakes.Settings,
  Files: {},
  events: { settingsChanged: { on: () => () => {} } },
}));

vi.mock("mygo-runtime", async (importOriginal) => ({
  ...(await importOriginal<typeof import("mygo-runtime")>()),
  Channel: fakes.FakeChannel,
}));

type FakeChannel = InstanceType<typeof fakes.FakeChannel<Progress>>;

const SCAN: ScanResult = {
  categories: [
    {
      id: "caches",
      title: "应用缓存",
      description: "",
      icon: "cache",
      kind: "cache",
      deletion: "permanent",
      selected: true,
      size: 300,
      items: [
        { id: "a", path: "/c/a", size: 200, modTime: "" },
        { id: "b", path: "/c/b", size: 100, modTime: "" },
      ],
    },
  ],
  size: 300,
  items: 2,
  denied: 0,
  scannedAt: "2026-11-14T01:00:00Z",
  duration: 1e9,
};

const RESULT: CleanResult = {
  freed: 300,
  trashed: 0,
  removed: 2,
  skipped: 0,
  failed: [],
  categories: [{ categoryId: "caches", title: "应用缓存", removed: 2, freed: 300, trashed: 0, failed: 0 }],
  partial: false,
  interrupted: false,
  startedAt: "2026-11-14T01:00:00Z",
  duration: 1e9,
};

function settings(confirm: boolean): AppSettings {
  return {
    version: 1,
    health: { cpuThreshold: 40, sustainMinutes: 10, enabled: true },
    cleanup: {
      exclusions: [],
      devProjectRoots: [],
      devArtifactMinAgeDays: 30,
      installerRoots: [],
      largeFileMinMB: 500,
    },
    history: { retentionDays: 180 },
    general: { launchAtLogin: false, confirmBeforeClean: confirm },
  };
}

/** The hook's latest value, published by Probe after each render. */
const current: { flow?: CleanerFlow } = {};

function Probe() {
  const f = useCleaner("system-junk");
  useEffect(() => {
    current.flow = f;
  });
  return (
    <span data-testid="state">{`${f.phase} ${f.totals.count} ${f.progress?.path ?? "-"} ${f.scanError ?? ""}`}</span>
  );
}

function flow(): CleanerFlow {
  if (!current.flow) throw new Error("Probe has not rendered");
  return current.flow;
}

function renderHook() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <Probe />
    </QueryClientProvider>,
  );
}

const state = () => screen.getByTestId("state").textContent;

beforeEach(() => {
  fakes.Settings.get.mockResolvedValue(settings(true));
});

afterEach(() => {
  vi.resetAllMocks();
  current.flow = undefined;
});

describe("useCleaner", () => {
  it("scans with progress, selects the defaults, confirms and cleans", async () => {
    let channel: FakeChannel | undefined;
    let finishScan!: (r: ScanResult) => void;
    fakes.Cleanup.scan.mockImplementation((_id, ch) => {
      channel = ch as unknown as FakeChannel;
      return new Promise((resolve) => (finishScan = resolve));
    });
    fakes.Cleanup.clean.mockResolvedValue(RESULT);
    renderHook();
    expect(state()).toBe("idle 0 - ");
    await waitFor(() => expect(fakes.Settings.get).toHaveBeenCalled());

    act(() => flow().startScan());
    await waitFor(() =>
      expect(fakes.Cleanup.scan).toHaveBeenCalledWith("system-junk", expect.any(fakes.FakeChannel)),
    );
    act(() => channel?.send({ phase: "scanning", path: "/c", done: 0, total: 0, bytes: 10 }));
    await waitFor(() => expect(state()).toBe("scanning 0 /c "));
    act(() => finishScan(SCAN));
    await waitFor(() => expect(state()).toBe("results 2 - "));

    act(() => flow().requestClean());
    await waitFor(() => expect(flow().confirming).toBe(true));
    expect(fakes.Cleanup.clean).not.toHaveBeenCalled();
    act(() => flow().confirmClean());
    await waitFor(() => expect(state()).toBe("done 2 - "));
    expect(fakes.Cleanup.clean).toHaveBeenCalledWith(
      "system-junk",
      { categoryIds: ["caches"] },
      expect.any(fakes.FakeChannel),
    );
    expect(flow().result).toEqual(RESULT);

    act(() => flow().finish());
    await waitFor(() => expect(state()).toBe("idle 0 - "));
  });

  it("cleans without asking when the user turned confirmation off, and returns to idle on cancel", async () => {
    fakes.Settings.get.mockResolvedValue(settings(false));
    fakes.Cleanup.scan.mockResolvedValue(SCAN);
    fakes.Cleanup.clean.mockResolvedValue(RESULT);
    renderHook();
    await waitFor(() => expect(fakes.Settings.get).toHaveBeenCalled());
    act(() => flow().startScan());
    await waitFor(() => expect(state()).toBe("results 2 - "));
    // A partial selection sends the item IDs.
    act(() => flow().setSelection(new Map([["caches", new Set(["b"])]])));
    await waitFor(() => expect(state()).toBe("results 1 - "));
    act(() => flow().requestClean());
    await waitFor(() => expect(fakes.Cleanup.clean).toHaveBeenCalled());
    expect(flow().confirming).toBe(false);
    expect(fakes.Cleanup.clean).toHaveBeenCalledWith(
      "system-junk",
      { categoryIds: ["caches"], itemIds: ["b"] },
      expect.any(fakes.FakeChannel),
    );
  });

  it("goes back to idle without an error when the user stops a scan, and shows a real failure", async () => {
    let channel: FakeChannel | undefined;
    let fail!: (e: Error) => void;
    fakes.Cleanup.scan.mockImplementation((_id, ch) => {
      channel = ch as unknown as FakeChannel;
      return new Promise((_resolve, reject) => (fail = reject));
    });
    renderHook();
    act(() => flow().startScan());
    await waitFor(() => expect(state()).toBe("scanning 0 - "));
    act(() => flow().cancel());
    expect(channel?.closed).toBe(true);
    act(() => fail(new Error("已取消")));
    await waitFor(() => expect(state()).toBe("idle 0 - "));

    act(() => flow().startScan());
    await waitFor(() => expect(state()).toBe("scanning 0 - "));
    act(() => fail(new Error("扫描没能完成，详情已记录到日志")));
    await waitFor(() => expect(state()).toBe("idle 0 - 扫描没能完成，详情已记录到日志"));
  });

  it("stops the running scan when the page goes away", async () => {
    let channel: FakeChannel | undefined;
    fakes.Cleanup.scan.mockImplementation((_id, ch) => {
      channel = ch as unknown as FakeChannel;
      return new Promise(() => {});
    });
    const view = renderHook();
    act(() => flow().startScan());
    await waitFor(() => expect(channel).toBeDefined());
    view.unmount();
    expect(channel?.closed).toBe(true);
  });
});

describe("useCleaner with a page", () => {
  it("is usable from a button", async () => {
    const user = userEvent.setup();
    fakes.Cleanup.scan.mockResolvedValue(SCAN);
    function Page() {
      const f = useCleaner("dev-junk");
      return (
        <button type="button" onClick={f.startScan}>
          {f.phase}
        </button>
      );
    }
    render(
      <QueryClientProvider client={new QueryClient()}>
        <Page />
      </QueryClientProvider>,
    );
    await user.click(screen.getByRole("button", { name: "idle" }));
    expect(await screen.findByRole("button", { name: "results" })).toBeTruthy();
  });
});
