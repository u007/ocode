import { describe, expect, it } from "vitest";
import {
  actionForEvent,
  buildKeybindTable,
  chordKey,
  parseGhosttyChord,
  resolveKeybind,
} from "./ghosttyKeybinds";

const event = (partial: Partial<KeyboardEvent>): KeyboardEvent =>
  ({
    key: "",
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    metaKey: false,
    ...partial,
  }) as KeyboardEvent;

describe("parseGhosttyChord", () => {
  it("parses modifier chains with a letter key", () => {
    expect(parseGhosttyChord("cmd+shift+t")).toEqual({ key: "t", ctrl: false, alt: false, shift: true, meta: true });
  });

  it("accepts every modifier spelling Ghostty allows", () => {
    expect(parseGhosttyChord("control+option+super+a")).toEqual({
      key: "a",
      ctrl: true,
      alt: true,
      shift: false,
      meta: true,
    });
    expect(parseGhosttyChord("ctrl+opt+meta+a")).toMatchObject({ ctrl: true, alt: true, meta: true });
  });

  it("maps Ghostty key names onto KeyboardEvent.key", () => {
    expect(parseGhosttyChord("ctrl+plus")?.key).toBe("+");
    expect(parseGhosttyChord("ctrl+grave_accent")?.key).toBe("`");
    expect(parseGhosttyChord("cmd+one")?.key).toBe("1");
    expect(parseGhosttyChord("shift+arrow_up")?.key).toBe("ArrowUp");
    expect(parseGhosttyChord("ctrl+page_down")?.key).toBe("PageDown");
    expect(parseGhosttyChord("f5")?.key).toBe("F5");
    expect(parseGhosttyChord("ctrl+space")?.key).toBe(" ");
  });

  it("rejects chords a browser cannot express", () => {
    expect(parseGhosttyChord("")).toBeNull();
    expect(parseGhosttyChord("ctrl+")).toBeNull();
    expect(parseGhosttyChord("hyper+a")).toBeNull();
    expect(parseGhosttyChord("physical:a")).toBeNull();
    expect(parseGhosttyChord("ctrl+not_a_key")).toBeNull();
  });
});

describe("chordKey", () => {
  it("is order-independent and distinguishes modifiers", () => {
    const a = parseGhosttyChord("shift+cmd+t")!;
    const b = parseGhosttyChord("cmd+shift+t")!;
    expect(chordKey(a)).toBe(chordKey(b));
    expect(chordKey(a)).not.toBe(chordKey(parseGhosttyChord("cmd+t")!));
  });
});

describe("resolveKeybind", () => {
  it("maps the terminal-local actions", () => {
    expect(resolveKeybind({ chord: "cmd+c", action: "copy_to_clipboard" })).toEqual({ kind: "copy" });
    expect(resolveKeybind({ chord: "cmd+v", action: "paste_from_clipboard" })).toEqual({ kind: "paste" });
    expect(resolveKeybind({ chord: "cmd+plus", action: "increase_font_size", arg: "2" })).toEqual({
      kind: "font-size",
      delta: 2,
    });
    expect(resolveKeybind({ chord: "cmd+minus", action: "decrease_font_size" })).toEqual({
      kind: "font-size",
      delta: -1,
    });
    expect(resolveKeybind({ chord: "cmd+zero", action: "reset_font_size" })).toEqual({ kind: "font-size-reset" });
    expect(resolveKeybind({ chord: "ctrl+f", action: "search" })).toEqual({ kind: "search" });
    expect(resolveKeybind({ chord: "shift+page_up", action: "scroll_page_up" })).toEqual({
      kind: "scroll-pages",
      pages: -1,
    });
  });

  it("refuses global and unbound chords", () => {
    expect(resolveKeybind({ chord: "global:ctrl+grave_accent", action: "toggle_quick_terminal", global: true })).toBeNull();
    expect(resolveKeybind({ chord: "cmd+k", action: "unbind", unbound: true })).toBeNull();
  });

  it("reports tab/window actions as unsupported with a reason", () => {
    const action = resolveKeybind({ chord: "cmd+shift+t", action: "new_tab" });
    expect(action?.kind).toBe("unsupported");
    if (action?.kind === "unsupported") {
      expect(action.reason).toContain("ocode's own shortcuts");
    }
  });

  it("reports unknown actions as unsupported", () => {
    const action = resolveKeybind({ chord: "cmd+g", action: "toggle_background_opacity" });
    expect(action).toMatchObject({ kind: "unsupported", action: "toggle_background_opacity" });
  });
});

describe("buildKeybindTable", () => {
  it("later bindings replace earlier ones for the same chord", () => {
    const table = buildKeybindTable([
      { chord: "cmd+t", action: "new_tab" },
      { chord: "cmd+t", action: "copy_to_clipboard" },
    ]);
    expect(table.unsupported).toHaveLength(0);
    expect(table.bindings.get("meta+t")).toEqual({ kind: "copy" });
  });

  it("drops a chord unbound later and re-adds it when bound again", () => {
    const unbound = buildKeybindTable([
      { chord: "cmd+v", action: "paste_from_clipboard" },
      { chord: "cmd+v", action: "unbind", unbound: true },
    ]);
    expect(unbound.bindings.size).toBe(0);

    const rebound = buildKeybindTable([
      { chord: "cmd+v", action: "unbind", unbound: true },
      { chord: "cmd+v", action: "paste_from_clipboard" },
    ]);
    expect(rebound.bindings.get("meta+v")).toEqual({ kind: "paste" });
  });

  it("collects every unapplicable binding for the Settings UI", () => {
    const table = buildKeybindTable([
      { chord: "cmd+shift+t", action: "new_tab" },
      { chord: "ctrl+not_a_key", action: "copy_to_clipboard" },
      { chord: "ctrl+grave_accent", action: "toggle_quick_terminal", global: true },
      { chord: "cmd+c", action: "copy_to_clipboard" },
    ]);
    expect(table.bindings.size).toBe(1);
    expect(table.unsupported.map((u) => u.action).sort()).toEqual(
      ["copy_to_clipboard", "new_tab", "toggle_quick_terminal"].sort(),
    );
  });

  it("tolerates a missing keybind list", () => {
    expect(buildKeybindTable(undefined).bindings.size).toBe(0);
  });
});

describe("actionForEvent", () => {
  const table = buildKeybindTable([
    { chord: "cmd+shift+t", action: "copy_to_clipboard" },
    { chord: "ctrl+plus", action: "increase_font_size" },
    { chord: "shift+page_up", action: "scroll_page_up" },
  ]);

  it("matches modifiers and lower-cases character keys", () => {
    expect(actionForEvent(table, event({ key: "T", metaKey: true, shiftKey: true }))).toEqual({ kind: "copy" });
    expect(actionForEvent(table, event({ key: "+", ctrlKey: true }))).toEqual({ kind: "font-size", delta: 1 });
    expect(actionForEvent(table, event({ key: "PageUp", shiftKey: true }))).toEqual({ kind: "scroll-pages", pages: -1 });
  });

  it("does not fire when a modifier differs", () => {
    expect(actionForEvent(table, event({ key: "T", metaKey: true }))).toBeUndefined();
    expect(actionForEvent(table, event({ key: "t" }))).toBeUndefined();
    expect(actionForEvent(table, event({ key: "+", ctrlKey: true, altKey: true }))).toBeUndefined();
  });

  it("is a no-op with an empty table", () => {
    expect(actionForEvent(buildKeybindTable([]), event({ key: "c", metaKey: true }))).toBeUndefined();
  });
});
