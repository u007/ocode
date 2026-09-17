import { act, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TerminalPanel from "./TerminalPanel";

// Regression coverage for the "remote SSH terminal times out/crashes → no
// disconnect, and mouse movement spews gibberish" report:
//   1. a fresh (resumed=false) attach must disable xterm's mouse-tracking modes
//      so mouse movement stops emitting escape sequences into the new shell;
//   2. term.onData must not drive the pty before the attach handshake;
//   3. reconnect backoff must actually grow instead of resetting every attempt.

const h = vi.hoisted(() => ({
  sockets: [] as MockSocket[],
  terminals: [] as MockTerminal[],
  dataHandlers: [] as ((data: string) => void)[],
}));

type MockTerminal = {
  write: ReturnType<typeof vi.fn>;
  reset: ReturnType<typeof vi.fn>;
};

class MockSocket {
  static OPEN = 1;
  readyState = 1;
  binaryType = "";
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: ((event: { wasClean: boolean; code: number; reason: string }) => void) | null = null;
  send = vi.fn();
  close = vi.fn();

  constructor(public url: string) {
    h.sockets.push(this);
  }
}

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    options: Record<string, unknown> = {};
    buffer = { active: { length: 24 } };
    loadAddon = vi.fn();
    open = vi.fn();
    write = vi.fn((_text: string, callback?: () => void) => callback?.());
    reset = vi.fn();
    focus = vi.fn();
    getSelection = vi.fn(() => "");
    onData = vi.fn((cb: (data: string) => void) => {
      h.dataHandlers.push(cb);
      return { dispose: vi.fn() };
    });
    onSelectionChange = vi.fn(() => ({ dispose: vi.fn() }));
    onBell = vi.fn(() => ({ dispose: vi.fn() }));
    onTitleChange = vi.fn(() => ({ dispose: vi.fn() }));
    parser = { registerOscHandler: vi.fn(() => ({ dispose: vi.fn() })) };
    attachCustomKeyEventHandler = vi.fn(() => true);
    dispose = vi.fn();

    constructor() {
      h.terminals.push(this);
    }
  },
}));

vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit = vi.fn(); } }));
vi.mock("@xterm/addon-search", () => ({
  SearchAddon: class {
    onDidChangeResults = vi.fn(() => ({ dispose: vi.fn() }));
    findNext = vi.fn();
    findPrevious = vi.fn();
    clearDecorations = vi.fn();
    dispose = vi.fn();
  },
}));
vi.mock("@xterm/addon-serialize", () => ({ SerializeAddon: class { serialize = vi.fn(() => ""); } }));
vi.mock("@xterm/addon-webgl", () => ({
  WebglAddon: class {
    onContextLoss = vi.fn();
    dispose = vi.fn();
  },
}));
vi.mock("@xterm/addon-web-links", () => ({ WebLinksAddon: class { dispose = vi.fn(); } }));
vi.mock("@xterm/xterm/css/xterm.css", () => ({}));
vi.mock("./terminalLinkProvider", () => ({ registerFileLinkProvider: vi.fn(() => ({ dispose: vi.fn() })) }));

vi.mock("@/api/client", () => ({
  apiPath: (path: string) => path,
  apiWsPath: (path: string) => `ws://localhost${path}`,
  authHeaders: () => ({}),
  authToken: () => "token",
  isRemoteSession: () => false,
}));

vi.mock("../../stores/terminalStore", () => ({
  useTerminalState: () => ({
    markAlerted: vi.fn(),
    setOscTitle: vi.fn(),
    openTerminal: vi.fn(),
    closeTerminal: vi.fn(),
  }),
}));

vi.mock("@/lib/debug/terminalRegistry", () => ({
  registerTerminal: vi.fn(),
  unregisterTerminal: vi.fn(),
}));

function renderPanel() {
  return render(
    <TerminalPanel id="t1" active projectPath="/project" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
  );
}

beforeEach(() => {
  h.sockets.length = 0;
  h.terminals.length = 0;
  h.dataHandlers.length = 0;
  window.localStorage.clear();
  vi.stubGlobal("WebSocket", MockSocket as unknown as typeof WebSocket);
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
  vi.stubGlobal("requestAnimationFrame", (() => 0) as unknown as typeof requestAnimationFrame);
  vi.stubGlobal("cancelAnimationFrame", (() => {}) as unknown as typeof cancelAnimationFrame);
  // No server history → connectSocket() runs immediately with no cursor.
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("missing", { status: 404 })));
});

function writeText(): string {
  return h.terminals[0].write.mock.calls.map(([text]) => String(text)).join("");
}

function attach(socket: MockSocket, resumed: boolean) {
  act(() => {
    socket.onmessage?.({ data: JSON.stringify({ type: "attach", resumed }) });
  });
}

// A restored history page whose replayed bytes contain a TUI's mouse-enable
// DECSET — exactly what the remote server's on-disk log holds after the app
// quit while a mouse-owning TUI was running.
function historyPage(id: string, text: string) {
  return {
    id,
    offset: 0,
    next_offset: text.length,
    snapshot_end: text.length,
    eof: true,
    data: btoa(text),
    state: "active",
  };
}

describe("TerminalPanel remote-disconnect mouse-mode recovery", () => {
  it("disables mouse tracking when a fresh shell attaches", async () => {
    const { unmount } = renderPanel();
    await waitFor(() => expect(h.sockets).toHaveLength(1));

    attach(h.sockets[0], false);

    const written = writeText();
    expect(written).toContain("\x1b[?1000l");
    expect(written).toContain("\x1b[?1002l");
    expect(written).toContain("\x1b[?1003l");
    expect(written).toContain("\x1b[?1006l");
    // Must not blow away scrollback the way term.reset() would.
    expect(h.terminals[0].reset).not.toHaveBeenCalled();
    unmount();
  });

  it("leaves mouse modes alone when a live shell reattaches", async () => {
    const { unmount } = renderPanel();
    await waitFor(() => expect(h.sockets).toHaveLength(1));

    attach(h.sockets[0], true);

    expect(writeText()).not.toContain("\x1b[?1000l");
    unmount();
  });

  it("clears replayed mouse modes when a restored terminal reattaches (desktop restart)", async () => {
    // The remote workspace keeps the shell alive on the remote server, so after
    // quitting and reopening the desktop app the terminal reattaches
    // resumed:true — but the REST scrollback replayed a dead TUI's DECSET.
    const replayed = "\x1b[?1003h\x1b[?1006h" + "user@remote:~$ ";
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify(historyPage("t1", replayed)), {
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    const { unmount } = renderPanel();
    await waitFor(() => expect(h.sockets).toHaveLength(1));

    expect(h.sockets[0].url).toContain("history_offset=");
    attach(h.sockets[0], true);

    const written = writeText();
    expect(written).toContain("\x1b[?1003l");
    expect(written).toContain("\x1b[?1006l");
    // The reset must land after the replayed history it is undoing.
    expect(written.lastIndexOf("\x1b[?1006l")).toBeGreaterThan(written.indexOf("\x1b[?1006h"));
    // Restored scrollback is preserved (no full term.reset).
    expect(h.terminals[0].reset).not.toHaveBeenCalled();
    unmount();
  });

  it("does not forward onData to the pty before the attach handshake", async () => {
    const { unmount } = renderPanel();
    await waitFor(() => expect(h.sockets).toHaveLength(1));
    const socket = h.sockets[0];

    h.dataHandlers[0]("\x1b[<35;10;10M");
    expect(socket.send).not.toHaveBeenCalled();

    attach(socket, false);
    h.dataHandlers[0]("\x1b[<35;10;10M");
    expect(socket.send).toHaveBeenCalledWith("\x1b[<35;10;10M");
    unmount();
  });

  it("grows the reconnect backoff across failed attempts", async () => {
    const { unmount } = renderPanel();
    await waitFor(() => expect(h.sockets).toHaveLength(1));

    const delays: number[] = [];
    const realSetTimeout = window.setTimeout;
    const spy = vi.spyOn(window, "setTimeout").mockImplementation(((
      fn: () => void,
      ms?: number,
    ) => {
      delays.push(Number(ms ?? 0));
      fn();
      return 0 as unknown as number;
    }) as typeof window.setTimeout);

    try {
      // First unexpected close: attempt 0 → 1s.
      act(() => {
        h.sockets[0].onclose?.({ wasClean: false, code: 1006, reason: "" });
      });
      // The synchronous reconnect must NOT have reset the attempt counter, so
      // closing again without a successful open backs off to 2s.
      act(() => {
        h.sockets[1].onclose?.({ wasClean: false, code: 1006, reason: "" });
      });
    } finally {
      spy.mockRestore();
      window.setTimeout = realSetTimeout;
    }

    expect(delays).toContain(1000);
    expect(delays).toContain(2000);
    unmount();
  });
});
