import { afterEach, beforeEach, describe, expect, it } from "vitest";

// Importing debug.ts installs the Date.prototype.toLocaleString patch as a
// module-load side effect (matches how main.tsx pulls it in for real).
import "./debug";

const NativeDateTimeFormat = Intl.DateTimeFormat;

describe("Date.prototype.toLocaleString caching patch", () => {
  let constructions = 0;
  const patchActive = () =>
    typeof (Date.prototype.toLocaleString as unknown as Record<string, unknown>)
      .__resetCacheForTests === "function";

  beforeEach(() => {
    constructions = 0;
    class CountingDateTimeFormat extends NativeDateTimeFormat {
      constructor(locales?: string | string[], options?: Intl.DateTimeFormatOptions) {
        super(locales, options);
        constructions++;
      }
    }
    // @ts-expect-error -- test-only stand-in for the global constructor
    Intl.DateTimeFormat = CountingDateTimeFormat;
    const reset = (Date.prototype.toLocaleString as unknown as Record<string, unknown>)
      .__resetCacheForTests as (() => void) | undefined;
    reset?.();
  });

  afterEach(() => {
    Intl.DateTimeFormat = NativeDateTimeFormat;
    const reset = (Date.prototype.toLocaleString as unknown as Record<string, unknown>)
      .__resetCacheForTests as (() => void) | undefined;
    reset?.();
  });

  it("reuses one Intl.DateTimeFormat for repeated calls with identical options", () => {
    const d = new Date("2026-08-28T10:00:00Z");
    const opts: Intl.DateTimeFormatOptions = { year: "numeric", month: "2-digit", day: "2-digit" };

    const first = d.toLocaleString("en-US", opts);
    const second = d.toLocaleString("en-US", opts);

    expect(second).toBe(first);
    expect(constructions).toBe(1);
  });

  it("reuses cache for semantically identical options with different key order (canonical key)", () => {
    const d = new Date("2026-08-28T10:00:00Z");
    d.toLocaleString("en-US", { year: "numeric", month: "2-digit" });
    d.toLocaleString("en-US", { month: "2-digit", year: "numeric" });
    expect(constructions).toBe(1);
  });

  it("builds a new formatter when options actually differ", () => {
    const d = new Date("2026-08-28T10:00:00Z");
    d.toLocaleString("en-US", { year: "numeric" });
    d.toLocaleString("en-US", { month: "long" });

    expect(constructions).toBe(2);
  });

  it("leaves bare toLocaleString() (no date/time fields) on the native path", () => {
    const d = new Date("2026-08-28T10:00:00Z");

    d.toLocaleString();
    d.toLocaleString("en-US", { hour12: false });

    expect(constructions).toBe(0);
  });

  it("produces the same output as the native formatter for a cached call", () => {
    const d = new Date("2026-08-28T10:00:00Z");
    const opts: Intl.DateTimeFormatOptions = { year: "numeric", month: "2-digit", day: "2-digit" };

    const patched = d.toLocaleString("en-US", opts);
    const native = new Intl.DateTimeFormat("en-US", opts).format(d);

    expect(patched).toBe(native);
  });

  it("returns 'Invalid Date' for invalid dates on the cached path (and does not cache)", () => {
    const bad = new Date("invalid");
    const opts: Intl.DateTimeFormatOptions = { year: "numeric" };
    const result = bad.toLocaleString("en-US", opts);
    expect(result).toBe("Invalid Date");
    expect(constructions).toBe(0);
    expect(new Date("invalid").toLocaleString("en-US", opts)).toBe("Invalid Date");
    expect(constructions).toBe(0);
  });

  it("throws TypeError for non-Date receivers on the cached path", () => {
    expect(() =>
      (Date.prototype.toLocaleString as unknown as (...args: unknown[]) => unknown).call(
        {},
        "en-US",
        { year: "numeric" },
      ),
    ).toThrow(TypeError);
    expect(() =>
      (Date.prototype.toLocaleString as unknown as (...args: unknown[]) => unknown).call(
        "not a date",
        "en-US",
        { year: "numeric" },
      ),
    ).toThrow(TypeError);
  });

  it("separates cache entries by locale", () => {
    const d = new Date("2026-08-28T10:00:00Z");
    const opts: Intl.DateTimeFormatOptions = { year: "numeric" };
    d.toLocaleString("en-US", opts);
    d.toLocaleString("fr-FR", opts);
    expect(constructions).toBe(2);
    d.toLocaleString("en-US", opts);
    expect(constructions).toBe(2);
  });

  it("evicts oldest entry when cache exceeds DTF_CACHE_MAX (FIFO)", () => {
    if (!patchActive()) return;
    const d = new Date("2026-08-28T10:00:00Z");
    const baseOpts: Intl.DateTimeFormatOptions = { year: "numeric", month: "numeric" };
    d.toLocaleString("en-US", baseOpts);
    expect(constructions).toBe(1);
    // Fill to 50 distinct cache keys by varying a synthetic options property.
    // Intl ignores unknown keys, but canonicalOptionsKey includes them, so each
    // entry is distinct and valid without needing invalid locale tags.
    for (let i = 1; i < 50; i++) {
      const opts = { ...baseOpts, __seq: i } as unknown as Intl.DateTimeFormatOptions;
      d.toLocaleString("en-US", opts);
    }
    expect(constructions).toBe(50);
    d.toLocaleString("en-US", {
      ...baseOpts,
      __seq: 999,
    } as unknown as Intl.DateTimeFormatOptions);
    expect(constructions).toBe(51);
    // First entry was the oldest, now evicted — must reconstruct.
    d.toLocaleString("en-US", baseOpts);
    expect(constructions).toBe(52);
  });
});
