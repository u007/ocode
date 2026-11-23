import { beforeEach, describe, expect, it } from "vitest";
import { browserActions, browserStore, type BrowserTabState } from "./browserStore";

const KEY = "tab:abc" as const;
const ROOT = { id: "2", tag: "html", attrs: {}, children: [{ id: "7", tag: "button", attrs: {} }] };

function tab(overrides: Partial<BrowserTabState> = {}): BrowserTabState {
  return {
    url: "https://example.com/",
    status: 200,
    loading: false,
    mode: "chrome",
    userMode: null,
    error: null,
    history: ["https://example.com/"],
    historyIndex: 0,
    panelOpen: true,
    collapsed: false,
    reader: false,
    consoleEvents: [],
    networkEvents: [],
    responseBodies: {},
    pageTitle: null,
    scrollY: 0,
    scrollByUrl: {},
    perfMetrics: {},
    perfRecording: true,
    zoom: 1,
    dom: null,
    ...overrides,
  };
}

function seed(overrides: Partial<BrowserTabState> = {}) {
  browserStore.setState(() => ({ byKey: { [KEY]: tab(overrides) } }));
}

function dom() {
  return browserStore.state.byKey[KEY].dom;
}

beforeEach(() => {
  browserStore.setState(() => ({ byKey: {} }));
});

describe("DOM inspector store actions", () => {
  it("bumps the generation and reports it so a stale reply can be dropped", () => {
    seed();
    const first = browserActions.beginDomInspect(KEY, "chrome");
    expect(first).toBe(1);
    expect(dom()).toMatchObject({ mode: "chrome", status: "loading", gen: 1 });

    const second = browserActions.beginDomInspect(KEY, "chrome");
    expect(second).toBe(2);
    // The reply for the first fetch must not overwrite the second.
    browserActions.setDomTree(KEY, first, { root: ROOT, truncated: false });
    expect(dom()?.status).toBe("loading");
    browserActions.setDomTree(KEY, second, { root: ROOT, truncated: true });
    expect(dom()).toMatchObject({ status: "ready", truncated: true, root: ROOT });
  });

  it("records an error for the matching generation only", () => {
    seed();
    const gen = browserActions.beginDomInspect(KEY, "local");
    browserActions.setDomError(KEY, gen + 1, "stale");
    expect(dom()?.error).toBeNull();
    browserActions.setDomError(KEY, gen, "boom");
    expect(dom()).toMatchObject({ status: "error", error: "boom", root: null });
  });

  it("selecting a node disarms the picker; hover and pick are independent", () => {
    seed();
    const gen = browserActions.beginDomInspect(KEY, "chrome");
    browserActions.setDomTree(KEY, gen, { root: ROOT, truncated: false });
    browserActions.setDomPick(KEY, true);
    browserActions.hoverDomNode(KEY, "7");
    expect(dom()).toMatchObject({ hoverId: "7", pick: true });

    browserActions.selectDomNode(KEY, "7");
    expect(dom()).toMatchObject({ selectedId: "7", pick: false });
    browserActions.hoverDomNode(KEY, null);
    expect(dom()?.hoverId).toBeNull();
  });

  it("clearDom drops the tree", () => {
    seed();
    browserActions.beginDomInspect(KEY, "chrome");
    browserActions.clearDom(KEY);
    expect(dom()).toBeNull();
  });
});

describe("DOM inspector invalidation", () => {
  function ready(overrides: Partial<BrowserTabState> = {}) {
    seed({
      ...overrides,
      dom: {
        mode: "chrome",
        status: "ready",
        error: null,
        root: ROOT,
        truncated: false,
        selectedId: "7",
        hoverId: null,
        pick: false,
        gen: 3,
      },
    });
  }

  it("a real navigation clears the tree", () => {
    ready();
    browserActions.navigate(KEY, "https://example.com/next");
    expect(dom()).toBeNull();

    ready({ history: ["https://example.com/", "https://example.com/b"], historyIndex: 1 });
    browserActions.back(KEY);
    expect(dom()).toBeNull();

    ready({ history: ["https://example.com/", "https://example.com/b"], historyIndex: 0 });
    browserActions.forward(KEY);
    expect(dom()).toBeNull();
  });

  it("a same-document URL update keeps the tree (the DOM did not reload)", () => {
    ready();
    const applied = browserActions.syncPageURL(KEY, "https://example.com/route", "push");
    expect(applied).not.toBeNull();
    expect(dom()?.root).toBe(ROOT);
  });

  it("a shared navigation to another URL clears it, the same URL keeps it", () => {
    ready();
    browserActions.applySharedNavigation(KEY, {
      url: "https://example.com/",
      history: ["https://example.com/"],
      historyIndex: 0,
      userMode: null,
      pageTitle: null,
    });
    expect(dom()?.root).toBe(ROOT);

    browserActions.applySharedNavigation(KEY, {
      url: "https://example.com/other",
      history: ["https://example.com/", "https://example.com/other"],
      historyIndex: 1,
      userMode: null,
      pageTitle: null,
    });
    expect(dom()).toBeNull();
  });
});
