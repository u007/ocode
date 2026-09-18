import { describe, it, expect, beforeEach } from "vitest";
import {
  loadAgentPreviewCollapsed,
  saveAgentPreviewCollapsed,
  subscribeAgentPreviewCollapsed,
  AGENT_PREVIEW_COLLAPSED_STORAGE_KEY,
} from "./agentPreviewPersistence";

const KEY = AGENT_PREVIEW_COLLAPSED_STORAGE_KEY;

describe("agentPreviewPersistence", () => {
  beforeEach(() => {
    window.localStorage.removeItem(KEY);
  });

  it("defaults to false (expanded) when nothing stored", () => {
    expect(loadAgentPreviewCollapsed()).toBe(false);
  });

  it("persists true", () => {
    saveAgentPreviewCollapsed(true);
    expect(loadAgentPreviewCollapsed()).toBe(true);
    expect(window.localStorage.getItem(KEY)).toBe("true");
  });

  it("persists false", () => {
    saveAgentPreviewCollapsed(false);
    expect(loadAgentPreviewCollapsed()).toBe(false);
    expect(window.localStorage.getItem(KEY)).toBe("false");
  });

  it("falls back to false on invalid value", () => {
    window.localStorage.setItem(KEY, "maybe");
    expect(loadAgentPreviewCollapsed()).toBe(false);
  });

  it("tolerates missing localStorage", () => {
    const orig = window.localStorage;
    delete (window as unknown as Record<string, unknown>).localStorage;
    expect(loadAgentPreviewCollapsed()).toBe(false);
    (window as unknown as { localStorage: Storage }).localStorage = orig;
  });

  it("notifies same-document subscribers on save", () => {
    const seen: boolean[] = [];
    const unsub = subscribeAgentPreviewCollapsed((v) => seen.push(v));
    saveAgentPreviewCollapsed(true);
    saveAgentPreviewCollapsed(false);
    unsub();
    saveAgentPreviewCollapsed(true);
    expect(seen).toEqual([true, false]);
  });

  it("notifies subscribers on cross-document storage events", () => {
    const seen: boolean[] = [];
    const unsub = subscribeAgentPreviewCollapsed((v) => seen.push(v));
    window.dispatchEvent(new StorageEvent("storage", { key: KEY, newValue: "true" }));
    unsub();
    expect(seen).toEqual([true]);
  });
});
