import { describe, expect, it, beforeEach } from "vitest";
import {
  SPEECH_TOOLBAR_VISIBLE_STORAGE_KEY,
  loadSpeechToolbarVisible,
  saveSpeechToolbarVisible,
} from "./speechToolbarPersistence";

describe("speechToolbarPersistence", () => {
  beforeEach(() => {
    window.localStorage.removeItem(SPEECH_TOOLBAR_VISIBLE_STORAGE_KEY);
  });

  it("defaults to visible when nothing is stored", () => {
    expect(loadSpeechToolbarVisible()).toBe(true);
  });

  it("round-trips hidden state", () => {
    saveSpeechToolbarVisible(false);
    expect(window.localStorage.getItem(SPEECH_TOOLBAR_VISIBLE_STORAGE_KEY)).toBe("false");
    expect(loadSpeechToolbarVisible()).toBe(false);
  });

  it("round-trips visible state", () => {
    saveSpeechToolbarVisible(false);
    saveSpeechToolbarVisible(true);
    expect(loadSpeechToolbarVisible()).toBe(true);
  });

  it("treats corrupt values as visible", () => {
    window.localStorage.setItem(SPEECH_TOOLBAR_VISIBLE_STORAGE_KEY, "not-json{{{");
    expect(loadSpeechToolbarVisible()).toBe(true);
  });

  it("tolerates missing localStorage", () => {
    const orig = window.localStorage;
    delete (window as unknown as Record<string, unknown>).localStorage;
    try {
      expect(loadSpeechToolbarVisible()).toBe(true);
      expect(() => saveSpeechToolbarVisible(false)).not.toThrow();
    } finally {
      (window as unknown as { localStorage: Storage }).localStorage = orig;
    }
  });
});
