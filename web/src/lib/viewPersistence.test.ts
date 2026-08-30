import { describe, it, expect, beforeEach } from "vitest";
import { getPersistedViewState, persistViewStates, loadViewStateForProject, saveViewStateForProject } from "./viewPersistence";

const KEY = "ocode.ui.view-state.v1";

function setRaw(v: unknown) {
  window.localStorage.setItem(KEY, JSON.stringify(v));
}

beforeEach(() => {
  window.localStorage.clear();
});

describe("viewPersistence", () => {
  it("defaults to empty when nothing stored", () => {
    expect(getPersistedViewState()).toEqual(Object.create(null));
    expect(loadViewStateForProject("/proj-a")).toBeNull();
  });

  it("persists and restores per-project view + focusedKind", () => {
    persistViewStates(
      Object.assign(Object.create(null), {
        "/proj-a": { view: "files" as const, focusedKind: "chat" as const },
        "/proj-b": { view: "git" as const, focusedKind: "terminal" as const },
      })
    );
    expect(loadViewStateForProject("/proj-a")).toEqual({ view: "files", focusedKind: "chat" });
    expect(loadViewStateForProject("/proj-b")).toEqual({ view: "git", focusedKind: "terminal" });
    // A→B→A restoration
    expect(getPersistedViewState()["/proj-a"]).toEqual({ view: "files", focusedKind: "chat" });
  });

  it("preserves focusedKind when view changes and vice versa (simulated via save)", () => {
    saveViewStateForProject("/proj-a", { view: "files", focusedKind: "chat" });
    saveViewStateForProject("/proj-a", { view: "git", focusedKind: "chat" });
    expect(loadViewStateForProject("/proj-a")).toEqual({ view: "git", focusedKind: "chat" });
    saveViewStateForProject("/proj-a", { view: "git", focusedKind: "terminal" });
    expect(loadViewStateForProject("/proj-a")).toEqual({ view: "git", focusedKind: "terminal" });
  });

  it("ignores prototype pollution keys", () => {
    setRaw({
      version: 1,
      projects: {
        "/proj-a": { view: "files", focusedKind: "chat" },
        "__proto__": { view: "git", focusedKind: "terminal" },
        "constructor": { view: "git", focusedKind: "terminal" },
      },
    });
    const all = getPersistedViewState();
    expect(all["/proj-a"]).toEqual({ view: "files", focusedKind: "chat" });
    expect((all as any)["__proto__"]).toBeUndefined();
    // saving should not persist polluted keys
    saveViewStateForProject("__proto__", { view: "files", focusedKind: "chat" });
    expect(loadViewStateForProject("__proto__")).toBeNull();
  });

  it("falls back safely on malformed storage", () => {
    window.localStorage.setItem(KEY, "not-json");
    expect(getPersistedViewState()).toEqual(Object.create(null));
    setRaw({ version: 99, projects: { "/proj-a": { view: "files", focusedKind: "chat" } } });
    expect(getPersistedViewState()).toEqual(Object.create(null));
    setRaw({ version: 1, projects: { "/proj-a": { view: "invalid", focusedKind: "chat" } } });
    expect(loadViewStateForProject("/proj-a")).toBeNull();
  });

  it("first visit defaults (no entry -> null, caller falls back to sessions/chat)", () => {
    // No entry for new project should be null, not throw
    expect(loadViewStateForProject("/new-proj")).toBeNull();
    // After saving, it appears
    saveViewStateForProject("/new-proj", { view: "sessions", focusedKind: "chat" });
    expect(loadViewStateForProject("/new-proj")).toEqual({ view: "sessions", focusedKind: "chat" });
  });
});
