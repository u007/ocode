import { describe, expect, it } from "vitest";
import { chunkSpeechText, sanitizeSpeechText } from "./speechUtils";

describe("speech text helpers", () => {
  it("removes terminal control bytes and whitespace", () => {
    expect(sanitizeSpeechText("\u001b[31m hello\nworld \u0000")).toBe("hello\nworld");
  });

  it("chunks long text without dropping content", () => {
    const chunks = chunkSpeechText("one two three four five six", 10);
    expect(chunks.length).toBeGreaterThan(1);
    expect(chunks.join(" ")).toBe("one two three four five six");
  });
});
