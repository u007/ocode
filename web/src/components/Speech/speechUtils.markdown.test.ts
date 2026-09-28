import { describe, expect, it } from "vitest";
import { sanitizeSpeechText, stripMarkdown } from "./speechUtils";

describe("stripMarkdown", () => {
  it("removes bold markers", () => {
    expect(stripMarkdown("This is **bold** text")).toBe("This is bold text");
    expect(stripMarkdown("This is __bold__ text")).toBe("This is bold text");
  });

  it("removes italic markers", () => {
    expect(stripMarkdown("This is *italic* text")).toBe("This is italic text");
  });

  it("removes inline code backticks", () => {
    expect(stripMarkdown("Run `go build` now")).toBe("Run go build now");
  });

  it("removes fenced code block fences but keeps content", () => {
    expect(stripMarkdown("```go\nfmt.Println()\n```")).toBe("fmt.Println()\n");
    expect(stripMarkdown("```\nplain code\n```")).toBe("plain code\n");
  });

  it("removes heading hashes", () => {
    expect(stripMarkdown("# Title\n## Subtitle\nBody")).toBe("Title\nSubtitle\nBody");
  });

  it("removes blockquote markers", () => {
    expect(stripMarkdown("> quoted text")).toBe("quoted text");
  });

  it("removes list markers", () => {
    expect(stripMarkdown("- first\n- second\n+ third\n1. fourth")).toBe("first\nsecond\nthird\nfourth");
  });

  it("removes horizontal rules", () => {
    expect(stripMarkdown("before\n---\nafter")).toBe("before\n\nafter");
    expect(stripMarkdown("before\n***\nafter")).toBe("before\n\nafter");
  });

  it("removes strikethrough markers", () => {
    expect(stripMarkdown("This is ~~deleted~~ text")).toBe("This is deleted text");
  });

  it("keeps link text but drops the URL", () => {
    expect(stripMarkdown("Open [the page](https://example.com) now")).toBe("Open the page now");
  });

  it("keeps image alt text but drops the URL", () => {
    expect(stripMarkdown("![screenshot](https://img.com/x.png)")).toBe("screenshot");
  });

  it("removes HTML tags", () => {
    expect(stripMarkdown("Some <strong>bold</strong> text")).toBe("Some bold text");
  });

  it("keeps comparison operators and generics", () => {
    expect(stripMarkdown("a < b and c > d")).toBe("a < b and c > d");
    expect(stripMarkdown("Use Vec<T>, Map<A, B> or List<String>")).toBe("Use Vec<T>, Map<A, B> or List<String>");
  });

  it("leaves legitimate math with asterisks alone", () => {
    expect(stripMarkdown("5 * 3 = 15")).toBe("5 * 3 = 15");
  });

  it("leaves snake_case identifiers alone", () => {
    expect(stripMarkdown("Use snake_case_name here")).toBe("Use snake_case_name here");
  });

  it("handles nested bold inside italic", () => {
    expect(stripMarkdown("***bold italic***")).toBe("bold italic");
  });
});

describe("sanitizeSpeechText with markdown", () => {
  it("strips markdown and control characters", () => {
    expect(sanitizeSpeechText("# Title\n\nSome **bold** and `code`.")).toBe("Title\n\nSome bold and code.");
  });

  it("strips markdown from raw assistant content fallback", () => {
    // This is the exact scenario from ChatPanel.tsx:159-160 — the at-bottom
    // fallback uses raw assistant.content when the rendered DOM is unavailable.
    const raw = "## Summary\n\nThe **quick** brown fox *jumps* over [the dog](https://example.com).";
    expect(sanitizeSpeechText(raw)).toBe("Summary\n\nThe quick brown fox jumps over the dog.");
  });
});
