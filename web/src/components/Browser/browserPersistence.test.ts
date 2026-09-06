import { describe, it, expect, beforeEach } from "vitest";
import {
  loadProjectBrowsers,
  saveProjectBrowsers,
  dropProjectBrowsers,
} from "./browserPersistence";

const PROJ = "/proj/persist";

beforeEach(() => {
  localStorage.clear();
});

describe("browserPersistence", () => {
  it("round-trips tabs, surfaces, manual flags, and scroll offsets", () => {
    saveProjectBrowsers(
      PROJ,
      [
        { id: "b1", title: "One", manualTitle: null },
        { id: "b2", title: "Mine", manualTitle: "Mine" },
      ],
      "b2",
      {
        b1: {
          url: "https://a.com/",
          history: ["https://a.com/"],
          historyIndex: 0,
          userMode: null,
          pageTitle: "A Site",
          scrollByUrl: { "https://a.com/": 120 },
        },
      },
    );
    const loaded = loadProjectBrowsers(PROJ);
    expect(loaded?.tabs).toHaveLength(2);
    expect(loaded?.activeId).toBe("b2");
    expect(loaded?.tabs[1].manualTitle).toBe("Mine");
    expect(loaded?.surfaces.b1.pageTitle).toBe("A Site");
    expect(loaded?.surfaces.b1.scrollByUrl["https://a.com/"]).toBe(120);
    expect(loaded?.surfaces.b2).toBeUndefined();
  });

  it("drops non-http URLs, caps history/scroll, and clamps the index", () => {
    saveProjectBrowsers(
      PROJ,
      [{ id: "b1", title: "X", manualTitle: null }],
      "b1",
      {
        b1: {
          url: "https://a.com/",
          history: ["javascript:alert(1)", "https://a.com/", "about:blank"],
          historyIndex: 99,
          userMode: "chrome",
          pageTitle: "T",
          scrollByUrl: { "https://a.com/": 10, "javascript:x": 5, "https://a.com/zero": 0 },
        },
      },
    );
    const loaded = loadProjectBrowsers(PROJ);
    expect(loaded?.surfaces.b1.history).toEqual(["https://a.com/"]);
    expect(loaded?.surfaces.b1.historyIndex).toBe(0);
    expect(loaded?.surfaces.b1.scrollByUrl).toEqual({ "https://a.com/": 10 });
  });

  it("returns null for missing projects, rejects bad versions, and drops on empty save", () => {
    expect(loadProjectBrowsers("/proj/nope")).toBeNull();
    localStorage.setItem("ocode.ui.browser.project.v1", JSON.stringify({ version: 2, projects: {} }));
    expect(loadProjectBrowsers(PROJ)).toBeNull();
    localStorage.setItem("ocode.ui.browser.project.v1", "not-json{{{");
    expect(loadProjectBrowsers(PROJ)).toBeNull();
    saveProjectBrowsers(PROJ, [{ id: "b1", title: "X", manualTitle: null }], "b1", {});
    expect(loadProjectBrowsers(PROJ)).not.toBeNull();
    saveProjectBrowsers(PROJ, [], null, {});
    expect(loadProjectBrowsers(PROJ)).toBeNull();
    saveProjectBrowsers(PROJ, [{ id: "b1", title: "X", manualTitle: null }], "b1", {});
    dropProjectBrowsers(PROJ);
    expect(loadProjectBrowsers(PROJ)).toBeNull();
  });

  it("scopes state per project", () => {
    saveProjectBrowsers(PROJ, [{ id: "b1", title: "X", manualTitle: null }], "b1", {});
    expect(loadProjectBrowsers("/proj/other")).toBeNull();
    expect(loadProjectBrowsers(PROJ)?.tabs).toHaveLength(1);
  });
});
