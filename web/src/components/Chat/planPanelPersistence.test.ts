import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  PLAN_PANEL_COLLAPSED_STORAGE_KEY as KEY,
  loadPlanPanelCollapsed,
  savePlanPanelCollapsed,
  subscribePlanPanelCollapsed,
} from "./planPanelPersistence";

describe("planPanelPersistence", () => {
  beforeEach(() => {
    window.localStorage.removeItem(KEY);
  });

  it("returns null when the session has no stored choice", () => {
    expect(loadPlanPanelCollapsed("ses_a")).toBeNull();
  });

  it("persists the choice per session", () => {
    savePlanPanelCollapsed("ses_a", true);
    savePlanPanelCollapsed("ses_b", false);
    expect(loadPlanPanelCollapsed("ses_a")).toBe(true);
    expect(loadPlanPanelCollapsed("ses_b")).toBe(false);
    expect(loadPlanPanelCollapsed("ses_c")).toBeNull();
  });

  it("logs and ignores corrupt storage", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    window.localStorage.setItem(KEY, "not json");
    expect(loadPlanPanelCollapsed("ses_a")).toBeNull();
    expect(warn).toHaveBeenCalledWith("plan panel: read fold state from localStorage failed", expect.anything());
    warn.mockRestore();
    window.localStorage.setItem(KEY, "true");
    expect(loadPlanPanelCollapsed("ses_a")).toBeNull();
  });

  it("logs a failed write and still notifies subscribers", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const setItem = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });
    const handler = vi.fn();
    const off = subscribePlanPanelCollapsed(handler);
    savePlanPanelCollapsed("ses_q", true);
    expect(warn).toHaveBeenCalledWith("plan panel: save fold state to localStorage failed", expect.any(Error));
    expect(handler).toHaveBeenCalledWith("ses_q", true);
    off();
    setItem.mockRestore();
    warn.mockRestore();
  });

  it("drops the oldest sessions past the cap", () => {
    for (let i = 0; i < 205; i++) savePlanPanelCollapsed(`ses_${i}`, true);
    expect(loadPlanPanelCollapsed("ses_0")).toBeNull();
    expect(loadPlanPanelCollapsed("ses_204")).toBe(true);
  });

  it("notifies same-tab subscribers and stops after unsubscribe", () => {
    const handler = vi.fn();
    const off = subscribePlanPanelCollapsed(handler);
    savePlanPanelCollapsed("ses_a", true);
    expect(handler).toHaveBeenCalledWith("ses_a", true);
    off();
    savePlanPanelCollapsed("ses_a", false);
    expect(handler).toHaveBeenCalledTimes(1);
  });
});
