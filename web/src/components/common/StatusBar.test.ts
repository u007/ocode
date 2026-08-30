import { describe, expect, it } from "vitest";
import { runningStatusPartsForTests as parts } from "./StatusBar";
import type { TUIStatus } from "../../api/types";

// The status bar is the single session-wide "working" indicator (the chat
// transcript's "Working…" label was removed in its favor). These tests pin its
// two contracts:
//   1. Presence follows the authoritative per-session liveness flag
//      (isStreaming || turnActive) — never the (possibly stale/late) TUI
//      snapshot — so it disappears immediately on turn_done/turn_error.
//   2. Within a running turn it prefers snapshot details, falls back to the
//      in-flight live tool, and finally to the base "working…" label — without
//      ever duplicating tool info from two sources.
describe("StatusBar runningStatusParts", () => {
  it("shows nothing when idle with no snapshot", () => {
    expect(parts(false, null, undefined)).toEqual([]);
  });

  it("shows nothing when idle even if a stale TUI snapshot still reports activity", () => {
    const snap: TUIStatus = {
      llm_running: true,
      active_tools: [{ name: "bash" }],
      active_agents: ["explore"],
    };
    // turn_done/turn_error already cleared isRunning; a late "status" event
    // must not resurrect the indicator.
    expect(parts(false, snap, "grep")).toEqual([]);
  });

  it("shows the snapshot LLM activity while running", () => {
    expect(parts(true, { llm_running: true }, undefined)).toEqual(["⟳ llm"]);
  });

  it("orders snapshot details as llm · agents · tools", () => {
    const snap: TUIStatus = {
      llm_running: true,
      active_agents: ["explore", "general"],
      active_tools: [{ name: "read" }],
    };
    expect(parts(true, snap, undefined)).toEqual([
      "⟳ llm",
      "@ explore",
      "@ general",
      "⚙ read",
    ]);
  });

  it("renders a tool start time like the TUI activity row", () => {
    const started = new Date(2026, 7, 30, 12, 34, 56).toISOString();
    const out = parts(true, { active_tools: [{ name: "bash", started_at: started }] }, undefined);
    expect(out).toHaveLength(1);
    expect(out[0]).toMatch(/^⚙ bash \[\d{2}:\d{2}:\d{2}\]$/);
  });

  it("truncates overly long tool names", () => {
    const long = "a-very-long-tool-name-overflow";
    const out = parts(true, { active_tools: [{ name: long }] }, undefined);
    expect(out[0]).toBe("⚙ a-very-long-tool-name…");
  });

  it("falls back to the in-flight live tool in headless/desktop mode", () => {
    expect(parts(true, null, "grep")).toEqual(["⚙ grep"]);
    expect(parts(true, {}, "bash")).toEqual(["⚙ bash"]);
  });

  it("does not duplicate the live tool when the snapshot already reports activity", () => {
    // Snapshot has a different in-flight tool; the live-buffer name must not
    // be appended next to it.
    expect(parts(true, { active_tools: [{ name: "read" }] }, "bash")).toEqual(["⚙ read"]);
    expect(parts(true, { llm_running: true }, "bash")).toEqual(["⟳ llm"]);
  });

  it("shows the base working label during silent gaps of a running turn", () => {
    expect(parts(true, null, undefined)).toEqual(["working…"]);
    expect(parts(true, {}, undefined)).toEqual(["working…"]);
    // isStreaming alone (deltas flowing, no tool in flight) still reads working.
    expect(parts(true, {}, undefined)).toEqual(["working…"]);
  });
});
