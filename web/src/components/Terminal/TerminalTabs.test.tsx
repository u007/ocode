import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// Real xterm needs canvas/layout that jsdom does not have, so the terminal
// itself is stubbed: this test covers the tab strip's open/close behaviour and
// that closing a tab tears down that tab's socket (which is what kills the pty
// server-side).
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
  WebglAddon: class { dispose = vi.fn(); },
}));
vi.mock("@/api/client", () => ({
  api: {
    getTerminalConfig: () =>
      Promise.resolve({ available: true, scrollback_lines: 9999, work_dir: "/project" }),
    getTerminalProcesses: () => Promise.resolve([]),
  },
  apiPath: (p: string) => p,
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
  render(<TerminalTabs active projectPath={projectPath} />);
  // First terminal is opened lazily once the panel becomes active.
  await waitFor(() => expect(sockets.length).toBe(1));
}

describe("TerminalTabs", () => {
  // Terminal titles are numbered from a module-level counter, so tests assert
  // on the count of close buttons rather than on specific labels.
  const closeButtons = () =>
    screen.getAllByRole("button", { name: /^Close Terminal / }) as HTMLElement[];

  it("connects each terminal socket with the tab's project_path", async () => {
    await renderTabs();
    expect(sockets[0].url).toContain(`project_path=${encodeURIComponent("/project")}`);
  });

  it("opens an additional terminal (and socket) when + is clicked", async () => {
    await renderTabs();
    expect(closeButtons()).toHaveLength(1);

    fireEvent.click(screen.getByLabelText("New terminal"));

    await waitFor(() => expect(sockets.length).toBe(2));
    expect(closeButtons()).toHaveLength(2);
  });

  it("closes a terminal and its websocket when x is clicked", async () => {
    await renderTabs();
    fireEvent.click(screen.getByLabelText("New terminal"));
    await waitFor(() => expect(sockets.length).toBe(2));

    const firstSocket = sockets[0];
    const firstLabel = closeButtons()[0].getAttribute("aria-label");
    fireEvent.click(closeButtons()[0]);

    await waitFor(() => expect(closeButtons()).toHaveLength(1));
    expect(closeButtons()[0].getAttribute("aria-label")).not.toBe(firstLabel);
    expect(firstSocket.close).toHaveBeenCalled();
  });

  it("restores the same number of terminal tabs (fresh sockets) after remount", async () => {
    const { default: TerminalTabs } = await import("./TerminalTabs");
    const { unmount } = render(<TerminalTabs active projectPath="/project" />);
    await waitFor(() => expect(sockets.length).toBe(1));
    fireEvent.click(screen.getByLabelText("New terminal"));
    await waitFor(() => expect(sockets.length).toBe(2));
    await waitFor(() => expect(closeButtons()).toHaveLength(2));
    unmount();

    sockets.length = 0;
    render(<TerminalTabs active projectPath="/project" />);

    await waitFor(() => expect(sockets.length).toBe(2));
    expect(closeButtons()).toHaveLength(2);
  });
});
