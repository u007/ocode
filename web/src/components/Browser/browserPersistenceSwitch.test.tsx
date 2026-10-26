import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import type { ServerProjectBrowsers } from "../../api/types";
import { BrowserTabsProvider, useBrowserTabs } from "../../stores/browserTabsStore";
import { browserStore } from "../../lib/browserStore";
import { useBrowserPersistence } from "./browserPersistence";

/** In-memory stand-in for GET/PUT /api/browser-tabs, with the server's merge
 *  rules: a provided key replaces that project, an empty strip deletes it, and
 *  absent keys are kept. */
const fake = {
  projects: {} as Record<string, ServerProjectBrowsers>,
  puts: [] as Record<string, ServerProjectBrowsers>[],
};

vi.mock("../../api/client", async () => {
  const actual = await vi.importActual<typeof import("../../api/client")>("../../api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getBrowserTabs: () => Promise.resolve({ projects: structuredClone(fake.projects) }),
      setBrowserTabs: (projects: Record<string, ServerProjectBrowsers>) => {
        fake.puts.push(structuredClone(projects));
        for (const [k, v] of Object.entries(projects)) {
          if (v.tabs.length === 0) delete fake.projects[k];
          else fake.projects[k] = structuredClone(v);
        }
        return Promise.resolve({ status: "ok" });
      },
    },
  };
});

vi.mock("../../lib/eventBus", () => ({
  eventBus: {
    on: () => () => {},
    onReconnect: () => () => {},
  },
}));

function wrap({ children }: { children: ReactNode }) {
  return <BrowserTabsProvider>{children}</BrowserTabsProvider>;
}

const PROJ_A = "/proj/switch-a";
const PROJ_B = "/proj/switch-b";

function oneTab(id: string, url: string): ServerProjectBrowsers {
  return {
    tabs: [{ id, title: id }],
    active_id: id,
    surfaces: { [id]: { url, history: [url], history_index: 0 } },
    revision: 1,
  };
}

/** Lets pending reads, debounced writes and their promises settle. */
async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
}

beforeEach(() => {
  localStorage.clear();
  fake.projects = {};
  fake.puts = [];
  browserStore.setState(() => ({ byKey: {} }));
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("useBrowserPersistence project switching", () => {
  it("restores project B from the server while project A's surfaces are still live", async () => {
    fake.projects[PROJ_B] = oneTab("b1", "https://b.com/");
    // Project A's tab surface is still mounted globally after a switch.
    browserStore.setState(() => ({
      byKey: {
        "tab:a1": {
          url: "https://a.com/", status: 200, loading: false, mode: "local", userMode: null, error: null,
          history: ["https://a.com/"], historyIndex: 0, panelOpen: true, collapsed: false,
          consoleEvents: [], networkEvents: [], responseBodies: {}, pageTitle: "A Site", scrollY: 0, scrollByUrl: {},
          perfMetrics: {}, perfRecording: true, zoom: 1,
        },
      },
    }));

    const { result } = renderHook(
      () => ({ browser: useBrowserTabs(PROJ_B), persist: useBrowserPersistence(PROJ_B) }),
      { wrapper: wrap },
    );
    await settle();

    expect(result.current.browser.tabs.map((t) => t.id)).toEqual(["b1"]);
    expect(result.current.browser.activeId).toBe("b1");
    expect(browserStore.state.byKey["tab:b1"]?.url).toBe("https://b.com/");
    expect(browserStore.state.byKey["tab:a1"]?.url).toBe("https://a.com/");
    // Restoring must not write B back. The server keeps B's entry.
    expect(fake.puts).toEqual([]);
    expect(fake.projects[PROJ_B].tabs.map((t) => t.id)).toEqual(["b1"]);
  });

  it("keeps a tab opened here but not yet sent when switching away and back", async () => {
    fake.projects[PROJ_A] = oneTab("saved1", "https://saved.example/");
    fake.projects[PROJ_B] = oneTab("b1", "https://b.com/");

    const { result, rerender } = renderHook(
      ({ proj }: { proj: string }) => {
        const browser = useBrowserTabs(proj);
        useBrowserPersistence(proj);
        return browser;
      },
      { wrapper: wrap, initialProps: { proj: PROJ_A } },
    );
    await settle();
    expect(result.current.tabs.map((t) => t.id)).toEqual(["saved1"]);

    rerender({ proj: PROJ_B });
    await settle();
    expect(result.current.tabs.map((t) => t.id)).toEqual(["b1"]);

    // Open a tab in B and switch away before the debounce fires. The switch
    // must send it, so the read on the way back includes it.
    let liveId = "";
    act(() => {
      liveId = result.current.openBrowserTab();
    });
    rerender({ proj: PROJ_A });
    await settle();
    expect(result.current.tabs.map((t) => t.id)).toEqual(["saved1"]);
    expect(fake.projects[PROJ_B].tabs.map((t) => t.id)).toEqual(["b1", liveId]);

    rerender({ proj: PROJ_B });
    await settle();
    expect(result.current.tabs.map((t) => t.id)).toEqual(["b1", liveId]);
  });
  it("keeps a tab opened in the background for another project, and sends it on switch", async () => {
    fake.projects[PROJ_A] = oneTab("saved1", "https://saved.example/");

    // The `browse_newtab` bridge can open a tab in a project that is not shown.
    const { result, rerender } = renderHook(
      ({ proj }: { proj: string }) => ({
        browser: useBrowserTabs(proj),
        other: useBrowserTabs(PROJ_B),
        persist: useBrowserPersistence(proj),
      }),
      { wrapper: wrap, initialProps: { proj: PROJ_A } },
    );
    await settle();
    act(() => {
      result.current.other.openBackgroundBrowserTab("bg1");
    });

    rerender({ proj: PROJ_B });
    await settle();
    // The restore's render arms the debounced write after the first advance
    // window has closed, so it needs a second window to fire.
    await settle();

    // The server never held bg1, so it was not closed as "closed elsewhere".
    expect(result.current.browser.tabs.map((t) => t.id)).toEqual(["bg1"]);
    expect(fake.projects[PROJ_B].tabs.map((t) => t.id)).toEqual(["bg1"]);
  });
});
