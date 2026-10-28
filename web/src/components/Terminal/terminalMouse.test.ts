import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import type { Terminal } from "ghostty-web";
import { attachMouseReporting, sendWheelReport } from "./terminalMouse";

// 80x24 grid on a 800x480 canvas at (100, 50): each cell is 10x20 px.
const COLS = 80;
const ROWS = 24;

function makeFake() {
  const host = document.createElement("div");
  const canvas = document.createElement("canvas");
  const textarea = document.createElement("textarea");
  host.append(canvas, textarea);
  document.body.append(host);
  canvas.getBoundingClientRect = () =>
    ({ left: 100, top: 50, width: 800, height: 480, right: 900, bottom: 530, x: 100, y: 50, toJSON() {} }) as DOMRect;

  const modes = new Set<number>();
  let wheelHandler: ((ev: WheelEvent) => boolean) | undefined;
  const sent: string[] = [];
  const fake = {
    cols: COLS,
    rows: ROWS,
    element: host,
    textarea,
    input: vi.fn((data: string, user?: boolean) => {
      expect(user).toBe(true);
      sent.push(data);
    }),
    getMode: vi.fn((m: number) => modes.has(m)),
    hasMouseTracking: vi.fn(() => [9, 1000, 1002, 1003].some((m) => modes.has(m))),
    hasFocusEvents: vi.fn(() => modes.has(1004)),
    focus: vi.fn(),
    attachCustomWheelEventHandler: vi.fn((fn?: (ev: WheelEvent) => boolean) => {
      wheelHandler = fn;
    }),
  };
  const term = fake as unknown as Terminal;
  return { host, canvas, textarea, modes, fake, term, sent, wheel: () => wheelHandler };
}

// Pointer at the centre of cell (col, row), both 1-based.
const px = (col: number) => 100 + (col - 1) * 10 + 5;
const py = (row: number) => 50 + (row - 1) * 20 + 10;

function mouse(type: string, col: number, row: number, init: MouseEventInit = {}) {
  return new MouseEvent(type, { bubbles: true, cancelable: true, clientX: px(col), clientY: py(row), ...init });
}

let t: ReturnType<typeof makeFake>;
beforeEach(() => {
  t = makeFake();
});
afterEach(() => {
  document.body.innerHTML = "";
});

describe("attachMouseReporting presses", () => {
  it("reports nothing when the program does not track the mouse", () => {
    attachMouseReporting(t.term, t.host);
    t.canvas.dispatchEvent(mouse("mousedown", 3, 4));
    window.dispatchEvent(mouse("mouseup", 3, 4));
    expect(t.sent).toEqual([]);
  });

  it("encodes SGR press and release with 1-based cells", () => {
    t.modes.add(1000).add(1006);
    attachMouseReporting(t.term, t.host);
    t.canvas.dispatchEvent(mouse("mousedown", 3, 4));
    window.dispatchEvent(mouse("mouseup", 5, 6));
    expect(t.sent).toEqual(["\x1b[<0;3;4M", "\x1b[<0;5;6m"]);
  });

  it("treats 1016 like SGR", () => {
    t.modes.add(1000).add(1016);
    attachMouseReporting(t.term, t.host);
    t.canvas.dispatchEvent(mouse("mousedown", 1, 1));
    expect(t.sent).toEqual(["\x1b[<0;1;1M"]);
  });

  it("encodes X10 press and a release that uses button 3", () => {
    t.modes.add(1000);
    attachMouseReporting(t.term, t.host);
    t.canvas.dispatchEvent(mouse("mousedown", 3, 4));
    window.dispatchEvent(mouse("mouseup", 3, 4));
    expect(t.sent).toEqual(["\x1b[M" + String.fromCharCode(32, 35, 36), "\x1b[M" + String.fromCharCode(35, 35, 36)]);
  });

  it("clamps X10 coordinates to 223 and cells to the grid", () => {
    t.modes.add(1000);
    attachMouseReporting(t.term, t.host);
    t.canvas.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, clientX: 99999, clientY: 99999 }));
    expect(t.sent[0]).toBe("\x1b[M" + String.fromCharCode(32, 32 + 80, 32 + 24));
    t.sent.length = 0;
    t.modes.add(1006);
    t.canvas.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, clientX: -50, clientY: -50 }));
    expect(t.sent[0]).toBe("\x1b[<0;1;1M");
  });

  it("encodes buttons and modifiers (ctrl +16, meta +8)", () => {
    t.modes.add(1000).add(1006);
    attachMouseReporting(t.term, t.host);
    t.canvas.dispatchEvent(mouse("mousedown", 1, 1, { button: 1 }));
    t.canvas.dispatchEvent(mouse("mousedown", 1, 1, { button: 2 }));
    t.canvas.dispatchEvent(mouse("mousedown", 1, 1, { button: 0, ctrlKey: true }));
    t.canvas.dispatchEvent(mouse("mousedown", 1, 1, { button: 0, metaKey: true }));
    expect(t.sent).toEqual(["\x1b[<1;1;1M", "\x1b[<2;1;1M", "\x1b[<16;1;1M", "\x1b[<8;1;1M"]);
  });

  it("does not report a press with shift or alt, and leaves it for ghostty-web", () => {
    t.modes.add(1000).add(1006);
    attachMouseReporting(t.term, t.host);
    const below = vi.fn();
    t.canvas.addEventListener("mousedown", below);
    const shift = mouse("mousedown", 2, 2, { shiftKey: true });
    const alt = mouse("mousedown", 2, 2, { altKey: true });
    t.canvas.dispatchEvent(shift);
    t.canvas.dispatchEvent(alt);
    expect(t.sent).toEqual([]);
    expect(below).toHaveBeenCalledTimes(2);
    expect(shift.defaultPrevented).toBe(false);
    // The matching release of an unreported press is not reported either.
    window.dispatchEvent(mouse("mouseup", 2, 2));
    expect(t.sent).toEqual([]);
  });

  it("stops a reported left press from reaching the selection manager and focuses", () => {
    t.modes.add(1000).add(1006);
    attachMouseReporting(t.term, t.host);
    const below = vi.fn();
    t.canvas.addEventListener("mousedown", below);
    const ev = mouse("mousedown", 2, 2);
    t.canvas.dispatchEvent(ev);
    expect(below).not.toHaveBeenCalled();
    expect(ev.defaultPrevented).toBe(true);
    expect(t.fake.focus).toHaveBeenCalledTimes(1);
  });

  it("reports but does not stop propagation for the right button", () => {
    t.modes.add(1000).add(1006);
    attachMouseReporting(t.term, t.host);
    const below = vi.fn();
    t.canvas.addEventListener("mousedown", below);
    const ev = mouse("mousedown", 2, 2, { button: 2 });
    t.canvas.dispatchEvent(ev);
    expect(t.sent).toEqual(["\x1b[<2;2;2M"]);
    expect(below).toHaveBeenCalledTimes(1);
    expect(ev.defaultPrevented).toBe(false);
    expect(t.fake.focus).not.toHaveBeenCalled();
  });
});

describe("attachMouseReporting motion", () => {
  it("1002 reports motion only while a reported button is held, once per cell, with +32", () => {
    t.modes.add(1002).add(1006);
    attachMouseReporting(t.term, t.host);
    t.canvas.dispatchEvent(mouse("mousemove", 2, 2));
    expect(t.sent).toEqual([]);
    t.canvas.dispatchEvent(mouse("mousedown", 2, 2));
    t.canvas.dispatchEvent(mouse("mousemove", 2, 2)); // same cell
    t.canvas.dispatchEvent(mouse("mousemove", 3, 2));
    t.canvas.dispatchEvent(mouse("mousemove", 3, 2)); // same cell again
    expect(t.sent).toEqual(["\x1b[<0;2;2M", "\x1b[<32;3;2M"]);
    window.dispatchEvent(mouse("mouseup", 3, 2));
    t.canvas.dispatchEvent(mouse("mousemove", 4, 2));
    expect(t.sent).toHaveLength(3); // release only
  });

  it("1003 reports buttonless motion with button 3 + 32", () => {
    t.modes.add(1003).add(1006);
    attachMouseReporting(t.term, t.host);
    t.canvas.dispatchEvent(mouse("mousemove", 7, 8));
    expect(t.sent).toEqual(["\x1b[<35;7;8M"]);
  });

  it("1000 never reports motion", () => {
    t.modes.add(1000).add(1006);
    attachMouseReporting(t.term, t.host);
    t.canvas.dispatchEvent(mouse("mousedown", 2, 2));
    t.canvas.dispatchEvent(mouse("mousemove", 5, 5));
    expect(t.sent).toEqual(["\x1b[<0;2;2M"]);
  });
});

describe("wheel", () => {
  it("sendWheelReport sends one report per row, up = 64, down = 65", () => {
    t.modes.add(1000).add(1006);
    sendWheelReport(t.term, -2, px(3), py(4));
    sendWheelReport(t.term, 1, px(3), py(4));
    expect(t.sent).toEqual(["\x1b[<64;3;4M", "\x1b[<64;3;4M", "\x1b[<65;3;4M"]);
  });

  it("sendWheelReport uses X10 when SGR is off and does nothing without tracking", () => {
    sendWheelReport(t.term, 1, px(1), py(1));
    expect(t.sent).toEqual([]);
    t.modes.add(1000);
    sendWheelReport(t.term, 1, px(1), py(1));
    expect(t.sent).toEqual(["\x1b[M" + String.fromCharCode(32 + 65, 33, 33)]);
  });

  it("handler passes through when the program does not track the mouse", () => {
    attachMouseReporting(t.term, t.host);
    expect(t.wheel()!(new WheelEvent("wheel", { deltaY: 100 }))).toBe(false);
    expect(t.sent).toEqual([]);
  });

  it("sends exactly one report per wheel event, by direction only (xterm parity)", () => {
    t.modes.add(1000).add(1006);
    attachMouseReporting(t.term, t.host);
    const w = t.wheel()!;
    const wheel = (deltaY: number, deltaMode = 0) =>
      w(new WheelEvent("wheel", { deltaY, deltaMode, clientX: px(2), clientY: py(2) }));
    expect(wheel(120)).toBe(true); // one mouse notch
    expect(t.sent).toEqual(["\x1b[<65;2;2M"]);
    expect(wheel(-4)).toBe(true); // a tiny trackpad delta still counts as one line
    expect(t.sent).toEqual(["\x1b[<65;2;2M", "\x1b[<64;2;2M"]);
    expect(wheel(3, 1)).toBe(true); // LINE and PAGE modes are not scaled either
    expect(wheel(1, 2)).toBe(true);
    expect(t.sent).toHaveLength(4);
    expect(wheel(0)).toBe(true); // consumed but nothing to report
    expect(t.sent).toHaveLength(4);
  });
});

describe("focus reporting and disposer", () => {
  it("sends focus in/out only when the program enabled 1004", () => {
    attachMouseReporting(t.term, t.host);
    t.textarea.dispatchEvent(new Event("focus"));
    expect(t.sent).toEqual([]);
    t.modes.add(1004);
    t.textarea.dispatchEvent(new Event("focus"));
    t.textarea.dispatchEvent(new Event("blur"));
    expect(t.sent).toEqual(["\x1b[I", "\x1b[O"]);
  });

  it("disposer removes every listener and clears the wheel handler", () => {
    t.modes.add(1000).add(1006).add(1004).add(1003);
    const dispose = attachMouseReporting(t.term, t.host);
    dispose();
    t.canvas.dispatchEvent(mouse("mousedown", 2, 2));
    t.canvas.dispatchEvent(mouse("mousemove", 3, 3));
    window.dispatchEvent(mouse("mouseup", 3, 3));
    t.textarea.dispatchEvent(new Event("focus"));
    expect(t.sent).toEqual([]);
    expect(t.wheel()).toBeUndefined();
    expect(t.fake.attachCustomWheelEventHandler).toHaveBeenLastCalledWith(undefined);
  });

  it("throws a descriptive error when the terminal is not open", () => {
    const closed = { ...t.fake, textarea: undefined } as unknown as Terminal;
    expect(() => attachMouseReporting(closed, t.host)).toThrow(/no textarea/);
  });
});
