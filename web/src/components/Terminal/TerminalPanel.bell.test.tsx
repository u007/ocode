import { render, screen, act, fireEvent } from "@testing-library/react";
import { useEffect, useState } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TerminalProvider, useTerminalState, getProjectTerminals } from "../../stores/terminalStore";
import { playAlertSound } from "./terminalAlertSound";
import TerminalPanel from "./TerminalPanel";

// xterm needs canvas/layout jsdom lacks, so the Terminal (and its addons) are
// stubbed. The stub captures the bell + OSC handlers the panel registers so the
// test can fire a BEL / OSC 9 / 777 / 99 the way a real pty would and assert the
// panel raises a background-activity alert + plays the alert sound.
const h = vi.hoisted(() => ({
  terminals: [] as Array<{ _bell: (() => void) | null; _osc: Record<number, () => boolean> }>,
  sockets: [] as Array<{ onopen: (() => void) | null; url: string }>,
}));

vi.mock("@xterm/xterm", () => {
  class Terminal {
    cols = 80;
    rows = 24;
    options: Record<string, unknown> = {};
    _bell: (() => void) | null = null;
    _osc: Record<number, () => boolean> = {};
    constructor() {
      h.terminals.push(this);
    }
    loadAddon = vi.fn();
    open = vi.fn();
    write = vi.fn();
    focus = vi.fn();
    onData = vi.fn(() => ({ dispose: vi.fn() }));
    onBell = vi.fn((cb: () => void) => {
      this._bell = cb;
      return { dispose: vi.fn() };
    });
    parser = {
      registerOscHandler: vi.fn((ident: number, cb: () => boolean) => {
        this._osc[ident] = cb;
        return { dispose: vi.fn() };
      }),
    };
    dispose = vi.fn();
    attachCustomKeyEventHandler = vi.fn(() => true);
  }
  return { Terminal };
});
vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit = vi.fn(); } }));
vi.mock("@xterm/addon-serialize", () => ({ SerializeAddon: class { serialize = vi.fn(() => ""); } }));
vi.mock("@xterm/addon-webgl", () => ({ WebglAddon: class { dispose = vi.fn(); onContextLoss = vi.fn(); } }));
vi.mock("@xterm/xterm/css/xterm.css", () => ({}));
vi.mock("@/api/client", () => ({
  apiPath: (p: string) => p,
  apiWsPath: (p: string) => `ws://localhost${p}`,
  authToken: () => "tok",
  authHeaders: () => ({}),
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
  return <div data-testid="alerted">{alerted}</div>;
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
  h.terminals.length = 0;
  h.sockets.length = 0;
  // playAlertSound is a single vi.fn() shared across every test in this file, so
  // its call count must be reset each run or assertions from earlier tests leak.
  vi.mocked(playAlertSound).mockClear();
  window.localStorage.clear();
  vi.stubGlobal("WebSocket", MockSocket as unknown as typeof WebSocket);
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

// Flips readyRef true the way the live pty socket's onopen would, so a BEL baked
// into restored scrollback can't false-alert (the panel gates on readyRef).
function openReady() {
  act(() => {
    (h.sockets[0].onopen as (() => void) | null)?.();
  });
}

describe("TerminalPanel bell/OSC detection", () => {
  it("alerts the store and plays sound on BEL while the terminal is backgrounded", () => {
    render(
      <TerminalProvider>
        <SeedHarness projectPath="/proj" />
        <PanelHost initialActive={false} />
        <AlertReader projectPath="/proj" />
      </TerminalProvider>,
    );
    openReady();
    const term = h.terminals[0];
    act(() => {
      term._bell?.();
    });
    expect(screen.getByTestId("alerted").textContent).toBe("t1");
    expect(playAlertSound).toHaveBeenCalledTimes(1);
  });

  it("alerts on the common notification OSC sequences (9 / 777 / 99)", () => {
    render(
      <TerminalProvider>
        <SeedHarness projectPath="/proj" />
        <PanelHost initialActive={false} />
        <AlertReader projectPath="/proj" />
      </TerminalProvider>,
    );
    openReady();
    const term = h.terminals[0];
    act(() => {
      term._osc[9]();
    });
    act(() => {
      vi.advanceTimersByTime(500);
    });
    act(() => {
      term._osc[777]();
    });
    act(() => {
      vi.advanceTimersByTime(500);
    });
    act(() => {
      term._osc[99]();
    });
    expect(screen.getByTestId("alerted").textContent).toBe("t1");
    // playAlertSound is mocked (no 400ms throttle), so each handler counts.
    expect(playAlertSound).toHaveBeenCalledTimes(3);
  });

  it("does not alert while focused, but does once backgrounded", () => {
    render(
      <TerminalProvider>
        <SeedHarness projectPath="/proj" />
        <PanelHost initialActive={true} />
        <AlertReader projectPath="/proj" />
      </TerminalProvider>,
    );
    openReady();
    const term = h.terminals[0];
    act(() => {
      term._bell?.();
    });
    expect(screen.getByTestId("alerted").textContent).toBe("");
    expect(playAlertSound).not.toHaveBeenCalled();
    // Background the terminal, then a bell must alert.
    act(() => {
      fireEvent.click(screen.getByText("deactivate"));
    });
    act(() => {
      term._bell?.();
    });
    expect(screen.getByTestId("alerted").textContent).toBe("t1");
    expect(playAlertSound).toHaveBeenCalledTimes(1);
  });
});
