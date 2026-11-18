import { render, waitFor } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TerminalProvider } from "../../stores/terminalStore";
import TerminalPanel from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";
import { pauseRenderLoop, resumeRenderLoop } from "./ghosttyInternals";

// A hidden tab's terminal must stop its render loop, and the loop must restart when the tab
// comes back. The render helpers are spies on the shared ghosttyInternals mock (FakeTerminal
// has no renderer, so the real helpers would be no-ops here).

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
    getTerminalTabs: () => Promise.resolve({ projects: {} }),
    setTerminalTabs: () => Promise.resolve({ status: "ok" }),
  },
  authedFetch: () => Promise.resolve({ ok: true, status: 204 }),
  apiPath: (p: string) => p,
  apiWsPath: (p: string) => `ws://localhost${p}`,
  remoteApiBase: () => "",
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
  constructor(public url: string) {}
}

function panel(active: boolean) {
  return (
    <TerminalProvider>
      <TerminalPanel active={active} id="t1" projectPath="/proj" scrollbackLines={1000} fontFamily="mono" fontSize={12} />
    </TerminalProvider>
  );
}

beforeEach(() => {
  FakeTerminal.instances.length = 0;
  vi.clearAllMocks();
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
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("TerminalPanel render loop", () => {
  it("pauses the render loop of a terminal created while its tab is hidden", async () => {
    render(panel(false));
    await waitFor(() => expect(FakeTerminal.instances).toHaveLength(1));
    await waitFor(() => expect(pauseRenderLoop).toHaveBeenCalledWith(FakeTerminal.instances[0]));
    expect(resumeRenderLoop).not.toHaveBeenCalled();
  });

  it("resumes the render loop when the tab becomes active, and pauses it again when hidden", async () => {
    const { rerender } = render(panel(false));
    await waitFor(() => expect(FakeTerminal.instances).toHaveLength(1));
    const term = FakeTerminal.instances[0];
    await waitFor(() => expect(pauseRenderLoop).toHaveBeenCalledWith(term));

    rerender(panel(true));
    expect(resumeRenderLoop).toHaveBeenCalledWith(term);

    rerender(panel(false));
    expect(pauseRenderLoop).toHaveBeenCalledTimes(2);
  });

  it("does not pause the render loop of a terminal created while its tab is active", async () => {
    render(panel(true));
    await waitFor(() => expect(FakeTerminal.instances).toHaveLength(1));
    expect(pauseRenderLoop).not.toHaveBeenCalled();
  });
});
