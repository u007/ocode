import { act, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TerminalPanel from "./TerminalPanel";

const h = vi.hoisted(() => ({
  events: [] as string[],
  sockets: [] as MockSocket[],
  terminals: [] as MockTerminal[],
}));

type MockTerminal = {
  reset: ReturnType<typeof vi.fn>;
  write: ReturnType<typeof vi.fn>;
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
    h.events.push("socket");
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
    reset = vi.fn(() => h.events.push("reset"));
    focus = vi.fn();
    getSelection = vi.fn(() => "");
    onData = vi.fn(() => ({ dispose: vi.fn() }));
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

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit = vi.fn();
  },
}));

vi.mock("@xterm/addon-search", () => ({
  SearchAddon: class {
    onDidChangeResults = vi.fn(() => ({ dispose: vi.fn() }));
    findNext = vi.fn();
    findPrevious = vi.fn();
    clearDecorations = vi.fn();
    dispose = vi.fn();
  },
}));

vi.mock("@xterm/addon-serialize", () => ({
  SerializeAddon: class {
    serialize = vi.fn(() => "");
  },
}));

vi.mock("@xterm/addon-webgl", () => ({
  WebglAddon: class {
    onContextLoss = vi.fn();
    dispose = vi.fn();
  },
}));

vi.mock("@xterm/addon-web-links", () => ({
  WebLinksAddon: class {
    dispose = vi.fn();
  },
}));

vi.mock("@xterm/xterm/css/xterm.css", () => ({}));

vi.mock("./terminalLinkProvider", () => ({
  registerFileLinkProvider: vi.fn(() => ({ dispose: vi.fn() })),
}));

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

function base64(text: string): string {
  return btoa(text);
}

function page(id: string, offset: number, text: string, snapshotEnd: number, eof: boolean) {
  return {
    id,
    offset,
    next_offset: offset + text.length,
    snapshot_end: snapshotEnd,
    eof,
    data: base64(text),
    state: "active",
  };
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json" },
  });
}

beforeEach(() => {
  h.events.length = 0;
  h.sockets.length = 0;
  h.terminals.length = 0;
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
});

describe("TerminalPanel history restore handoff", () => {
  it("attaches with the restore snapshot cursor without resetting the restored terminal", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn()
        .mockResolvedValueOnce(jsonResponse(page("t1", 0, "hello", 11, false)))
        .mockResolvedValueOnce(jsonResponse(page("t1", 5, " world", 11, true))),
    );

    const { unmount } = render(
      <TerminalPanel id="t1" active projectPath="/project" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
    );

    await waitFor(() => expect(h.sockets).toHaveLength(1));

    expect(h.sockets[0].url).toContain("history_offset=11");
    expect(h.terminals[0].reset).not.toHaveBeenCalled();
    expect(h.events).toEqual(["socket"]);
    unmount();
  });

  it("resets partial restored output before attaching without a history cursor", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn()
        .mockResolvedValueOnce(jsonResponse(page("t1", 0, "partial", 12, false)))
        .mockResolvedValueOnce(new Response("gone", { status: 404 })),
    );

    const { unmount } = render(
      <TerminalPanel id="t1" active projectPath="/project" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
    );

    await waitFor(() => expect(h.sockets).toHaveLength(1));

    expect(h.terminals[0].reset).toHaveBeenCalledTimes(1);
    expect(h.sockets[0].url).not.toContain("history_offset=");
    expect(h.events.indexOf("reset")).toBeLessThan(h.events.indexOf("socket"));
    unmount();
  });

  it("uses the localStorage buffer and writes the explicit fallback marker for first-page 404", async () => {
    window.localStorage.setItem(
      "ocode.term.buf.t1",
      JSON.stringify({ text: "cached terminal output", cols: 80, rows: 24 }),
    );
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("missing", { status: 404 })));

    const { unmount } = render(
      <TerminalPanel id="t1" active projectPath="/project" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
    );

    await waitFor(() => expect(h.sockets).toHaveLength(1));

    const writes = h.terminals[0].write.mock.calls.map(([text]) => String(text)).join("");
    expect(writes).toContain("cached terminal output");
    expect(writes).toContain("── local terminal cache fallback; server history unavailable ──");
    expect(h.sockets[0].url).not.toContain("history_offset=");
    unmount();
  });

  it("omits the fallback marker on first-page 404 when there is no localStorage buffer", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("missing", { status: 404 })));

    const { unmount } = render(
      <TerminalPanel id="t1" active projectPath="/project" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
    );

    await waitFor(() => expect(h.sockets).toHaveLength(1));

    const writes = h.terminals[0].write.mock.calls.map(([text]) => String(text)).join("");
    expect(writes).not.toContain("local terminal cache fallback");
    expect(h.sockets[0].url).not.toContain("history_offset=");
    unmount();
  });

  it("persists the rendered buffer when the shell exits (desktop-quit path)", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn()
        .mockResolvedValueOnce(jsonResponse(page("t1", 0, "hello", 11, false)))
        .mockResolvedValueOnce(jsonResponse(page("t1", 5, " world", 11, true))),
    );

    const { unmount } = render(
      <TerminalPanel id="t1" active projectPath="/project" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
    );

    await waitFor(() => expect(h.sockets).toHaveLength(1));
    // A large final frame arrives right before the exit close and lands in
    // the chunked render queue; the close must flush it before saving, or
    // the persisted buffer would miss the last output.
    const finalOutput = `FINAL-${"x".repeat(70 * 1024)}`;
    h.sockets[0].onmessage?.({ data: new TextEncoder().encode(finalOutput).buffer });
    h.sockets[0].onclose?.({ wasClean: true, code: 1000, reason: "" });

    // xterm.write is async (mock invokes the callback synchronously, real
    // xterm defers it); wait for the save the close handler schedules.
    await waitFor(() => expect(window.localStorage.getItem("ocode.term.buf.t1")).not.toBeNull());

    const writes = h.terminals[0].write.mock.calls.map(([text]) => String(text)).join("");
    expect(writes).toContain("FINAL-");
    expect(writes).toContain("[terminal session ended]");
    const saved = window.localStorage.getItem("ocode.term.buf.t1");
    expect(saved).not.toBeNull();
    unmount();
  });
});

// The live socket is only opened after the REST history restore settles, so a
// restore fetch that never settles would leave the terminal permanently blank.
// The restore timeout must attach the socket anyway (without a cursor) and say
// so, instead of hanging with no output.
describe("TerminalPanel history restore timeout", () => {
  it("attaches the live socket when the restore fetch never settles", async () => {
    vi.useFakeTimers();
    try {
      vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>(() => {})));

      const { unmount } = render(
        <TerminalPanel id="t1" active projectPath="/project" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
      );

      // Restore is still pending: the socket must not be open yet.
      expect(h.sockets).toHaveLength(0);

      await act(async () => {
        vi.advanceTimersByTime(15000);
        await Promise.resolve();
      });

      expect(h.sockets).toHaveLength(1);
      const writes = h.terminals[0].write.mock.calls.map(([text]) => String(text)).join("");
      expect(writes).toContain("restore timed out");
      unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not open a second live socket when a stalled restore resolves after the timeout", async () => {
    vi.useFakeTimers();
    try {
      const deferred: { resolve?: (response: Response) => void } = {};
      vi.stubGlobal(
        "fetch",
        vi.fn(
          () =>
            new Promise<Response>((resolve) => {
              deferred.resolve = resolve;
            }),
        ),
      );

      const { unmount } = render(
        <TerminalPanel id="t1" active projectPath="/project" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
      );

      await act(async () => {
        vi.advanceTimersByTime(15000);
        await Promise.resolve();
      });
      expect(h.sockets).toHaveLength(1);
      expect(h.sockets[0].readyState).toBe(MockSocket.OPEN);

      // The stalled REST fetch resolves after the timeout already aborted the
      // controller and attached the live socket. The success path must NOT
      // attach a second socket: two live sockets for one terminal id make the
      // server's supersede close them in turn, and each close reconnects —
      // the endless ~1s "connection lost" loop on remote terminals.
      deferred.resolve?.(new Response("missing", { status: 404 }));
      await act(async () => {
        await Promise.resolve();
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(h.sockets).toHaveLength(1);
      expect(h.sockets[0].readyState).toBe(MockSocket.OPEN);
      unmount();
    } finally {
      vi.useRealTimers();
    }
  });
});

// Two live sockets for one terminal id on the host server supersede each other
// in turn: each new attach closes the previous socket, whose unexpected close
// arms another reconnect. The loop runs forever at the 1s backoff floor and
// makes a remote terminal print "[terminal connection lost — reconnecting in
// 1s…]" once per second. A socket that is no longer the panel's current one
// must never arm a reconnect or paint a lost banner.
describe("TerminalPanel reconnect supersede", () => {
  it("ignores onclose from a socket that was superseded by a later reconnect", async () => {
    vi.useFakeTimers();
    try {
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("missing", { status: 404 })));

      const { unmount } = render(
        <TerminalPanel id="t1" active projectPath="/project" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
      );
      await act(async () => {
        await Promise.resolve();
        await Promise.resolve();
        await Promise.resolve();
      });
      expect(h.sockets).toHaveLength(1);
      const first = h.sockets[0];

      // Unexpected drop: the panel arms a 1s reconnect, then opens a new socket.
      first.readyState = 3; // CLOSED
      await act(async () => {
        first.onclose?.({ wasClean: false, code: 1006, reason: "" });
      });
      await act(async () => {
        vi.advanceTimersByTime(1000);
        await Promise.resolve();
      });
      expect(h.sockets).toHaveLength(2);
      const second = h.sockets[1];
      expect(second.readyState).toBe(MockSocket.OPEN);

      // A late onclose from the superseded socket must be inert. Without the
      // socketRef guard it arms another reconnect and a third socket appears.
      await act(async () => {
        first.onclose?.({ wasClean: false, code: 1006, reason: "" });
        vi.advanceTimersByTime(5000);
        await Promise.resolve();
      });
      expect(h.sockets).toHaveLength(2);
      unmount();
    } finally {
      vi.useRealTimers();
    }
  });
});
