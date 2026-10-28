import { render, act } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TerminalProvider } from "../../stores/terminalStore";
import TerminalPanel from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";

// A remote host inside its retry window is not dialled. The panel used to sleep
// the WHOLE window (minutes) in one timer; it now re-checks the in-memory gate
// every REMOTE_HOST_RECHECK_MS (10 s) so a host that recovers early reconnects
// promptly. These tests pin the re-check, the one-banner rule and the teardown.
const h = vi.hoisted(() => ({
  sockets: [] as MockSocket[],
  /** What the gate reports as the remaining retry window. */
  retryMs: 0,
  /** Calls to remoteHostRetryInMs — proof the panel re-checks rather than sleeping. */
  calls: 0,
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

// Only the retry window is overridden; every other export stays real so other
// modules importing this file behave normally.
vi.mock("@/lib/remoteHostGate", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../lib/remoteHostGate")>();
  return {
    ...actual,
    remoteHostRetryInMs: (_host: string) => {
      h.calls += 1;
      return h.retryMs;
    },
  };
});

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
  isRemoteSession: () => true,
}));
vi.mock("./terminalAlertSound", () => ({ playAlertSound: vi.fn() }));
vi.mock("./terminalFocus", () => ({
  registerTerminalFocus: vi.fn(),
  unregisterTerminalFocus: vi.fn(),
}));
vi.mock("./terminalPersistence", () => ({
  loadProjectTerminals: () => ({ terminals: [{ id: "t1", title: "Terminal 1" }], activeId: "t1" }),
  saveProjectTerminals: vi.fn(),
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
  h.retryMs = 0;
  h.calls = 0;
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

/** Mounts a remote-project panel with the gate reporting `h.retryMs` remaining. */
async function mountArmed() {
  const utils = render(
    <TerminalProvider>
      <TerminalPanel
        active
        id="t1"
        projectPath="/proj"
        host="dev@box"
        scrollbackLines={1000}
        fontFamily="mono"
        fontSize={12}
      />
    </TerminalProvider>,
  );
  await act(async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
  });
  return utils;
}

function banners(): string[] {
  const term = FakeTerminal.instances[FakeTerminal.instances.length - 1];
  if (!term) return [];
  return term.write.mock.calls
    .map((c) => String(c[0]))
    .filter((s) => s.includes("remote host unreachable"));
}

describe("TerminalPanel remote-host retry window", () => {
  it("re-checks an armed host every 10 s instead of sleeping the whole window", async () => {
    h.retryMs = 120_000;
    await mountArmed();

    // No dial, one banner naming the real window, one gate read for the mount.
    expect(h.sockets.length).toBe(0);
    expect(banners()).toHaveLength(1);
    expect(banners()[0]).toContain("120s");
    const callsAtMount = h.calls;
    expect(callsAtMount).toBeGreaterThan(0);

    // 10 s later the panel re-arms WITHOUT connecting and WITHOUT a second
    // banner — that second gate read is the whole point (a single sleep would
    // show no extra call here).
    await act(async () => {
      vi.advanceTimersByTime(10_000);
    });
    expect(h.sockets.length).toBe(0);
    expect(banners()).toHaveLength(1);
    expect(h.calls).toBe(callsAtMount + 1);

    // ...and it keeps re-checking rather than waiting out the remaining 110 s.
    await act(async () => {
      vi.advanceTimersByTime(10_000);
    });
    expect(h.sockets.length).toBe(0);
    expect(h.calls).toBe(callsAtMount + 2);
  });

  it("connects as soon as the window clears, before the full window elapses", async () => {
    h.retryMs = 120_000;
    await mountArmed();
    expect(h.sockets.length).toBe(0);

    await act(async () => {
      vi.advanceTimersByTime(10_000);
    });
    expect(h.sockets.length).toBe(0); // still armed

    // The status probe saw the host recover; the next re-check connects.
    h.retryMs = 0;
    await act(async () => {
      vi.advanceTimersByTime(10_000);
      for (let i = 0; i < 20; i++) await Promise.resolve();
    });
    expect(h.sockets.length).toBe(1);
    expect(h.sockets[0].url).toContain("/api/remote/");
  });

  it("stops re-checking after unmount", async () => {
    h.retryMs = 120_000;
    const { unmount } = await mountArmed();
    const callsAtUnmount = h.calls;

    unmount();
    await act(async () => {
      vi.advanceTimersByTime(120_000);
    });

    expect(h.calls).toBe(callsAtUnmount);
    expect(h.sockets.length).toBe(0);
  });
});
