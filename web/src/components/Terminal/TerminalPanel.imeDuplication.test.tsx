import { render, act, cleanup } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TerminalProvider } from "../../stores/terminalStore";
import TerminalPanel from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";

// Regression suite for a PHONE soft keyboard committing the same word twice.
//
// Measured on the real terminal (2026-11-02, Chromium + CDP): ghostty-web
// suppresses the second delivery only inside its own 100 ms window, so the same
// event sequence sends `ls` once at a 60 ms re-commit gap and TWICE at 250 ms.
// The shell then runs the command twice. The panel now records every
// `compositionend` and drops an identical commit that follows its own accepted
// frame, whatever the delay.

const h = vi.hoisted(() => ({
  sockets: [] as Array<{
    onopen: (() => void) | null;
    onmessage: ((e: { data: unknown }) => void) | null;
    send: ReturnType<typeof vi.fn>;
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
  focusTerminalInput: vi.fn(),
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

beforeEach(() => {
  FakeTerminal.instances.length = 0;
  h.sockets.length = 0;
  cleanup();
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
  await act(async () => {
    h.sockets[0]?.onopen?.();
    h.sockets[0]?.onmessage?.({ data: JSON.stringify({ type: "attach", resumed: true }) });
  });
  return { utils, term, socket: h.sockets[0] };
}

/** What ghostty-web does for one IME commit: `compositionend`, then onData. */
async function commitIme(term: FakeTerminal, text: string) {
  await act(async () => {
    term.textarea?.dispatchEvent(new CompositionEvent("compositionend", { data: text, bubbles: true }));
    term.input(text, true);
  });
}

describe("terminal IME commit de-duplication", () => {
  it("sends a composed word once, not twice, when the soft keyboard re-commits it", async () => {
    const { term, socket } = await mountPanel();
    socket.send.mockClear();

    await commitIme(term, "ls");
    expect(socket.send).toHaveBeenCalledTimes(1);
    expect(socket.send).toHaveBeenLastCalledWith("ls");

    // The slow re-commit ghostty-web's 100 ms window does not swallow.
    await commitIme(term, "ls");
    expect(socket.send).toHaveBeenCalledTimes(1);
  });

  it("keeps a re-commit after an intervening keystroke (a real second word)", async () => {
    const { term, socket } = await mountPanel();
    socket.send.mockClear();

    await commitIme(term, "ls");
    await act(async () => term.input("\r", true));
    await commitIme(term, "ls");

    expect(socket.send.mock.calls.map((c) => c[0])).toEqual(["ls", "\r", "ls"]);
  });

  it("never suppresses ordinary per-keystroke typing", async () => {
    const { term, socket } = await mountPanel();
    socket.send.mockClear();

    await act(async () => {
      for (const ch of "ll") term.input(ch, true);
    });
    await act(async () => term.input("\r", true));

    expect(socket.send.mock.calls.map((c) => c[0])).toEqual(["l", "l", "\r"]);
  });

  it("sends a different composed word straight after an identical one", async () => {
    const { term, socket } = await mountPanel();
    socket.send.mockClear();

    await commitIme(term, "ls");
    await commitIme(term, "la");

    expect(socket.send.mock.calls.map((c) => c[0])).toEqual(["ls", "la"]);
  });
});

// Input that reaches the socket without going through the guarded accept must
// end a pending commit. The clock is pinned so each case is inside the 600 ms
// duplicate window whatever the test machine's speed.
describe("terminal IME guard wiring for other input paths", () => {
  let nowSpy: ReturnType<typeof vi.spyOn> | undefined;
  afterEach(() => {
    nowSpy?.mockRestore();
    nowSpy = undefined;
  });

  it("a paste of the word just composed is not dropped as an IME re-delivery", async () => {
    nowSpy = vi.spyOn(performance, "now").mockReturnValue(1000);
    const { term, socket } = await mountPanel();
    socket.send.mockClear();
    // ghostty-web's term.paste reaches the pty through onData.
    term.paste.mockImplementation((text: string) => term.input(text, true));

    await commitIme(term, "ls");
    await act(async () => {
      const paste = new Event("paste", { bubbles: true, cancelable: true });
      Object.defineProperty(paste, "clipboardData", { value: { getData: () => "ls" } });
      term.textarea?.dispatchEvent(paste);
    });

    expect(socket.send.mock.calls.map((c) => c[0])).toEqual(["ls", "ls"]);
  });

  it("Shift+Enter ends a pending commit, so the next identical word is sent", async () => {
    nowSpy = vi.spyOn(performance, "now").mockReturnValue(1000);
    const { term, socket } = await mountPanel();
    socket.send.mockClear();

    await commitIme(term, "ls");
    await act(async () => {
      term._customKeyHandler!(new KeyboardEvent("keydown", { key: "Enter", shiftKey: true, cancelable: true }));
    });
    await commitIme(term, "ls");

    expect(socket.send.mock.calls.map((c) => c[0])).toEqual(["ls", "\x1b[13;2u", "ls"]);
  });
});
