import { render } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import TerminalPanel from "./TerminalPanel";

// Verify that TerminalPanel registers the URL link provider (http(s) URLs, e.g. Vite
// localhost) and the file-link provider (src/*.ts:line → ocode:open-file) exactly
// once per terminal and disposes both on unmount. This is the wiring that was
// missing before the original fix — the screenshot shows https://localhost:3510/
// in the terminal with no clickable underline.

const urlProviderSpy = vi.fn((..._args: unknown[]) => ({ dispose: vi.fn() }));
const fileProviderSpy = vi.fn((..._args: unknown[]) => ({ dispose: vi.fn() }));

const openExternalSpy = vi.fn();
vi.mock("../../lib/externalLinks", () => ({
  openExternalURL: (...args: unknown[]) => openExternalSpy(...args),
}));

vi.mock("./terminalLinkProvider", () => ({
  registerFileLinkProvider: (...args: unknown[]) => fileProviderSpy(...args),
  registerUrlLinkProvider: (...args: unknown[]) => urlProviderSpy(...args),
}));

vi.mock("ghostty-web", async () => (await import("./ghosttyWebMock")).ghosttyWebModule());
vi.mock("./ghosttyInternals", async () => (await import("./ghosttyWebMock")).ghosttyInternalsMock());
vi.mock("./terminalMouse", async () => (await import("./ghosttyWebMock")).terminalMouseMock());
vi.mock("../../api/client", () => ({
  // TerminalPanel mounts the voice recorder; its lifecycle reads these.
  api: {
    prewarmSpeech: () => Promise.resolve(true),
    cancelPrewarmSpeech: () => Promise.resolve(true),
  },
  apiPath: (p: string) => p,
  apiWsPath: (p: string) => `ws://localhost${p}`,
  authToken: () => null,
  authHeaders: () => ({}),
  isRemoteSession: () => false,
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
    urlProviderSpy.mockClear();
    fileProviderSpy.mockClear();
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response("", { status: 404 }))));
    // jsdom lacks ResizeObserver
    (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = class {
      observe = vi.fn();
      disconnect = vi.fn();
      unobserve = vi.fn();
    };
  });

  it("registers the URL and file link providers exactly once per terminal", async () => {
    const { unmount } = render(
      <TerminalPanel id="t1" active projectPath="/proj" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
    );
    // The mount is async (it awaits the libghostty wasm load), so let it settle.
    await new Promise((r) => setTimeout(r, 0));
    expect(urlProviderSpy).toHaveBeenCalledTimes(1);
    expect(fileProviderSpy).toHaveBeenCalledTimes(1);
    expect(fileProviderSpy).toHaveBeenCalledWith(expect.any(Object), "/proj", expect.any(Function));
    const fileDispose = fileProviderSpy.mock.results[0].value.dispose as ReturnType<typeof vi.fn>;
    const urlDispose = urlProviderSpy.mock.results[0].value.dispose as ReturnType<typeof vi.fn>;
    unmount();
    expect(fileDispose).toHaveBeenCalledTimes(1);
    expect(urlDispose).toHaveBeenCalledTimes(1);
  });

  it("opens only http(s) URLs externally", async () => {
    openExternalSpy.mockClear();
    const { unmount } = render(
      <TerminalPanel id="t3" active projectPath="/proj" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
    );
    await new Promise((r) => setTimeout(r, 0));
    const open = urlProviderSpy.mock.calls[0][1] as (uri: string) => void;
    const url =
      "https://claude.com/cai/oauth/authorize?code=true&client_id=9d1c250a&redirect_uri=https%3A%2F%2Fplatform.claude.com%2Foauth%2Fcode%2Fcallback&scope=org%3Acreate_api_key+user%3Aprofile&state=Lbv_ZqZchTvEz5Zyxdt";
    open(url);
    expect(openExternalSpy).toHaveBeenCalledWith(url);
    openExternalSpy.mockClear();
    open("ftp://example.com");
    expect(openExternalSpy).not.toHaveBeenCalled();
    unmount();
  });

  it("passes the drag guard to the URL provider", async () => {
    const { unmount } = render(
      <TerminalPanel id="t4" active projectPath="/proj" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
    );
    await new Promise((r) => setTimeout(r, 0));
    expect(typeof urlProviderSpy.mock.calls[0][2]).toBe("function");
    unmount();
  });

  it("passes current projectPath to file provider", async () => {
    const { unmount } = render(
      <TerminalPanel id="t2" active projectPath="/my/project" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
    );
    await new Promise((r) => setTimeout(r, 0));
    expect(fileProviderSpy).toHaveBeenCalledWith(expect.any(Object), "/my/project", expect.any(Function));
    unmount();
  });
});


  it("passes drag-guard callback to file provider", async () => {
    const { unmount } = render(
      <TerminalPanel id="t5" active projectPath="/proj" scrollbackLines={100} fontFamily="mono" fontSize={12} />,
    );
    await new Promise((r) => setTimeout(r, 0));
    const lastCall = fileProviderSpy.mock.calls[fileProviderSpy.mock.calls.length - 1];
    expect(typeof lastCall[2]).toBe("function");
    unmount();
  });
