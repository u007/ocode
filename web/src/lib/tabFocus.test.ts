import { beforeEach, describe, expect, it } from "vitest";
import { tabFocusActions, tabFocusStore } from "./tabFocus";

describe("tabFocusStore", () => {
  beforeEach(() => tabFocusActions.clear());

  it("queues a request and clears it once applied", () => {
    expect(tabFocusStore.state.pending).toBeNull();
    const req = { kind: "chat" as const, projectPath: "/srv", host: "dev@box" };
    tabFocusActions.request(req);
    expect(tabFocusStore.state.pending).toEqual(req);
    tabFocusActions.clear();
    expect(tabFocusStore.state.pending).toBeNull();
  });

  it("keeps the latest request when several are queued", () => {
    tabFocusActions.request({ kind: "chat", projectPath: "/a" });
    tabFocusActions.request({ kind: "terminal", projectPath: "/b", terminalId: "t1" });
    expect(tabFocusStore.state.pending).toEqual({
      kind: "terminal",
      projectPath: "/b",
      terminalId: "t1",
    });
  });

  it("clear is a no-op (same state object) when nothing is pending", () => {
    const before = tabFocusStore.state;
    tabFocusActions.clear();
    expect(tabFocusStore.state).toBe(before);
  });
});
