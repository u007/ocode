import { render, waitFor, act } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { TerminalProvider } from "../../stores/terminalStore";
import TerminalTabs from "./TerminalTabs";

/**
 * A terminal has ONE attachment slot server-side, and a socket is not a passive
 * read: attaching it DISPLACES whoever holds it. The terminal tab LIST is now
 * shared across clients, so the question "does merely learning about a terminal
 * open a socket for it?" is a cross-client safety property, not a detail.
 *
 * If hydration attached, then a second browser that had merely loaded the page
 * would silently evict the desktop app from every running shell — with no click
 * anywhere. These tests pin the gate that prevents that.
 */

// xterm needs canvas/layout jsdom lacks; the panel is stubbed. What matters is
// whether a WebSocket is constructed at all.
vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    options: Record<string, unknown> = {};
    loadAddon = vi.fn();
    open = vi.fn();
    write = vi.fn();
    focus = vi.fn();
    reset = vi.fn();
    dispose = vi.fn();
    onData = vi.fn(() => ({ dispose: vi.fn() }));
    onSelectionChange = vi.fn(() => ({ dispose: vi.fn() }));
    onBell = vi.fn(() => ({ dispose: vi.fn() }));
    onTitleChange = vi.fn(() => ({ dispose: vi.fn() }));
    getSelection = vi.fn(() => "");
    parser = { registerOscHandler: vi.fn(() => ({ dispose: vi.fn() })) };
    attachCustomKeyEventHandler = vi.fn(() => true);
  },
}));
vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit = vi.fn(); } }));
vi.mock("@xterm/addon-serialize", () => ({ SerializeAddon: class { serialize = vi.fn(() => ""); } }));
vi.mock("@xterm/addon-webgl", () => ({ WebglAddon: class { dispose = vi.fn(); onContextLoss = vi.fn(); } }));
vi.mock("@xterm/xterm/css/xterm.css", () => ({}));

const sockets: string[] = [];
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
    sockets.push(url);
  }
}

vi.mock("@/api/client", () => ({
  api: {
    getTerminalConfig: () => Promise.resolve({ available: true, scrollback_lines: 1000, work_dir: "/project" }),
    getTerminalProcesses: () => Promise.resolve([]),
    getBrowseProcesses: () => Promise.resolve([]),
    // The server already holds another client's terminals for this project.
    getTerminalTabs: () =>
      Promise.resolve({
        projects: {
          "/project": {
            terminals: [
              { id: "desktop-1", title: "Desktop shell" },
              { id: "desktop-2", title: "Second shell" },
            ],
          },
        },
      }),
    setTerminalTabs: () => Promise.resolve({ status: "ok" }),
  },
  apiPath: (p: string) => p,
  apiWsPath: (p: string) => `ws://localhost${p}`,
  remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
  authToken: () => "tok",
  authHeaders: () => ({}),
  authedFetch: () => Promise.resolve({ ok: true, status: 204 }),
  isRemoteSession: () => false,
}));

vi.mock("@/hooks/useTerminalConfig", () => ({
  useTerminalConfig: () => ({
    available: true,
    loading: false,
    error: null,
    scrollbackLines: 1000,
    fontFamily: "mono",
    fontSize: 12,
  }),
}));
vi.mock("../../stores/projectStore", () => ({
  findTabForSession: () => undefined,
  useProjectState: () => ({ state: { tabsByProject: {}, activeTabByProject: {} } }),
}));
vi.mock("../../stores/browserTabsStore", () => ({ useBrowserTabs: () => ({ tabs: [] }) }));
// ProcessesPanel (rendered by TerminalTabs once active) reads the chat store.
vi.mock("../../stores/chatStore", () => ({
  useChatStateRef: () => ({ current: { sessions: {} } }),
}));
vi.mock("@/lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {}, handlers: new Map() },
}));
vi.mock("./terminalAlertSound", () => ({ playAlertSound: vi.fn() }));
vi.mock("./terminalFocus", () => ({
  registerTerminalFocus: vi.fn(),
  unregisterTerminalFocus: vi.fn(),
  focusTerminalById: vi.fn(),
}));
vi.mock("@/lib/debug/terminalRegistry", () => ({
  registerTerminal: vi.fn(),
  unregisterTerminal: vi.fn(),
}));

beforeEach(() => {
  sockets.length = 0;
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
});

describe("TerminalTabs cross-client attachment safety", () => {
  // THE property. The server handed us another client's terminals, but this
  // client has not opened the terminal region, so nothing may attach — a page
  // load that opened sockets here would evict the other client from its own
  // live shells without a single click.
  it("opens no socket for a hydrated project the user has not opened", async () => {
    render(
      <TerminalProvider>
        <TerminalTabs active={false} projectPath="/project" />
      </TerminalProvider>,
    );
    // Let the server read, the mirror rewrite, and several re-renders settle.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });
    expect(sockets).toHaveLength(0);
  });

  // Even after hydrating several other clients' terminals, mounting inactive
  // must stay socket-free. (Guards against a future "attach on hydrate" fix
  // that forgets the active gate.)
  it("stays socket-free across repeated renders while inactive", async () => {
    const { rerender } = render(
      <TerminalProvider>
        <TerminalTabs active={false} projectPath="/project" />
      </TerminalProvider>,
    );
    for (let i = 0; i < 3; i++) {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 30));
      });
      rerender(
        <TerminalProvider>
          <TerminalTabs active={false} projectPath="/project" />
        </TerminalProvider>,
      );
    }
    expect(sockets).toHaveLength(0);
  });

  // The positive control: opening the region DOES attach, so the tests above
  // are not passing merely because sockets are never constructed in jsdom.
  it("attaches once the project is actually opened (positive control)", async () => {
    render(
      <TerminalProvider>
        <TerminalTabs active projectPath="/project" />
      </TerminalProvider>,
    );
    await waitFor(() => expect(sockets.length).toBeGreaterThan(0));
  });
});
