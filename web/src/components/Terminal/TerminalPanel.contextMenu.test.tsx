import { render, fireEvent, act, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import TerminalPanel from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";
import { requestSpeech } from "../Speech/SpeechProvider";

// Every right-click menu item, the find bar wiring, and the load-failure state of
// the ghostty-web port. The panel is mounted against the shared fake Terminal
// (ghosttyWebMock.ts); the assertions are on what the panel asks the terminal to do.

const h = vi.hoisted(() => ({
  sockets: [] as Array<{ onopen: (() => void) | null }>,
  openTerminal: vi.fn(),
  closeTerminal: vi.fn(),
  search: {
    findNext: vi.fn(),
    findPrevious: vi.fn(),
    clear: vi.fn(),
    invalidate: vi.fn(),
    dispose: vi.fn(),
    onDidChangeResults: vi.fn(() => ({ dispose: vi.fn() })),
  },
}));

vi.mock("ghostty-web", async () => (await import("./ghosttyWebMock")).ghosttyWebModule());
vi.mock("./ghosttyInternals", async () => (await import("./ghosttyWebMock")).ghosttyInternalsMock());
vi.mock("./terminalMouse", async () => (await import("./ghosttyWebMock")).terminalMouseMock());
vi.mock("./terminalSearch", () => ({ createTerminalSearch: () => h.search }));
vi.mock("../../stores/terminalStore", () => ({
  useTerminalState: () => ({
    markAlerted: vi.fn(),
    setOscTitle: vi.fn(),
    openTerminal: h.openTerminal,
    closeTerminal: h.closeTerminal,
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
vi.mock("./terminalAlertSound", () => ({ playAlertSound: vi.fn() }));
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
  onclose: (() => void) | null = null;
  send = vi.fn();
  close = vi.fn();
  constructor(public url: string) {
    h.sockets.push(this);
  }
}

const writeText = vi.fn<(text: string) => Promise<void>>(() => Promise.resolve());
const readText = vi.fn<() => Promise<string>>(() => Promise.resolve("from clipboard"));

beforeEach(() => {
  FakeTerminal.instances.length = 0;
  h.sockets.length = 0;
  h.openTerminal.mockClear();
  h.closeTerminal.mockClear();
  for (const fn of [h.search.findNext, h.search.findPrevious, h.search.clear, h.search.dispose]) fn.mockClear();
  vi.mocked(requestSpeech).mockClear();
  writeText.mockClear();
  readText.mockClear();
  Object.defineProperty(navigator, "clipboard", { value: { writeText, readText }, configurable: true, writable: true });
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

async function mountPanel(host?: string) {
  const utils = render(
    <TerminalPanel active id="t1" projectPath="/proj" host={host} scrollbackLines={1000} fontFamily="mono" fontSize={12} />,
  );
  await act(async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
  });
  expect(FakeTerminal.instances.length).toBe(1);
  return { ...utils, root: utils.container.firstElementChild as HTMLElement, term: FakeTerminal.instances[0] };
}

async function openMenu(root: HTMLElement) {
  fireEvent.contextMenu(root, { clientX: 20, clientY: 20 });
  expect(await screen.findByRole("menu")).toBeTruthy();
}

function clickItem(name: string) {
  fireEvent.click(screen.getByRole("menuitem", { name }));
}

describe("terminal context menu (right-click)", () => {
  it("renders every item", async () => {
    const { root } = await mountPanel();
    await openMenu(root);
    const labels = screen.getAllByRole("menuitem").map((b) => b.textContent);
    expect(labels).toEqual([
      "Copy",
      "Play selection",
      "Paste",
      "Select All",
      "Clear Terminal",
      "Reset Terminal",
      "Scroll to Top",
      "Scroll to Bottom",
      "Speak visible",
      "Find…",
      "New Terminal",
      "Close Terminal",
    ]);
  });

  it("Copy writes the selection and Play selection speaks it", async () => {
    const { root, term } = await mountPanel();
    term.selectionText = "picked text";
    fireEvent.contextMenu(root, { clientX: 20, clientY: 20 });
    clickItem("Copy");
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("picked text"));

    fireEvent.contextMenu(root, { clientX: 20, clientY: 20 });
    clickItem("Play selection");
    expect(requestSpeech).toHaveBeenCalledWith("picked text");
  });

  it("Paste reads the clipboard and feeds it through term.paste", async () => {
    const { root, term } = await mountPanel();
    await openMenu(root);
    clickItem("Paste");
    await waitFor(() => expect(term.paste).toHaveBeenCalledWith("from clipboard"));
  });

  it("Select All, Clear and the scroll items drive the terminal", async () => {
    const { root, term } = await mountPanel();
    for (const [item, spy] of [
      ["Select All", term.selectAll],
      ["Clear Terminal", term.clear],
      ["Scroll to Top", term.scrollToTop],
      ["Scroll to Bottom", term.scrollToBottom],
    ] as const) {
      await openMenu(root);
      clickItem(item);
      expect(spy, item).toHaveBeenCalledTimes(1);
    }
  });

  it("Reset Terminal writes RIS to the pty path instead of calling term.reset", async () => {
    const { root, term } = await mountPanel();
    await openMenu(root);
    clickItem("Reset Terminal");
    expect(term.reset).not.toHaveBeenCalled();
    expect(term.write).toHaveBeenCalledWith("\x1bc");
  });

  it("Speak visible reads the rows in view, derived from how far the viewport is scrolled back", async () => {
    const { root, term } = await mountPanel();
    // ghostty-web reports buffer.active.viewportY as 0 always; the panel must use
    // getViewportY(). 100 rows total, 24 visible, scrolled back 10 -> first row 66.
    term.buffer.active.length = 100;
    term.viewportY = 10;
    const asked: number[] = [];
    (term.buffer.active.getLine as unknown as { mockImplementation: (f: (y: number) => unknown) => void }).mockImplementation(
      (y: number) => {
        asked.push(y);
        return { translateToString: () => `row ${y}` };
      },
    );
    await openMenu(root);
    clickItem("Speak visible");
    expect(asked[0]).toBe(66);
    expect(asked[asked.length - 1]).toBe(89);
    expect(asked.length).toBe(24);
    expect(requestSpeech).toHaveBeenCalledWith(expect.stringContaining("row 66\nrow 67"));
  });

  it("Find… opens the find bar", async () => {
    const { root } = await mountPanel();
    await openMenu(root);
    clickItem("Find…");
    expect(await screen.findByRole("search")).toBeTruthy();
  });

  it("New Terminal and Close Terminal call the terminal store", async () => {
    const { root } = await mountPanel("dev@example.com");
    await openMenu(root);
    clickItem("New Terminal");
    expect(h.openTerminal).toHaveBeenCalledWith("/proj", "dev@example.com");
    await openMenu(root);
    clickItem("Close Terminal");
    expect(h.closeTerminal).toHaveBeenCalledWith("/proj", "t1", "dev@example.com");
  });
});

describe("terminal find", () => {
  function press(term: FakeTerminal, init: { key: string; ctrlKey?: boolean; metaKey?: boolean; shiftKey?: boolean }) {
    const e = new KeyboardEvent("keydown", { ...init, bubbles: true, cancelable: true });
    return term._customKeyHandler!(e);
  }

  it("Ctrl/Cmd+F is handled (not sent to the pty) and opens the find bar; Escape closes and clears it", async () => {
    const { term } = await mountPanel();
    let handled = false;
    act(() => {
      handled = press(term, { key: "f", ctrlKey: true });
    });
    expect(handled).toBe(true);
    expect(await screen.findByRole("search")).toBeTruthy();
    act(() => {
      handled = press(term, { key: "Escape" });
    });
    expect(handled).toBe(true);
    expect(h.search.clear).toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("search")).toBeNull());
  });

  it("typing searches incrementally without options, Enter advances, Shift+Enter retreats", async () => {
    const { root } = await mountPanel();
    await openMenu(root);
    clickItem("Find…");
    const input = await screen.findByPlaceholderText("Find…");
    fireEvent.change(input, { target: { value: "needle" } });
    expect(h.search.findNext).toHaveBeenLastCalledWith("needle");
    fireEvent.keyDown(input, { key: "Enter" });
    expect(h.search.findNext).toHaveBeenCalledTimes(2);
    fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
    expect(h.search.findPrevious).toHaveBeenLastCalledWith("needle");
    fireEvent.change(input, { target: { value: "" } });
    expect(h.search.clear).toHaveBeenCalled();
  });

  it("F3 / Ctrl+G navigate matches once a query exists, and are handled", async () => {
    const { root, term } = await mountPanel();
    await openMenu(root);
    clickItem("Find…");
    fireEvent.change(await screen.findByPlaceholderText("Find…"), { target: { value: "needle" } });
    h.search.findNext.mockClear();
    expect(press(term, { key: "F3" })).toBe(true);
    expect(h.search.findNext).toHaveBeenCalledWith("needle");
    expect(press(term, { key: "F3", shiftKey: true })).toBe(true);
    expect(h.search.findPrevious).toHaveBeenCalledWith("needle");
    expect(press(term, { key: "g", ctrlKey: true })).toBe(true);
  });
});

describe("selection ends on input (xterm parity; ghostty-web keeps it otherwise)", () => {
  function press(term: FakeTerminal, init: { key: string; ctrlKey?: boolean; metaKey?: boolean; shiftKey?: boolean }) {
    const e = new KeyboardEvent("keydown", { ...init, bubbles: true, cancelable: true });
    return term._customKeyHandler!(e);
  }

  it("a key that becomes pty input clears the terminal selection, so the next Ctrl+C is SIGINT again", async () => {
    const { term } = await mountPanel();
    term.selectionText = "whole screen";
    // With a selection, Ctrl+C is a copy (handled) and the selection stays.
    expect(press(term, { key: "c", ctrlKey: true })).toBe(true);
    expect(term.clearSelection).not.toHaveBeenCalled();
    // A plain key is input: the selection is cleared along with the snapshot.
    expect(press(term, { key: "a" })).toBe(false);
    expect(term.clearSelection).toHaveBeenCalledTimes(1);
    // Bare modifiers and Cmd-combos produce no input and leave it alone.
    press(term, { key: "Shift", shiftKey: true });
    press(term, { key: "k", metaKey: true });
    expect(term.clearSelection).toHaveBeenCalledTimes(1);
  });

  it("a paste clears the terminal selection before feeding the text in", async () => {
    const { term } = await mountPanel();
    term.selectionText = "selected";
    const dt = { getData: () => "pasted" } as unknown as DataTransfer;
    fireEvent.paste(term.element!, { clipboardData: dt });
    expect(term.clearSelection).toHaveBeenCalledTimes(1);
    expect(term.paste).toHaveBeenCalledWith("pasted");
  });
});

describe("context menu stays open across ghostty-web's own right-click side effects", () => {
  it("ignores a scroll from inside the terminal host but closes on a page scroll", async () => {
    const { root, term } = await mountPanel();
    await openMenu(root);
    // ghostty-web focuses its hidden textarea on contextmenu; the focus scrolls it.
    fireEvent.scroll(term.textarea!);
    expect(screen.queryByRole("menu")).toBeTruthy();
    fireEvent.scroll(document.body);
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
  });
});
