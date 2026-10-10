import { describe, expect, it } from "vitest";
import { bashCommandFromArgs, formatToolArgsHint, skillNameFromArgs } from "./toolHint";

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

  // Without this case the header read a bare "skill" and the name sat hidden in
  // a collapsed JSON block. `load_skill` is the registered alias some models
  // emit instead (internal/tool/misc.go SkillAliasTool).
  it("names the skill for both the skill tool and its load_skill alias", () => {
    expect(formatToolArgsHint("skill", '{"name":"git-commit-push"}')).toBe('Skill "git-commit-push"');
    expect(formatToolArgsHint("load_skill", '{"name":"git-commit-push"}')).toBe('Skill "git-commit-push"');
  });

  it("returns empty for a skill call with no usable name", () => {
    expect(formatToolArgsHint("skill", "{}")).toBe("");
    expect(formatToolArgsHint("skill", '{"name":42}')).toBe("");
    expect(formatToolArgsHint("skill", "not json")).toBe("");
    expect(formatToolArgsHint("skill", undefined)).toBe("");
  });
});

describe("skillNameFromArgs", () => {
  it("extracts the name the composer bar shows", () => {
    expect(skillNameFromArgs('{"name":"brainstorming"}')).toBe("brainstorming");
  });

  it("returns empty when the name is missing or the payload is malformed", () => {
    expect(skillNameFromArgs("{}")).toBe("");
    expect(skillNameFromArgs('{"name":42}')).toBe("");
    expect(skillNameFromArgs("not json")).toBe("");
    expect(skillNameFromArgs(undefined)).toBe("");
    expect(skillNameFromArgs('["skill"]')).toBe("");
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
