import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  getLogPrefs,
  setLogPrefs,
  resetLogPrefs,
  LOG_PREFS_CHANGE_EVENT,
  LOG_PREFS_MIN_ENTRIES,
  LOG_PREFS_MAX_ENTRIES,
  LOG_PREFS_DEFAULT_ENTRIES,
} from "./logViewPrefs";

const KEY = "ocode.ui.logs.v1";

beforeEach(() => {
  window.localStorage.removeItem(KEY);
});

afterEach(() => {
  window.localStorage.removeItem(KEY);
  vi.restoreAllMocks();
});

describe("logViewPrefs", () => {
  it("returns defaults when nothing is stored", () => {
    expect(getLogPrefs()).toEqual({
      backgroundBuffering: false,
      maxEntries: LOG_PREFS_DEFAULT_ENTRIES,
    });
  });

  it("clamps maxEntries into the allowed range", () => {
    setLogPrefs({ maxEntries: 5 });
    expect(getLogPrefs().maxEntries).toBe(LOG_PREFS_MIN_ENTRIES);

    setLogPrefs({ maxEntries: 999_999 });
    expect(getLogPrefs().maxEntries).toBe(LOG_PREFS_MAX_ENTRIES);

    setLogPrefs({ maxEntries: 250.7 });
    expect(getLogPrefs().maxEntries).toBe(251);

    setLogPrefs({ maxEntries: Number.NaN });
    expect(getLogPrefs().maxEntries).toBe(LOG_PREFS_DEFAULT_ENTRIES);
  });

  it("falls back to defaults on corrupt JSON instead of throwing", () => {
    window.localStorage.setItem(KEY, "{not json");
    expect(getLogPrefs()).toEqual({
      backgroundBuffering: false,
      maxEntries: LOG_PREFS_DEFAULT_ENTRIES,
    });
  });

  it("persists writes and fires the change event", () => {
    const spy = vi.fn();
    window.addEventListener(LOG_PREFS_CHANGE_EVENT, spy);
    try {
      setLogPrefs({ backgroundBuffering: true, maxEntries: 2000 });
      expect(spy).toHaveBeenCalledTimes(1);
      expect(window.localStorage.getItem(KEY)).toContain('"backgroundBuffering":true');
      expect(getLogPrefs()).toEqual({ backgroundBuffering: true, maxEntries: 2000 });
    } finally {
      window.removeEventListener(LOG_PREFS_CHANGE_EVENT, spy);
    }
  });

  it("resetLogPrefs restores defaults", () => {
    setLogPrefs({ backgroundBuffering: true, maxEntries: 5000 });
    const prefs = resetLogPrefs();
    expect(prefs).toEqual({
      backgroundBuffering: false,
      maxEntries: LOG_PREFS_DEFAULT_ENTRIES,
    });
    expect(getLogPrefs()).toEqual(prefs);
  });
});
