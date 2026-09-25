/**
 * Regression: a FAILED initial transcript load used to render as the empty
 * "Start a conversation" placeholder — indistinguishable from a genuinely
 * empty session, i.e. it looked like the whole conversation had been wiped.
 * Reported live: after a desktop reload the tab showed only the composer while
 * the transcript was intact server-side (the remote session fetch had failed).
 *
 * Contract pinned here:
 *  - a rejected initial load surfaces an alert with the reason, never the
 *    empty-state placeholder;
 *  - Retry re-fetches, and a successful retry restores the transcript and
 *    clears the error.
 */
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup, waitFor } from "@testing-library/react";
import ChatPanel from "./ChatPanel";
import { ChatProvider } from "../../stores/chatStore";
import { dropPrefetchedSession } from "../../lib/sessionPrefetch";
import type { Message } from "../../api/types";

const hoisted = vi.hoisted(() => ({ projectDispatch: vi.fn() }));
vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    on: () => () => {},
    onReconnect: () => () => {},
  },
}));

vi.mock("../../api/client", () => ({
  api: {
    getSession: vi.fn(),
    // ChatPanel mounts the shared chat-display policy hook; without this the
    // store rejects with "not a function" as an unhandled rejection.
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
  },
}));
vi.mock("../../stores/projectStore", () => ({
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
    const rect = makeRect(400, 96);
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

let originalGBCR: typeof HTMLElement.prototype.getBoundingClientRect;
let originalOffsetParent: PropertyDescriptor | undefined;
let originalOffsetHeight: PropertyDescriptor | undefined;
let originalResizeObserver: unknown;

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
});

afterAll(() => {
  HTMLElement.prototype.getBoundingClientRect = originalGBCR;
  if (originalOffsetParent) {
    Object.defineProperty(HTMLElement.prototype, "offsetParent", originalOffsetParent);
  }
  if (originalOffsetHeight) {
    Object.defineProperty(HTMLElement.prototype, "offsetHeight", originalOffsetHeight);
  }
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = originalResizeObserver as never;
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  roInstances.length = 0;
  dropPrefetchedSession("sess-loadfail", undefined);
  dropPrefetchedSession("sess-loadfail-remote", "james@example.test");
});

function detail(messages: Message[]): unknown {
  return {
    id: "sess-loadfail",
    title: "t",
    created_at: "2026-09-21T10:00:00+08:00",
    updated_at: "2026-09-21T10:00:00+08:00",
    total: messages.length,
    messages,
  };
}

describe("ChatPanel initial-load failure", () => {
  it("shows an alert with Retry instead of the empty placeholder, and recovers on Retry", async () => {
    const getSession = vi.mocked(api.getSession);
    getSession.mockRejectedValueOnce(new Error("remote connect failed: 502"));

    render(
      <ChatProvider>
        <ChatPanel sessionId="sess-loadfail-remote" host="james@example.test" />
      </ChatProvider>,
    );

    // The failure is surfaced — and explicitly NOT the empty-conversation state.
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Couldn't load this conversation");
    expect(alert.textContent).toContain("remote connect failed: 502");
    expect(screen.queryByText("Start a conversation")).toBeNull();
    expect(getSession).toHaveBeenCalledTimes(1);

    // Retry re-fetches; the recovered transcript renders and the alert clears.
    getSession.mockResolvedValueOnce(detail([{ role: "assistant", content: "recovered transcript" }]) as never);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(getSession).toHaveBeenCalledTimes(2));
    await screen.findByText("recovered transcript");
    expect(screen.queryByText("Couldn't load this conversation")).toBeNull();
  });

  it("keeps the empty placeholder for a genuinely empty session", async () => {
    const getSession = vi.mocked(api.getSession);
    getSession.mockResolvedValueOnce(detail([]) as never);

    render(
      <ChatProvider>
        <ChatPanel sessionId="sess-loadfail" />
      </ChatProvider>,
    );

    await screen.findByText("Start a conversation");
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
