import type { GhosttyKeybind } from "./terminalAppearance";

/**
 * Ghostty keybindings, mapped onto what a browser terminal can actually do.
 *
 * A Ghostty `keybind = chord=action[:arg]` line names an ACTION from Ghostty's
 * own action set, most of which describe GTK/macOS window management (splits,
 * tabs, quick terminal, fullscreen) that a browser tab cannot implement. This
 * module is the honest boundary: it parses the chord, maps the actions ocode can
 * perform, and marks the rest as unsupported so the Settings UI can list them
 * instead of silently dropping a user's configuration.
 */

/** A parsed Ghostty key chord. */
export interface ParsedChord {
  /** Normalised `KeyboardEvent.key` equivalent, lower-cased for single chars. */
  key: string;
  ctrl: boolean;
  alt: boolean;
  shift: boolean;
  /** Cmd on macOS / Super on Linux — Ghostty's `super:`/`cmd:`. */
  meta: boolean;
}

/** What a bound chord does in ocode's terminal. */
export type KeybindAction =
  | { kind: "copy" }
  | { kind: "paste" }
  | { kind: "font-size"; delta: number }
  | { kind: "font-size-reset" }
  | { kind: "scroll-pages"; pages: number }
  | { kind: "scroll-top" }
  | { kind: "scroll-bottom" }
  | { kind: "search" }
  /** Recognised but not performable here; `reason` is shown in Settings. */
  | { kind: "unsupported"; action: string; reason: string };

/** Ghostty key names → `KeyboardEvent.key`. */
const KEY_NAMES: Record<string, string> = {
  zero: "0",
  one: "1",
  two: "2",
  three: "3",
  four: "4",
  five: "5",
  six: "6",
  seven: "7",
  eight: "8",
  nine: "9",
  grave_accent: "`",
  minus: "-",
  equal: "=",
  plus: "+",
  comma: ",",
  period: ".",
  slash: "/",
  backslash: "\\",
  semicolon: ";",
  apostrophe: "'",
  left_bracket: "[",
  right_bracket: "]",
  space: " ",
  enter: "Enter",
  tab: "Tab",
  escape: "Escape",
  backspace: "Backspace",
  delete: "Delete",
  insert: "Insert",
  home: "Home",
  end: "End",
  page_up: "PageUp",
  page_down: "PageDown",
  insert_key: "Insert",
  arrow_up: "ArrowUp",
  arrow_down: "ArrowDown",
  arrow_left: "ArrowLeft",
  arrow_right: "ArrowRight",
  up: "ArrowUp",
  down: "ArrowDown",
  left: "ArrowLeft",
  right: "ArrowRight",
  f1: "F1",
  f2: "F2",
  f3: "F3",
  f4: "F4",
  f5: "F5",
  f6: "F6",
  f7: "F7",
  f8: "F8",
  f9: "F9",
  f10: "F10",
  f11: "F11",
  f12: "F12",
  f13: "F13",
  f14: "F14",
  f15: "F15",
  f16: "F16",
  f17: "F17",
  f18: "F18",
  f19: "F19",
  f20: "F20",
};

const MODIFIERS: Record<string, keyof Pick<ParsedChord, "ctrl" | "alt" | "shift" | "meta">> = {
  ctrl: "ctrl",
  control: "ctrl",
  alt: "alt",
  opt: "alt",
  option: "alt",
  shift: "shift",
  super: "meta",
  cmd: "meta",
  command: "meta",
  meta: "meta",
};

/**
 * Parses a Ghostty chord such as `cmd+shift+t`, `ctrl+plus`, `super+equal`.
 * Returns null for anything ocode cannot represent (unknown key name, a
 * physical-key prefix, a keyless chord).
 */
export function parseGhosttyChord(chord: string): ParsedChord | null {
  const raw = chord.trim();
  if (!raw) return null;
  const parts = raw.split("+");
  // "ctrl++" would be ctrl + the plus key; Ghostty writes that as `ctrl+plus`,
  // so an empty trailing token is a malformed chord, not a plus.
  if (parts.some((p) => p.trim() === "")) return null;

  const keyToken = parts.pop()!.trim().toLowerCase();
  const parsed: ParsedChord = { key: "", ctrl: false, alt: false, shift: false, meta: false };
  for (const part of parts) {
    const mod = MODIFIERS[part.trim().toLowerCase()];
    if (!mod) return null;
    parsed[mod] = true;
  }

  // `physical:key` means "this position on the keyboard", which a browser
  // KeyboardEvent cannot express portably.
  if (keyToken.startsWith("physical:")) return null;

  if (/^[a-z]$/.test(keyToken)) {
    parsed.key = keyToken;
    return parsed;
  }
  if (/^[0-9]$/.test(keyToken)) {
    parsed.key = keyToken;
    return parsed;
  }
  const mapped = KEY_NAMES[keyToken];
  if (!mapped) return null;
  parsed.key = mapped;
  return parsed;
}

/** A stable identity for a parsed chord (used as the binding-table key). */
export function chordKey(chord: ParsedChord): string {
  const mods = [chord.ctrl && "ctrl", chord.alt && "alt", chord.shift && "shift", chord.meta && "meta"]
    .filter(Boolean)
    .join("+");
  return mods ? `${mods}+${chord.key}` : chord.key;
}

const APP_OWNED_REASON =
  "tab and window actions are owned by ocode's own shortcuts (⌘T/⌘W/⌘⇧[/]), so this binding is not applied";

/** Maps a Ghostty action to something this terminal can do, or null when the
 *  binding itself is not usable (unbound / global / unparseable chord). */
export function resolveKeybind(binding: GhosttyKeybind): KeybindAction | null {
  if (binding.unbound) return null;
  if (binding.global) return null;
  const action = binding.action.trim();
  const arg = Number.parseInt(binding.arg ?? "", 10);
  const delta = Number.isFinite(arg) && arg > 0 ? arg : 1;
  switch (action) {
    case "copy_to_clipboard":
      return { kind: "copy" };
    case "paste_from_clipboard":
      return { kind: "paste" };
    case "increase_font_size":
      return { kind: "font-size", delta };
    case "decrease_font_size":
      return { kind: "font-size", delta: -delta };
    case "reset_font_size":
      return { kind: "font-size-reset" };
    case "scroll_page_up":
      return { kind: "scroll-pages", pages: -1 };
    case "scroll_page_down":
      return { kind: "scroll-pages", pages: 1 };
    case "scroll_to_top":
      return { kind: "scroll-top" };
    case "scroll_to_bottom":
      return { kind: "scroll-bottom" };
    case "search":
      return { kind: "search" };
    case "new_tab":
    case "close_surface":
    case "close_tab":
    case "next_tab":
    case "previous_tab":
    case "goto_tab":
    case "move_tab":
    case "open_config":
    case "new_window":
    case "close_window":
    case "new_split":
    case "toggle_split_zoom":
    case "equalize_splits":
    case "toggle_fullscreen":
    case "toggle_quick_terminal":
      return { kind: "unsupported", action, reason: APP_OWNED_REASON };
    default:
      return { kind: "unsupported", action, reason: "this action has no browser-terminal equivalent" };
  }
}

export interface KeybindTable {
  /** chordKey -> action */
  bindings: Map<string, KeybindAction>;
  /** Bindings that were read but cannot be applied, for the Settings UI. */
  unsupported: { chord: string; action: string; reason: string }[];
}

/**
 * Builds the dispatch table from the server's parsed keybinds.
 *
 * Later bindings for the same chord WIN as a whole (Ghostty's own rule): a later
 * unsupported action removes the earlier working one rather than leaving both
 * in the report, and `unbind` removes the chord so ocode's existing handling
 * applies again.
 */
export function buildKeybindTable(keybinds: GhosttyKeybind[] | undefined): KeybindTable {
  // Collapse to the binding in effect per chord first. Order is preserved so the
  // unsupported list reads the way the config does.
  const latest = new Map<string, { raw: string; binding: GhosttyKeybind; chord: ParsedChord | null }>();
  for (const binding of keybinds ?? []) {
    const chord = parseGhosttyChord(binding.chord);
    const id = chord ? chordKey(chord) : `raw:${binding.chord}`;
    latest.set(id, { raw: binding.chord, binding, chord });
  }

  const bindings = new Map<string, KeybindAction>();
  const unsupported: KeybindTable["unsupported"] = [];
  for (const [id, entry] of latest) {
    const { binding, chord, raw } = entry;
    if (binding.unbound) continue;
    if (!chord) {
      unsupported.push({
        chord: raw,
        action: binding.action,
        reason: "this key chord cannot be expressed in a browser",
      });
      continue;
    }
    if (binding.global) {
      unsupported.push({
        chord: raw,
        action: binding.action,
        reason: "system-wide (global:) shortcuts cannot be registered by a browser tab",
      });
      continue;
    }
    const action = resolveKeybind(binding);
    if (!action) continue;
    if (action.kind === "unsupported") {
      unsupported.push({ chord: raw, action: action.action, reason: action.reason });
      continue;
    }
    bindings.set(id, action);
  }
  return { bindings, unsupported };
}

/** Matches a keydown against the table. */
export function actionForEvent(table: KeybindTable, ev: KeyboardEvent): KeybindAction | undefined {
  if (table.bindings.size === 0) return undefined;
  const key = ev.key.length === 1 ? ev.key.toLowerCase() : ev.key;
  const id = chordKey({
    key,
    ctrl: ev.ctrlKey,
    alt: ev.altKey,
    shift: ev.shiftKey,
    meta: ev.metaKey,
  });
  return table.bindings.get(id);
}
