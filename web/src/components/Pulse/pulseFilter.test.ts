import { describe, it, expect } from "vitest";
import { pulseProjectBasename, pulseRowMatchesFilter } from "./pulseFilter";
import type { PulseRow } from "@/api/types";

function row(over: Partial<PulseRow> & { session_id: string }): PulseRow {
  return {
    project_path: "/proj/alpha",
    title: "Fix the parser",
    status: "idle",
    current_task: null,
    todo: null,
    pending_ask: null,
    turn_started_at: "",
    updated_at: "2020-01-01T00:00:00Z",
    child_count: 0,
    ...over,
  };
}

describe("pulseProjectBasename", () => {
  it("returns the last path segment", () => {
    expect(pulseProjectBasename("/Users/james/www/aimsai2")).toBe("aimsai2");
  });

  it("strips trailing slashes instead of returning an empty label", () => {
    expect(pulseProjectBasename("/proj/alpha/")).toBe("alpha");
    expect(pulseProjectBasename("/proj/alpha///")).toBe("alpha");
  });

  it("handles a bare name and the filesystem root", () => {
    expect(pulseProjectBasename("alpha")).toBe("alpha");
    expect(pulseProjectBasename("/")).toBe("");
  });

  it("handles a Windows-style path", () => {
    // The last separator is still "/" in the URLs the server emits, but a path
    // copied from a Windows config can carry backslashes; at minimum the
    // helper must not throw or return the whole string.
    expect(pulseProjectBasename("C:\\dev\\app")).toBe("C:\\dev\\app");
  });
});

describe("pulseRowMatchesFilter", () => {
  it("an empty or whitespace-only needle matches everything", () => {
    expect(pulseRowMatchesFilter(row({ session_id: "a" }), "")).toBe(true);
    expect(pulseRowMatchesFilter(row({ session_id: "a" }), "   ")).toBe(true);
  });

  it("matches the project basename case-insensitively", () => {
    expect(pulseRowMatchesFilter(row({ session_id: "a" }), "ALPHA")).toBe(true);
  });

  it("matches the title case-insensitively", () => {
    expect(pulseRowMatchesFilter(row({ session_id: "a" }), "PARSER")).toBe(true);
  });

  it("does NOT match a directory segment the user never typed", () => {
    // "/proj/alpha" must not match "proj": a full-path substring match would
    // light up every project under a common parent, which is indistinguishable
    // from a broken filter.
    expect(pulseRowMatchesFilter(row({ session_id: "a" }), "proj")).toBe(false);
  });

  it("does not match a substring of a different project", () => {
    expect(pulseRowMatchesFilter(row({ session_id: "a" }), "beta")).toBe(false);
  });
});
