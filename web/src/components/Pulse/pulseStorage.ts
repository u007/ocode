/**
 * Guarded localStorage access shared by the Pulse per-viewer pref stores
 * (pulseAssistantPrefs, pulseStreamPrefs). Storage that throws or is blocked
 * only costs the remembered value, so every read and write is logged, never
 * thrown.
 */

export function readPref(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch (err) {
    console.warn(`Pulse: reading localStorage key ${key} failed:`, err);
    return null;
  }
}

export function writePref(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch (err) {
    console.warn(`Pulse: writing localStorage key ${key} failed:`, err);
  }
}
