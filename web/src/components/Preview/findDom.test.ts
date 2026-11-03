import { beforeEach, describe, expect, it } from "vitest";
import {
  buildTextIndex,
  escapeRegExp,
  findMatches,
  previewWantsFindShortcut,
  rangeForMatch,
} from "./findDom";

function html(markup: string): HTMLDivElement {
  const root = document.createElement("div");
  root.innerHTML = markup;
  document.body.appendChild(root);
  return root;
}

beforeEach(() => {
  document.body.innerHTML = "";
});

describe("buildTextIndex", () => {
  it("joins inline nodes without inserting a space", () => {
    const root = html("<p><strong>Al</strong>pha <em>beta</em></p>");
    const index = buildTextIndex(root);
    expect(index.text).toContain("Alpha beta");
    expect(findMatches(index, "Alpha beta", { caseSensitive: false, wholeWord: false })).toHaveLength(1);
  });

  it("separates blocks so a query cannot match across a paragraph boundary", () => {
    const root = html("<p>end</p><p>start</p>");
    const index = buildTextIndex(root);
    expect(findMatches(index, "endstart", { caseSensitive: false, wholeWord: false })).toHaveLength(0);
  });

  it("ignores script and style content", () => {
    const root = html("<style>.x{color:red}</style><script>var needle = 1;</script><p>body</p>");
    const index = buildTextIndex(root);
    expect(index.text).not.toContain("needle");
    expect(findMatches(index, "needle", { caseSensitive: false, wholeWord: false })).toHaveLength(0);
  });
});

describe("findMatches", () => {
  const index = () => buildTextIndex(html("<p>Alpha beta gamma betamax.</p><p>Alpha BETA delta.</p>"));

  it("is case-insensitive by default", () => {
    expect(findMatches(index(), "beta", { caseSensitive: false, wholeWord: false })).toHaveLength(3);
  });

  it("honours match case", () => {
    expect(findMatches(index(), "beta", { caseSensitive: true, wholeWord: false })).toHaveLength(2);
  });

  it("honours whole word", () => {
    expect(findMatches(index(), "beta", { caseSensitive: true, wholeWord: true })).toHaveLength(1);
  });

  it("returns no matches for an empty query", () => {
    expect(findMatches(index(), "", { caseSensitive: false, wholeWord: false })).toHaveLength(0);
  });

  it("finds repeated matches without stalling", () => {
    expect(findMatches(index(), "a", { caseSensitive: false, wholeWord: false }).length).toBeGreaterThan(3);
  });
});

describe("rangeForMatch", () => {
  it("maps offsets back onto the live text nodes", () => {
    const root = html("<p><strong>Al</strong>pha beta</p>");
    const index = buildTextIndex(root);
    const [match] = findMatches(index, "Alpha", { caseSensitive: false, wholeWord: false });
    const range = rangeForMatch(index, match);
    expect(range).not.toBeNull();
    expect(range?.toString()).toBe("Alpha");
  });

  it("clamps a match whose start sits on a block separator", () => {
    const root = html("<p>one</p><p>two</p>");
    const index = buildTextIndex(root);
    // "one\ntwo" — a range that starts at the newline must resolve to "two".
    const range = rangeForMatch(index, { start: index.text.indexOf("two") - 1, end: index.text.indexOf("two") + 3 });
    expect(range?.toString()).toContain("two");
  });
});

describe("escapeRegExp", () => {
  it("escapes regex metacharacters so a literal search works", () => {
    expect(escapeRegExp("$5.00 (net)")).toBe("\\$5\\.00 \\(net\\)");
  });

  it("treats the query literally when searching", () => {
    const root = html("<p>price is $5.00 (net)</p>");
    const index = buildTextIndex(root);
    expect(findMatches(index, "$5.00 (net)", { caseSensitive: false, wholeWord: false })).toHaveLength(1);
  });
});

describe("previewWantsFindShortcut", () => {
  it("is false without a container", () => {
    expect(previewWantsFindShortcut(null)).toBe(false);
  });

  it("claims the shortcut when focus is inside the preview", () => {
    const original = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetParent");
    Object.defineProperty(HTMLElement.prototype, "offsetParent", { configurable: true, get: () => document.body });
    const root = html('<div tabindex="0">text</div>');
    (root.firstElementChild as HTMLElement).focus();
    expect(previewWantsFindShortcut(root)).toBe(true);
    if (original) Object.defineProperty(HTMLElement.prototype, "offsetParent", original);
    else delete (HTMLElement.prototype as unknown as Record<string, unknown>).offsetParent;
  });

  it("does not claim the shortcut while focus is elsewhere", () => {
    const original = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetParent");
    Object.defineProperty(HTMLElement.prototype, "offsetParent", { configurable: true, get: () => document.body });
    const root = html("<p>text</p>");
    (document.body as HTMLElement).focus?.();
    expect(previewWantsFindShortcut(root)).toBe(false);
    if (original) Object.defineProperty(HTMLElement.prototype, "offsetParent", original);
    else delete (HTMLElement.prototype as unknown as Record<string, unknown>).offsetParent;
  });
});
