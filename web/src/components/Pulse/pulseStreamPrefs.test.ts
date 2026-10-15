import { beforeEach, describe, expect, it } from "vitest";
import {
  getStreamPrefs,
  pruneStreamOverrides,
  resetStreamPrefsForTests,
  setAllLiveStreamsTall,
  setStreamTall,
} from "./pulseStreamPrefs";

const KEY = "pulse.card.streamTall";

describe("pulseStreamPrefs", () => {
  beforeEach(() => {
    window.localStorage.clear();
    resetStreamPrefsForTests();
  });

  it("defaults to the short stream when nothing is persisted", () => {
    expect(getStreamPrefs().tall).toBe(false);
  });

  it("writes each bulk choice through to localStorage", () => {
    setAllLiveStreamsTall(true);
    expect(window.localStorage.getItem(KEY)).toBe("1");

    setAllLiveStreamsTall(false);
    expect(window.localStorage.getItem(KEY)).toBe("0");
  });

  it("restores the persisted choice on a fresh load", () => {
    setAllLiveStreamsTall(true);

    // A reload drops the in-memory store and re-reads localStorage.
    resetStreamPrefsForTests();

    expect(getStreamPrefs().tall).toBe(true);
  });

  it("keeps a manual toggle per session, in memory only", () => {
    setStreamTall("ses_a", true);

    expect(getStreamPrefs().overrides).toEqual({ ses_a: true });
    expect(window.localStorage.getItem(KEY)).toBeNull();
  });

  it("a bulk action drops every manual toggle, even one that leaves tall unchanged", () => {
    setStreamTall("ses_a", true);

    setAllLiveStreamsTall(false);

    expect(getStreamPrefs()).toEqual({ tall: false, overrides: {} });
  });

  it("prunes manual toggles for sessions no longer listed, and leaves the state untouched otherwise", () => {
    setStreamTall("ses_a", true);
    setStreamTall("ses_b", false);
    const before = getStreamPrefs();

    pruneStreamOverrides(new Set(["ses_a", "ses_b"]));
    expect(getStreamPrefs()).toBe(before);

    pruneStreamOverrides(new Set(["ses_b"]));
    expect(getStreamPrefs().overrides).toEqual({ ses_b: false });
  });

  it("follows another tab's bulk action through the storage event, keeping its own manual toggles", () => {
    setStreamTall("ses_a", false);

    window.dispatchEvent(new StorageEvent("storage", { key: KEY, newValue: "1" }));
    expect(getStreamPrefs()).toEqual({ tall: true, overrides: { ses_a: false } });

    window.dispatchEvent(new StorageEvent("storage", { key: "pulse.assistant.open", newValue: "1" }));
    expect(getStreamPrefs().tall).toBe(true);

    window.dispatchEvent(new StorageEvent("storage", { key: KEY, newValue: "0" }));
    expect(getStreamPrefs().tall).toBe(false);
  });
});
