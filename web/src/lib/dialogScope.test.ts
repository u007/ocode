import { describe, it, expect } from "vitest";
import { sessionAskSurfaceVisible } from "./dialogScope";

describe("sessionAskSurfaceVisible", () => {
  it("allows the ask only on the session's Chat sub-tab of the Sessions view", () => {
    expect(
      sessionAskSurfaceVisible({
        activeView: "sessions",
        focusedKind: "chat",
        activeSubTab: "chat",
      }),
    ).toBe(true);
  });

  it.each(["files", "git", "cron", "assets", "settings"] as const)(
    "hides the ask on the %s view",
    (activeView) => {
      expect(
        sessionAskSurfaceVisible({ activeView, focusedKind: "chat", activeSubTab: "chat" }),
      ).toBe(false);
    },
  );

  it.each(["terminal", "browser"] as const)(
    "hides the ask when the %s half of Sessions is focused",
    (focusedKind) => {
      expect(
        sessionAskSurfaceVisible({ activeView: "sessions", focusedKind, activeSubTab: "chat" }),
      ).toBe(false);
    },
  );

  it.each(["agents", "changes", "logs", "status", "preview"] as const)(
    "hides the ask on the %s sub-tab of the same session",
    (activeSubTab) => {
      expect(
        sessionAskSurfaceVisible({ activeView: "sessions", focusedKind: "chat", activeSubTab }),
      ).toBe(false);
    },
  );

  it("hides the ask when no session tab is active", () => {
    expect(
      sessionAskSurfaceVisible({ activeView: "sessions", focusedKind: "chat", activeSubTab: undefined }),
    ).toBe(false);
    expect(
      sessionAskSurfaceVisible({ activeView: "sessions", focusedKind: "chat", activeSubTab: null }),
    ).toBe(false);
  });
});
