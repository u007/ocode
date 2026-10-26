import { describe, it, expect, beforeEach } from "vitest";
import {
  fromWireProject,
  loadScrollByUrl,
  saveScrollByUrl,
  sharedStateFromLocal,
  takeLegacyProjectBrowsers,
  toWireProject,
} from "./browserPersistence";
import type { BrowserTabState } from "../../lib/browserStore";
import type { BrowserTab } from "../../stores/browserTabsStore";

beforeEach(() => {
  localStorage.clear();
});

function surface(overrides: Partial<BrowserTabState>): BrowserTabState {
  return {
    url: "https://a.example/",
    status: 200,
    loading: false,
    mode: "local",
    userMode: null,
    error: null,
    history: ["https://a.example/"],
    historyIndex: 0,
    panelOpen: true,
    collapsed: false,
    consoleEvents: [],
    networkEvents: [],
    responseBodies: {},
    pageTitle: "A Site",
    scrollY: 0,
    scrollByUrl: { "https://a.example/": 120 },
    perfMetrics: {},
    perfRecording: true,
    zoom: 1,
    ...overrides,
  };
}

const TABS: BrowserTab[] = [
  { id: "b1", title: "One", manualTitle: null },
  { id: "b2", title: "Mine", manualTitle: "Mine" },
];

describe("sharedStateFromLocal", () => {
  it("keeps strip order, manual renames and the active tab", () => {
    const shared = sharedStateFromLocal(TABS, "b2", { "tab:b1": surface({}) });
    expect(shared.tabs.map((t) => t.id)).toEqual(["b1", "b2"]);
    expect(shared.tabs[1].manualTitle).toBe("Mine");
    expect(shared.activeId).toBe("b2");
    expect(shared.surfaces.b1.pageTitle).toBe("A Site");
  });

  it("never shares scroll offsets", () => {
    const shared = sharedStateFromLocal(TABS, "b1", { "tab:b1": surface({}) });
    expect(JSON.stringify(shared)).not.toContain("scroll");
    expect(JSON.stringify(shared)).not.toContain("120");
  });

  it("omits surfaces without a web URL, and clears an active id that is not a tab", () => {
    const shared = sharedStateFromLocal(TABS, "gone", {
      "tab:b1": surface({ url: "javascript:alert(1)", history: ["javascript:alert(1)"] }),
      "tab:b2": surface({ url: "" }),
    });
    expect(shared.surfaces).toEqual({});
    expect(shared.activeId).toBeNull();
  });

  it("caps history and clamps the history index", () => {
    const history = Array.from({ length: 30 }, (_, i) => `https://a.example/${i}`);
    const shared = sharedStateFromLocal([TABS[0]], "b1", {
      "tab:b1": surface({ url: "https://a.example/29", history, historyIndex: 99 }),
    });
    expect(shared.surfaces.b1.history).toHaveLength(20);
    expect(shared.surfaces.b1.historyIndex).toBe(19);
  });

  it("applies the server's UTF-8 byte cap, not the UTF-16 length", () => {
    // 1100 characters of two bytes each: under a 2048 length check, over the
    // 2048-byte cap the server enforces.
    const long = `https://a.example/${"é".repeat(1100)}`;
    const shared = sharedStateFromLocal([TABS[0]], "b1", { "tab:b1": surface({ url: long, history: [long] }) });
    expect(shared.surfaces).toEqual({});
  });

  it("drops an over-long history entry and keeps the surface", () => {
    const long = `https://a.example/${"é".repeat(1100)}`;
    const shared = sharedStateFromLocal([TABS[0]], "b1", {
      "tab:b1": surface({ url: "https://a.example/", history: [long, "https://a.example/"] }),
    });
    expect(shared.surfaces.b1.history).toEqual(["https://a.example/"]);
  });

  it("maps a blank page title to null", () => {
    const shared = sharedStateFromLocal([TABS[0]], "b1", { "tab:b1": surface({ pageTitle: "   " }) });
    expect(shared.surfaces.b1.pageTitle).toBeNull();
  });
});

describe("wire mapping", () => {
  it("round-trips the shared state and omits absent optional fields", () => {
    const local = sharedStateFromLocal(TABS, null, { "tab:b1": surface({ userMode: "chrome" }) });
    const wire = toWireProject(local, 4);
    expect(wire.revision).toBe(4);
    expect(wire.active_id).toBeUndefined();
    expect(wire.tabs[0]).toEqual({ id: "b1", title: "One" });
    expect(wire.tabs[1]).toEqual({ id: "b2", title: "Mine", manual_title: "Mine" });
    expect(wire.surfaces.b1).toEqual({
      url: "https://a.example/",
      history: ["https://a.example/"],
      history_index: 0,
      user_mode: "chrome",
      page_title: "A Site",
    });
    expect(fromWireProject(wire)).toEqual({ state: local, revision: 4 });
  });

  it("returns null for a project the server does not hold", () => {
    expect(fromWireProject(undefined)).toBeNull();
  });

  it("drops server surfaces with non-web URLs and an active id that is not a tab", () => {
    const parsed = fromWireProject({
      tabs: [{ id: "b1", title: "One" }],
      active_id: "ghost",
      surfaces: {
        b1: { url: "data:text/html,hi", history: ["data:text/html,hi"], history_index: 0 },
      },
      revision: 1,
    });
    expect(parsed?.state.activeId).toBeNull();
    expect(parsed?.state.surfaces).toEqual({});
  });
});

describe("local scroll offsets", () => {
  it("are kept per tab on this viewer", () => {
    saveScrollByUrl("b1", { "https://a.example/": 300 });
    expect(loadScrollByUrl("b1")).toEqual({ "https://a.example/": 300 });
    expect(loadScrollByUrl("b2")).toEqual({});
  });

  it("drop non-web URLs and non-positive offsets", () => {
    saveScrollByUrl("b1", { "https://a.example/": 10, "javascript:x": 5, "https://a.example/zero": 0 });
    expect(loadScrollByUrl("b1")).toEqual({ "https://a.example/": 10 });
  });
});

describe("legacy localStorage migration", () => {
  const LEGACY = "ocode.ui.browser.project.v1";
  const legacyEntry = {
    tabs: [{ id: "b1", title: "One", manualTitle: null }],
    activeId: "b1",
    surfaces: {
      b1: {
        url: "https://a.example/",
        history: ["https://a.example/"],
        historyIndex: 0,
        userMode: null,
        pageTitle: "A Site",
        scrollByUrl: { "https://a.example/": 90 },
      },
    },
  };

  it("returns the entry with its scroll offsets, and removes only that project", () => {
    localStorage.setItem(
      LEGACY,
      JSON.stringify({ version: 1, projects: { "/proj/a": legacyEntry, "/proj/b": legacyEntry } }),
    );
    const migrated = takeLegacyProjectBrowsers("/proj/a");
    expect(migrated?.shared.tabs.map((t) => t.id)).toEqual(["b1"]);
    expect(migrated?.shared.surfaces.b1.url).toBe("https://a.example/");
    expect(migrated?.scrollByTab.b1).toEqual({ "https://a.example/": 90 });
    const left = JSON.parse(localStorage.getItem(LEGACY) ?? "null");
    expect(Object.keys(left.projects)).toEqual(["/proj/b"]);
  });

  it("removes the key once no project remains", () => {
    localStorage.setItem(LEGACY, JSON.stringify({ version: 1, projects: { "/proj/a": legacyEntry } }));
    takeLegacyProjectBrowsers("/proj/a");
    expect(localStorage.getItem(LEGACY)).toBeNull();
  });

  it("returns null and leaves storage alone when a project has no legacy entry", () => {
    localStorage.setItem(LEGACY, JSON.stringify({ version: 1, projects: { "/proj/b": legacyEntry } }));
    expect(takeLegacyProjectBrowsers("/proj/a")).toBeNull();
    expect(Object.keys(JSON.parse(localStorage.getItem(LEGACY) ?? "null").projects)).toEqual(["/proj/b"]);
  });
});
