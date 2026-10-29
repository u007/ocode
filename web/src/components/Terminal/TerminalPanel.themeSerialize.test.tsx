import { render, act, cleanup } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import type { ITheme } from "ghostty-web";
import { TerminalProvider } from "../../stores/terminalStore";
import TerminalPanel from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";
import { serializeDefaultsFor } from "./terminalOptions";

// Regression coverage for "switching the terminal theme leaves the scrollback in
// the previous palette". A theme change rebuilds the wasm terminal by
// serializing the buffer and replaying it. The serializer recognises "default
// colour" cells by comparing them against a foreground/background pair, so it
// MUST be given the pair of the theme that painted the buffer — the already
// applied one. Handing it the requested theme's pair marks every plain cell as
// explicitly coloured, and the replay then pins the old RGB into the cells for
// good (they can never be recognised as default again).

const h = vi.hoisted(() => ({
  sockets: [] as Array<{ onopen: (() => void) | null; send: (data: string) => void }>,
  /** The defaults argument of every serializeTerminalBuffer call. */
  serialized: [] as Array<{ foreground: number; background: number }>,
}));

vi.mock("./terminalSerialize", () => ({
  serializeTerminalBuffer: (
    _term: unknown,
    defaults: { foreground: number; background: number },
  ) => {
    h.serialized.push(defaults);
    return "";
  },
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
    // TerminalProvider restores the shared tab list on mount.
    getTerminalTabs: () => Promise.resolve({ projects: {} }),
    setTerminalTabs: () => Promise.resolve({ ok: true }),
  },
  apiPath: (p: string) => p,
  apiWsPath: (p: string) => `ws://localhost${p}`,
  // The rebuild test switches to a remote host, so the WS URL helper runs.
  remoteApiBase: (h: string) => `/api/remote/${encodeURIComponent(h)}`,
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
  // TerminalProvider's one-time restore reads the local mirror first.
  readAllMirrorProjects: () => ({}),
  getProjectTerminals: () => [],
  projectTerminalsKey: (path: string, host?: string) => (host ? `${host}::${path}` : path),
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

const THEME_A: ITheme = { background: "#000000", foreground: "#b3c9d7" };
const THEME_B: ITheme = { background: "#111111", foreground: "#eeeeee" };

beforeEach(() => {
  FakeTerminal.instances.length = 0;
  h.sockets.length = 0;
  h.serialized.length = 0;
  cleanup();
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

function Panel({
  theme,
  themeKey,
  scrollbackLines,
  host,
}: {
  theme?: ITheme;
  themeKey: string;
  scrollbackLines: number;
  host?: string;
}) {
  return (
    <TerminalProvider>
      <TerminalPanel
        active
        id="t1"
        projectPath="/proj"
        scrollbackLines={scrollbackLines}
        fontFamily="mono"
        fontSize={12}
        terminalTheme={theme}
        themeKey={themeKey}
        host={host}
      />
    </TerminalProvider>
  );
}

async function flush() {
  await act(async () => {
    for (let i = 0; i < 10; i++) await Promise.resolve();
  });
}

async function mountPanel(theme: ITheme, themeKey: string) {
  const utils = render(<Panel theme={theme} themeKey={themeKey} scrollbackLines={1000} />);
  await flush();
  expect(FakeTerminal.instances.length).toBe(1);
  return utils;
}

describe("TerminalPanel theme serialization", () => {
  it("replays a theme change with the defaults of the theme that painted the buffer", async () => {
    const utils = await mountPanel(THEME_A, "a");
    h.serialized.length = 0;

    utils.rerender(<Panel theme={THEME_B} themeKey="b" scrollbackLines={1000} />);
    await flush();

    expect(h.serialized.length).toBeGreaterThan(0);
    // The buffer still holds THEME_A's cells at this point: comparing them with
    // THEME_B's pair would emit every plain cell as an explicit old colour.
    expect(h.serialized[0]).toEqual(serializeDefaultsFor(THEME_A));
  });

  it("uses the applied theme when a scrollback and a theme change land together", async () => {
    const utils = await mountPanel(THEME_A, "a");
    h.serialized.length = 0;

    // Both effects run in one commit, and the scrollback one runs first.
    utils.rerender(<Panel theme={THEME_B} themeKey="b" scrollbackLines={2000} />);
    await flush();

    expect(h.serialized.length).toBeGreaterThan(0);
    expect(h.serialized[0]).toEqual(serializeDefaultsFor(THEME_A));
  });

  it("advances to the new defaults once the switch has been applied", async () => {
    const utils = await mountPanel(THEME_A, "a");
    utils.rerender(<Panel theme={THEME_B} themeKey="b" scrollbackLines={1000} />);
    await flush();

    // Any later reserialize (here: a scrollback change) must compare against the
    // palette now baked into the terminal.
    h.serialized.length = 0;
    utils.rerender(<Panel theme={THEME_B} themeKey="b" scrollbackLines={3000} />);
    await flush();

    expect(h.serialized.length).toBeGreaterThan(0);
    expect(h.serialized[0]).toEqual(serializeDefaultsFor(THEME_B));
  });

  it("uses the theme a rebuilt terminal was constructed with", async () => {
    const utils = await mountPanel(THEME_A, "a");

    // A host switch rebuilds the terminal; this commit also changes the theme, so
    // the rebuild already paints B while the live effect sees that key as applied.
    utils.rerender(<Panel theme={THEME_B} themeKey="b" scrollbackLines={1000} host="u@h" />);
    await flush();
    h.serialized.length = 0;

    utils.rerender(<Panel theme={THEME_B} themeKey="b" scrollbackLines={2000} host="u@h" />);
    await flush();

    expect(h.serialized.length).toBeGreaterThan(0);
    expect(h.serialized[0]).toEqual(serializeDefaultsFor(THEME_B));
  });
});
