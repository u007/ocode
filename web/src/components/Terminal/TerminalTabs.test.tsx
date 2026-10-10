import { render, waitFor, act } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createRef } from "react";
import { TerminalProvider } from "../../stores/terminalStore";
import type { TerminalTabsHandle } from "./TerminalTabs";
import TerminalTabs from "./TerminalTabs";

// Real xterm needs canvas/layout that jsdom does not have, so the terminal
// itself is stubbed: this test covers activation/open/close behaviour and
// that closing a terminal tears down that terminal's socket (which is what
// kills the pty server-side).
const disposeSpy = vi.fn();
vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    options: Record<string, unknown> = {};
    loadAddon = vi.fn();
    open = vi.fn();
    write = vi.fn();
    focus = vi.fn();
    onData = vi.fn(() => ({ dispose: vi.fn() }));
    onSelectionChange = vi.fn(() => ({ dispose: vi.fn() }));
    getSelection = vi.fn(() => "");
    onBell = vi.fn(() => ({ dispose: vi.fn() }));
    onTitleChange = vi.fn(() => ({ dispose: vi.fn() }));
    parser = {
      registerOscHandler: vi.fn(() => ({ dispose: vi.fn() })),
    };
    dispose = disposeSpy;
    attachCustomKeyEventHandler = vi.fn(() => true);
  },
}));
vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit = vi.fn();
  },
}));
vi.mock("@xterm/addon-serialize", () => ({
  SerializeAddon: class {
    serialize = vi.fn(() => "");
  },
}));
vi.mock("@xterm/xterm/css/xterm.css", () => ({}));
vi.mock("@xterm/addon-webgl", () => ({
  WebglAddon: class {
    dispose = vi.fn();
  },
}));
vi.mock("@/api/client", () => ({
  api: {
    getTerminalConfig: () => Promise.resolve({ available: true, scrollback_lines: 9999, work_dir: "/project" }),
    getTerminalProcesses: () => Promise.resolve([]),
    getBrowseProcesses: () => Promise.resolve([]),
    // The open-terminal LIST is server state now; these suites drive it through
    // localStorage, so the server answers "nothing persisted" and the mirror
    // they seeded is what the store reads.
    getTerminalTabs: () => Promise.resolve({ projects: {} }),
    setTerminalTabs: () => Promise.resolve({ status: "ok" }),
  },
  apiPath: (p: string) => p,
  apiWsPath: (p: string) => `ws://localhost${p}`,
  remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
  authToken: () => "tok",
  authHeaders: () => ({}),
  authedFetch: (...args: unknown[]) => authedFetchMock(...args),
  isRemoteSession: () => false,
}));

// ProcessesPanel (rendered inside TerminalTabs) reads the session/browser
// tab stores and subscribes to the event bus for its local estimate rows.
// Static no-op stubs keep this terminal-focused test self-contained; the
// real providers live in the app's composition root (see App.tsx).
vi.mock("../../stores/projectStore", () => ({
  // resolveSessionHost (useSessionHost.ts) imports this directly, so the
  // real implementation runs against the stub state in these tests.
  findTabForSession: () => undefined,
  useProjectState: () => ({ state: { tabsByProject: {}, activeTabByProject: {} } }),
}));
vi.mock("../../stores/browserTabsStore", () => ({
  useBrowserTabs: () => ({ tabs: [] }),
}));
vi.mock("../../stores/chatStore", () => ({
  useChatStateRef: () => ({ current: { sessions: {} } }),
}));
vi.mock("@/lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {}, handlers: new Map() },
}));

const authedFetchMock = vi.fn((..._args: unknown[]) => Promise.resolve({ ok: true, status: 204 }));

const sockets: MockSocket[] = [];
let terminalFetchMock: ReturnType<typeof vi.fn>;

class MockSocket {
  static OPEN = 1;
  readyState = 1;
  binaryType = "arraybuffer";
  onopen: (() => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: ((e: { wasClean: boolean; code: number; reason: string }) => void) | null = null;
  send = vi.fn();
  close = vi.fn();
  constructor(public url: string) {
    sockets.push(this);
  }
}

beforeEach(() => {
  sockets.length = 0;
  disposeSpy.mockClear();
  authedFetchMock.mockClear();
  window.localStorage.clear();
  vi.stubGlobal("WebSocket", MockSocket as unknown as typeof WebSocket);
  terminalFetchMock = vi.fn(() => Promise.resolve(new Response("", { status: 404 })));
  vi.stubGlobal("fetch", terminalFetchMock);
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
});

async function renderTabs(projectPath = "/project", host?: string) {
  const ref = createRef<TerminalTabsHandle>();
  const utils = render(
    <TerminalProvider>
      <TerminalTabs ref={ref} active projectPath={projectPath} host={host} />
    </TerminalProvider>,
  );
  // activate() no longer auto-seeds a shell (see the B change in
  // terminalStore.activate), so the fixture opens one explicitly — the same
  // path the ⌨️+ button and Cmd/Ctrl+T take. These tests assert socket
  // behaviour per terminal, so they need a terminal to exist; the seeding
  // policy itself is covered in terminalStore.test.tsx.
  act(() => ref.current?.openTerminal());
  await waitFor(() => expect(sockets.length).toBe(1));
  return { ref, ...utils };
}

describe("TerminalTabs", () => {
  it("connects each terminal socket with the tab's project_path", async () => {
    await renderTabs();
    expect(sockets[0].url).toContain(`project_path=${encodeURIComponent("/project")}`);
  });

  it("starts a remote terminal once through the remote proxy, with no host param", async () => {
    await renderTabs("/remote", "dev@example.com");

    expect(sockets).toHaveLength(1);
    const prefix = `/api/remote/${encodeURIComponent("dev@example.com")}/api/terminal/ws`;
    expect(sockets[0].url).toContain(prefix);
    expect(sockets[0].url).not.toContain("host=");
    expect(sockets[0].url).not.toContain("port=");
    const historyRequest = terminalFetchMock.mock.calls.find(([input]) => String(input).includes("/history"));
    expect(historyRequest?.[0]).toContain(`/api/remote/${encodeURIComponent("dev@example.com")}/api/terminal/`);
    expect(disposeSpy).not.toHaveBeenCalled();
  });

  it("does not restart the lifecycle when metadata refresh keeps the same host", async () => {
    const first = await renderTabs("/remote", "dev@example.com");
    const socket = sockets[0];

    first.rerender(
      <TerminalProvider>
        <TerminalTabs active projectPath="/remote" host="dev@example.com" />
      </TerminalProvider>,
    );

    await waitFor(() => expect(sockets).toHaveLength(1));
    expect(socket.close).not.toHaveBeenCalled();
    expect(disposeSpy).not.toHaveBeenCalled();
  });

  it("rebuilds the lifecycle when the host identity deliberately changes", async () => {
    const first = await renderTabs("/remote", "old@example.com");
    const oldSocket = sockets[0];

    first.rerender(
      <TerminalProvider>
        <TerminalTabs ref={first.ref} active projectPath="/remote" host="new@example.com" />
      </TerminalProvider>,
    );
    // A different host is a DIFFERENT store key, so activate() finds nothing
    // persisted for it and — since it no longer seeds — opens no terminal. Open
    // one explicitly so there is a pty lifecycle to tear down and rebuild.
    act(() => first.ref.current?.openTerminal());

    await waitFor(() => expect(sockets).toHaveLength(2));
    expect(oldSocket.close).toHaveBeenCalled();
    expect(disposeSpy).toHaveBeenCalled();
    expect(sockets[1].url).toContain(`/api/remote/${encodeURIComponent("new@example.com")}/api/terminal/ws`);
  });

  it("opens an additional terminal (and socket) via the imperative openTerminal() handle", async () => {
    const { ref } = await renderTabs();
    expect(sockets.length).toBe(1);

    ref.current?.openTerminal();

    await waitFor(() => expect(sockets.length).toBe(2));
  });

  it("closes the active terminal and its websocket via closeActiveTerminal()", async () => {
    const { ref } = await renderTabs();
    ref.current?.openTerminal();
    await waitFor(() => expect(sockets.length).toBe(2));
    const secondSocket = sockets[1]; // newest terminal becomes active

    // One terminal remains after closing the second of two.
    const remaining = ref.current?.closeActiveTerminal();

    expect(remaining).toBe(1);
    await waitFor(() => expect(secondSocket.close).toHaveBeenCalled());
    // Closing the socket only detaches the shell; an explicit tab close must
    // also kill it server-side.
    const secondId = new URL(secondSocket.url).searchParams.get("terminal_id");
    expect(authedFetchMock).toHaveBeenCalledWith(`/api/terminal/${secondId}`, { method: "DELETE" });
  });

  it("kills a remote terminal through the proxy with the project header", async () => {
    const { ref } = await renderTabs("/remote", "dev@example.com");
    ref.current?.openTerminal();
    await waitFor(() => expect(sockets.length).toBe(2));
    const secondSocket = sockets[1];
    const secondId = new URL(secondSocket.url).searchParams.get("terminal_id");

    // Two terminals are open here (the fixture plus the one opened above), so
    // closing the active one leaves 1.
    expect(ref.current?.closeActiveTerminal()).toBe(1);
    await waitFor(() =>
      expect(authedFetchMock).toHaveBeenCalledWith(
        `/api/remote/${encodeURIComponent("dev@example.com")}/api/terminal/${secondId}`,
        expect.objectContaining({ method: "DELETE" }),
      ),
    );
    const call = authedFetchMock.mock.calls.find(([url]) => String(url).includes("/api/terminal/"));
    const headers = (call?.[1] as RequestInit | undefined)?.headers as Headers;
    expect(headers.get("X-Ocode-Project")).toBe("/remote");
  });

  it("closeActiveTerminal() returns 0 then null once no terminal remains", async () => {
    const { ref } = await renderTabs();

    // 0 = this close emptied the project; null = nothing left to close. The
    // second call must not remove a neighbour (there is none) nor re-fire the
    // shell kill.
    expect(ref.current?.closeActiveTerminal()).toBe(0);
    expect(ref.current?.closeActiveTerminal()).toBeNull();
  });

  it("restores the same number of terminal tabs (fresh sockets) after remount", async () => {
    const { default: TerminalTabs } = await import("./TerminalTabs");
    const ref = createRef<TerminalTabsHandle>();
    const { unmount } = render(
      <TerminalProvider>
        <TerminalTabs ref={ref} active projectPath="/project" />
      </TerminalProvider>,
    );
    act(() => ref.current?.openTerminal());
    await waitFor(() => expect(sockets.length).toBe(1));
    ref.current?.openTerminal();
    await waitFor(() => expect(sockets.length).toBe(2));
    // A fresh <TerminalProvider> below means a brand-new (empty) store, so
    // this only passes if the second terminal was actually persisted to
    // localStorage and restored from there — not carried over in memory.
    unmount();

    sockets.length = 0;
    const ref2 = createRef<TerminalTabsHandle>();
    render(
      <TerminalProvider>
        <TerminalTabs ref={ref2} active projectPath="/project" />
      </TerminalProvider>,
    );

    // Lazy panel mount: only the restored ACTIVE terminal attaches; the other
    // restored tab exists in the store but opens its socket the first time it
    // is selected (and then stays mounted).
    await waitFor(() => expect(sockets.length).toBe(1));
    // Both persisted tabs were restored: closing the active one leaves one.
    expect(ref2.current?.closeActiveTerminal()).toBe(1);
  });
});
