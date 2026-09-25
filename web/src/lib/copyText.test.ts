import { describe, expect, it } from "vitest";
import { renderedCopyText } from "./copyText";

function el(html: string): HTMLElement {
  const host = document.createElement("div");
  host.innerHTML = html;
  return host;
}

describe("renderedCopyText", () => {
  it("extracts marked copyable regions and skips surrounding controls", () => {
    const root = el(`
      <button>Show output</button>
      <div data-copy-content><p>first line</p></div>
      <button>Hide output</button>
      <div data-copy-content><p>second line</p></div>
    `);
    expect(renderedCopyText(root)).toBe("first line\nsecond line");
  });

  it("falls back to the whole subtree when nothing is marked", () => {
    const root = el(`<p>only text</p>`);
    expect(renderedCopyText(root)).toBe("only text");
  });

  it("returns an empty string for a missing root", () => {
    expect(renderedCopyText(null)).toBe("");
    expect(renderedCopyText(undefined)).toBe("");
  });
});
