import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createRef } from "react";
import { TerminalProvider } from "../../stores/terminalStore";
import type { TerminalTabsHandle } from "./TerminalTabs";

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
    onBell = vi.fn(() => ({ dispose: vi.fn() }));
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
  },
  apiPath: (p: string) => p,
  apiWsPath: (p: string) => `ws://localhost${p}`,
  authToken: () => "tok",
}));

const sockets: MockSocket[] = [];

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
  window.localStorage.clear();
  vi.stubGlobal("WebSocket", MockSocket as unknown as typeof WebSocket);
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
});

async function renderTabs(projectPath = "/project") {
  const { default: TerminalTabs } = await import("./TerminalTabs");
  const ref = createRef<TerminalTabsHandle>();
  const utils = render(
    <TerminalProvider>
      <TerminalTabs ref={ref} active projectPath={projectPath} />
    </TerminalProvider>,
  );
  // First terminal is opened lazily once the panel becomes active.
  await waitFor(() => expect(sockets.length).toBe(1));
  return { ref, ...utils };
}

describe("TerminalTabs", () => {
  it("connects each terminal socket with the tab's project_path", async () => {
    await renderTabs();
    expect(sockets[0].url).toContain(`project_path=${encodeURIComponent("/project")}`);
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

    const closed = ref.current?.closeActiveTerminal();

    expect(closed).toBe(true);
    await waitFor(() => expect(secondSocket.close).toHaveBeenCalled());
  });

  it("closeActiveTerminal() returns false once no terminal remains", async () => {
    const { ref } = await renderTabs();

    expect(ref.current?.closeActiveTerminal()).toBe(true);
    expect(ref.current?.closeActiveTerminal()).toBe(false);
  });

  it("restores the same number of terminal tabs (fresh sockets) after remount", async () => {
    const { default: TerminalTabs } = await import("./TerminalTabs");
    const ref = createRef<TerminalTabsHandle>();
    const { unmount } = render(
      <TerminalProvider>
        <TerminalTabs ref={ref} active projectPath="/project" />
      </TerminalProvider>,
    );
    await waitFor(() => expect(sockets.length).toBe(1));
    ref.current?.openTerminal();
    await waitFor(() => expect(sockets.length).toBe(2));
    // A fresh <TerminalProvider> below means a brand-new (empty) store, so
    // this only passes if the second terminal was actually persisted to
    // localStorage and restored from there — not carried over in memory.
    unmount();

    sockets.length = 0;
    render(
      <TerminalProvider>
        <TerminalTabs active projectPath="/project" />
      </TerminalProvider>,
    );

    await waitFor(() => expect(sockets.length).toBe(2));
  });
});
