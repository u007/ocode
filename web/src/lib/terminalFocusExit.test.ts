import { describe, expect, it } from "vitest";
import { shouldLeaveTerminalView } from "./terminalFocusExit";

/**
 * Closing the last terminal hands the user back to the chat, because
 * `focusedKind` is persisted per project — leaving it on "terminal" would
 * restore an empty terminal panel on every launch. See the module doc for the
 * full rationale and for the paths that must NOT trigger it.
 *
 * The input is the POST-close count reported by the store, never a count the
 * caller measured beforehand. That is the property under test here: a caller
 * holding a pre-close snapshot cannot even express the decision any more, so
 * the stale-snapshot race is closed structurally instead of by convention.
 */
describe("shouldLeaveTerminalView", () => {
  const base = { remaining: 0, focusedKind: "terminal" as const };

  it("leaves when the close emptied the focused terminal view", () => {
    expect(shouldLeaveTerminalView(base)).toBe(true);
  });

  it("stays when the store reports a terminal still open after the close", () => {
    // The regression this shape exists for. A pre-close snapshot saying "1"
    // while the store held 2 made the caller treat this close as the last one
    // and hand the user back to the chat with a shell still running. Keyed on
    // the post-close count, "1 left" is unambiguously "stay".
    expect(shouldLeaveTerminalView({ ...base, remaining: 1 })).toBe(false);
    expect(shouldLeaveTerminalView({ ...base, remaining: 2 })).toBe(false);
  });

  it("stays when the close removed nothing (a repeated close of a gone terminal)", () => {
    // `null`, not `false`: the store distinguishes "removed nothing" from a
    // count. Collapsing the two would let a failed close hand the user away
    // from a view that still has a live terminal in it.
    expect(shouldLeaveTerminalView({ ...base, remaining: null })).toBe(false);
  });

  it("stays when the terminal view is not what the user is looking at", () => {
    expect(shouldLeaveTerminalView({ ...base, focusedKind: "chat" })).toBe(false);
    expect(shouldLeaveTerminalView({ ...base, focusedKind: "browser" })).toBe(false);
  });
});
