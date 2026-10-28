import { render, act, screen } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TerminalProvider } from "../../stores/terminalStore";
import TerminalPanel from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";

// ghostty-web needs canvas/layout jsdom lacks; stub the Terminal and FitAddon
// (ghosttyWebMock.ts). The socket is a controllable fake so the test can drive an
// unexpected close (which arms the reconnect backoff) and then a wake.
const h = vi.hoisted(() => ({
  sockets: [] as MockSocket[],
}));

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

vi.mock("ghostty-web", async () => (await import("./ghosttyWebMock")).ghosttyWebModule());
vi.mock("./ghosttyInternals", async () => (await import("./ghosttyWebMock")).ghosttyInternalsMock());
vi.mock("./terminalMouse", async () => (await import("./ghosttyWebMock")).terminalMouseMock());
vi.mock("@/api/client", () => ({
  // The open-terminal list is server state; the store hydrates from it on mount.
  api: {
    getTerminalTabs: () => Promise.resolve({ projects: {} }),
    setTerminalTabs: () => Promise.resolve({ status: "ok" }),
  },
  authedFetch: () => Promise.resolve({ ok: true, status: 204 }),
  apiPath: (p: string) => p,
  apiWsPath: (p: string) => `ws://localhost${p}`,
  remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
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
  // The store's server-restore sweep calls this; a missing fixture would make
  // the migration throw inside a catch and silently skip, so declare it.
  readAllMirrorProjects: () => ({}),
  projectTerminalsKey: (path: string, host?: string) => (host ? `${host}::${path}` : path),
  loadTerminalBuffer: () => null,
  saveTerminalBuffer: vi.fn(),
}));
vi.mock("@/lib/debug/terminalRegistry", () => ({
  registerTerminal: vi.fn(),
  unregisterTerminal: vi.fn(),
}));

class MockSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 3;
  readyState = MockSocket.OPEN;
  binaryType = "";
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
  window.localStorage.clear();
  vi.stubGlobal("WebSocket", MockSocket as unknown as typeof WebSocket);
  vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response("", { status: 404 }))));
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
  vi.stubGlobal("requestAnimationFrame", (() => 0) as unknown as typeof requestAnimationFrame);
  vi.stubGlobal("cancelAnimationFrame", (() => {}) as unknown as typeof cancelAnimationFrame);
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

async function mountAndOpen(): Promise<MockSocket> {
  render(
    <TerminalProvider>
      <TerminalPanel active id="t1" projectPath="/proj" scrollbackLines={1000} fontFamily="mono" fontSize={12} />
    </TerminalProvider>,
  );
  await act(async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
  });
  expect(h.sockets.length).toBe(1);
  const first = h.sockets[0];
  act(() => {
    first.onopen?.();
  });
  return first;
}

function dropUnexpectedly(sock: MockSocket): void {
  sock.readyState = MockSocket.CLOSED;
  act(() => {
    sock.onclose?.({ wasClean: false, code: 1006, reason: "" });
  });
}

describe("TerminalPanel wake reconnect", () => {
  it("opens a new socket immediately on wake while a reconnect timer is pending", async () => {
    const first = await mountAndOpen();
    dropUnexpectedly(first);
    expect(h.sockets.length).toBe(1); // reconnect armed on a backoff timer

    act(() => {
      window.dispatchEvent(new Event("online"));
    });
    expect(h.sockets.length).toBe(2); // wake skipped the wait
  });

  it("does not open a second socket on wake while the socket is open", async () => {
    await mountAndOpen();
    act(() => {
      window.dispatchEvent(new Event("online"));
    });
    expect(h.sockets.length).toBe(1);
  });

  it("resets the backoff attempt counter so the next drop retries at the floor", async () => {
    const first = await mountAndOpen();
    dropUnexpectedly(first); // attempt 0 → counter now 1, timer at 1s
    act(() => {
      window.dispatchEvent(new Event("online"));
    });
    expect(h.sockets.length).toBe(2);

    const second = h.sockets[1];
    act(() => {
      second.onopen?.();
    });
    dropUnexpectedly(second);
    // Counter was reset by the wake: the banner must advertise the 1s floor,
    // not the 2s the un-reset counter would have produced.
    const writes = FakeTerminal.instances[0].write.mock.calls.map((c) => String(c[0])).join("\n");
    expect(writes).toContain("reconnecting in 1s");
  });

  it("does not reconnect after the panel unmounts (closing a tab must not respawn its shell)", async () => {
    const view = render(
      <TerminalProvider>
        <TerminalPanel active id="t1" projectPath="/proj" scrollbackLines={1000} fontFamily="mono" fontSize={12} />
      </TerminalProvider>,
    );
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(h.sockets.length).toBe(1);
    const first = h.sockets[0];
    act(() => {
      first.onopen?.();
    });

    view.unmount();
    // Closing the tab kills the shell server-side, so the server's close can
    // arrive after the cleanup ran. A reconnect here would outlive the panel
    // and spawn a fresh shell under the closed tab's id.
    act(() => {
      first.onclose?.({ wasClean: false, code: 1006, reason: "" });
    });
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(h.sockets.length).toBe(1);
  });
});

/**
 * The terminal websocket has ONE attachment slot: when a second client attaches
 * the same terminal id the server closes the first socket with a
 * `detached: superseded` frame. The close is clean (1000) and indistinguishable
 * from a shell exit, so without handling the frame both auto-reconnect paths
 * (the backoff timer and onWake) would evict the new client, and the two
 * clients would ping-pong — the same failure shape as the 2026-09-19
 * two-sockets-per-terminal reconnect loop.
 */
describe("TerminalPanel takeover", () => {
  async function superseded(): Promise<MockSocket> {
    const first = await mountAndOpen();
    act(() => {
      first.onmessage?.({ data: JSON.stringify({ type: "detached", reason: "superseded" }) });
      first.readyState = MockSocket.CLOSED;
      first.onclose?.({ wasClean: true, code: 1000, reason: "" });
    });
    return first;
  }

  it("does not reconnect on the backoff timer after being superseded", async () => {
    await superseded();
    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(h.sockets.length).toBe(1);
  });

  it("does not steal the terminal back on wake", async () => {
    await superseded();
    act(() => {
      window.dispatchEvent(new Event("online"));
    });
    expect(h.sockets.length).toBe(1);
  });

  it("explains the takeover instead of claiming the session ended", async () => {
    await superseded();
    const term = FakeTerminal.instances[0];
    const written = term.write.mock.calls.map((c) => String(c[0])).join("");
    expect(written).toContain("another client took over");
    expect(written).not.toContain("[terminal session ended]");
    expect(screen.getByText(/Taken over by another client/)).toBeTruthy();
  });

  it("reconnects only when the user explicitly takes the terminal back", async () => {
    await superseded();
    act(() => {
      screen.getByText("Take over").click();
    });
    expect(h.sockets.length).toBe(2);
  });
});
