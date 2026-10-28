import type { Terminal } from "ghostty-web";

// Mouse and focus REPORTING to the pty. The vendored ghostty-web build has its own press, release,
// motion and wheel reporting, which the panel removes via `disableBuiltinMouseReporting`
// (ghosttyInternals.ts) because it lacks focus reports (1004), reports shift/alt presses that ocode
// reserves for selection, reports every wheel pixel-delta, and only sees releases inside the host.
// This module is therefore the single sender. `hasMouseTracking()` / `getMode(n)` only expose the
// state. Every report goes through `term.input(report, true)` so it flows through `onData` (and
// the panel's attach gate) exactly like typed input.

const WHEEL_UP = 64;
const WHEEL_DOWN = 65;
const MOTION = 32;
const X10_RELEASE = 3;
const X10_MAX_COORD = 223; // 32 + 223 = 255, the last byte value X10 can carry

type Reportable = Pick<Terminal, "cols" | "rows" | "element" | "input" | "getMode" | "hasMouseTracking">;

function canvasOf(host: HTMLElement): HTMLCanvasElement {
  const canvas = host.querySelector("canvas");
  if (!canvas) throw new Error("terminal mouse reporting: no <canvas> inside the terminal host (is the terminal open?)");
  return canvas;
}

/** 1-based cell under the pointer, clamped to the grid. */
function cellAt(term: Pick<Terminal, "cols" | "rows">, host: HTMLElement, clientX: number, clientY: number) {
  const rect = canvasOf(host).getBoundingClientRect();
  const cellW = rect.width / term.cols;
  const cellH = rect.height / term.rows;
  const x = Math.min(term.cols, Math.max(1, Math.floor((clientX - rect.left) / cellW) + 1));
  const y = Math.min(term.rows, Math.max(1, Math.floor((clientY - rect.top) / cellH) + 1));
  return { x, y };
}

function modifierBits(ev: Pick<MouseEvent, "shiftKey" | "altKey" | "metaKey" | "ctrlKey">): number {
  return (ev.shiftKey ? 4 : 0) + (ev.altKey || ev.metaKey ? 8 : 0) + (ev.ctrlKey ? 16 : 0);
}

function encode(term: Pick<Terminal, "getMode">, cb: number, x: number, y: number, release: boolean): string {
  // 1016 is pixel-coordinate SGR. We still report cells: it is rare, and a program asking for
  // pixels that gets cells misplaces clicks by a constant factor rather than breaking outright.
  if (term.getMode(1006) || term.getMode(1016)) {
    return `\x1b[<${cb};${x};${y}${release ? "m" : "M"}`;
  }
  const code = release ? X10_RELEASE + (cb & ~3) : cb;
  const cx = Math.min(x, X10_MAX_COORD);
  const cy = Math.min(y, X10_MAX_COORD);
  return "\x1b[M" + String.fromCharCode(32 + code, 32 + cx, 32 + cy);
}

function requireElement(term: Pick<Terminal, "element">): HTMLElement {
  if (!term.element) throw new Error("terminal mouse reporting: terminal is not open");
  return term.element;
}

/** Encodes and sends one wheel report per row when the program tracks the mouse.
 *  rows < 0 = wheel up (button 64), rows > 0 = wheel down (65). clientX/Y locate the cell. */
export function sendWheelReport(term: Reportable, rows: number, clientX: number, clientY: number): void {
  if (!term.hasMouseTracking() || rows === 0) return;
  const { x, y } = cellAt(term, requireElement(term), clientX, clientY);
  const report = encode(term, rows < 0 ? WHEEL_UP : WHEEL_DOWN, x, y, false);
  for (let i = 0; i < Math.abs(rows); i++) term.input(report, true);
}

/** Installs press/release/motion/wheel/focus reporting on an opened terminal; returns the disposer. */
export function attachMouseReporting(
  term: Reportable &
    Pick<Terminal, "focus" | "hasFocusEvents" | "attachCustomWheelEventHandler" | "textarea">,
  host: HTMLElement,
): () => void {
  const textarea = term.textarea;
  if (!textarea) throw new Error("terminal mouse reporting: terminal has no textarea (is the terminal open?)");

  let held: number | null = null; // button of a REPORTED press still down
  let lastCell = { x: 0, y: 0 };

  const onMouseDown = (ev: MouseEvent) => {
    if (!term.hasMouseTracking()) return;
    // shift/alt+press is left to ghostty-web so the user can still select text under a
    // mouse-owning program (same convention on every platform).
    if (ev.shiftKey || ev.altKey) return;
    if (ev.button < 0 || ev.button > 2) return;
    const { x, y } = cellAt(term, host, ev.clientX, ev.clientY);
    held = ev.button;
    lastCell = { x, y };
    term.input(encode(term, ev.button + modifierBits(ev), x, y, false), true);
    if (ev.button === 0) {
      // Keep SelectionManager (canvas listener) from starting a selection. That also skips
      // ghostty-web's own focus-on-mousedown, so focus here. Other buttons must still reach
      // the panel's onContextMenu, so they propagate.
      ev.preventDefault();
      ev.stopImmediatePropagation();
      term.focus();
    }
  };

  const onMouseMove = (ev: MouseEvent) => {
    if (!term.hasMouseTracking()) return;
    const allMotion = term.getMode(1003);
    if (!allMotion && !(term.getMode(1002) && held !== null)) return;
    const { x, y } = cellAt(term, host, ev.clientX, ev.clientY);
    if (x === lastCell.x && y === lastCell.y) return;
    lastCell = { x, y };
    const button = held ?? X10_RELEASE; // 3 = "no button" for motion
    term.input(encode(term, button + MOTION + modifierBits(ev), x, y, false), true);
  };

  // On window: the release can land outside the host.
  const onMouseUp = (ev: MouseEvent) => {
    if (held === null) return;
    const button = held;
    held = null;
    if (!term.hasMouseTracking()) return;
    const { x, y } = cellAt(term, host, ev.clientX, ev.clientY);
    term.input(encode(term, button + modifierBits(ev), x, y, true), true);
  };

  const onFocus = () => {
    if (term.hasFocusEvents()) term.input("\x1b[I", true);
  };
  const onBlur = () => {
    if (term.hasFocusEvents()) term.input("\x1b[O", true);
  };

  term.attachCustomWheelEventHandler((ev) => {
    if (!term.hasMouseTracking()) return false;
    // One report per DOM wheel event, direction only, the same as xterm.js did: a mouse
    // notch is one line and a trackpad flick is a burst of events. Scaling by the pixel
    // delta sent a notch (deltaY 120) as nine lines, which is unusable in a TUI.
    if (ev.deltaY !== 0) sendWheelReport(term, ev.deltaY < 0 ? -1 : 1, ev.clientX, ev.clientY);
    return true;
  });

  host.addEventListener("mousedown", onMouseDown, true);
  host.addEventListener("mousemove", onMouseMove, true);
  window.addEventListener("mouseup", onMouseUp, true);
  textarea.addEventListener("focus", onFocus);
  textarea.addEventListener("blur", onBlur);

  return () => {
    host.removeEventListener("mousedown", onMouseDown, true);
    host.removeEventListener("mousemove", onMouseMove, true);
    window.removeEventListener("mouseup", onMouseUp, true);
    textarea.removeEventListener("focus", onFocus);
    textarea.removeEventListener("blur", onBlur);
    term.attachCustomWheelEventHandler(undefined);
  };
}
