import { describe, expect, it } from "vitest";
import { nextSpeechMode } from "./speechUtils";

describe("SpeechToolbar mode cycling", () => {
  it("cycles from manual to at-bottom", () => {
    expect(nextSpeechMode("manual")).toBe("at-bottom");
  });

  it("cycles from at-bottom to manual", () => {
    expect(nextSpeechMode("at-bottom")).toBe("manual");
  });

  it("normalizes unknown/legacy modes to at-bottom (never auto)", () => {
    expect(nextSpeechMode("auto")).toBe("at-bottom");
    expect(nextSpeechMode("")).toBe("at-bottom");
    expect(nextSpeechMode(undefined as never)).toBe("at-bottom");
  });
});
