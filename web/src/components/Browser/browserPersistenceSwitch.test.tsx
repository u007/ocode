import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { BrowserTabsProvider, useBrowserTabs } from "../../stores/browserTabsStore";
import { browserStore } from "../../lib/browserStore";
import {
  saveProjectBrowsers,
  loadProjectBrowsers,
  useBrowserPersistence,
} from "./browserPersistence";

function wrap({ children }: { children: ReactNode }) {
  return <BrowserTabsProvider>{children}</BrowserTabsProvider>;
}

const PROJ_A = "/proj/switch-a";
const PROJ_B = "/proj/switch-b";

beforeEach(() => {
  localStorage.clear();
  browserStore.setState(() => ({ byKey: {} }));
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("useBrowserPersistence project switching", () => {
  it("restores project B even when project A's surfaces are still live", () => {
    // Project A's tab surface is still mounted globally after a switch.
    browserStore.setState(() => ({
      byKey: {
        "tab:a1": {
          url: "https://a.com/", status: 200, loading: false, mode: "local", userMode: null, error: null,
          history: ["https://a.com/"], historyIndex: 0, panelOpen: true, collapsed: false,
          consoleEvents: [], networkEvents: [], responseBodies: {}, pageTitle: "A Site", scrollY: 0, scrollByUrl: {},
          perfMetrics: {}, perfRecording: true,
        },
      },
    }));
    saveProjectBrowsers(
      PROJ_B,
      [{ id: "b1", title: "B Site", manualTitle: null }],
      "b1",
      {
        b1: {
          url: "https://b.com/", history: ["https://b.com/"], historyIndex: 0,
          userMode: null, pageTitle: "B Site", scrollByUrl: {},
        },
      },
    );

    const { result } = renderHook(
      () => ({ browser: useBrowserTabs(PROJ_B), persist: useBrowserPersistence(PROJ_B) }),
      { wrapper: wrap },
    );
    // B's strip restored despite A's foreign surface being live.
    expect(result.current.browser.tabs.map((t) => t.id)).toEqual(["b1"]);
    expect(result.current.browser.activeId).toBe("b1");
    expect(browserStore.state.byKey["tab:b1"]?.url).toBe("https://b.com/");
    // A's surface untouched.
    expect(browserStore.state.byKey["tab:a1"]?.url).toBe("https://a.com/");

    // Let the debounced save run: B's entry must survive (the old global
    // gate skipped restore, then saved B's empty strip and deleted it).
    act(() => { vi.advanceTimersByTime(1000); });
    expect(loadProjectBrowsers(PROJ_B)?.tabs.map((t) => t.id)).toEqual(["b1"]);
  });

  it("switching away and back keeps the live strip (no clobber)", () => {
    saveProjectBrowsers(
      PROJ_A,
      [{ id: "saved1", title: "Saved", manualTitle: null }],
      "saved1",
      {},
    );
    saveProjectBrowsers(
      PROJ_B,
      [{ id: "b1", title: "B Site", manualTitle: null }],
      "b1",
      {},
    );
    // Same wrapper type across rerenders = one mounted provider, mirroring
    // the app where the strip store outlives project switches.
    const { result, rerender } = renderHook(
      ({ proj }: { proj: string }) => {
        const browser = useBrowserTabs(proj);
        useBrowserPersistence(proj);
        return browser;
      },
      { wrapper: wrap, initialProps: { proj: PROJ_A } },
    );
    // Fresh mount with an empty strip restores the saved entry.
    expect(result.current.tabs.map((t) => t.id)).toEqual(["saved1"]);
    // Switch to B: B's strip is empty, so B restores.
    rerender({ proj: PROJ_B });
    expect(result.current.tabs.map((t) => t.id)).toEqual(["b1"]);
    // Open a live tab in B, switch away and back: the live strip wins and
    // the saved entry must not duplicate or clobber it.
    let liveId = "";
    act(() => { liveId = result.current.openBrowserTab(); });
    rerender({ proj: PROJ_A });
    expect(result.current.tabs.map((t) => t.id)).toEqual(["saved1"]);
    rerender({ proj: PROJ_B });
    expect(result.current.tabs.map((t) => t.id)).toEqual(["b1", liveId]);
    expect(browserStore.state.byKey["tab:saved1"]).toBeDefined();
  });
});
