import { vi } from "vitest";

// Shared test double for `ghostty-web`'s DOM Terminal. jsdom has no canvas, so a real
// Terminal.open() cannot run; the panel tests mock the module with this instead:
//
//   vi.mock("ghostty-web", async () => (await import("./ghosttyWebMock")).ghosttyWebModule());
//   vi.mock("./ghosttyInternals", async () => (await import("./ghosttyWebMock")).ghosttyInternalsMock());
//   vi.mock("./terminalMouse", async () => (await import("./ghosttyWebMock")).terminalMouseMock());
//
// Every constructed FakeTerminal is pushed onto `FakeTerminal.instances` (tests reset it in
// beforeEach). The panel mounts asynchronously (it awaits loadGhostty), so tests wait for
// `FakeTerminal.instances.length` before touching a terminal.

type Range = { start: { x: number; y: number }; end: { x: number; y: number } };
type KeyHandler = (ev: KeyboardEvent) => boolean;

/** ghostty-web's null cell (buffer.getNullCell()): the colours it synthesizes past a row's stored cells. */
export const fakeNullCell = {
  getChars: () => "",
  getWidth: () => 1,
  getFgColor: () => 0xcccccc,
  getBgColor: () => 0x000000,
  isBold: () => 0,
  isItalic: () => 0,
  isUnderline: () => 0,
  isStrikethrough: () => 0,
  isBlink: () => 0,
  isInverse: () => 0,
  isInvisible: () => 0,
  isFaint: () => 0,
};

export class FakeTerminal {
  static instances: FakeTerminal[] = [];

  cols = 80;
  rows = 24;
  options: Record<string, unknown> = {};
  /** The ghostty-web options object the panel constructed this terminal with. */
  ctorOptions: Record<string, unknown>;
  element: HTMLElement | undefined = undefined;
  textarea: HTMLTextAreaElement | undefined = undefined;
  buffer = {
    active: {
      // `viewportY` is deliberately the real library's always-0 field; callers must
      // use getViewportY(). `type` drives terminalScrollAnchor's alternate-screen guard.
      type: "normal" as "normal" | "alternate",
      length: 0,
      cursorX: 0,
      cursorY: 0,
      viewportY: 0,
      getLine: vi.fn((): undefined => undefined),
      getNullCell: () => fakeNullCell,
    },
  };

  // Test-controlled state.
  selectionText = "";
  selectionRange: Range | undefined = undefined;
  mouseTracking = false;
  focusEvents = false;
  viewportY = 0;
  /** Scrollback rows the fake write has produced (`getScrollbackLength`). */
  scrollbackLength = 0;
  modes: Record<number, boolean> = {};

  // Handlers the panel registers.
  _selectionChange: (() => void) | null = null;
  _customKeyHandler: KeyHandler | null = null;
  _data: ((data: string) => void) | null = null;
  _resize: (() => void) | null = null;
  _wheelHandler: ((ev: WheelEvent) => boolean) | undefined = undefined;
  _linkProviders: Array<{ provideLinks: (y: number, cb: (links: unknown[] | undefined) => void) => void }> = [];

  constructor(opts: Record<string, unknown> = {}) {
    this.ctorOptions = opts;
    this.options = { ...opts };
    if (typeof opts.cols === "number") this.cols = opts.cols;
    if (typeof opts.rows === "number") this.rows = opts.rows;
    FakeTerminal.instances.push(this);
  }

  loadAddon = vi.fn();
  open = vi.fn((host: HTMLElement) => {
    this.element = host;
    this.textarea = document.createElement("textarea");
    host.appendChild(this.textarea);
  });
  // Models ghostty-web 0.4.0's writeInternal: newlines append scrollback, and a
  // write resets a scrolled-back viewport to the bottom (the "bounce" that
  // terminalScrollAnchor.ts compensates for). Tests drive it through the panel.
  write = vi.fn((data: string | Uint8Array, _callback?: () => void) => {
    const text = typeof data === "string" ? data : new TextDecoder().decode(data);
    for (const ch of text) {
      if (ch === "\n") this.scrollbackLength += 1;
    }
    if (this.viewportY !== 0) this.viewportY = 0;
  });
  focus = vi.fn();
  blur = vi.fn();
  dispose = vi.fn();
  paste = vi.fn();
  // Mirrors ghostty-web: `input(data, true)` fires onData (the typed-input
  // path the pty is driven by); `input(data)` writes into the emulator only.
  input = vi.fn((data: string, wasUserInput = false) => {
    if (wasUserInput) this._data?.(data);
  });
  reset = vi.fn();
  clear = vi.fn();
  resize = vi.fn();
  onData = vi.fn((cb: (data: string) => void) => {
    this._data = cb;
    return { dispose: vi.fn() };
  });
  onResize = vi.fn((cb: () => void) => {
    this._resize = cb;
    return { dispose: vi.fn() };
  });
  onSelectionChange = vi.fn((cb: () => void) => {
    this._selectionChange = cb;
    return { dispose: vi.fn() };
  });
  getSelection = vi.fn(() => this.selectionText);
  hasSelection = vi.fn(() => this.selectionText.length > 0);
  clearSelection = vi.fn();
  selectAll = vi.fn();
  getSelectionPosition = vi.fn(() => this.selectionRange);
  attachCustomKeyEventHandler = vi.fn((cb: KeyHandler) => {
    this._customKeyHandler = cb;
  });
  attachCustomWheelEventHandler = vi.fn((cb: ((ev: WheelEvent) => boolean) | undefined) => {
    this._wheelHandler = cb;
  });
  registerLinkProvider = vi.fn((p: FakeTerminal["_linkProviders"][number]) => {
    this._linkProviders.push(p);
  });
  hasMouseTracking = vi.fn(() => this.mouseTracking);
  hasFocusEvents = vi.fn(() => this.focusEvents);
  getMode = vi.fn((mode: number) => this.modes[mode] ?? false);
  getViewportY = vi.fn(() => this.viewportY);
  getScrollbackLength = vi.fn(() => this.scrollbackLength);
  scrollLines = vi.fn((amount: number) => {
    this.viewportY = Math.max(0, Math.min(this.scrollbackLength, this.viewportY - amount));
  });
  scrollToTop = vi.fn(() => {
    this.viewportY = this.scrollbackLength;
  });
  scrollToBottom = vi.fn(() => {
    this.viewportY = 0;
  });
  scrollToLine = vi.fn((line: number) => {
    this.viewportY = Math.max(0, Math.min(this.scrollbackLength, line));
  });
}

export class FakeFitAddon {
  fit = vi.fn();
}

/** The `ghostty-web` module surface the panel uses. */
export function ghosttyWebModule() {
  class Ghostty {
    static load = vi.fn(async () => ({}));
  }
  return { Terminal: FakeTerminal, FitAddon: FakeFitAddon, Ghostty };
}

/** `./ghosttyInternals` pokes private members of a real open terminal; a fake cannot satisfy it. */
export function ghosttyInternalsMock() {
  return {
    disableBuiltinSelectionCopy: vi.fn(),
    clearBuiltinLinkProviders: vi.fn(),
    disableBuiltinMouseReporting: vi.fn(),
    setSelectionAbsolute: vi.fn(),
    // Appearance path: no-ops here. Tests that assert the cell/theme application
    // read the real helpers from ghosttyInternals.test.ts instead, because a mock
    // that pretends to scale metrics would assert nothing about the renderer.
    applyCellAdjust: vi.fn(),
    applyTerminalTheme: vi.fn(),
  };
}

/** `./terminalMouse` needs a real canvas rect; panel tests that do not exercise it stub it. */
export function terminalMouseMock() {
  return {
    attachMouseReporting: vi.fn(() => () => {}),
    sendWheelReport: vi.fn(),
  };
}
