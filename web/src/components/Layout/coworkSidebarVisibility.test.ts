import { describe, expect, it } from "vitest";
import { shouldRenderCoworkSidebar } from "./coworkSidebarVisibility";

describe("shouldRenderCoworkSidebar", () => {
  it("shows the sidebar on the Sessions view with the Chat sub-tab focused", () => {
    expect(
      shouldRenderCoworkSidebar({ activeView: "sessions", activeSubTab: "chat", focusedKind: "chat" }),
    ).toBe(true);
  });

  it("hides the sidebar while the terminal is focused", () => {
    expect(
      shouldRenderCoworkSidebar({ activeView: "sessions", activeSubTab: "chat", focusedKind: "terminal" }),
    ).toBe(false);
  });

  it("restores the sidebar when focus returns to chat (coworkOpen is preserved by the caller)", () => {
    // After switching away from the terminal back to chat, the mount gate re-opens.
    expect(
      shouldRenderCoworkSidebar({ activeView: "sessions", activeSubTab: "chat", focusedKind: "chat" }),
    ).toBe(true);
  });

  it("does not render outside the Sessions view", () => {
    expect(
      shouldRenderCoworkSidebar({ activeView: "files", activeSubTab: "chat", focusedKind: "chat" }),
    ).toBe(false);
    expect(
      shouldRenderCoworkSidebar({ activeView: "settings", activeSubTab: "chat", focusedKind: "chat" }),
    ).toBe(false);
  });

  it("does not render on non-chat session sub-tabs (agents/changes/logs/status)", () => {
    for (const subTab of ["agents", "changes", "logs", "status"] as const) {
      expect(shouldRenderCoworkSidebar({ activeView: "sessions", activeSubTab: subTab, focusedKind: "chat" })).toBe(
        false,
      );
    }
  });

  it("does not render when there is no active session tab", () => {
    expect(
      shouldRenderCoworkSidebar({ activeView: "sessions", activeSubTab: undefined, focusedKind: "chat" }),
    ).toBe(false);
  });
});
