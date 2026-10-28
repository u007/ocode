import { render, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import TerminalPanel from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";

// libghostty-vt is a wasm module loaded at mount. A failed load must surface in
// the panel (and the console) instead of leaving a silently blank terminal, and
// must not open a socket for a shell nobody can see.

const h = vi.hoisted(() => ({ serialized: "line one\r\nline two" }));

vi.mock("./ghosttyRuntime", () => ({
  loadGhostty: () =>
    Promise.resolve({}),
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
vi.mock("./terminalSerialize", () => ({ serializeTerminalBuffer: () => h.serialized }));
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

class MockSocket {
  close() {}
}

beforeEach(() => {
  FakeTerminal.instances.length = 0;
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

const panel = (scrollbackLines: number) => (
  <TerminalPanel active id="t1" projectPath="/proj" scrollbackLines={scrollbackLines} fontFamily="mono" fontSize={12} />
);

describe("TerminalPanel live scrollback", () => {
  it("does not reset on mount or on an unchanged re-render", async () => {
    const view = render(panel(1000));
    await waitFor(() => expect(FakeTerminal.instances.length).toBe(1));
    const term = FakeTerminal.instances[0];
    view.rerender(panel(1000));
    expect(term.reset).not.toHaveBeenCalled();
    expect(term.options.scrollback).toBe(1000);
    view.unmount();
  });

  it("applies a changed value to the open terminal and replays the buffer", async () => {
    const view = render(panel(1000));
    await waitFor(() => expect(FakeTerminal.instances.length).toBe(1));
    const term = FakeTerminal.instances[0];
    term.write.mockClear();
    term.clearSelection.mockClear();
    view.rerender(panel(5000));
    expect(term.options.scrollback).toBe(5000);
    expect(term.reset).toHaveBeenCalledTimes(1);
    expect(term.write).toHaveBeenCalledWith("line one\r\nline two");
    expect(term.reset.mock.invocationCallOrder[0]).toBeLessThan(term.write.mock.invocationCallOrder[0]);
    expect(term.clearSelection).toHaveBeenCalled();
    expect(FakeTerminal.instances.length).toBe(1);
    expect(term.dispose).not.toHaveBeenCalled();
    view.rerender(panel(5000));
    expect(term.reset).toHaveBeenCalledTimes(1);
    view.unmount();
  });
});
