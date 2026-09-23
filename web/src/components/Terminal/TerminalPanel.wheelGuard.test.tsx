import { render, act } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TerminalProvider } from "../../stores/terminalStore";
import TerminalPanel from "./TerminalPanel";

// Regression coverage for "cannot scroll the claude code that run inside the
// terminal, only can scroll the window":
//
// xterm consumes the wheel gestures it can use (its own scrollback, or a mouse
// report forwarded to the TUI that owns the mouse). Anything it leaves alone
// used to fall through to the browser, which scrolls the nearest scrollable
// ancestor — and in the desktop shell's WKWebView even the non-scrollable
// document rubber-bands, so a gesture over the terminal moved the whole app
// window instead of the terminal. The panel now swallows those leftover
// gestures on the container, while still letting the container's own overflow
// fallback scroll.
//
// The stubbed Terminal creates a child surface inside the container and installs
// a wheel listener there that consumes the gesture the way xterm's own listener
// does (preventDefault + stopPropagation). That keeps the real propagation
// shape — xterm's listener deeper than the panel's guard — so these tests can
// pin both sides of the contract.

const h = vi.hoisted(() => ({
  terminals: [] as Array<{
    container: HTMLElement | null;
    surface: HTMLElement | null;
    wheelHandler: ((e: WheelEvent) => void) | null;
  }>,
}));

vi.mock("@xterm/xterm", () => {
  class Terminal {
    cols = 80;
    rows = 24;
    options: Record<string, unknown> = {};
    buffer = { active: { length: 24 } };
    container: HTMLElement | null = null;
    surface: HTMLElement | null = null;
    wheelHandler: ((e: WheelEvent) => void) | null = null;
    constructor() {
      h.terminals.push(this);
    }
    loadAddon = vi.fn();
    write = vi.fn((_text: string, callback?: () => void) => callback?.());
    reset = vi.fn();
    focus = vi.fn();
    onData = vi.fn(() => ({ dispose: vi.fn() }));
    onSelectionChange = vi.fn(() => ({ dispose: vi.fn() }));
    getSelection = vi.fn(() => "");
    onBell = vi.fn(() => ({ dispose: vi.fn() }));
    onTitleChange = vi.fn(() => ({ dispose: vi.fn() }));
    onWriteParsed = vi.fn(() => ({ dispose: vi.fn() }));
    onResize = vi.fn(() => ({ dispose: vi.fn() }));
    parser = { registerOscHandler: vi.fn(() => ({ dispose: vi.fn() })) };
    dispose = vi.fn();
    attachCustomKeyEventHandler = vi.fn(() => true);
    open(container: HTMLElement) {
      this.container = container;
      // xterm builds its own surface element inside the container and hangs
      // its wheel handling off that. A gesture xterm can use is consumed
      // (preventDefault + stopPropagation); the panel's guard sits on the
      // container and therefore only ever sees what xterm left behind.
      const surface = document.createElement("div");
      surface.className = "xterm";
      container.appendChild(surface);
      this.surface = surface;
      this.wheelHandler = (e: WheelEvent) => {
        if (e.defaultPrevented) return;
        e.preventDefault();
        e.stopPropagation();
      };
      surface.addEventListener("wheel", this.wheelHandler, { passive: false });
    }
  }
  return { Terminal };
});
vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit = vi.fn(); } }));
vi.mock("@xterm/addon-serialize", () => ({ SerializeAddon: class { serialize = vi.fn(() => ""); } }));
vi.mock("@xterm/addon-search", () => ({
  SearchAddon: class {
    onDidChangeResults = vi.fn(() => ({ dispose: vi.fn() }));
    findNext = vi.fn();
    findPrevious = vi.fn();
    clearDecorations = vi.fn();
    dispose = vi.fn();
  },
}));
vi.mock("@xterm/addon-webgl", () => ({ WebglAddon: class { dispose = vi.fn(); onContextLoss = vi.fn(); } }));
vi.mock("@xterm/addon-web-links", () => ({ WebLinksAddon: class { dispose = vi.fn(); } }));
vi.mock("@xterm/xterm/css/xterm.css", () => ({}));
vi.mock("@/api/client", () => ({
  apiPath: (p: string) => p,
  apiWsPath: (p: string) => `ws://localhost${p}`,
  authToken: () => "tok",
  authHeaders: () => ({}),
  isRemoteSession: () => false,
}));
vi.mock("./terminalAlertSound", () => ({ playAlertSound: vi.fn() }));
vi.mock("./terminalFocus", () => ({
  registerTerminalFocus: vi.fn(),
  unregisterTerminalFocus: vi.fn(),
}));
vi.mock("./terminalPersistence", () => ({
  loadProjectTerminals: () => ({ terminals: [{ id: "t1", title: "Terminal 1" }], activeId: "t1" }),
  saveProjectTerminals: vi.fn(),
  loadTerminalBuffer: () => null,
  saveTerminalBuffer: vi.fn(),
}));
vi.mock("@/lib/debug/terminalRegistry", () => ({
  registerTerminal: vi.fn(),
  unregisterTerminal: vi.fn(),
}));
vi.mock("./terminalHistory", () => ({
  restoreTerminalHistory: vi.fn(() => Promise.resolve({ kind: "missing" as const })),
  TerminalHistoryError: class extends Error {},
}));
vi.mock("../Speech/SpeechProvider", () => ({ requestSpeech: vi.fn() }));
vi.mock("../Speech/speechUtils", () => ({ sanitizeSpeechText: (s: string) => s }));

class MockSocket {
  static OPEN = 1;
  readyState = 1;
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: unknown }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: ((e: { wasClean: boolean; code: number; reason: string }) => void) | null = null;
  send = vi.fn();
  close = vi.fn();
  constructor(public url: string) {}
}

/** The panel container xterm was opened into (the wheel guard lives here). */
function containerOf(): HTMLElement {
  const el = h.terminals[0]?.container;
  if (!el) throw new Error("terminal container not found");
  return el;
}

/** The surface xterm renders inside the container (the real wheel target). */
function surfaceOf(): HTMLElement {
  const el = h.terminals[0]?.surface;
  if (!el) throw new Error("terminal surface not found");
  return el;
}

/** jsdom has no layout; pin the container's scroll geometry by hand. */
function setScrollBox(
  el: HTMLElement,
  { scrollTop, clientHeight, scrollHeight }: { scrollTop: number; clientHeight: number; scrollHeight: number },
) {
  Object.defineProperty(el, "scrollTop", { value: scrollTop, writable: true, configurable: true });
  Object.defineProperty(el, "clientHeight", { value: clientHeight, configurable: true });
  Object.defineProperty(el, "scrollHeight", { value: scrollHeight, configurable: true });
}

/** Drop xterm's stand-in listener: the state a gesture xterm cannot use leaves. */
function dropTerminalWheelListener() {
  const term = h.terminals[0];
  if (term?.wheelHandler && term.surface) {
    term.surface.removeEventListener("wheel", term.wheelHandler);
  }
}

function dispatchWheel(el: HTMLElement, deltaY: number): WheelEvent {
  const event = new WheelEvent("wheel", { deltaY, bubbles: true, cancelable: true });
  act(() => {
    el.dispatchEvent(event);
  });
  return event;
}

beforeEach(() => {
  h.terminals.length = 0;
  window.localStorage.clear();
  vi.stubGlobal("WebSocket", MockSocket as unknown as typeof WebSocket);
  vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response("missing", { status: 404 }))));
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
  vi.stubGlobal("requestAnimationFrame", (() => 0) as unknown as typeof requestAnimationFrame);
  vi.stubGlobal("cancelAnimationFrame", (() => {}) as unknown as typeof cancelAnimationFrame);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

async function mountPanel() {
  const utils = render(
    <TerminalProvider>
      <TerminalPanel active id="t1" projectPath="/proj" scrollbackLines={1000} fontFamily="mono" fontSize={12} />
    </TerminalProvider>,
  );
  await act(async () => {
    await Promise.resolve();
  });
  return utils;
}

describe("TerminalPanel wheel gestures stay inside the terminal", () => {
  it("swallows an unconsumed wheel over the terminal when the box cannot scroll", async () => {
    await mountPanel();
    const container = containerOf();
    setScrollBox(container, { scrollTop: 0, clientHeight: 400, scrollHeight: 400 });
    // Only the panel's guard is armed now — the state a gesture xterm cannot
    // use (no scrollback left, or a mouse report the running app never
    // answered) leaves behind. It must not reach the browser as a page scroll.
    dropTerminalWheelListener();

    expect(dispatchWheel(surfaceOf(), -300).defaultPrevented).toBe(true);
    expect(dispatchWheel(surfaceOf(), 300).defaultPrevented).toBe(true);
  });

  it("leaves a gesture xterm already consumed alone", async () => {
    await mountPanel();
    const container = containerOf();
    setScrollBox(container, { scrollTop: 0, clientHeight: 400, scrollHeight: 400 });
    const seenByContainer: boolean[] = [];
    container.addEventListener("wheel", (e) => {
      seenByContainer.push(e.defaultPrevented);
    });

    const event = dispatchWheel(surfaceOf(), 300);

    // The stub surface sits deeper and stopped propagation, so neither the
    // guard nor the probe listener ran: xterm — not the page — handled it.
    expect(event.defaultPrevented).toBe(true);
    expect(seenByContainer).toEqual([]);
  });

  it("still lets the container's own overflow fallback scroll", async () => {
    await mountPanel();
    const container = containerOf();
    dropTerminalWheelListener();

    // Room to scroll up: the gesture belongs to the container.
    setScrollBox(container, { scrollTop: 10, clientHeight: 400, scrollHeight: 420 });
    expect(dispatchWheel(surfaceOf(), -100).defaultPrevented).toBe(false);

    // At the top edge there is nothing left to scroll, so it is swallowed
    // instead of chaining out to the page.
    setScrollBox(container, { scrollTop: 0, clientHeight: 400, scrollHeight: 420 });
    expect(dispatchWheel(surfaceOf(), -100).defaultPrevented).toBe(true);

    // Room to scroll down, then at the bottom edge.
    setScrollBox(container, { scrollTop: 10, clientHeight: 400, scrollHeight: 420 });
    expect(dispatchWheel(surfaceOf(), 100).defaultPrevented).toBe(false);
    setScrollBox(container, { scrollTop: 20, clientHeight: 400, scrollHeight: 420 });
    expect(dispatchWheel(surfaceOf(), 100).defaultPrevented).toBe(true);
  });

  it("marks the container as an overscroll boundary", async () => {
    await mountPanel();
    expect(containerOf().className).toContain("overscroll-contain");
  });
});
