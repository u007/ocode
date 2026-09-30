import { describe, it, expect } from "vitest";
import {
  resolveViewOnProjectSwitch,
  consumePendingJumpView,
  DEFAULT_VIEW_STATE,
} from "./viewPersistence";

/**
 * A Pulse card jump must land on the chat surface, never on the view the
 * target project happened to be left showing. These tests pin that precedence,
 * which is the part of App that is easy to regress silently.
 */
describe("resolveViewOnProjectSwitch", () => {
  const savedFiles = { view: "files" as const, focusedKind: "chat" as const };

  it("restores the saved view when no jump is pending", () => {
    expect(resolveViewOnProjectSwitch(savedFiles, null, "/p")).toEqual(savedFiles);
  });

  it("falls back to the chat surface for an unseen project", () => {
    expect(resolveViewOnProjectSwitch(null, null, "/p")).toEqual(DEFAULT_VIEW_STATE);
  });

  // The bug: a card click opened the tab but left the user on Files/Git in
  // the target project, then saved that foreign view against it.
  it("lets a jump onto this project override the saved view", () => {
    const forced = { path: "/target", view: "sessions" as const };
    expect(resolveViewOnProjectSwitch(savedFiles, forced, "/target")).toEqual({
      view: "sessions",
      focusedKind: "chat",
    });
  });

  // A same-project jump never re-runs the switch effect, so its arm lingers.
  // It must not dictate an unrelated later switch.
  it("ignores a jump armed for a different project", () => {
    const forced = { path: "/other", view: "sessions" as const };
    expect(resolveViewOnProjectSwitch(savedFiles, forced, "/target")).toEqual(savedFiles);
  });

  it("forces the chat kind, not just the view, on a jump", () => {
    const savedTerminal = { view: "sessions" as const, focusedKind: "terminal" as const };
    const forced = { path: "/target", view: "sessions" as const };
    expect(resolveViewOnProjectSwitch(savedTerminal, forced, "/target").focusedKind).toBe("chat");
  });

  it("a jump target project with no saved state still lands on chat", () => {
    const forced = { path: "/fresh", view: "sessions" as const };
    expect(resolveViewOnProjectSwitch(null, forced, "/fresh")).toEqual({
      view: "sessions",
      focusedKind: "chat",
    });
  });
});

describe("consumePendingJumpView", () => {
  const savedFiles = { view: "files" as const, focusedKind: "chat" as const };

  it("returns the arm for the project being switched to, and empties the slot", () => {
    const pending = { current: { path: "/target", view: "sessions" as const } };
    expect(consumePendingJumpView(pending, "/target")).toEqual({
      path: "/target",
      view: "sessions",
    });
    expect(pending.current).toBeNull();
  });

  // The stale-arm case. A same-project jump never re-runs the switch effect, so
  // its arm is still set when the user later switches somewhere unrelated. If
  // the slot were not cleared on a mismatch, that unrelated switch would be
  // hijacked into showing the chat surface.
  it("clears an arm that names a different project, so it cannot hijack a later switch", () => {
    const pending = { current: { path: "/stale", view: "sessions" as const } };

    expect(consumePendingJumpView(pending, "/other")).toBeNull();
    expect(pending.current).toBeNull();

    // A second switch is unaffected — the arm cannot fire twice.
    expect(consumePendingJumpView(pending, "/stale")).toBeNull();
    expect(resolveViewOnProjectSwitch(savedFiles, consumePendingJumpView(pending, "/stale"), "/stale"))
      .toEqual(savedFiles);
  });

  it("returns null and stays null when nothing is armed", () => {
    const pending = { current: null };
    expect(consumePendingJumpView(pending, "/target")).toBeNull();
    expect(pending.current).toBeNull();
  });

  it("an honoured arm also cannot fire twice", () => {
    const pending = { current: { path: "/target", view: "sessions" as const } };
    expect(consumePendingJumpView(pending, "/target")).not.toBeNull();
    expect(consumePendingJumpView(pending, "/target")).toBeNull();
  });
});
