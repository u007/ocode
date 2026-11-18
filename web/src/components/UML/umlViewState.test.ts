import { describe, expect, it, beforeEach } from "vitest";
import { loadUmlViewState, saveUmlViewState, umlViewStateKey } from "./umlViewState";

describe("umlViewState", () => {
  beforeEach(() => localStorage.clear());

  it("round-trips a saved view state", () => {
    const key = umlViewStateKey("local", "/root", "sess-1");
    saveUmlViewState(key, {
      view: "classes", zoom: 1.4, panX: 10, panY: -20,
      selectedId: "a.go:3:Thing", query: "thing",
      langs: ["Go"], kinds: ["struct"], pkg: "internal/agent",
    });
    const got = loadUmlViewState(key);
    expect(got?.view).toBe("classes");
    expect(got?.zoom).toBe(1.4);
    expect(got?.selectedId).toBe("a.go:3:Thing");
  });

  it("scopes by host, root and session", () => {
    const a = umlViewStateKey("local", "/root", "s1");
    const b = umlViewStateKey("prod", "/root", "s1");
    const c = umlViewStateKey("local", "/root", "s2");
    expect(new Set([a, b, c]).size).toBe(3);
  });

  it("tolerates corrupt JSON", () => {
    localStorage.setItem("ocode.ui.umlViewState.v1", "{not json");
    expect(loadUmlViewState("x")).toBeUndefined();
  });
});
