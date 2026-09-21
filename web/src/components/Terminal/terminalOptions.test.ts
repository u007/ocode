import { afterEach, describe, expect, it } from "vitest";
import { Terminal } from "@xterm/xterm";
import { SearchAddon } from "@xterm/addon-search";
import { buildTerminalOptions } from "./terminalOptions";

// Regression coverage for "terminal find never matches anything" — the find bar
// reported "No matches" for text that was plainly visible in the buffer.
//
// The component tests mock @xterm/addon-search, so they only verify the find
// bar's wiring, never that a real search succeeds. SearchAddon creates its
// match/decorations via Terminal.registerDecoration(), which xterm gates behind
// `allowProposedApi`; when the terminal was constructed without it, findNext()
// threw internally, selected nothing and never fired onDidChangeResults — so the
// counter stayed at 0 ("No matches") forever. These tests use the REAL addon
// with the REAL production options (buildTerminalOptions).

const DECORATIONS = {
  matchBackground: "#facc15",
  matchBorder: "#facc15",
  matchOverviewRuler: "#facc15",
  activeMatchBackground: "#f97316",
  activeMatchBorder: "#f97316",
  activeMatchColorOverviewRuler: "#f97316",
} as const;

const PROD_OPTIONS = { scrollbackLines: 1000, fontFamily: "monospace", fontSize: 12 };

/**
 * xterm's CoreBrowserService needs window.matchMedia + devicePixelRatio to
 * `open()`, neither of which jsdom implements. Returns a restore function.
 */
function stubBrowserApis(): () => void {
  const originalMatchMedia = window.matchMedia;
  const originalDpr = Object.getOwnPropertyDescriptor(window, "devicePixelRatio");
  window.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  });
  Object.defineProperty(window, "devicePixelRatio", { value: 1, configurable: true });
  return () => {
    window.matchMedia = originalMatchMedia;
    if (originalDpr) Object.defineProperty(window, "devicePixelRatio", originalDpr);
  };
}

function writeAll(term: Terminal, text: string): Promise<void> {
  return new Promise((resolve) => {
    const listener = term.onWriteParsed(() => {
      listener.dispose();
      resolve();
    });
    term.write(text);
  });
}

let restoreBrowser: (() => void) | null = null;
afterEach(() => {
  restoreBrowser?.();
  restoreBrowser = null;
});

describe("buildTerminalOptions", () => {
  it("enables allowProposedApi so SearchAddon can create match decorations", () => {
    expect(buildTerminalOptions(PROD_OPTIONS).allowProposedApi).toBe(true);
  });

  it("carries the configured scrollback, font and scroll behaviour", () => {
    const opts = buildTerminalOptions({ ...PROD_OPTIONS, scrollbackLines: 42 });
    expect(opts.scrollback).toBe(42);
    expect(opts.fontFamily).toBe("monospace");
    expect(opts.fontSize).toBe(12);
    expect(opts.scrollSensitivity).toBe(3);
    expect(opts.fastScrollSensitivity).toBe(5);
    expect(opts.smoothScrollDuration).toBe(0);
  });

  it("constructs at the saved buffer geometry and falls back to defaults otherwise", () => {
    const withGeom = buildTerminalOptions({ ...PROD_OPTIONS, savedBuffer: { cols: 120, rows: 40 } });
    expect(withGeom.cols).toBe(120);
    expect(withGeom.rows).toBe(40);
    const legacy = buildTerminalOptions({ ...PROD_OPTIONS, savedBuffer: { cols: 0, rows: 0 } });
    expect(legacy.cols).toBeUndefined();
    expect(legacy.rows).toBeUndefined();
  });

  it("findNext locates visible buffer text and fires result events", async () => {
    restoreBrowser = stubBrowserApis();
    const term = new Terminal(buildTerminalOptions(PROD_OPTIONS));
    const el = document.createElement("div");
    document.body.appendChild(el);
    const search = new SearchAddon();
    const events: Array<{ resultIndex: number; resultCount: number }> = [];
    try {
      term.open(el);
      term.loadAddon(search);
      search.onDidChangeResults((e) => events.push(e));

      await writeAll(term, '#NOVITA_API_KEY="sk_test"\r\nsecond line\r\n');

      // Case-insensitive by default, so the lowercase query must find NOVITA.
      // Before the fix this threw ("You must set the allowProposedApi option to
      // true to use proposed API"), returned null and emitted no results.
      expect(search.findNext("novita", { decorations: DECORATIONS })).toBe(true);
      expect(events[events.length - 1]).toEqual({ resultIndex: 0, resultCount: 1 });
    } finally {
      term.dispose();
      el.remove();
    }
  });

  it("navigates next/previous across every match and reports the total", async () => {
    // The find bar's counter ("2/3") and its next/prev buttons are driven
    // entirely by onDidChangeResults; decorations are what make the matches
    // visible in the buffer. Both come from the same allowProposedApi-gated
    // registerDecoration() call, so pin the full navigation contract here.
    restoreBrowser = stubBrowserApis();
    const term = new Terminal(buildTerminalOptions(PROD_OPTIONS));
    const el = document.createElement("div");
    document.body.appendChild(el);
    const search = new SearchAddon();
    const events: Array<{ resultIndex: number; resultCount: number }> = [];
    const last = () => events[events.length - 1];
    try {
      term.open(el);
      term.loadAddon(search);
      search.onDidChangeResults((e) => events.push(e));

      await writeAll(
        term,
        '#NOVITA_API_KEY="a"\r\nNOVITA_ENDPOINT=https://x\r\necho NOVITA done\r\n',
      );

      const opts = { decorations: DECORATIONS };
      // First search highlights all three occurrences and activates the first.
      expect(search.findNext("novita", opts)).toBe(true);
      expect(last()).toEqual({ resultIndex: 0, resultCount: 3 });
      // Next advances 0 -> 1 -> 2, then wraps back around to 0.
      search.findNext("novita", opts);
      expect(last()).toEqual({ resultIndex: 1, resultCount: 3 });
      search.findNext("novita", opts);
      expect(last()).toEqual({ resultIndex: 2, resultCount: 3 });
      search.findNext("novita", opts);
      expect(last()).toEqual({ resultIndex: 0, resultCount: 3 });
      // Previous goes back from the first match to the last (wrap-around).
      search.findPrevious("novita", opts);
      expect(last()).toEqual({ resultIndex: 2, resultCount: 3 });
      // The active match is really selected, so the viewport scrolls to it.
      expect(term.getSelection()).toBe("NOVITA");
    } finally {
      term.dispose();
      el.remove();
    }
  });
});
