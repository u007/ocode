import { render, act } from "@testing-library/react";
import { TerminalProvider } from "../../stores/terminalStore";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import TerminalPanel from "./TerminalPanel";

// Hoisted instance registry shared with the mocked module below.
const webglRegistry = vi.hoisted(() => ({ instances: [] as Array<{ dispose: ReturnType<typeof vi.fn>; onContextLoss: ReturnType<typeof vi.fn> }> }));

// Focused tests for the WebGL renderer release/restore lifecycle: hidden
// terminal tabs must release their WebGL context (dispose) so background
// terminals don't hold GPU contexts / exhaust the browser context pool, and
// re-acquire it on activation. Titles and shell output keep flowing through
// the unchanged data handlers in both states — only the GPU renderer cycles.

vi.mock("@xterm/xterm", () => {
  class Terminal {
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
    parser = { registerOscHandler: vi.fn(() => ({ dispose: vi.fn() })) };
    dispose = vi.fn();
    attachCustomKeyEventHandler = vi.fn(() => true);
  }
  return { Terminal: vi.fn(() => new Terminal()) };
});
vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit = vi.fn(); } }));
vi.mock("@xterm/addon-serialize", () => ({ SerializeAddon: class { serialize = vi.fn(() => ""); } }));
vi.mock("@xterm/addon-webgl", () => ({
  WebglAddon: class {
    dispose = vi.fn(function (this: { dispose: () => void }) {});
    onContextLoss = vi.fn();
    constructor() {
      webglRegistry.instances.push(this as unknown as { dispose: ReturnType<typeof vi.fn>; onContextLoss: ReturnType<typeof vi.fn> });
    }
  },
}));
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

class MockSocket {
  static OPEN = 1;
  readyState = 1;
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: unknown }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: ((e: { wasClean: boolean; code: number; reason: string }) => void) | null = null;
  send = vi.fn();
  close = vi.fn();
  constructor(public url: string) {}
}

function PanelHost({ initialActive }: { initialActive: boolean }) {
  return (
    <TerminalProvider>
      <TerminalPanel active={initialActive} id="t1" projectPath="/proj" scrollbackLines={1000} fontFamily="mono" fontSize={12} />
    </TerminalProvider>
  );
}

beforeEach(() => {
  webglRegistry.instances.length = 0;
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

describe("TerminalPanel WebGL renderer release for hidden tabs", () => {
  it("releases the WebGL addon when the tab becomes hidden and re-creates it on activation", async () => {
    const { rerender } = render(<PanelHost initialActive />);
    await act(async () => {
      await Promise.resolve();
    });
    // Active mount: exactly one WebGL addon loaded.
    expect(webglRegistry.instances.length).toBe(1);
    const first = webglRegistry.instances[0];
    expect(first.dispose).not.toHaveBeenCalled();

    // Hide the tab: the addon is disposed, no new addon created.
    rerender(<PanelHost initialActive={false} />);
    expect(first.dispose).toHaveBeenCalledTimes(1);

    // Reactivate: a fresh addon is created (the disposed one is not reused).
    rerender(<PanelHost initialActive={true} />);
    expect(webglRegistry.instances.length).toBe(2);
    expect(webglRegistry.instances[1].dispose).not.toHaveBeenCalled();
  });

  it("hiding twice without an addon does not double-dispose", async () => {
    const { rerender } = render(<PanelHost initialActive={false} />);
    await act(async () => {
      await Promise.resolve();
    });
    // Hidden mount: the mount-path effect created one, the hidden effect
    // disposed it. Hiding again must not dispose anything new.
    expect(webglRegistry.instances.length).toBe(1);
    const only = webglRegistry.instances[0];
    rerender(<PanelHost initialActive={false} />);
    rerender(<PanelHost initialActive={false} />);
    expect(only.dispose).toHaveBeenCalledTimes(1);
    expect(webglRegistry.instances.length).toBe(1);
  });
});
