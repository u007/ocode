/**
 * ChatPanel alt+up / alt+down jump between the user's own messages.
 *
 * NOTE on jsdom limits: jsdom has no layout engine, so "did the transcript
 * actually scroll to the right pixel" cannot be asserted here. These tests
 * cover the logic and structure instead: the key binding fires, the cursor
 * arithmetic matches the TUI's, the readout is correct, the server index list
 * is fetched lazily and honoured, and the composer/find-bar guards hold. The
 * scroll itself reuses the find bar's already-tested
 * virtualizer.scrollToIndex path.
 */
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from "vitest";
import { render, screen, fireEvent, act, cleanup } from "@testing-library/react";
import { useLayoutEffect } from "react";
import ChatPanel from "./ChatPanel";
import { ChatProvider, useChatDispatch } from "../../stores/chatStore";
import type { Message } from "../../api/types";

const hoisted = vi.hoisted(() => ({
  resolve: { current: (() => {}) as (v: unknown) => void },
  projectDispatch: vi.fn(),
}));

vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    on: () => () => {},
    onReconnect: () => () => {},
  },
}));

vi.mock("../../api/client", () => ({
  api: {
    getSession: vi.fn(
      (_id: string, _opts?: { limit?: number; offset?: number }) =>
        new Promise<unknown>((res) => {
          hoisted.resolve.current = res as (v: unknown) => void;
        }),
    ),
    getChatVerbosityConfig: vi.fn(async () => ({
      preset: "full",
      overrides: {
        older_thinking: "preset",
        tool_calls: "preset",
        tool_output: "preset",
        activity_notices: "preset",
      },
    })),
    searchSession: vi.fn(() => new Promise<unknown>(() => {})),
    // The authoritative user-message index list. Each test sets the value it
    // wants; the default is a resolved empty list so a test that does not care
    // about the fetch does not trip an unhandled rejection.
    userMessages: vi.fn(async () => ({ total: 0, indices: [], truncated: false, scanned: 0 })),
    truncateSession: vi.fn().mockResolvedValue({}),
  },
}));

vi.mock("../../stores/projectStore", () => ({
  findTabForSession: () => undefined,
  useProjectDispatch: () => hoisted.projectDispatch,
}));

import { api } from "../../api/client";

// --- Layout shims so @tanstack/react-virtual can compute a window in jsdom.
const roInstances: ResizeObserverMock[] = [];
class ResizeObserverMock {
  el: Element | null = null;
  constructor(private cb: (entries: unknown[]) => void) {}
  observe(el: Element) {
    this.el = el;
    roInstances.push(this);
    const rect = (el as HTMLElement).getBoundingClientRect();
    this.cb([
      {
        target: el,
        contentRect: rect,
        borderBoxSize: [{ inlineSize: rect.width, blockSize: rect.height }],
      },
    ]);
  }
  unobserve() {}
  disconnect() {
    const i = roInstances.indexOf(this);
    if (i >= 0) roInstances.splice(i, 1);
  }
}

let originalGBCR: typeof HTMLElement.prototype.getBoundingClientRect;
let originalOffsetParent: PropertyDescriptor | undefined;
let originalOffsetHeight: PropertyDescriptor | undefined;
let originalOffsetTop: PropertyDescriptor | undefined;
let originalResizeObserver: unknown;

function makeRect(w: number, h: number): DOMRect {
  return {
    width: w,
    height: h,
    top: 0,
    left: 0,
    right: w,
    bottom: h,
    x: 0,
    y: 0,
    toJSON() {},
  } as DOMRect;
}

beforeAll(() => {
  originalResizeObserver = (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver;
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = ResizeObserverMock;
  originalGBCR = HTMLElement.prototype.getBoundingClientRect;
  HTMLElement.prototype.getBoundingClientRect = function (this: HTMLElement) {
    if (this.className && typeof this.className === "string" && this.className.includes("overflow-y-auto")) {
      return makeRect(400, 600);
    }
    return makeRect(400, 96);
  } as typeof HTMLElement.prototype.getBoundingClientRect;

  originalOffsetParent = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetParent");
  Object.defineProperty(HTMLElement.prototype, "offsetParent", {
    configurable: true,
    get() {
      return document.body;
    },
  });

  originalOffsetHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetHeight");
  Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    get() {
      return 96;
    },
  });

  originalOffsetTop = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetTop");
  Object.defineProperty(HTMLElement.prototype, "offsetTop", {
    configurable: true,
    get() {
      return 0;
    },
  });
});

afterAll(() => {
  HTMLElement.prototype.getBoundingClientRect = originalGBCR;
  if (originalOffsetParent) {
    Object.defineProperty(HTMLElement.prototype, "offsetParent", originalOffsetParent);
  }
  if (originalOffsetHeight) {
    Object.defineProperty(HTMLElement.prototype, "offsetHeight", originalOffsetHeight);
  }
  if (originalOffsetTop) {
    Object.defineProperty(HTMLElement.prototype, "offsetTop", originalOffsetTop);
  }
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = originalResizeObserver as never;
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  hoisted.resolve.current = (() => {}) as (v: unknown) => void;
  roInstances.length = 0;
  vi.mocked(api.userMessages).mockResolvedValue({
    total: 0,
    indices: [],
    truncated: false,
    scanned: 0,
  });
});

// --- Helpers ---------------------------------------------------------------

function mk(role: Message["role"], content: string): Message {
  return { role, content };
}

/** Seeds a session slice via a layout effect so it lands before ChatPanel's
 *  passive mount effect, which would otherwise issue a fetch. */
function LiveSeed({
  sessionId,
  messages,
  total,
}: {
  sessionId: string;
  messages?: Message[];
  total?: number;
}) {
  const dispatch = useChatDispatch();
  useLayoutEffect(() => {
    if (messages) {
      dispatch({
        type: "MERGE_SNAPSHOT",
        sessionId,
        messages,
        total: total ?? messages.length,
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return null;
}

async function tick() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Renders a panel with a settled transcript and returns nothing; the initial
 *  getSession is resolved with an empty page so the panel stops loading. */
async function mountPanel(sessionId: string, messages: Message[], total?: number) {
  render(
    <ChatProvider>
      <LiveSeed sessionId={sessionId} messages={messages} total={total} />
      <ChatPanel sessionId={sessionId} />
    </ChatProvider>,
  );
  await tick();
  act(() => {
    hoisted.resolve.current({ messages: [], total: 0, title: "" });
  });
  await tick();
}

function pressAlt(key: "ArrowUp" | "ArrowDown", target?: HTMLElement) {
  const el = target ?? document.body;
  fireEvent.keyDown(el, { key, altKey: true });
}

function indicator() {
  return screen.queryByTestId("user-jump-indicator");
}

// --- Tests -----------------------------------------------------------------

describe("ChatPanel alt+up/alt+down user-message jump", () => {
  it("shows no indicator until a jump key is pressed", async () => {
    await mountPanel("ses-jump-idle", [mk("user", "one"), mk("assistant", "reply")]);
    expect(indicator()).toBeNull();
  });

  it("does not fetch the index list on mount", async () => {
    // Most sessions are opened and read without anyone jumping, so the list
    // must not cost a request per session open.
    await mountPanel("ses-jump-lazy", [mk("user", "one"), mk("assistant", "reply")]);
    expect(api.userMessages).not.toHaveBeenCalled();
  });

  it("fetches the index list on the first jump and shows msg N/total", async () => {
    vi.mocked(api.userMessages).mockResolvedValue({
      total: 3,
      indices: [0, 2, 4],
      truncated: false,
      scanned: 6,
    });
    await mountPanel("ses-jump-readout", [
      mk("user", "one"),
      mk("assistant", "r1"),
      mk("user", "two"),
      mk("assistant", "r2"),
      mk("user", "three"),
      mk("assistant", "r3"),
    ]);

    pressAlt("ArrowUp");
    await tick();

    expect(api.userMessages).toHaveBeenCalledTimes(1);
    // First press enters at the NEWEST user message (index 4 = "msg 3/3").
    expect(indicator()).toHaveTextContent("msg 3/3");

    pressAlt("ArrowUp");
    await tick();
    expect(indicator()).toHaveTextContent("msg 2/3");

    pressAlt("ArrowUp");
    await tick();
    expect(indicator()).toHaveTextContent("msg 1/3");
  });

  it("walks forward again with alt+ArrowDown", async () => {
    vi.mocked(api.userMessages).mockResolvedValue({
      total: 3,
      indices: [0, 2, 4],
      truncated: false,
      scanned: 6,
    });
    await mountPanel("ses-jump-forward", [
      mk("user", "one"),
      mk("assistant", "r1"),
      mk("user", "two"),
      mk("assistant", "r2"),
      mk("user", "three"),
      mk("assistant", "r3"),
    ]);

    pressAlt("ArrowUp");
    await tick();
    pressAlt("ArrowUp");
    await tick();
    expect(indicator()).toHaveTextContent("msg 2/3");

    pressAlt("ArrowDown");
    await tick();
    expect(indicator()).toHaveTextContent("msg 3/3");
  });

  it("clamps at the oldest message instead of wrapping", async () => {
    vi.mocked(api.userMessages).mockResolvedValue({
      total: 2,
      indices: [0, 2],
      truncated: false,
      scanned: 4,
    });
    await mountPanel("ses-jump-clamp", [
      mk("user", "one"),
      mk("assistant", "r1"),
      mk("user", "two"),
      mk("assistant", "r2"),
    ]);

    pressAlt("ArrowUp");
    await tick();
    pressAlt("ArrowUp");
    await tick();
    expect(indicator()).toHaveTextContent("msg 1/2");

    // A third press is already at the oldest; wrapping would read "msg 2/2".
    pressAlt("ArrowUp");
    await tick();
    expect(indicator()).toHaveTextContent("msg 1/2");
  });

  it("ignores the keys while a text field has focus", async () => {
    vi.mocked(api.userMessages).mockResolvedValue({
      total: 2,
      indices: [0, 2],
      truncated: false,
      scanned: 4,
    });
    // The composer lives outside ChatPanel (App.tsx owns the layout), so the
    // guard is exercised with a stand-in text field rendered alongside it.
    // The point under test is the window-level listener's target check, not
    // which element happens to be the composer.
    render(
      <ChatProvider>
        <LiveSeed sessionId="ses-jump-guard" messages={[mk("user", "one"), mk("assistant", "r1"), mk("user", "two")]} />
        <ChatPanel sessionId="ses-jump-guard" />
        <input aria-label="stand-in text field" />
      </ChatProvider>,
    );
    await tick();
    act(() => {
      hoisted.resolve.current({ messages: [], total: 0, title: "" });
    });
    await tick();

    const input = screen.getByLabelText("stand-in text field");
    pressAlt("ArrowUp", input);
    await tick();
    expect(indicator()).toBeNull();
    expect(api.userMessages).not.toHaveBeenCalled();
  });

  it("ignores modified variants it does not own", async () => {
    await mountPanel("ses-jump-modifiers", [mk("user", "one"), mk("assistant", "r1")]);

    fireEvent.keyDown(document.body, { key: "ArrowUp" }); // plain: composer history
    await tick();
    fireEvent.keyDown(document.body, { key: "ArrowUp", altKey: true, shiftKey: true });
    await tick();
    fireEvent.keyDown(document.body, { key: "ArrowUp", altKey: true, ctrlKey: true });
    await tick();

    expect(indicator()).toBeNull();
  });

  it("falls back to the loaded window when the index list request fails", async () => {
    vi.mocked(api.userMessages).mockRejectedValue(new Error("offline"));
    await mountPanel("ses-jump-offline", [
      mk("user", "one"),
      mk("assistant", "r1"),
      mk("user", "two"),
      mk("assistant", "r2"),
    ]);

    // The feature must degrade to what is already on screen rather than going
    // dead: two user messages are loaded, so the readout still works.
    pressAlt("ArrowUp");
    await tick();
    expect(indicator()).toHaveTextContent("msg 2/2");

    pressAlt("ArrowUp");
    await tick();
    expect(indicator()).toHaveTextContent("msg 1/2");
  });

  it("ignores slash-command echoes so the count matches the TUI's", async () => {
    // No server list: the client-side fallback predicate runs instead.
    vi.mocked(api.userMessages).mockRejectedValue(new Error("offline"));
    await mountPanel("ses-jump-slash", [
      mk("user", "one"),
      mk("assistant", "r1"),
      mk("user", "/theme dark"),
      mk("assistant", "themed"),
      mk("user", "two"),
    ]);

    pressAlt("ArrowUp");
    await tick();
    // 2 countable messages ("one" and "two"), not 3.
    expect(indicator()).toHaveTextContent("msg 2/2");
  });

  it("does nothing for a session with no user messages", async () => {
    await mountPanel("ses-jump-empty", [mk("assistant", "hello"), mk("assistant", "world")]);
    pressAlt("ArrowUp");
    await tick();
    expect(indicator()).toBeNull();
  });

  it("clears the readout when the tab switches to another session", async () => {
    vi.mocked(api.userMessages).mockResolvedValue({
      total: 2,
      indices: [0, 2],
      truncated: false,
      scanned: 4,
    });
    const { rerender } = render(
      <ChatProvider>
        <LiveSeed sessionId="ses-switch-a" messages={[mk("user", "one"), mk("assistant", "r"), mk("user", "two")]} />
        <ChatPanel sessionId="ses-switch-a" />
      </ChatProvider>,
    );
    await tick();
    act(() => {
      hoisted.resolve.current({ messages: [], total: 0, title: "" });
    });
    await tick();

    pressAlt("ArrowUp");
    await tick();
    expect(indicator()).toHaveTextContent("msg 2/2");

    rerender(
      <ChatProvider>
        <LiveSeed sessionId="ses-switch-b" messages={[mk("user", "fresh")]} />
        <ChatPanel sessionId="ses-switch-b" />
      </ChatProvider>,
    );
    await tick();
    // The cursor was a position in session A's list; carrying it over would
    // describe a message that does not exist in session B.
    expect(indicator()).toBeNull();
  });
});
