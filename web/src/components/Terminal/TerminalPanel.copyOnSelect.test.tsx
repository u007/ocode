import { render, fireEvent, act, waitFor } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TerminalProvider } from "../../stores/terminalStore";
import TerminalPanel, { writeClipboardText } from "./TerminalPanel";

// Regression suite for terminal copy-on-selection (TUI parity): the TUI copies
// a dragged selection to the system clipboard on mouse release
// (internal/tui/model.go clipboard.WriteAll); the desktop/web terminal only
// copied via right-click → Copy, so a selection never reached the system
// clipboard. This file locks in: (1) mouse-up copies the active selection,
// (2) a plain click without a selection never touches the clipboard, (3) the
// debounced onSelectionChange fallback copies after the selection settles, and
// (4) a cleared selection schedules a no-op that writes nothing.

// xterm needs canvas/layout jsdom lacks, so the Terminal is stubbed. The stub
// exposes the selection handlers the panel registers and a mutable selection
// string the tests control.
const h = vi.hoisted(() => ({
  terminals: [] as Array<{
    _selectionChange: (() => void) | null;
    _customKeyHandler: ((ev: KeyboardEvent) => boolean) | null;
    selectionText: string;
    paste: (text: string) => void;
  }>,
  sockets: [] as Array<{ onopen: (() => void) | null; send: (data: string) => void }>,
}));

vi.mock("@xterm/xterm", () => {
  class Terminal {
    cols = 80;
    rows = 24;
    options: Record<string, unknown> = {};
    selectionText = "";
    _selectionChange: (() => void) | null = null;
    _customKeyHandler: ((ev: KeyboardEvent) => boolean) | null = null;
    constructor() {
      h.terminals.push(this);
    }
    loadAddon = vi.fn();
    open = vi.fn();
    write = vi.fn();
    focus = vi.fn();
    onData = vi.fn(() => ({ dispose: vi.fn() }));
    onSelectionChange = vi.fn((cb: () => void) => {
      this._selectionChange = cb;
      return { dispose: vi.fn() };
    });
    getSelection = vi.fn(() => this.selectionText);
    onBell = vi.fn(() => ({ dispose: vi.fn() }));
    onTitleChange = vi.fn(() => ({ dispose: vi.fn() }));
    parser = {
      registerOscHandler: vi.fn(() => ({ dispose: vi.fn() })),
    };
    dispose = vi.fn();
    paste = vi.fn();
    attachCustomKeyEventHandler = vi.fn((cb: (ev: KeyboardEvent) => boolean) => {
      this._customKeyHandler = cb;
    });
  }
  return { Terminal };
});
vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit = vi.fn(); } }));
vi.mock("@xterm/addon-serialize", () => ({ SerializeAddon: class { serialize = vi.fn(() => ""); } }));
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
  constructor(public url: string) {
    h.sockets.push(this);
  }
}

// navigator.clipboard.writeText is undefined in jsdom; install a stub the
// tests can assert against.
const writeText = vi.fn<(text: string) => Promise<void>>(() => Promise.resolve());

beforeEach(() => {
  h.terminals.length = 0;
  h.sockets.length = 0;
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
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    configurable: true,
    writable: true,
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function Panel() {
  return (
    <TerminalProvider>
      <TerminalPanel active id="t1" projectPath="/proj" scrollbackLines={1000} fontFamily="mono" fontSize={12} />
    </TerminalProvider>
  );
}

describe("terminal clipboard shortcuts (Cmd/Ctrl+C copy, Cmd/Ctrl+V paste)", () => {
  // The stub Terminal must capture the custom key handler the panel installs.
  beforeEach(() => {
    for (const t of h.terminals) t._customKeyHandler = null;
  });

  function fireKey(init: { key: string; metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean; shiftKey?: boolean }) {
    const term = h.terminals[0];
    expect(term._customKeyHandler).toBeTruthy();
    const { key, ...modifiers } = init;
    const e = new KeyboardEvent("keydown", { key, ...modifiers, bubbles: true, cancelable: true });
    // The stub records the handler; invoke it the way xterm's _keyDown does.
    const allowed = term._customKeyHandler!(e);
    return { allowed: allowed, defaultPrevented: e.defaultPrevented };
  }

  it("copies with Cmd/Ctrl+C when a selection exists and blocks xterm (no \\x03)", async () => {
    render(<Panel />);
    const term = h.terminals[0];
    term.selectionText = "selected command output";

    await act(async () => {
      h.sockets[0]?.onopen?.();
    });

    // mac: metaKey+C
    const mac = fireKey({ key: "c", metaKey: true });
    expect(mac.allowed).toBe(false); // xterm must not process the keydown
    expect(mac.defaultPrevented).toBe(true); // nor run native copy a second time
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("selected command output"));

    // linux/windows: ctrlKey+C with selection → copy too
    writeText.mockClear();
    const win = fireKey({ key: "c", ctrlKey: true });
    expect(win.allowed).toBe(false);
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("selected command output"));
  });

  it("lets Ctrl+C fall through when there is no selection (SIGINT)", async () => {
    render(<Panel />);
    const term = h.terminals[0];
    term.selectionText = "";

    await act(async () => {
      h.sockets[0]?.onopen?.();
    });

    const r = fireKey({ key: "c", ctrlKey: true });
    expect(r.allowed).toBe(true); // xterm converts to \x03 as usual
    expect(r.defaultPrevented).toBe(false);
    expect(writeText).not.toHaveBeenCalled();
  });

  it("pastes via the async Clipboard API on Cmd/Ctrl+V and blocks xterm (no \\x16)", async () => {
    const readText = vi.fn(() => Promise.resolve("pasted text"));
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText, readText },
      configurable: true,
      writable: true,
    });
    render(<Panel />);
    const term = h.terminals[0];
    const paste = vi.fn();
    term.paste = paste;

    await act(async () => {
      h.sockets[0]?.onopen?.();
    });

    const mac = fireKey({ key: "v", metaKey: true });
    expect(mac.allowed).toBe(false);
    const win = fireKey({ key: "v", ctrlKey: true });
    expect(win.allowed).toBe(false);

    await waitFor(() => {
      expect(readText).toHaveBeenCalledTimes(2);
      expect(paste).toHaveBeenCalledWith("pasted text");
    });
    // Never a raw socket write: paste goes through term.paste (bracketed-paste aware).
    expect(h.sockets[0]?.send).not.toHaveBeenCalled();
  });

  it("does not send \\x03 or \\x16 through onData for clipboard shortcuts", async () => {
    const readText = vi.fn(() => Promise.resolve("x"));
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText, readText },
      configurable: true,
      writable: true,
    });
    render(<Panel />);
    const term = h.terminals[0];
    term.selectionText = "sel";
    const paste = vi.fn();
    term.paste = paste;

    await act(async () => {
      h.sockets[0]?.onopen?.();
    });

    fireKey({ key: "c", metaKey: true });
    fireKey({ key: "v", metaKey: true });
    await act(async () => {
      await Promise.resolve();
    });

    const sent = (h.sockets[0]!.send as ReturnType<typeof vi.fn>).mock.calls.map((c: unknown[]) => String(c[0]));
    expect(sent.join("")).not.toContain("\x03");
    expect(sent.join("")).not.toContain("\x16");
  });

  it("container copy event writes the xterm selection to clipboardData (desktop Edit-menu fallback)", async () => {
    const { container } = render(<Panel />);
    const term = h.terminals[0];
    term.selectionText = "menu copy text";

    await act(async () => {
      h.sockets[0]?.onopen?.();
    });

    const host = container.firstElementChild as HTMLElement;
    // jsdom has no DataTransfer; a minimal {getData, setData} stub is all
    // the handler touches.
    const store = new Map<string, string>();
    const dt = {
      setData: (type: string, value: string) => void store.set(type, value),
      getData: (type: string) => store.get(type) ?? "",
    };
    // jsdom lacks ClipboardEvent too — synthesize from Event.
    const ev = new Event("copy", { bubbles: true, cancelable: true }) as ClipboardEvent;
    Object.defineProperty(ev, "clipboardData", { value: dt });
    fireEvent(host, ev);

    expect(ev.defaultPrevented).toBe(true);
    expect(dt.getData("text/plain")).toBe("menu copy text");
  });

  it("leaves copying from unrelated text fields alone", () => {
    render(<Panel />);
    h.terminals[0].selectionText = "stale terminal selection";
    const input = document.createElement("input");
    document.body.appendChild(input);
    try {
      input.focus();
      const setData = vi.fn();
      const event = new Event("copy", { bubbles: true, cancelable: true });
      Object.defineProperty(event, "clipboardData", { value: { setData } });
      fireEvent(input, event);
      expect(event.defaultPrevented).toBe(false);
      expect(setData).not.toHaveBeenCalled();
      expect(writeText).not.toHaveBeenCalled();
    } finally {
      input.remove();
    }
  });
});

describe("terminal copy-on-selection", () => {
  it("copies the selection to the system clipboard on mouse-up (TUI parity)", async () => {
    const { container } = render(<Panel />);
    const term = h.terminals[0];
    term.selectionText = "hello from the pty";

    await act(async () => {
      h.sockets[0]?.onopen?.();
    });

    // The panel root div is xterm's fit parent and carries the onMouseUp
    // handler. TerminalProvider renders no DOM of its own, so it is the
    // container's first child.
    const host = container.firstElementChild as HTMLElement | null;
    expect(host).toBeTruthy();

    fireEvent.mouseDown(host!, { clientX: 10, clientY: 10 });
    fireEvent.mouseMove(host!, { clientX: 60, clientY: 12 });
    fireEvent.mouseUp(host!, { clientX: 60, clientY: 12 });

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("hello from the pty");
    });
  });

  it("does not touch the clipboard on a plain click with no selection", async () => {
    const { container } = render(<Panel />);
    const term = h.terminals[0];
    term.selectionText = ""; // plain click: no selection

    await act(async () => {
      h.sockets[0]?.onopen?.();
    });

    const host = container.firstElementChild as HTMLElement;
    fireEvent.mouseDown(host, { clientX: 10, clientY: 10 });
    fireEvent.mouseUp(host, { clientX: 10, clientY: 10 });

    // Give any stray async write a chance to run before asserting.
    await act(async () => {
      await Promise.resolve();
    });
    expect(writeText).not.toHaveBeenCalled();
  });

  it("copies via the debounced onSelectionChange fallback once the selection settles", async () => {
    vi.useFakeTimers();
    render(<Panel />);
    const term = h.terminals[0];

    await act(async () => {
      h.sockets[0]?.onopen?.();
    });

    // Simulate a drag producing a selection, released outside the container so
    // only the xterm onSelectionChange path fires.
    act(() => {
      term.selectionText = "dragged text";
      term._selectionChange?.();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    // Still inside the debounce window: nothing copied yet.
    expect(writeText).not.toHaveBeenCalled();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(writeText).toHaveBeenCalledWith("dragged text");
  });

  it("does not copy stale text when the selection is cleared", async () => {
    vi.useFakeTimers();
    render(<Panel />);
    const term = h.terminals[0];

    await act(async () => {
      h.sockets[0]?.onopen?.();
    });

    act(() => {
      term.selectionText = "do not copy me";
      term._selectionChange?.();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(200);
    });
    expect(writeText).toHaveBeenCalledWith("do not copy me");

    // Clear the selection (plain click / typing): the event fires again with an
    // empty selection; the settled callback must no-op.
    writeText.mockClear();
    act(() => {
      term.selectionText = "";
      term._selectionChange?.();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(writeText).not.toHaveBeenCalled();
  });
});

describe("writeClipboardText fallback", () => {
  it("preserves the field focused while an async write was pending", async () => {
    const original = document.createElement("input");
    const next = document.createElement("input");
    document.body.append(original, next);
    original.focus();
    let reject!: (error: Error) => void;
    writeText.mockImplementationOnce(() => new Promise<void>((_resolve, fail) => { reject = fail; }));
    Object.defineProperty(document, "execCommand", { configurable: true, value: vi.fn(() => true) });
    try {
      const pending = writeClipboardText("output");
      next.focus();
      reject(new Error("denied"));
      await pending;
      expect(document.activeElement).toBe(next);
    } finally {
      original.remove();
      next.remove();
      delete (document as unknown as Record<string, unknown>).execCommand;
    }
  });

  it("restores terminal input focus after a denied clipboard write", async () => {
    const terminalInput = document.createElement("textarea");
    document.body.appendChild(terminalInput);
    terminalInput.focus();
    writeText.mockRejectedValueOnce(new Error("denied"));
    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: vi.fn(() => {
        // Model the focus change caused by selecting the fallback textarea.
        const fallback = document.body.lastElementChild as HTMLTextAreaElement;
        fallback.focus();
        return true;
      }),
    });
    try {
      await writeClipboardText("selected output");
      expect(document.activeElement).toBe(terminalInput);
    } finally {
      terminalInput.remove();
      delete (document as unknown as Record<string, unknown>).execCommand;
    }
  });

  it("uses navigator.clipboard.writeText when available", async () => {
    await writeClipboardText("abc");
    expect(writeText).toHaveBeenCalledWith("abc");
  });

  it("falls back to execCommand when the async clipboard write rejects", async () => {
    // jsdom does not implement execCommand; install a document-level stub.
    const execCopy = vi.fn<(command: string) => boolean>(() => true);
    Object.defineProperty(document, "execCommand", {
      value: execCopy,
      configurable: true,
      writable: true,
    });
    try {
      writeText.mockRejectedValueOnce(new Error("denied"));

      await writeClipboardText("fallback text");

      expect(execCopy).toHaveBeenCalledWith("copy");
    } finally {
      delete (document as unknown as Record<string, unknown>).execCommand;
      writeText.mockClear();
    }
  });
});
