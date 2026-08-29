import { describe, it, expect, beforeEach, vi } from "vitest";
import { loadSoundSettings, saveSoundSettings } from "./terminalAlertSound";

beforeEach(() => {
  window.localStorage.clear();
  vi.restoreAllMocks();
});

describe("terminalAlertSound", () => {
  it("defaults to enabled with no custom sound", () => {
    const s = loadSoundSettings();
    expect(s.enabled).toBe(true);
    expect(s.dataUrl).toBeNull();
    expect(s.fileName).toBeNull();
  });

  it("persists enabled toggle and custom sound round-trip", () => {
    saveSoundSettings({ enabled: false, dataUrl: "data:audio/wav;base64,AAA", fileName: "ding.wav" });
    const loaded = loadSoundSettings();
    expect(loaded.enabled).toBe(false);
    expect(loaded.dataUrl).toBe("data:audio/wav;base64,AAA");
    expect(loaded.fileName).toBe("ding.wav");
  });

  it("clearing custom sound returns to default beep", () => {
    saveSoundSettings({ enabled: true, dataUrl: "data:audio/wav;base64,AAA", fileName: "ding.wav" });
    saveSoundSettings({ enabled: true, dataUrl: null, fileName: null });
    const s = loadSoundSettings();
    expect(s.dataUrl).toBeNull();
    expect(s.fileName).toBeNull();
  });

  it("tolerates corrupt storage and returns defaults", () => {
    window.localStorage.setItem("ocode.terminal.sound", "not-json");
    const s = loadSoundSettings();
    expect(s.enabled).toBe(true);
    expect(s.dataUrl).toBeNull();
  });
});
