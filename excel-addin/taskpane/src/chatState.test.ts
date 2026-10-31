import { describe, expect, it } from "vitest";
import { fromTranscript, initialState, reduce, resync } from "./chatState";
import type { Envelope } from "./ocodeClient";

let seq = 0;
const ev = (event: string, data: any, session_id = "s1"): Envelope => ({ event, data, session_id, seq: ++seq });
const run = (...es: Envelope[]) => es.reduce(reduce, initialState);

describe("chat reducer", () => {
  it("streams text into one assistant bubble and closes it on turn_done", () => {
    const s = run(ev("user_message", { content: "hi", user_seq: 1 }), ev("turn_started", {}), ev("text", { delta: "Hel" }), ev("text", { delta: "lo" }), ev("turn_done", {}));
    expect(s.sessions.s1.messages.map((m) => [m.kind, m.text, m.status])).toEqual([["user", "hi", undefined], ["assistant", "Hello", "done"]]);
    expect(s.sessions.s1.streaming).toBe(false);
  });
  it("pairs tool results to their start by call id, even when parallel and out of order", () => {
    const s = run(ev("tool_start", { tool: "excel_read_range", call_id: "a", command: "{}" }), ev("tool_start", { tool: "excel_check_errors", call_id: "b" }), ev("tool_result", { call_id: "b", output: "B" }), ev("tool_result", { call_id: "a", output: "A" }));
    expect(s.sessions.s1.messages.map((m) => [m.tool, m.output, m.status])).toEqual([["excel_read_range", "A", "done"], ["excel_check_errors", "B", "done"]]);
  });
  it("text after a tool call opens a new bubble below it", () => {
    const s = run(ev("text", { delta: "Reading" }), ev("tool_start", { tool: "t", call_id: "a" }), ev("text", { delta: "Done" }));
    expect(s.sessions.s1.messages.map((m) => m.kind)).toEqual(["assistant", "tool", "assistant"]);
  });
  it("tracks pending permissions and questions until resolved, and dedupes a repeated ask", () => {
    let s = run(ev("permission", { request_id: "p1", tool: "excel_write_range" }), ev("permission", { request_id: "p1", tool: "excel_write_range" }), ev("question", { request_id: "q1", questions: [] }));
    expect(s.sessions.s1.permissions).toHaveLength(1);
    expect(s.sessions.s1.questions).toHaveLength(1);
    s = [ev("permission_resolved", { request_id: "p1" }), ev("question_resolved", { request_id: "q1" })].reduce(reduce, s);
    expect(s.sessions.s1.permissions).toEqual([]);
    expect(s.sessions.s1.questions).toEqual([]);
  });
  it("ignores replayed frames and keeps sessions apart", () => {
    const a = ev("text", { delta: "x" }, "s1");
    const s = run(a, a, ev("text", { delta: "y" }, "s2"));
    expect(s.sessions.s1.messages[0].text).toBe("x");
    expect(s.sessions.s2.messages[0].text).toBe("y");
  });
  it("does not duplicate a user message already present from the snapshot", () => {
    const s = run(ev("user_message", { content: "hi", user_seq: 4 }), ev("user_message", { content: "hi", user_seq: 4 }), ev("user_message", { content: "hi", user_seq: 5 }));
    expect(s.sessions.s1.messages.filter((m) => m.kind === "user")).toHaveLength(2);
  });
  it("turn_error stops streaming and surfaces the message", () => {
    const s = run(ev("turn_started", {}), ev("turn_error", { error: "rate limited" }));
    expect(s.sessions.s1).toMatchObject({ streaming: false, error: "rate limited" });
  });
  it("rebuilds history from a stored transcript, attaching tool output to its call", () => {
    const m = fromTranscript([
      { role: "user", content: "total?" },
      { role: "assistant", content: "Let me look", tool_calls: [{ id: "c1", function: { name: "excel_read_range", arguments: "{}" } }] },
      { role: "tool", content: "42", tool_call_id: "c1" },
      { role: "assistant", content: "It is 42" },
    ]);
    expect(m.map((x) => [x.kind, x.text || x.tool, x.output])).toEqual([["user", "total?", undefined], ["assistant", "Let me look", undefined], ["tool", "excel_read_range", "42"], ["assistant", "It is 42", undefined]]);
  });
  it("restores a compaction summary as a notice with the marker stripped", () => {
    const m = fromTranscript([
      { role: "user", content: "make a chart" },
      { role: "system", content: "[ocode:compaction-summary]\n## Original Request\nmake a chart" },
    ]);
    expect(m).toEqual([
      { id: "h0", kind: "user", text: "make a chart" },
      { id: "h1", kind: "notice", text: "## Original Request\nmake a chart" },
    ]);
  });
  it("ignores ordinary system messages", () => {
    const m = fromTranscript([{ role: "system", content: "You are a spreadsheet assistant." }, { role: "user", content: "hi" }]);
    expect(m.map((x) => [x.kind, x.text])).toEqual([["user", "hi"]]);
  });
  it("leaves assistant and tool messages unchanged next to a compaction notice", () => {
    const m = fromTranscript([
      { role: "system", content: "[ocode:compaction-summary]\nsummary" },
      { role: "assistant", content: "", tool_calls: [{ id: "c2", function: { name: "excel_check_errors", arguments: "{}" } }] },
      { role: "tool", content: "none", tool_call_id: "c2" },
      { role: "assistant", content: "No errors" },
    ]);
    expect(m.map((x) => [x.kind, x.text || x.tool, x.output])).toEqual([["notice", "summary", undefined], ["tool", "excel_check_errors", "none"], ["assistant", "No errors", undefined]]);
  });
  it("resync replaces the view with the transcript and the pending asks from state", () => {
    const s = resync(run(ev("text", { delta: "stale" })), "s1", [{ role: "user", content: "q" }], { permissions: [{ request_id: "p", tool: "t" }] });
    expect(s.sessions.s1.messages).toHaveLength(1);
    expect(s.sessions.s1.permissions).toHaveLength(1);
    expect(s.sessions.s1.streaming).toBe(false);
  });
});
