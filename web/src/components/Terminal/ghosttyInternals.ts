import type { Terminal } from "ghostty-web";

// The ONLY file allowed to touch private ghostty-web members.
//
// These helpers exist because ghostty-web has no public API for them yet. The package is pinned
// to the vendored fork tarball in `web/vendor/` (see web/package.json). Each helper validates the
// private shape with typeof checks and throws "ghostty-web internals changed: ..." when it is not
// what that build ships, so a dependency bump fails loudly at open() time instead of silently
// degrading (double clipboard writes, a window.open from the desktop webview, a find bar that
// selects the wrong rows, a second mouse reporter).

type Obj = Record<string, unknown>;

function isObj(v: unknown): v is Obj {
  return typeof v === "object" && v !== null;
}

function internal(term: Terminal, key: string): Obj {
  const v = (term as unknown as Obj)[key];
  if (!isObj(v)) {
    throw new Error(`ghostty-web internals changed: Terminal.${key} is not an object (is the terminal open?)`);
  }
  return v;
}

function selectionManager(term: Terminal): Obj {
  return internal(term, "selectionManager");
}

/** ghostty-web copies every mouse selection to the clipboard itself on mouseup/dblclick.
 *  ocode owns copy (dedupe against Cmd+C / Edit▸Copy, desktop native clipboard, selection
 *  snapshot), so that path is replaced by a no-op. Call after open(). */
export function disableBuiltinSelectionCopy(term: Terminal): void {
  const sm = selectionManager(term);
  if (typeof sm.copyToClipboard !== "function") {
    throw new Error("ghostty-web internals changed: SelectionManager.copyToClipboard is not a function");
  }
  sm.copyToClipboard = () => Promise.resolve();
}

/** Removes the OSC 8 + URL providers open() registered (they window.open on ctrl/cmd-click,
 *  which must never happen inside the desktop webview). Call after open(), before registering ours. */
export function clearBuiltinLinkProviders(term: Terminal): void {
  const detector = internal(term, "linkDetector");
  const providers = detector.providers;
  if (!Array.isArray(providers)) {
    throw new Error("ghostty-web internals changed: LinkDetector.providers is not an array");
  }
  for (const p of providers) {
    if (isObj(p) && typeof p.dispose === "function") p.dispose();
  }
  providers.length = 0;
  // Drop links cached from the providers just removed.
  if (typeof detector.invalidateCache !== "function") {
    throw new Error("ghostty-web internals changed: LinkDetector.invalidateCache is not a function");
  }
  detector.invalidateCache();
}

/** The vendored ghostty-web build reports the mouse itself (press/release/motion/wheel listeners on
 *  the host). ocode's terminalMouse.ts owns reporting (focus events, shift/alt selection escape,
 *  one wheel report per event, release outside the host), so the library's listeners are removed.
 *  Call after open(). */
export function disableBuiltinMouseReporting(term: Terminal): void {
  const ih = internal(term, "inputHandler");
  const container = ih.container;
  if (!(container instanceof EventTarget)) {
    throw new Error("ghostty-web internals changed: InputHandler.container is not an EventTarget");
  }
  const pairs = [
    ["mousedownListener", "mousedown"],
    ["mouseupListener", "mouseup"],
    ["mousemoveListener", "mousemove"],
    ["wheelListener", "wheel"],
  ] as const;
  for (const [field, type] of pairs) {
    const fn = ih[field];
    if (typeof fn !== "function") {
      throw new Error(`ghostty-web internals changed: InputHandler.${field} is not a function (is the terminal open?)`);
    }
    container.removeEventListener(type, fn as EventListener);
    // Nulled so the library's own dispose() skips the removal it would otherwise repeat.
    ih[field] = null;
  }
}

interface AbsPos {
  col: number;
  absoluteRow: number;
}

function isAbsPos(v: unknown): v is AbsPos {
  return isObj(v) && typeof v.col === "number" && typeof v.absoluteRow === "number";
}

/** Sets the selection in ABSOLUTE buffer coordinates (0-based, inclusive). Terminal.select()
 *  is viewport-relative and mis-computes when scrollback exists, so the find bar uses this. */
export function setSelectionAbsolute(
  term: Terminal,
  startCol: number,
  startAbsRow: number,
  endCol: number,
  endAbsRow: number,
): void {
  const sm = selectionManager(term);
  const dirty = sm.dirtySelectionRows;
  if (!(dirty instanceof Set)) {
    throw new Error("ghostty-web internals changed: SelectionManager.dirtySelectionRows is not a Set");
  }
  const emitter = sm.selectionChangedEmitter;
  if (!isObj(emitter) || typeof emitter.fire !== "function") {
    throw new Error("ghostty-web internals changed: SelectionManager.selectionChangedEmitter.fire is not a function");
  }
  if (!("selectionStart" in sm) || !("selectionEnd" in sm)) {
    throw new Error("ghostty-web internals changed: SelectionManager.selectionStart/selectionEnd are missing");
  }
  if (typeof sm.requestRender !== "function") {
    throw new Error("ghostty-web internals changed: SelectionManager.requestRender is not a function");
  }

  // The renderer only repaints rows flagged dirty: flag the old selection's rows (to un-paint
  // them) and the new ones.
  if (isAbsPos(sm.selectionStart) && isAbsPos(sm.selectionEnd)) {
    const lo = Math.min(sm.selectionStart.absoluteRow, sm.selectionEnd.absoluteRow);
    const hi = Math.max(sm.selectionStart.absoluteRow, sm.selectionEnd.absoluteRow);
    for (let r = lo; r <= hi; r++) dirty.add(r);
  }
  for (let r = Math.min(startAbsRow, endAbsRow); r <= Math.max(startAbsRow, endAbsRow); r++) dirty.add(r);

  sm.selectionStart = { col: startCol, absoluteRow: startAbsRow } satisfies AbsPos;
  sm.selectionEnd = { col: endCol, absoluteRow: endAbsRow } satisfies AbsPos;
  sm.requestRender();
  emitter.fire();
}
