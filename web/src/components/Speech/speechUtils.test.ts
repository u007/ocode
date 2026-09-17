import { describe, expect, it } from "vitest";
import {
  chunkSpeechText,
  lastRenderedSpeechText,
  renderedSpeechText,
  renderedSpeechTexts,
  sanitizeSpeechText,
} from "./speechUtils";

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

describe("renderedSpeechText", () => {
  // Mirrors the DOM ReactMarkdown paints in AssistantText. The point of these
  // tests: speech reads the RENDERED tree, so markdown syntax (which no longer
  // exists in the DOM) can never be read aloud.
  function renderHTML(html: string) {
    const root = document.createElement("div");
    root.innerHTML = html;
    return root;
  }

  it("reads rendered prose without markdown syntax", () => {
    expect(
      renderedSpeechText(
        renderHTML("<h1>Title</h1><p>Some <strong>bold</strong> text with <code>code</code>.</p>"),
      ),
    ).toBe("Title\nSome bold text with code.");
  });

  it("separates block elements so blocks do not run together", () => {
    // `textContent` alone would yield "onetwo" — the historical bug.
    expect(renderedSpeechText(renderHTML("<p>one</p><p>two</p>"))).toBe("one\ntwo");
  });

  it("keeps link text but drops the link target", () => {
    expect(renderedSpeechText(renderHTML('<p>Open <a href="https://x.dev/a">the page</a>.</p>'))).toBe(
      "Open the page.",
    );
  });

  it("keeps table cell text, one cell per line", () => {
    expect(
      renderedSpeechText(
        renderHTML(
          "<table><tbody><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></tbody></table>",
        ),
      ),
    ).toBe("A\nB\n1\n2");
  });

  it("keeps list item text", () => {
    expect(renderedSpeechText(renderHTML("<ul><li>first</li><li>second</li></ul>"))).toBe("first\nsecond");
  });

  it("skips speak controls and decorative icons", () => {
    expect(
      renderedSpeechText(
        renderHTML(
          '<p>hello</p><button data-speech-exclude>Speak</button><svg aria-hidden="true"><title>icon</title></svg>',
        ),
      ),
    ).toBe("hello");
  });

  it("collapses whitespace and blank lines", () => {
    expect(renderedSpeechText(renderHTML("<p>  spaced   out  </p><hr/><p>next</p>"))).toBe("spaced out\nnext");
  });

  it("returns an empty string when no root is given", () => {
    expect(renderedSpeechText(null)).toBe("");
    expect(renderedSpeechText(undefined)).toBe("");
  });
});

describe("renderedSpeechTexts / lastRenderedSpeechText", () => {
  function transcript() {
    const root = document.createElement("div");
    root.innerHTML =
      '<div data-speech-content=""><p>first message</p></div>' +
      '<div data-speech-content=""><p>second message</p></div>';
    return root;
  }

  it("joins every rendered block in DOM order (speak visible)", () => {
    expect(renderedSpeechTexts(transcript())).toBe("first message\nsecond message");
  });

  it("returns the last rendered block (at-bottom auto-speak)", () => {
    expect(lastRenderedSpeechText(transcript())).toBe("second message");
  });

  it("returns empty strings when nothing is rendered", () => {
    expect(renderedSpeechTexts(null)).toBe("");
    expect(lastRenderedSpeechText(document.createElement("div"))).toBe("");
  });
});

