import { describe, expect, it } from "vitest";
import { shouldRenderSidePane } from "./sidePaneVisibility";
import type { SessionSubTabId } from "../stores/projectStore";

const base = { activeView: "sessions", focusedKind: "chat" as const };

describe("shouldRenderSidePane", () => {
  it("renders on the chat sub-tab", () => {
    expect(shouldRenderSidePane({ ...base, activeSubTab: "chat" })).toBe(true);
  });

  it("renders while the terminal is focused (its sub-tab value is chat)", () => {
    expect(shouldRenderSidePane({ activeView: "sessions", activeSubTab: "chat", focusedKind: "terminal" })).toBe(true);
  });

  it("hides on non-chat session sub-tabs", () => {
    for (const sub of ["agents", "changes", "logs", "status", "preview"] as SessionSubTabId[]) {
      expect(shouldRenderSidePane({ ...base, activeSubTab: sub }, ), `sub=${sub}`).toBe(false);
    }
  });

  it("hides outside the sessions view and for browser focus", () => {
    expect(shouldRenderSidePane({ activeView: "files", activeSubTab: "chat", focusedKind: "chat" })).toBe(false);
    expect(shouldRenderSidePane({ activeView: "sessions", activeSubTab: "chat", focusedKind: "browser" })).toBe(false);
  });

  it("hides with no session tab", () => {
    expect(shouldRenderSidePane({ activeView: "sessions", activeSubTab: undefined, focusedKind: "chat" })).toBe(false);
  });

  it("never renders on mobile, even on the chat sub-tab", () => {
    expect(shouldRenderSidePane({ ...base, activeSubTab: "chat", isMobile: true })).toBe(false);
    // terminal focus is likewise gated off on mobile
    expect(
      shouldRenderSidePane({ activeView: "sessions", activeSubTab: "chat", focusedKind: "terminal", isMobile: true }),
    ).toBe(false);
  });

  it("still renders on desktop (isMobile false / undefined)", () => {
    expect(shouldRenderSidePane({ ...base, activeSubTab: "chat", isMobile: false })).toBe(true);
    expect(shouldRenderSidePane({ ...base, activeSubTab: "chat" })).toBe(true);
  });
});