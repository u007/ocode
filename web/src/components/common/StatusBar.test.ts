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
    expect(parts(false, null, undefined, Date.now())).toEqual([]);
  });

  it("shows nothing when idle even if a stale TUI snapshot still reports activity", () => {
    const snap: TUIStatus = {
      llm_running: true,
      active_tools: [{ name: "bash" }],
      active_agents: ["explore"],
    };
    // turn_done/turn_error already cleared isRunning; a late "status" event
    // must not resurrect the indicator.
    expect(parts(false, snap, "grep", Date.now())).toEqual([]);
  });

  it("shows the snapshot LLM activity while running", () => {
    expect(parts(true, { llm_running: true }, undefined, Date.now())).toEqual(["⟳ llm"]);
  });

  it("orders snapshot details as llm · agents · tools", () => {
    const snap: TUIStatus = {
      llm_running: true,
      active_agents: ["explore", "general"],
      active_tools: [{ name: "read" }],
    };
    expect(parts(true, snap, undefined, Date.now())).toEqual([
      "⟳ llm",
      "@ explore",
      "@ general",
      "⚙ read",
    ]);
  });

  it("renders a tool start time like the TUI activity row", () => {
    const started = new Date(2026, 7, 30, 12, 34, 56).toISOString();
    // Use a now close to start so elapsed <1s and old format is preserved.
    const nowClose = new Date(started).getTime() + 500;
    const out = parts(true, { active_tools: [{ name: "bash", started_at: started }] }, undefined, nowClose);
    expect(out).toHaveLength(1);
    expect(out[0]).toMatch(/^⚙ bash \[\d{2}:\d{2}:\d{2}\]$/);
  });

  it("truncates overly long tool names", () => {
    const long = "a-very-long-tool-name-overflow";
    const out = parts(true, { active_tools: [{ name: long }] }, undefined, Date.now());
    expect(out[0]).toBe("⚙ a-very-long-tool-name…");
  });

  it("falls back to the in-flight live tool in headless/desktop mode", () => {
    expect(parts(true, null, "grep", Date.now())).toEqual(["⚙ grep"]);
    expect(parts(true, {}, "bash", Date.now())).toEqual(["⚙ bash"]);
  });

  it("does not duplicate the live tool when the snapshot already reports activity", () => {
    // Snapshot has a different in-flight tool; the live-buffer name must not
    // be appended next to it.
    expect(parts(true, { active_tools: [{ name: "read" }] }, "bash", Date.now())).toEqual(["⚙ read"]);
    expect(parts(true, { llm_running: true }, "bash", Date.now())).toEqual(["⟳ llm"]);
  });

  it("shows the base working label during silent gaps of a running turn", () => {
    expect(parts(true, null, undefined, Date.now())).toEqual(["working…"]);
    expect(parts(true, {}, undefined, Date.now())).toEqual(["working…"]);
    // isStreaming alone (deltas flowing, no tool in flight) still reads working.
    expect(parts(true, {}, undefined, Date.now())).toEqual(["working…"]);
  });

  it("includes elapsed on LLM when turn has been running", () => {
    const started = new Date(Date.now() - 12_000).toISOString();
    const out = parts(true, { llm_running: true, turn_started_at: started }, undefined, Date.now());
    expect(out[0]).toMatch(/⟳ llm · \d+s/);
  });

  it("includes elapsed on tool after 1s", () => {
    const started = new Date(Date.now() - 5_000).toISOString();
    const out = parts(true, { active_tools: [{ name: "bash", started_at: started }] }, undefined, Date.now());
    expect(out[0]).toMatch(/⚙ bash \[.* · 5s\]/);
  });

  it("shows working with elapsed when no LLM/tools but turn is running", () => {
    const started = new Date(Date.now() - 8_000).toISOString();
    const out = parts(true, { turn_started_at: started }, undefined, Date.now());
    expect(out).toEqual(["working… · 8s"]);
  });
});
