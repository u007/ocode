import { render, act, cleanup } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import type { ITheme } from "ghostty-web";
import { TerminalProvider } from "../../stores/terminalStore";
import TerminalPanel from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";
import { applyTerminalTheme } from "./ghosttyInternals";
import { DEFAULT_TERMINAL_THEME } from "./terminalOptions";

// Regression suite for the terminal's LIVE theme application. ghostty-web fixes
// its wasm colour palette at construction, so a theme change rebuilds the
// terminal (serialize -> reset -> replay). These tests pin the two transitions
// that are easy to get wrong: applying a theme, and CLEARING one back to ocode's
// own palette.

const h = vi.hoisted(() => ({
  sockets: [] as Array<{ onopen: (() => void) | null; send: (data: string) => void }>,
}));

vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    on: () => () => {},
    off: () => {},
    onReconnect: () => () => {},
    offReconnect: () => () => {},
  },
}));

vi.mock("ghostty-web", async () => (await import("./ghosttyWebMock")).ghosttyWebModule());
vi.mock("./ghosttyInternals", async () => (await import("./ghosttyWebMock")).ghosttyInternalsMock());
vi.mock("./terminalMouse", async () => (await import("./ghosttyWebMock")).terminalMouseMock());
vi.mock("@/api/client", () => ({
  api: {
    cancelPrewarmSpeech: () => Promise.resolve(true),
    prewarmSpeech: () => Promise.resolve(true),
  },
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
vi.mock("./terminalHistory", () => ({
  restoreTerminalHistory: vi.fn(() => Promise.resolve({ kind: "missing" as const })),
  TerminalHistoryError: class extends Error {},
}));
vi.mock("../Speech/SpeechProvider", () => ({ requestSpeech: vi.fn() }));
vi.mock("../Speech/speechUtils", () => ({ sanitizeSpeechText: (s: string) => s }));

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

const GHOSTTY_THEME: ITheme = {
  background: "#000000",
  foreground: "#b3c9d7",
  cursor: "#f34b00",
};

beforeEach(() => {
  FakeTerminal.instances.length = 0;
  h.sockets.length = 0;
  cleanup();
  vi.mocked(applyTerminalTheme).mockClear();
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

afterEach(() => {
  vi.unstubAllGlobals();
});

function Panel({ theme, themeKey }: { theme?: ITheme; themeKey: string }) {
  return (
    <TerminalProvider>
      <TerminalPanel
        active
        id="t1"
        projectPath="/proj"
        scrollbackLines={1000}
        fontFamily="mono"
        fontSize={12}
        terminalTheme={theme}
        themeKey={themeKey}
      />
    </TerminalProvider>
  );
}

async function mountPanel(theme: ITheme | undefined, themeKey: string) {
  const utils = render(<Panel theme={theme} themeKey={themeKey} />);
  await act(async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
  });
  expect(FakeTerminal.instances.length).toBe(1);
  return { utils, term: FakeTerminal.instances[0] };
}

describe("TerminalPanel live theme", () => {
  it("constructs with the Ghostty theme and does not re-apply it on mount", async () => {
    const { term } = await mountPanel(GHOSTTY_THEME, "ghostty");
    expect(term.ctorOptions.theme).toEqual(GHOSTTY_THEME);
    expect(applyTerminalTheme).not.toHaveBeenCalled();
  });

  it("rebuilds the terminal when the theme changes", async () => {
    const { utils, term } = await mountPanel(GHOSTTY_THEME, "ghostty");
    const next: ITheme = { background: "#111111", foreground: "#eeeeee" };

    utils.rerender(<Panel theme={next} themeKey="other" />);
    await act(async () => {
      for (let i = 0; i < 5; i++) await Promise.resolve();
    });

    expect(applyTerminalTheme).toHaveBeenCalledTimes(1);
    expect(vi.mocked(applyTerminalTheme).mock.calls[0][1]).toEqual(next);
    // The wasm palette only changes on rebuild: serialize -> reset -> replay.
    expect(term.reset).toHaveBeenCalled();
    expect(term.write).toHaveBeenCalled();
  });

  it("resets to ocode's own palette when the theme is cleared (source switch)", async () => {
    const { utils } = await mountPanel(GHOSTTY_THEME, "ghostty");

    utils.rerender(<Panel theme={undefined} themeKey="default" />);
    await act(async () => {
      for (let i = 0; i < 5; i++) await Promise.resolve();
    });

    expect(applyTerminalTheme).toHaveBeenCalledTimes(1);
    expect(vi.mocked(applyTerminalTheme).mock.calls[0][1]).toEqual(DEFAULT_TERMINAL_THEME);
  });

  it("does nothing when the theme identity is unchanged", async () => {
    const same: ITheme = { background: "#000000", foreground: "#ffffff" };
    const { utils } = await mountPanel(same, "same");
    utils.rerender(<Panel theme={{ ...same }} themeKey="same" />);
    await act(async () => {
      for (let i = 0; i < 5; i++) await Promise.resolve();
    });
    expect(applyTerminalTheme).not.toHaveBeenCalled();
  });
});
