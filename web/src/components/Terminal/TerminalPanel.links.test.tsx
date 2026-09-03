import { render } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import TerminalPanel from "./TerminalPanel";

// Verify that TerminalPanel loads WebLinksAddon (http(s) URLs, e.g. Vite localhost)
// and the file-link provider (src/*.ts:line → ocode:open-file) and disposes both
// on unmount. This is the wiring that was missing before the fix — the screenshot
// shows https://localhost:3510/ in the terminal with no clickable underline.

const webLinksInstances: unknown[] = [];
const fileProviderSpy = vi.fn((..._args: unknown[]) => ({ dispose: vi.fn() }));

vi.mock("@xterm/addon-web-links", () => ({
  WebLinksAddon: class {
    dispose = vi.fn();
    constructor() {
      webLinksInstances.push(this);
    }
  },
}));

vi.mock("./terminalLinkProvider", () => ({
  registerFileLinkProvider: (...args: unknown[]) => fileProviderSpy(...args),
}));

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    options: Record<string, unknown> = {};
    loadAddon = vi.fn();
    open = vi.fn();
    write = vi.fn();
    writeln = vi.fn();
    focus = vi.fn();
    onData = vi.fn(() => ({ dispose: vi.fn() }));
    onBell = vi.fn(() => ({ dispose: vi.fn() }));
    parser = { registerOscHandler: vi.fn(() => ({ dispose: vi.fn() })) };
    dispose = vi.fn();
    attachCustomKeyEventHandler = vi.fn(() => true);
    buffer = {
      active: {
        getLine: () => undefined,
        getNullCell: () => ({ getChars: () => "", getWidth: () => 0 }),
      },
    };
    registerLinkProvider = vi.fn(() => ({ dispose: vi.fn() }));
    getSelection = vi.fn(() => "");
  },
}));

vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit = vi.fn(); } }));
vi.mock("@xterm/addon-serialize", () => ({ SerializeAddon: class { serialize = vi.fn(() => ""); } }));
vi.mock("@xterm/addon-webgl", () => ({ WebglAddon: class { dispose = vi.fn(); onContextLoss = vi.fn(); } }));
vi.mock("@xterm/xterm/css/xterm.css", () => ({}));
vi.mock("../../api/client", () => ({
  apiPath: (p: string) => p,
  apiWsPath: (p: string) => `ws://localhost${p}`,
  authToken: () => null,
  authHeaders: () => ({}),
}));
vi.mock("./terminalPersistence", () => ({
  loadTerminalBuffer: () => null,
  saveTerminalBuffer: vi.fn(),
}));
vi.mock("../../lib/debug/terminalRegistry", () => ({
  registerTerminal: vi.fn(),
  unregisterTerminal: vi.fn(),
}));
vi.mock("../../stores/terminalStore", () => ({
  useTerminalState: () => ({
    markAlerted: vi.fn(),
    openTerminal: vi.fn(),
    closeTerminal: vi.fn(),
  }),
}));
vi.mock("./terminalFocus", () => ({
  registerTerminalFocus: vi.fn(),
  unregisterTerminalFocus: vi.fn(),
}));
vi.mock("./terminalAlertSound", () => ({ playAlertSound: vi.fn() }));

// Stub WebSocket globally so TerminalPanel doesn't throw on `new WebSocket(...)`
class StubWS {
  static OPEN = 1;
  readyState = 1;
  binaryType = "";
  onopen: (() => void) | null = null;
  onmessage: ((e: unknown) => void) | null = null;
  onclose: ((e: unknown) => void) | null = null;
  onerror: ((e: unknown) => void) | null = null;
  send = vi.fn();
  close = vi.fn();
  addEventListener = vi.fn();
  removeEventListener = vi.fn();
}
(globalThis as unknown as { WebSocket: unknown }).WebSocket = StubWS as unknown as typeof WebSocket;

describe("TerminalPanel link wiring", () => {
  beforeEach(() => {
    webLinksInstances.length = 0;
    fileProviderSpy.mockClear();
    // jsdom lacks ResizeObserver
    (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = class {
      observe = vi.fn();
      disconnect = vi.fn();
      unobserve = vi.fn();
    };
  });

  it("loads WebLinksAddon and file link provider exactly once per terminal", async () => {
    const { unmount } = render(
      <TerminalPanel id="t1" active projectPath="/proj" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
    );
    // Terminal constructor + loadAddon should have been called synchronously in the effect
    // (effects run after mount in React 18, but render flushes them in test env)
    await new Promise((r) => setTimeout(r, 0));
    expect(webLinksInstances.length).toBe(1);
    expect(fileProviderSpy).toHaveBeenCalledTimes(1);
    expect(fileProviderSpy).toHaveBeenCalledWith(expect.any(Object), "/proj");
    const dispose = fileProviderSpy.mock.results[0].value.dispose as ReturnType<typeof vi.fn>;
    const webLinksDispose = (webLinksInstances[0] as { dispose: ReturnType<typeof vi.fn> }).dispose;
    unmount();
    expect(dispose).toHaveBeenCalledTimes(1);
    expect(webLinksDispose).toHaveBeenCalledTimes(1);
  });

  it("passes current projectPath to file provider", async () => {
    const { unmount } = render(
      <TerminalPanel id="t2" active projectPath="/my/project" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
    );
    await new Promise((r) => setTimeout(r, 0));
    expect(fileProviderSpy).toHaveBeenCalledWith(expect.any(Object), "/my/project");
    unmount();
  });
});
