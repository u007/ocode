import { render, screen, act, fireEvent } from "@testing-library/react";
import { useEffect, useState } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TerminalProvider, useTerminalState, getProjectTerminals, terminalDisplayTitle } from "../../stores/terminalStore";
import { playAlertSound } from "./terminalAlertSound";
import TerminalPanel from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";

// ghostty-web needs canvas/layout jsdom lacks, so the Terminal is stubbed
// (ghosttyWebMock.ts). The bell / title / attention signals are not terminal
// events: the panel scans the pty byte stream itself, so the tests push real
// bytes (BEL, OSC 0/2/9/777/99) through the mocked WebSocket the way a real pty
// would and assert the panel raises a background-activity alert + plays the
// alert sound.
const h = vi.hoisted(() => ({
  sockets: [] as Array<{ onopen: (() => void) | null; onmessage: ((e: { data: unknown }) => void) | null; url: string }>,
}));

// terminalStore subscribes to the server's `terminal_tabs_changed` bus event;
// the real eventBus auto-starts an SSE stream on the first on(), calling
// remoteApiBase() from this suite's partial api mock. Mirrors the real bus
// surface the store uses (on/off/onReconnect/offReconnect).
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
  apiPath: (p: string) => p,
  apiWsPath: (p: string) => `ws://localhost${p}`,
  remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
  authToken: () => "tok",
  authHeaders: () => ({}),
  isRemoteSession: () => false,
}));
vi.mock("./terminalAlertSound", () => ({
  playAlertSound: vi.fn(),
}));
vi.mock("./terminalFocus", () => ({
  registerTerminalFocus: vi.fn(),
  unregisterTerminalFocus: vi.fn(),
}));
vi.mock("./terminalPersistence", () => ({
  // loadProjectTerminals is consumed by the store (activate / getProjectTerminals),
  // so it must exist on the mock and return a terminal whose id matches the
  // panel's `id` prop ("t1") for the alert to attach.
  loadProjectTerminals: () => ({ terminals: [{ id: "t1", title: "Terminal 1" }], activeId: "t1" }),
  saveProjectTerminals: vi.fn(),
  projectTerminalsKey: (path: string, host?: string) => (host ? `${host}::${path}` : path),
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
  constructor(public url: string) {
    h.sockets.push(this);
  }
}

function AlertReader({ projectPath }: { projectPath: string }) {
  const { state } = useTerminalState();
  const { terminals } = getProjectTerminals(state, projectPath);
  const alerted = terminals.filter((t) => (t as { alerted?: boolean }).alerted).map((t) => t.id).join(",");
  return (
    <>
      <div data-testid="alerted">{alerted}</div>
      <div data-testid="titles">{terminals.map(terminalDisplayTitle).join(",")}</div>
    </>
  );
}

// Makes the project's terminal region live (seeded from the mocked
// loadProjectTerminals) so MARK_ALERTED has a live entry to attach to — the same
// lifecycle the real app goes through when a terminal is first opened.
function SeedHarness({ projectPath }: { projectPath: string }) {
  const { activate } = useTerminalState();
  useEffect(() => {
    activate(projectPath);
  }, [activate, projectPath]);
  return null;
}

function PanelHost({ initialActive = false }: { initialActive?: boolean }) {
  const [active, setActive] = useState(initialActive);
  return (
    <div>
      <button onClick={() => setActive(true)}>activate</button>
      <button onClick={() => setActive(false)}>deactivate</button>
      <TerminalPanel active={active} id="t1" projectPath="/proj" scrollbackLines={1000} fontFamily="mono" fontSize={12} />
    </div>
  );
}

beforeEach(() => {
  FakeTerminal.instances.length = 0;
  h.sockets.length = 0;
  // playAlertSound is a single vi.fn() shared across every test in this file, so
  // its call count must be reset each run or assertions from earlier tests leak.
  vi.mocked(playAlertSound).mockClear();
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
  // fitAndResize defers to double-rAF on first fit; noop keeps mount cheap and
  // avoids driving real animation frames during the test.
  vi.stubGlobal("requestAnimationFrame", (() => 0) as unknown as typeof requestAnimationFrame);
  vi.stubGlobal("cancelAnimationFrame", (() => {}) as unknown as typeof cancelAnimationFrame);
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

// The panel mounts asynchronously (it awaits the libghostty wasm load, then the
// history restore, before opening the socket), so drain microtasks until the
// socket exists.
async function mounted() {
  await act(async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
  });
  expect(FakeTerminal.instances.length).toBe(1);
  expect(h.sockets.length).toBe(1);
}

// Flips readyRef true the way the live pty socket's onopen would, so a BEL baked
// into restored scrollback can't false-alert (the panel gates on readyRef).
async function openReady() {
  await mounted();
  act(() => {
    (h.sockets[0].onopen as (() => void) | null)?.();
  });
}

// Delivers pty output to the panel as a binary WebSocket frame, so it goes
// through the real stream scanner.
function pty(text: string) {
  act(() => {
    h.sockets[0].onmessage?.({ data: new TextEncoder().encode(text).buffer });
  });
}

describe("TerminalPanel bell/OSC detection", () => {
  it("alerts the store and plays sound on BEL while the terminal is backgrounded", async () => {
    render(
      <TerminalProvider>
        <SeedHarness projectPath="/proj" />
        <PanelHost initialActive={false} />
        <AlertReader projectPath="/proj" />
      </TerminalProvider>,
    );
    await openReady();
    pty("\x07");
    expect(screen.getByTestId("alerted").textContent).toBe("t1");
    expect(playAlertSound).toHaveBeenCalledTimes(1);
  });

  it("alerts on the common notification OSC sequences (9 / 777 / 99)", async () => {
    render(
      <TerminalProvider>
        <SeedHarness projectPath="/proj" />
        <PanelHost initialActive={false} />
        <AlertReader projectPath="/proj" />
      </TerminalProvider>,
    );
    await openReady();
    pty("\x1b]9;build done\x07");
    act(() => {
      vi.advanceTimersByTime(500);
    });
    pty("\x1b]777;notify;title;body\x07");
    act(() => {
      vi.advanceTimersByTime(500);
    });
    pty("\x1b]99;;hello\x1b\\");
    expect(screen.getByTestId("alerted").textContent).toBe("t1");
    // playAlertSound is mocked (no 400ms throttle), so each handler counts.
    expect(playAlertSound).toHaveBeenCalledTimes(3);
  });

  it("does not alert while focused, but does once backgrounded", async () => {
    render(
      <TerminalProvider>
        <SeedHarness projectPath="/proj" />
        <PanelHost initialActive={true} />
        <AlertReader projectPath="/proj" />
      </TerminalProvider>,
    );
    await openReady();
    pty("\x07");
    expect(screen.getByTestId("alerted").textContent).toBe("");
    expect(playAlertSound).not.toHaveBeenCalled();
    // Background the terminal, then a bell must alert.
    act(() => {
      fireEvent.click(screen.getByText("deactivate"));
    });
    pty("\x07");
    expect(screen.getByTestId("alerted").textContent).toBe("t1");
    expect(playAlertSound).toHaveBeenCalledTimes(1);
  });

  it("does not ring the bell for the BEL that terminates an OSC title", async () => {
    render(
      <TerminalProvider>
        <SeedHarness projectPath="/proj" />
        <PanelHost initialActive={false} />
        <AlertReader projectPath="/proj" />
      </TerminalProvider>,
    );
    await openReady();
    // ghostty-web's own onBell would fire for this chunk; the scanner must not.
    pty("\x1b]0;a title\x07");
    expect(screen.getByTestId("alerted").textContent).toBe("");
    expect(playAlertSound).not.toHaveBeenCalled();
  });

  it("forwards the program's OSC window title to the tab name", async () => {
    render(
      <TerminalProvider>
        <SeedHarness projectPath="/proj" />
        <PanelHost initialActive={true} />
        <AlertReader projectPath="/proj" />
      </TerminalProvider>,
    );
    await mounted();
    pty("\x1b]0;⦿ ocode — fix bug\x07");
    expect(screen.getByTestId("titles").textContent).toBe("⦿ ocode — fix bug");
  });

  it("forwards OSC 0 and OSC 2 titles while the terminal is backgrounded", async () => {
    render(
      <TerminalProvider>
        <SeedHarness projectPath="/proj" />
        <PanelHost initialActive={false} />
        <AlertReader projectPath="/proj" />
      </TerminalProvider>,
    );
    await mounted();
    pty("\x1b]0;shell title\x07");
    pty("\x1b]2;running command\x1b\\");
    expect(screen.getByTestId("titles").textContent).toBe("running command");
  });
});
