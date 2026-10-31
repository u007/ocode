import { render, act, waitFor, cleanup } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TerminalProvider } from "../../stores/terminalStore";
import TerminalPanel, { writeClipboardText } from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";

// Regression suite for nav keys typed on a PHONE soft keyboard (SwiftKey's
// arrow row, Gboard's cursor keys). ghostty-web resolves these from
// `event.code` and bails out entirely on `keyCode === 229` (the Android IME
// marker), so a soft-key arrow used to reach neither the encoder nor the pty
// while ordinary text worked (that rides `beforeinput`, not `keydown`). The
// panel now owns the leftovers in a capture-phase listener.

const h = vi.hoisted(() => ({
  sockets: [] as Array<{
    onopen: (() => void) | null;
    onmessage: ((e: { data: unknown }) => void) | null;
    send: ((data: string) => void) & { mockClear: () => void };
  }>,
}));

vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    on: () => () => {},
    off: () => {},
    onReconnect: () => () => {},
    offReconnect: () => {},
  },
}));

vi.mock("ghostty-web", async () => (await import("./ghosttyWebMock")).ghosttyWebModule());
vi.mock("./ghosttyInternals", async () => (await import("./ghosttyWebMock")).ghosttyInternalsMock());
vi.mock("./terminalMouse", async () => (await import("./ghosttyWebMock")).terminalMouseMock());
vi.mock("@/api/client", () => ({
  api: {
    cancelPrewarmSpeech: () => Promise.resolve(true),
    prewarmSpeech: () => Promise.resolve(true),
  },
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
  constructor(public url: string) {
    h.sockets.push(this);
  }
}

const writeText = vi.fn<(text: string) => Promise<void>>(() => Promise.resolve());

Object.defineProperty(navigator, "clipboard", {
  value: { writeText },
  configurable: true,
  writable: true,
});

beforeEach(async () => {
  FakeTerminal.instances.length = 0;
  h.sockets.length = 0;
  cleanup();
  await writeClipboardText("__reset__");
  await writeClipboardText("");
  writeText.mockClear();
  vi.stubGlobal("WebSocket", MockSocket as unknown as typeof WebSocket);
  vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response("", { status: 404 }))));
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

async function mountPanel() {
  const utils = render(
    <TerminalProvider>
      <TerminalPanel
        active
        id="t1"
        projectPath="/proj"
        scrollbackLines={1000}
        fontFamily="mono"
        fontSize={12}
        keybinds={[]}
      />
    </TerminalProvider>,
  );
  await act(async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
  });
  expect(FakeTerminal.instances.length).toBe(1);
  const term = FakeTerminal.instances[0];
  // Finish the attach handshake so `term.onData` is allowed to drive the pty.
  await act(async () => {
    h.sockets[0]?.onopen?.();
    h.sockets[0]?.onmessage?.({ data: JSON.stringify({ type: "attach", resumed: true }) });
  });
  return { utils, term, socket: h.sockets[0] };
}

interface SoftKeyInit {
  key: string;
  code?: string;
  keyCode?: number;
  ctrlKey?: boolean;
  altKey?: boolean;
  metaKey?: boolean;
  shiftKey?: boolean;
}

function fireKeyDown(target: HTMLElement, init: SoftKeyInit): { event: KeyboardEvent } {
  const event = new KeyboardEvent("keydown", {
    key: init.key,
    code: init.code ?? "",
    bubbles: true,
    cancelable: true,
    ctrlKey: init.ctrlKey ?? false,
    altKey: init.altKey ?? false,
    metaKey: init.metaKey ?? false,
    shiftKey: init.shiftKey ?? false,
  });
  Object.defineProperty(event, "keyCode", { get: () => init.keyCode ?? 0 });
  target.dispatchEvent(event);
  return { event };
}

describe("soft-keyboard navigation keys in the terminal", () => {
  it("sends an arrow that ghostty-web drops (empty code + Android IME keyCode 229)", async () => {
    const { term, socket } = await mountPanel();
    // SwiftKey delegates its arrow row through InputConnection.sendKeyEvent,
    // and Android marks soft-key events with keyCode 229.
    const { event } = fireKeyDown(term.textarea!, { key: "ArrowUp", code: "", keyCode: 229 });
    expect(socket.send).toHaveBeenCalledWith("\x1b[A");
    expect(event.defaultPrevented).toBe(true);
  });

  it("maps all four arrows", async () => {
    const { term, socket } = await mountPanel();
    const cases: Array<[string, string]> = [
      ["ArrowUp", "\x1b[A"],
      ["ArrowDown", "\x1b[B"],
      ["ArrowRight", "\x1b[C"],
      ["ArrowLeft", "\x1b[D"],
    ];
    for (const [key, seq] of cases) {
      socket.send.mockClear();
      fireKeyDown(term.textarea!, { key, code: "", keyCode: 229 });
      expect(socket.send).toHaveBeenCalledWith(seq);
    }
  });

  it("uses the application-cursor form when DECCKM (mode 1) is on", async () => {
    const { term, socket } = await mountPanel();
    term.modes[1] = true;
    fireKeyDown(term.textarea!, { key: "ArrowUp", code: "", keyCode: 229 });
    expect(socket.send).toHaveBeenCalledWith("\x1bOA");
  });

  it("leaves a hardware arrow to ghostty-web (code present, real keyCode)", async () => {
    const { term, socket } = await mountPanel();
    const { event } = fireKeyDown(term.textarea!, {
      key: "ArrowUp",
      code: "ArrowUp",
      keyCode: 38,
    });
    // ghostty-web's own keydown path encodes this one; intercepting it here
    // would bypass its Kitty/CSI-u encoding and double-send.
    expect(socket.send).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });

  it("leaves a keypad arrow to ghostty-web (physical code it encodes separately)", async () => {
    const { term, socket } = await mountPanel();
    // Numpad8 with numlock off reports key ArrowUp + keyCode 38 but a physical
    // `code`; the keypad has its own encoding (application keypad mode) that
    // only ghostty-web's encoder knows, so it must not be synthesised here.
    const { event } = fireKeyDown(term.textarea!, {
      key: "ArrowUp",
      code: "Numpad8",
      keyCode: 38,
    });
    expect(socket.send).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });

  it("never claims a modified arrow", async () => {
    const { term, socket } = await mountPanel();
    for (const mods of [{ ctrlKey: true }, { altKey: true }, { metaKey: true }, { shiftKey: true }]) {
      const { event } = fireKeyDown(term.textarea!, {
        key: "ArrowLeft",
        code: "",
        keyCode: 229,
        ...mods,
      });
      expect(event.defaultPrevented).toBe(false);
    }
    expect(socket.send).not.toHaveBeenCalled();
  });

  it("ignores a nav key while an IME composition is in progress", async () => {
    const { term, socket } = await mountPanel();
    // During a CJK/predictive composition the arrow keys move between candidates;
    // sending an escape sequence then would corrupt the composition.
    const event = new KeyboardEvent("keydown", {
      key: "ArrowUp",
      code: "",
      bubbles: true,
      cancelable: true,
      isComposing: true,
    });
    Object.defineProperty(event, "keyCode", { get: () => 229 });
    term.textarea!.dispatchEvent(event);
    expect(socket.send).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });

  it("falls back to keyCode when the key string is Unidentified", async () => {
    const { term, socket } = await mountPanel();
    fireKeyDown(term.textarea!, { key: "Unidentified", code: "", keyCode: 40 });
    expect(socket.send).toHaveBeenCalledWith("\x1b[B");
  });

  it("maps Home/End/PageUp/PageDown soft keys too", async () => {
    const { term, socket } = await mountPanel();
    const cases: Array<[string, number, string]> = [
      ["Home", 36, "\x1b[H"],
      ["End", 35, "\x1b[F"],
      ["PageUp", 33, "\x1b[5~"],
      ["PageDown", 34, "\x1b[6~"],
    ];
    for (const [key, keyCode, seq] of cases) {
      socket.send.mockClear();
      fireKeyDown(term.textarea!, { key, code: "", keyCode });
      expect(socket.send).toHaveBeenCalledWith(seq);
    }
  });

  it("ignores keys typed into the find bar", async () => {
    const { term, socket } = await mountPanel();
    // ctrl+f opens the panel's own find bar (inside the container, outside the
    // terminal host) — its ArrowUp must edit the field, never the shell.
    act(() => {
      const e = new KeyboardEvent("keydown", { key: "f", ctrlKey: true, cancelable: true });
      term._customKeyHandler!(e);
    });
    const input = await waitFor(() => {
      const el = document.querySelector<HTMLInputElement>('input[aria-label="Find in terminal"]');
      expect(el).toBeTruthy();
      return el!;
    });
    const { event } = fireKeyDown(input, { key: "ArrowUp", code: "", keyCode: 229 });
    expect(socket.send).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });
});
