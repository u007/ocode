// Client-side settings + playback for terminal bell/notification alerts.
//
// Sound settings are intentionally stored in the browser/webview (not the
// server-side ocodeconfig) because they are device-specific and selecting a
// custom sound means choosing a file from the local machine. Persisting a data
// URL through the shared server config would also bloat every config response.
//
// Two playback paths:
//   - a custom sound file the user picked (stored as a data URL), or
//   - a synthesized Web Audio "beep" that approximates the OS notification
//     sound without needing the Notification permission or popping a visible
//     OS toast.

export interface TerminalSoundSettings {
  /** Master on/off for audible alerts. */
  enabled: boolean;
  /** Data URL of a user-selected sound file, or null to use the default beep. */
  dataUrl: string | null;
  /** Original file name, for display only. */
  fileName: string | null;
}

const STORAGE_KEY = "ocode.terminal.sound";

// Avoid an audible "machine-gun" of beeps when a command spams BEL.
const THROTTLE_MS = 400;

let lastPlay = 0;
let audioCtx: AudioContext | null = null;

export function loadSoundSettings(): TerminalSoundSettings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<TerminalSoundSettings>;
      return {
        enabled: parsed.enabled !== false,
        dataUrl: typeof parsed.dataUrl === "string" ? parsed.dataUrl : null,
        fileName: typeof parsed.fileName === "string" ? parsed.fileName : null,
      };
    }
  } catch {
    /* fall through to default */
  }
  return { enabled: true, dataUrl: null, fileName: null };
}

export function saveSoundSettings(s: TerminalSoundSettings): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(s));
  } catch (err) {
    console.error("terminal: failed to persist sound settings", err);
  }
}

function ensureAudioContext(): AudioContext | null {
  if (typeof window === "undefined") return null;
  const Ctor =
    window.AudioContext ||
    (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!Ctor) return null;
  if (!audioCtx) audioCtx = new Ctor();
  // Browsers start the context suspended until a user gesture; resuming here
  // (the page has already seen interaction by the time a terminal is running)
  // lets the beep play without a fresh click.
  if (audioCtx.state === "suspended") audioCtx.resume().catch(() => {});
  return audioCtx;
}

// A short two-tone blip used when no custom sound file is configured.
function playDefaultBeep(): void {
  const ctx = ensureAudioContext();
  if (!ctx) return;
  const now = ctx.currentTime;
  const osc = ctx.createOscillator();
  const gain = ctx.createGain();
  osc.type = "sine";
  osc.frequency.setValueAtTime(880, now);
  osc.frequency.setValueAtTime(660, now + 0.09);
  gain.gain.setValueAtTime(0.0001, now);
  gain.gain.exponentialRampToValueAtTime(0.18, now + 0.01);
  gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.2);
  osc.connect(gain).connect(ctx.destination);
  osc.start(now);
  osc.stop(now + 0.22);
}

/** Play the alert sound if enabled. Safe to call on every detected bell. */
export function playAlertSound(): void {
  const settings = loadSoundSettings();
  if (!settings.enabled) return;
  const now = Date.now();
  if (now - lastPlay < THROTTLE_MS) return;
  lastPlay = now;

  if (settings.dataUrl) {
    try {
      const audio = new Audio(settings.dataUrl);
      const p = audio.play() as unknown as Promise<void> | undefined;
      if (p && typeof p.catch === "function") p.catch(() => playDefaultBeep());
    } catch {
      playDefaultBeep();
    }
  } else {
    playDefaultBeep();
  }
}
