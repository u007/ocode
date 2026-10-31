import { beforeEach, describe, expect, it } from "vitest";
import { formatEntry, log, logClear, logSnapshot, logSubscribe } from "./log";

describe("pane log", () => {
  beforeEach(() => logClear());

  it("records entries in order and notifies subscribers", () => {
    let n = 0;
    const off = logSubscribe(() => n++);
    log("info", "tool", "read_range ok", 12);
    log("error", "ocode", "POST /api/chat -> 400 no model configured");
    off();
    log("info", "x", "after unsubscribe");
    expect(n).toBe(2);
    expect(logSnapshot().map((e) => e.level)).toEqual(["info", "error", "info"]);
  });

  it("keeps only the newest 500 lines", () => {
    for (let i = 0; i < 520; i++) log("info", "t", `m${i}`);
    expect(logSnapshot()).toHaveLength(500);
    expect(logSnapshot()[0]!.msg).toBe("m20");
  });

  it("formats a line with level, source and timing", () => {
    log("warn", "bridge", "stream error", 7);
    expect(formatEntry(logSnapshot()[0]!)).toMatch(/WARN\s+bridge: stream error \(7 ms\)$/);
  });
});
