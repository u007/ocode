import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import TerminalPanel from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";
import { disableBuiltinSelectionCopy } from "./ghosttyInternals";

// libghostty-vt is a wasm module loaded at mount. A failed load must surface in
// the panel (and the console) instead of leaving a silently blank terminal, and
// must not open a socket for a shell nobody can see.

const h = vi.hoisted(() => ({ sockets: 0, loadFails: true }));

vi.mock("./ghosttyRuntime", () => ({
  loadGhostty: () =>
    h.loadFails ? Promise.reject(new Error("wasm instantiate failed")) : Promise.resolve({}),
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
  apiPath: (p: string) => p,
  apiWsPath: (p: string) => `ws://localhost${p}`,
  remoteApiBase: () => "",
  authToken: () => "tok",
  authHeaders: () => ({}),
  isRemoteSession: () => false,
}));
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
  constructor() {
    h.sockets++;
  }
}

beforeEach(() => {
  FakeTerminal.instances.length = 0;
  h.sockets = 0;
  h.loadFails = true;
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

describe("TerminalPanel startup failure", () => {
  it("shows the error, logs it, and opens neither a terminal nor a socket", async () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const view = render(
        <TerminalPanel active id="t1" projectPath="/proj" scrollbackLines={1000} fontFamily="mono" fontSize={12} />,
      );
      expect((await screen.findByRole("alert")).textContent).toContain("wasm instantiate failed");
      expect(errorSpy).toHaveBeenCalled();
      expect(FakeTerminal.instances.length).toBe(0);
      expect(h.sockets).toBe(0);
      view.unmount();
    } finally {
      errorSpy.mockRestore();
      vi.unstubAllGlobals();
    }
  });
});

describe("TerminalPanel setup failure after load", () => {
  it("shows the error and disposes the opened terminal", async () => {
    h.loadFails = false;
    vi.mocked(disableBuiltinSelectionCopy).mockImplementationOnce(() => {
      throw new Error("ghostty-web internals changed: test");
    });
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const view = render(
        <TerminalPanel active id="t2" projectPath="/proj" scrollbackLines={1000} fontFamily="mono" fontSize={12} />,
      );
      expect((await screen.findByRole("alert")).textContent).toContain("ghostty-web internals changed: test");
      expect(FakeTerminal.instances.length).toBe(1);
      expect(FakeTerminal.instances[0].dispose).toHaveBeenCalledTimes(1);
      view.unmount();
    } finally {
      errorSpy.mockRestore();
      vi.unstubAllGlobals();
    }
  });
});
