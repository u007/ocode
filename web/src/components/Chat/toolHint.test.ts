import { describe, expect, it } from "vitest";
import { bashCommandFromArgs, formatToolArgsHint } from "./toolHint";

describe("formatToolArgsHint", () => {
  it("summarizes bash as a shell prompt line", () => {
    expect(formatToolArgsHint("bash", '{"command":"ls -la /tmp"}')).toBe("$ ls -la /tmp");
  });

  it("summarizes read with offset/limit when present", () => {
    expect(formatToolArgsHint("read", '{"path":"/src/app.ts","offset":10,"limit":20}')).toBe(
      "read /src/app.ts offset=10 limit=20",
    );
    expect(formatToolArgsHint("read", '{"path":"/src/app.ts","offset":10}')).toBe(
      "read /src/app.ts offset=10",
    );
    expect(formatToolArgsHint("read", '{"path":"/src/app.ts"}')).toBe("read /src/app.ts");
  });

  it("summarizes write as the target path only (never the file body)", () => {
    const hint = formatToolArgsHint("write", '{"path":"/src/new.ts","content":"SECRET_BODY"}');
    expect(hint).toBe("write /src/new.ts");
    expect(hint).not.toContain("SECRET_BODY");
  });

  it("returns empty for tools without a summary", () => {
    expect(formatToolArgsHint("glob", '{"pattern":"**/*.ts"}')).toBe("");
    expect(formatToolArgsHint("edit", '{"path":"/src/app.ts"}')).toBe("");
  });

  it("returns empty for missing or malformed arguments", () => {
    expect(formatToolArgsHint("bash", undefined)).toBe("");
    expect(formatToolArgsHint("bash", "")).toBe("");
    expect(formatToolArgsHint("bash", "not json")).toBe("");
    expect(formatToolArgsHint("bash", "[]")).toBe("");
  });

  it("falls back to empty for bash with no command", () => {
    expect(formatToolArgsHint("bash", "{}")).toBe("");
  });
});

describe("bashCommandFromArgs", () => {
  it("extracts the raw command without the shell prompt prefix", () => {
    expect(bashCommandFromArgs('{"command":"ls -la /tmp"}')).toBe("ls -la /tmp");
  });

  it("returns empty when the command is missing or the payload is malformed", () => {
    expect(bashCommandFromArgs(undefined)).toBe("");
    expect(bashCommandFromArgs("")).toBe("");
    expect(bashCommandFromArgs("not json")).toBe("");
    expect(bashCommandFromArgs("[]")).toBe("");
    expect(bashCommandFromArgs("{}")).toBe("");
  });
});
