import { describe, expect, it } from "vitest";
import {
  registerTerminal,
  terminalRegistrySnapshot,
  unregisterTerminal,
} from "./terminalRegistry";

function fakeTerminal(length: number) {
  return { buffer: { active: { length } } } as never;
}

describe("terminalRegistry", () => {
  it("tracks registration and cleanup", () => {
    registerTerminal("test-terminal", fakeTerminal(12));
    expect(terminalRegistrySnapshot()).toEqual({ count: 1, totalLines: 12 });

    unregisterTerminal("test-terminal");
    expect(terminalRegistrySnapshot()).toEqual({ count: 0, totalLines: 0 });
  });
});
