import { describe, expect, it } from "vitest";
import { MAX_TITLE_CHARS, truncateTitle } from "./title";

describe("truncateTitle", () => {
  it("passes a short title through, collapsing newlines", () => {
    expect(truncateTitle("hello\nworld")).toBe("hello world");
    expect(truncateTitle("  spaced  ")).toBe("spaced");
  });

  it("truncates with an ellipsis at the cap", () => {
    const got = truncateTitle("x".repeat(500), 80);
    expect(got.length).toBe(80);
    expect(got.endsWith("...")).toBe(true);
    expect(got.startsWith("x".repeat(77))).toBe(true);
  });

  it("is rune-safe for multibyte titles", () => {
    const got = truncateTitle("日".repeat(200), 10);
    expect(Array.from(got)).toHaveLength(10);
    expect(got).not.toContain("\uFFFD");
  });

  it("returns empty for a non-positive cap", () => {
    expect(truncateTitle("abc", 0)).toBe("");
  });

  // Regression: a session title can be mirrored from a multi-megabyte first
  // user message. `Array.from(s)` on the whole string allocates one array slot
  // per character (~100ms+ for 10M chars) and was the client-side jank; work
  // must stay bounded to a prefix. 50ms is a generous ceiling — the bounded
  // implementation is well under 5ms.
  it("bounds a multi-megabyte title without expanding it", () => {
    const huge = "y".repeat(10_000_000);
    const t0 = performance.now();
    const got = truncateTitle(huge);
    const elapsed = performance.now() - t0;
    expect(Array.from(got).length).toBeLessThanOrEqual(MAX_TITLE_CHARS);
    expect(got.endsWith("...")).toBe(true);
    expect(elapsed).toBeLessThan(50);
  });
});
