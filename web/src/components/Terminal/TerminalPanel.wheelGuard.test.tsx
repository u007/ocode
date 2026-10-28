import { render, act } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TerminalProvider } from "../../stores/terminalStore";
import TerminalPanel from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";
import { sendWheelReport } from "./terminalMouse";

// Regression coverage for "cannot scroll the claude code that run inside the
// terminal, only can scroll the window":
//
// ghostty-web consumes every wheel over its host element (its own scrollback,
// arrow keys on the alt screen, or a mouse report via terminalMouse.ts) in the
// CAPTURE phase and stopPropagation()s. Anything over the panel's own chrome, or
// whatever the host leaves alone, used to fall through to the browser, which
// scrolls the nearest scrollable ancestor — and in the desktop shell's WKWebView
// even the non-scrollable document rubber-bands, so a gesture over the terminal
// moved the whole app window instead of the terminal. The panel swallows those
// leftover gestures on the container, while still letting the container's own
// overflow fallback scroll.
//
// The stubbed Terminal adds a <canvas> inside the host and installs a capture
// wheel listener on the host that consumes the gesture the way ghostty-web's
// own does (preventDefault + stopPropagation). That keeps the real propagation
// shape — ghostty-web's listener deeper than the panel's guard — so these tests
// can pin both sides of the contract.

type WheelTerminal = FakeTerminal & { wheelHandler: ((e: WheelEvent) => void) | null };

// terminalStore subscribes to the server's `terminal_tabs_changed` bus event;
// the real eventBus auto-starts an SSE stream on the first on(), calling
// remoteApiBase() from this suite's partial api mock. Mirrors the real bus
// surface the store uses (on/off/onReconnect/offReconnect).
vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    on: () => () => {},
    off: () => {},
    onReconnect: () => () => {},
    offReconnect: () => {},
  },
}));

vi.mock("ghostty-web", async () => {
  const mod = (await import("./ghosttyWebMock")).ghosttyWebModule();
  class Terminal extends mod.Terminal {
    wheelHandler: ((e: WheelEvent) => void) | null = null;
    constructor(opts: Record<string, unknown>) {
      super(opts);
      const baseOpen = this.open;
      this.open = vi.fn((host: HTMLElement) => {
        baseOpen(host);
        // jsdom has no layout, so pin a 480x240 grid (24 rows -> 10px per row)
        // starting at (0, 0).
        const canvas = document.createElement("canvas");
        canvas.getBoundingClientRect = () => new DOMRect(0, 0, 480, 240);
        host.appendChild(canvas);
        this.wheelHandler = (e: WheelEvent) => {
          e.preventDefault();
          e.stopPropagation();
        };
        host.addEventListener("wheel", this.wheelHandler, { passive: false, capture: true });
      });
    }
  }
  return { ...mod, Terminal };
});
vi.mock("./ghosttyInternals", async () => (await import("./ghosttyWebMock")).ghosttyInternalsMock());
vi.mock("./terminalMouse", async () => (await import("./ghosttyWebMock")).terminalMouseMock());
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

function termOf(): WheelTerminal {
  const term = FakeTerminal.instances[0] as WheelTerminal | undefined;
  if (!term) throw new Error("terminal not constructed");
  return term;
}

/** The element ghostty-web was opened into (the real wheel target). */
function surfaceOf(): HTMLElement {
  const el = termOf().element;
  if (!el) throw new Error("terminal host not found");
  return el;
}

/** The panel container (the wheel guard lives here): the host's parent. */
function containerOf(): HTMLElement {
  const el = surfaceOf().parentElement;
  if (!el) throw new Error("terminal container not found");
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

/** Drop ghostty-web's stand-in listener: the state a gesture it cannot use leaves. */
function dropTerminalWheelListener() {
  const term = termOf();
  if (term.wheelHandler && term.element) {
    term.element.removeEventListener("wheel", term.wheelHandler, true);
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
  FakeTerminal.instances.length = 0;
  vi.mocked(sendWheelReport).mockClear();
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
    for (let i = 0; i < 20; i++) await Promise.resolve();
  });
  expect(FakeTerminal.instances.length).toBe(1);
  return utils;
}

describe("TerminalPanel wheel gestures stay inside the terminal", () => {
  it("swallows an unconsumed wheel over the terminal when the box cannot scroll", async () => {
    await mountPanel();
    const container = containerOf();
    setScrollBox(container, { scrollTop: 0, clientHeight: 400, scrollHeight: 400 });
    // Only the panel's guard is armed now — the state a gesture ghostty-web cannot
    // use (no scrollback left, or a mouse report the running app never
    // answered) leaves behind. It must not reach the browser as a page scroll.
    dropTerminalWheelListener();

    expect(dispatchWheel(surfaceOf(), -300).defaultPrevented).toBe(true);
    expect(dispatchWheel(surfaceOf(), 300).defaultPrevented).toBe(true);
  });

  it("leaves a gesture ghostty-web already consumed alone", async () => {
    await mountPanel();
    const container = containerOf();
    setScrollBox(container, { scrollTop: 0, clientHeight: 400, scrollHeight: 400 });
    const seenByContainer: boolean[] = [];
    container.addEventListener("wheel", (e) => {
      seenByContainer.push(e.defaultPrevented);
    });

    const event = dispatchWheel(surfaceOf(), 300);

    // The stub host sits deeper and stopped propagation, so neither the
    // guard nor the probe listener ran: ghostty-web — not the page — handled it.
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

// ghostty-web has no touch scrolling of its own, so a one-finger drag on a phone
// browser did nothing. Synthetic wheel events are not a way out, so the panel
// drives the terminal's API: cell rows of drag become scrollLines, or wheel
// reports (terminalMouse.sendWheelReport) when a TUI owns the mouse.
function dispatchTouch(el: HTMLElement, type: string, touches: Array<{ clientX: number; clientY: number }>) {
  const event = new Event(type, { bubbles: true, cancelable: true });
  Object.defineProperty(event, "touches", { value: touches });
  act(() => {
    el.dispatchEvent(event);
  });
}

const at = (clientY: number, clientX = 5) => [{ clientX, clientY }];

describe("TerminalPanel touch drags scroll the terminal", () => {
  it("opts the container out of native touch panning", async () => {
    await mountPanel();
    expect(containerOf().className).toContain("touch-none");
  });

  it("scrolls scrollback by whole rows, carrying the remainder (drag down scrolls up)", async () => {
    await mountPanel();
    const surface = surfaceOf();
    const term = termOf();

    dispatchTouch(surface, "touchstart", at(100));
    dispatchTouch(surface, "touchmove", at(125)); // 25px = 2 rows + 5px carried
    expect(term.scrollLines).toHaveBeenLastCalledWith(-2);
    dispatchTouch(surface, "touchmove", at(120)); // -5px more drag back: net 0 rows
    expect(term.scrollLines).toHaveBeenCalledTimes(1);
    dispatchTouch(surface, "touchmove", at(100)); // 20px up
    expect(term.scrollLines).toHaveBeenLastCalledWith(2);
    expect(sendWheelReport).not.toHaveBeenCalled();
  });

  it("sends wheel reports instead when an app owns the mouse", async () => {
    await mountPanel();
    const surface = surfaceOf();
    const term = termOf();
    term.mouseTracking = true;

    dispatchTouch(surface, "touchstart", at(100, 245));
    dispatchTouch(surface, "touchmove", at(70, 245)); // 30px up = 3 rows of wheel-down
    // The cell geometry (x=245 of 480 over 80 cols -> col 41; y=70 of 240 over
    // 24 rows -> row 8) is terminalMouse.ts's job and is covered in its own tests.
    expect(sendWheelReport).toHaveBeenCalledWith(term, 3, 245, 70);
    expect(term.scrollLines).not.toHaveBeenCalled();
  });

  it("ignores multi-touch and stops after the touch ends", async () => {
    await mountPanel();
    const surface = surfaceOf();
    const term = termOf();

    dispatchTouch(surface, "touchstart", [...at(100), ...at(100, 50)]);
    dispatchTouch(surface, "touchmove", [...at(140), ...at(140, 50)]);
    expect(term.scrollLines).not.toHaveBeenCalled();

    dispatchTouch(surface, "touchstart", at(100));
    dispatchTouch(surface, "touchend", []);
    dispatchTouch(surface, "touchmove", at(140));
    expect(term.scrollLines).not.toHaveBeenCalled();
  });
});
