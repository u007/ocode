import { describe, expect, it } from "vitest";
import { isInstantCommand } from "./instantCommands";

describe("isInstantCommand", () => {
  it("matches both /btw aliases, with or without an argument", () => {
    for (const text of ["/btw", "/btw use tabs", "/by-the-way", "/by-the-way use tabs"]) {
      expect(isInstantCommand(text), text).toBe(true);
    }
  });

  it("is case-insensitive and tolerates surrounding whitespace", () => {
    expect(isInstantCommand("  /BTW use tabs  ")).toBe(true);
    expect(isInstantCommand("/By-The-Way x")).toBe(true);
  });

  it("does not match a command that merely starts with an instant one", () => {
    // A prefix match would let "/btwx" (or a future "/btw-something") bypass
    // the queue without having a server-side mid-turn path.
    expect(isInstantCommand("/btwx hello")).toBe(false);
    expect(isInstantCommand("/btw-later hello")).toBe(false);
  });

  it("does not match commands that mutate or start a turn", () => {
    for (const text of ["/compact", "/clear", "/new", "/undo", "/recap", "hello", "!ls"]) {
      expect(isInstantCommand(text), text).toBe(false);
    }
  });

  it("does not match an instant command word buried in prose", () => {
    expect(isInstantCommand("tell me about /btw")).toBe(false);
  });
});
