import { beforeEach, describe, expect, it } from "vitest";
import {
  SPEECH_SPEAK_MODE_STORAGE_KEY,
  loadSpeechSpeakMode,
  saveSpeechSpeakMode,
} from "./speechToolbarPersistence";

describe("loadSpeechSpeakMode", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("defaults to summarised, so reading aloud does not recite code", () => {
    // The feature's whole point: with nothing stored, a first-time user's
    // speak button must go through the summariser.
    expect(loadSpeechSpeakMode()).toBe("summarised");
  });

  it("round-trips the opt-out", () => {
    saveSpeechSpeakMode("full");
    expect(loadSpeechSpeakMode()).toBe("full");
    saveSpeechSpeakMode("summarised");
    expect(loadSpeechSpeakMode()).toBe("summarised");
  });

  it("falls back to summarised for a corrupted or unknown stored value", () => {
    // A bad key must not be able to break speaking entirely.
    window.localStorage.setItem(SPEECH_SPEAK_MODE_STORAGE_KEY, JSON.stringify("verbose"));
    expect(loadSpeechSpeakMode()).toBe("summarised");
    window.localStorage.setItem(SPEECH_SPEAK_MODE_STORAGE_KEY, "not json");
    expect(loadSpeechSpeakMode()).toBe("summarised");
  });
});
