import { useSyncExternalStore } from "react";

/**
 * Per-viewer layout of the Pulse assistant window: open or closed, its mode and
 * its floating rectangle. The window lives at app level, so this store is read by
 * the toolbar toggle in PulseView and by the window itself. One module-level store
 * keeps them in step; two hook instances each reading localStorage once would drift.
 *
 * localStorage on purpose, not the pulse store: each window or device wants its
 * own layout, and nothing else reads these keys. Storage that throws or is blocked
 * only costs the remembered layout, so every read and write is guarded and logged.
 */

export type AssistantMode = "normal" | "minimized" | "maximized";

export interface AssistantRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface Viewport {
  width: number;
  height: number;
}

export const ASSISTANT_MIN_WIDTH = 320;
export const ASSISTANT_MIN_HEIGHT = 240;
export const ASSISTANT_DEFAULT_WIDTH = 420;
export const ASSISTANT_DEFAULT_HEIGHT = 560;
/** Below this viewport width the window is an inset sheet and cannot be dragged or resized. */
export const ASSISTANT_NARROW_BREAKPOINT = 640;
/** Height of the minimised bar, which is the header alone. */
export const ASSISTANT_BAR_HEIGHT = 40;
/** Gap kept between the window and the viewport edges. */
export const ASSISTANT_EDGE = 8;

const KEY_OPEN = "pulse.assistant.open";
const KEY_MODE = "pulse.assistant.mode";
const KEY_RECT = "pulse.assistant.rect";

export interface AssistantPrefs {
  open: boolean;
  mode: AssistantMode;
  /** null until the user moves or resizes the window; the default is computed per viewport. */
  rect: AssistantRect | null;
}

function readPref(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch (err) {
    console.warn(`PulseAssistant: reading localStorage key ${key} failed:`, err);
    return null;
  }
}

function writePref(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch (err) {
    console.warn(`PulseAssistant: writing localStorage key ${key} failed:`, err);
  }
}

function isFiniteNumber(v: unknown): v is number {
  return typeof v === "number" && Number.isFinite(v);
}

/** Parses a stored rect. Anything malformed is dropped, never repaired. */
export function parseRect(raw: string | null): AssistantRect | null {
  if (raw === null || raw === "") return null;
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch (err) {
    console.warn(`PulseAssistant: stored ${KEY_RECT} is not JSON, ignoring it:`, err);
    return null;
  }
  if (typeof value !== "object" || value === null) return null;
  const r = value as Record<string, unknown>;
  if (!isFiniteNumber(r.x) || !isFiniteNumber(r.y) || !isFiniteNumber(r.width) || !isFiniteNumber(r.height)) {
    return null;
  }
  return { x: r.x, y: r.y, width: r.width, height: r.height };
}

function parseMode(raw: string | null): AssistantMode {
  return raw === "minimized" || raw === "maximized" ? raw : "normal";
}

function loadPrefs(): AssistantPrefs {
  return {
    open: readPref(KEY_OPEN) === "1",
    mode: parseMode(readPref(KEY_MODE)),
    rect: parseRect(readPref(KEY_RECT)),
  };
}

let current: AssistantPrefs | null = null;
const listeners = new Set<() => void>();

function snapshot(): AssistantPrefs {
  if (current === null) current = loadPrefs();
  return current;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function update(patch: Partial<AssistantPrefs>): void {
  const next = { ...snapshot(), ...patch };
  current = next;
  if (patch.open !== undefined) writePref(KEY_OPEN, next.open ? "1" : "0");
  if (patch.mode !== undefined) writePref(KEY_MODE, next.mode);
  if (patch.rect !== undefined) writePref(KEY_RECT, next.rect === null ? "" : JSON.stringify(next.rect));
  for (const listener of listeners) listener();
}

export function setAssistantOpen(open: boolean): void {
  update({ open });
}

export function setAssistantMode(mode: AssistantMode): void {
  update({ mode });
}

export function setAssistantRect(rect: AssistantRect): void {
  update({ rect });
}

/**
 * The app-level toggle, used by the sidebar button and Cmd/Ctrl+Shift+A. Closed
 * opens. A minimised window restores (pressing the key on the bar must not make
 * it disappear). Anything else closes.
 */
export function toggleAssistantWindow(): void {
  const s = snapshot();
  if (!s.open) update({ open: true });
  else if (s.mode === "minimized") update({ mode: "normal" });
  else update({ open: false });
}

/** The store, read by React. The snapshot object changes only when a value does. */
export function usePulseAssistantPrefs() {
  const prefs = useSyncExternalStore(subscribe, snapshot, snapshot);
  return {
    open: prefs.open,
    mode: prefs.mode,
    rect: prefs.rect,
    setOpen: setAssistantOpen,
    setMode: setAssistantMode,
    setRect: setAssistantRect,
  };
}

/** Test-only: drop the in-memory copy so the next read comes from localStorage. */
export function resetAssistantPrefsForTests(): void {
  current = null;
  for (const listener of listeners) listener();
}

/** Keeps a rectangle inside the viewport, with its header always on screen. */
export function clampRect(r: AssistantRect, vp: Viewport): AssistantRect {
  const maxWidth = Math.max(ASSISTANT_MIN_WIDTH, vp.width - 2 * ASSISTANT_EDGE);
  const maxHeight = Math.max(ASSISTANT_MIN_HEIGHT, vp.height - 2 * ASSISTANT_EDGE);
  const width = Math.min(Math.max(r.width, ASSISTANT_MIN_WIDTH), maxWidth);
  const height = Math.min(Math.max(r.height, ASSISTANT_MIN_HEIGHT), maxHeight);
  const maxX = Math.max(ASSISTANT_EDGE, vp.width - width - ASSISTANT_EDGE);
  const maxY = Math.max(ASSISTANT_EDGE, vp.height - ASSISTANT_BAR_HEIGHT - ASSISTANT_EDGE);
  return {
    x: Math.min(Math.max(r.x, ASSISTANT_EDGE), maxX),
    y: Math.min(Math.max(r.y, ASSISTANT_EDGE), maxY),
    width,
    height,
  };
}

/** The bottom-right rectangle a first-time user sees. */
export function defaultRect(vp: Viewport): AssistantRect {
  const width = ASSISTANT_DEFAULT_WIDTH;
  const height = ASSISTANT_DEFAULT_HEIGHT;
  return clampRect(
    { x: vp.width - width - 2 * ASSISTANT_EDGE, y: vp.height - height - 2 * ASSISTANT_EDGE, width, height },
    vp,
  );
}

export function isNarrowViewport(vp: Viewport): boolean {
  return vp.width < ASSISTANT_NARROW_BREAKPOINT;
}
