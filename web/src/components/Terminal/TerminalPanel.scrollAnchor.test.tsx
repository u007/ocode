import { act, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TerminalPanel from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";

// Regression coverage for "scrolling up in a streaming terminal bounces back to
// the bottom". ghostty-web's writeInternal resets viewportY to 0 on every write
// while the user is scrolled back; the panel must anchor the viewport to the
// same content across each pty write (see terminalScrollAnchor.ts).

const h = vi.hoisted(() => ({ sockets: [] as MockSocket[] }));

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
    h.sockets.push(this);
  }
}

vi.mock("./ghosttyRuntime", () => ({
  loadGhostty: () => Promise.resolve({}),
}));
vi.mock("ghostty-web", async () => (await import("./ghosttyWebMock")).ghosttyWebModule());
vi.mock("./ghosttyInternals", async () => (await import("./ghosttyWebMock")).ghosttyInternalsMock());
vi.mock("./terminalMouse", async () => (await import("./ghosttyWebMock")).terminalMouseMock());
vi.mock("../../stores/terminalStore", () => ({
  useTerminalState: () => ({
    markAlerted: vi.fn(),
    setOscTitle: vi.fn(),
    openTerminal: vi.fn(),
    closeTerminal: vi.fn(),
  }),
}));
vi.mock("@/api/client", () => ({
  // TerminalPanel mounts the voice recorder; its lifecycle reads these.
  api: {
    prewarmSpeech: () => Promise.resolve(true),
    cancelPrewarmSpeech: () => Promise.resolve(true),
  },
  apiPath: (p: string) => p,
  apiWsPath: (p: string) => `ws://localhost${p}`,
  remoteApiBase: () => "",
  authToken: () => "tok",
  authHeaders: () => ({}),
  isRemoteSession: () => false,
}));
vi.mock("./terminalSerialize", () => ({ serializeTerminalBuffer: () => "" }));
vi.mock("./terminalFocus", () => ({
  registerTerminalFocus: vi.fn(),
  unregisterTerminalFocus: vi.fn(),
}));
vi.mock("./terminalPersistence", () => ({
  loadTerminalBuffer: () => null,
  saveTerminalBuffer: vi.fn(),
}));
vi.mock("@/lib/debug/terminalRegistry", () => ({
  registerTerminal: vi.fn(),
  unregisterTerminal: vi.fn(),
}));

function output(text: string) {
  return new TextEncoder().encode(text).buffer;
}

beforeEach(() => {
  h.sockets.length = 0;
  FakeTerminal.instances.length = 0;
  vi.stubGlobal("WebSocket", MockSocket as unknown as typeof WebSocket);
  // No usable REST history: the panel falls straight through to the live socket.
  vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response("", { status: 404 }))));
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
});

const panel = () => (
  <TerminalPanel active id="t1" projectPath="/proj" scrollbackLines={5000} fontFamily="mono" fontSize={12} />
);

async function mount() {
  const view = render(panel());
  await waitFor(() => expect(h.sockets).toHaveLength(1));
  await waitFor(() => expect(FakeTerminal.instances).toHaveLength(1));
  return { view, term: FakeTerminal.instances[0], socket: h.sockets[0] };
}

describe("TerminalPanel scroll anchoring", () => {
  it("keeps a scrolled-back viewport on the same content while output streams", async () => {
    const { view, term, socket } = await mount();

    // Fill scrollback, then simulate the user scrolling up 5 lines.
    act(() => socket.onmessage?.({ data: output("line\r\n".repeat(40)) }));
    expect(term.scrollbackLength).toBe(40);
    term.viewportY = 5;

    act(() => socket.onmessage?.({ data: output("streaming line\r\n") }));

    // One new scrollback line: the same text stays on screen (5 + 1), instead of
    // the library resetting the viewport to 0 and snapping to the bottom.
    expect(term.scrollbackLength).toBe(41);
    expect(term.viewportY).toBe(6);
    expect(term.scrollToLine).toHaveBeenLastCalledWith(6);
    view.unmount();
  });

  it("does not move the viewport while it is at the bottom", async () => {
    const { view, term, socket } = await mount();

    act(() => socket.onmessage?.({ data: output("line\r\n".repeat(40)) }));
    act(() => socket.onmessage?.({ data: output("streaming line\r\n") }));

    expect(term.viewportY).toBe(0);
    expect(term.scrollToLine).not.toHaveBeenCalled();
    view.unmount();
  });
});
