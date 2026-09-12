import { render, waitFor } from "@testing-library/react";
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
