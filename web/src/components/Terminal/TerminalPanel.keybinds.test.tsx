import { render, act, waitFor, cleanup } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TerminalProvider } from "../../stores/terminalStore";
import TerminalPanel, { writeClipboardText } from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";
import type { GhosttyKeybind } from "./terminalAppearance";

// Regression suite for the Ghostty keybindings the terminal applies. The panel
// installs ONE custom key handler at mount; these tests drive it the way
// ghostty-web's keydown path does (true = handled + preventDefault, false =
// carry on), so a binding that silently fails to reach the terminal, or one that
// breaks the native-paste contract, fails here.

const h = vi.hoisted(() => ({
  sockets: [] as Array<{ onopen: (() => void) | null; send: (data: string) => void }>,
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
  // Clear the panel's same-text copy dedupe window from a previous test.
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

const BINDS: GhosttyKeybind[] = [
  { chord: "cmd+shift+k", action: "copy_to_clipboard" },
  { chord: "cmd+plus", action: "increase_font_size", arg: "2" },
  { chord: "cmd+zero", action: "reset_font_size" },
  { chord: "cmd+shift+v", action: "paste_from_clipboard" },
  { chord: "cmd+shift+t", action: "new_tab" },
];

function Panel({ keybinds = BINDS }: { keybinds?: GhosttyKeybind[] }) {
  return (
    <TerminalProvider>
      <TerminalPanel
        active
        id="t1"
        projectPath="/proj"
        scrollbackLines={1000}
        fontFamily="mono"
        fontSize={12}
        keybinds={keybinds}
      />
    </TerminalProvider>
  );
}

async function mountPanel(keybinds?: GhosttyKeybind[]) {
  const utils = render(<Panel keybinds={keybinds} />);
  await act(async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
  });
  expect(FakeTerminal.instances.length).toBe(1);
  return { utils, term: FakeTerminal.instances[0] };
}

function fireKey(
  term: FakeTerminal,
  init: { key: string; metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean; shiftKey?: boolean },
) {
  expect(term._customKeyHandler).toBeTruthy();
  const { key, ...modifiers } = init;
  const e = new KeyboardEvent("keydown", { key, ...modifiers, bubbles: true, cancelable: true });
  const handled = term._customKeyHandler!(e);
  if (handled) e.preventDefault();
  return { handled, defaultPrevented: e.defaultPrevented };
}

describe("Ghostty keybindings in the terminal", () => {
  it("runs copy_to_clipboard for the bound chord and consumes the key", async () => {
    const { term } = await mountPanel();
    term.selectionText = "bound copy";
    await act(async () => {
      h.sockets[0]?.onopen?.();
    });

    const result = fireKey(term, { key: "K", metaKey: true, shiftKey: true });
    expect(result.handled).toBe(true);
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("bound copy"));
  });

  it("zooms the font by the binding's argument and resets to the configured size", async () => {
    const { term } = await mountPanel();
    expect(term.options.fontSize).toBe(12);

    const increase = fireKey(term, { key: "+", metaKey: true });
    expect(increase.handled).toBe(true);
    expect(term.options.fontSize).toBe(14);

    const reset = fireKey(term, { key: "0", metaKey: true });
    expect(reset.handled).toBe(true);
    expect(term.options.fontSize).toBe(12);
  });

  it("never preventDefaults a paste binding (the native paste event owns clipboard text)", async () => {
    const { term } = await mountPanel();
    const result = fireKey(term, { key: "V", metaKey: true, shiftKey: true });
    expect(result.handled).toBe(false);
    expect(result.defaultPrevented).toBe(false);
  });

  it("leaves tab actions to ocode's own shortcuts", async () => {
    const { term } = await mountPanel();
    // new_tab is reported as unsupported, so the panel's normal handling (not
    // the binding) decides — here it simply falls through to ghostty-web.
    const result = fireKey(term, { key: "T", metaKey: true, shiftKey: true });
    expect(result.handled).toBe(false);
  });

  it("does not fire a binding whose modifiers differ", async () => {
    const { term } = await mountPanel();
    term.selectionText = "should not copy";
    await act(async () => {
      h.sockets[0]?.onopen?.();
    });

    const result = fireKey(term, { key: "k", metaKey: true });
    expect(result.handled).toBe(false);
    expect(writeText).not.toHaveBeenCalled();
  });

  it("applies no bindings when the config has none", async () => {
    const { term } = await mountPanel([]);
    term.selectionText = "unbound";
    await act(async () => {
      h.sockets[0]?.onopen?.();
    });

    const result = fireKey(term, { key: "K", metaKey: true, shiftKey: true });
    expect(result.handled).toBe(false);
    expect(writeText).not.toHaveBeenCalled();
  });
});
