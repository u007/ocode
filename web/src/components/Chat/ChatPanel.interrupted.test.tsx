/**
 * The interrupted-turn notice: when the server reports the session has SETTLED
 * on an unfinished turn (`slice.interrupted`, from GET /state), ChatPanel shows
 * one inline row at the end of the transcript with a Continue action.
 *
 * Contract pinned here:
 *  - the row renders with role="status" (informational, NOT role="alert") and a
 *    Continue button;
 *  - clicking Continue calls the wired handler with the session id and hides
 *    the row immediately;
 *  - the row is suppressed while `wasInterrupted` (the live user-Stop signal
 *    that blocks sending), a turn is active/streaming, an ask is pending, the
 *    transcript is empty, the tab is a draft (`new-*`), or the load failed.
 */
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup, waitFor, act } from "@testing-library/react";
import { useEffect } from "react";
import ChatPanel from "./ChatPanel";
import { ChatProvider, useChatDispatch, type ChatAction } from "../../stores/chatStore";
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
class ResizeObserverMock {
  constructor(private cb: (entries: unknown[]) => void) {}
  observe(el: Element) {
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
  disconnect() {}
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
  dispatchRef = null;
  dropPrefetchedSession("sess-int", undefined);
});

function detail(messages: Message[]): unknown {
  return {
    id: "sess-int",
    title: "t",
    created_at: "2026-09-21T10:00:00+08:00",
    updated_at: "2026-09-21T10:00:00+08:00",
    total: messages.length,
    messages,
  };
}

// The panel's dispatch, captured so a test can apply slice state AFTER the
// transcript load has settled (MERGE_SNAPSHOT overwrites pending ask state, so
// dispatching an ask before the merge would be clobbered).
let dispatchRef: ((a: ChatAction) => void) | null = null;

function DispatchCapture() {
  const dispatch = useChatDispatch();
  useEffect(() => {
    dispatchRef = dispatch;
    return () => {
      dispatchRef = null;
    };
  }, [dispatch]);
  return null;
}

function renderPanel(opts: { sessionId?: string; onContinue?: (sessionId: string) => void }) {
  return render(
    <ChatProvider>
      <ChatPanel sessionId={opts.sessionId ?? "sess-int"} onContinueInterrupted={opts.onContinue} />
      <DispatchCapture />
    </ChatProvider>,
  );
}

const NOTICE = "The previous reply was interrupted";

function dispatch(action: ChatAction) {
  act(() => dispatchRef?.(action));
}

describe("ChatPanel interrupted-turn notice", () => {
  it("renders the row with role=status and Continue; clicking calls the handler and hides it", async () => {
    vi.mocked(api.getSession).mockResolvedValueOnce(
      detail([{ role: "assistant", content: "working on it" }]) as never,
    );
    const onContinue = vi.fn();

    renderPanel({ onContinue });
    await screen.findByText("working on it");
    dispatch({ type: "SET_INTERRUPTED", sessionId: "sess-int", interrupted: true });

    const notice = await screen.findByText(NOTICE);
    // Informational, not an error: role=status (readers are not interrupted).
    expect(notice.closest('[role="status"]')).not.toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    expect(onContinue).toHaveBeenCalledWith("sess-int");
    await waitFor(() => expect(screen.queryByText(NOTICE)).toBeNull());
  });

  it("stays hidden while wasInterrupted (the live user-Stop signal) is set", async () => {
    vi.mocked(api.getSession).mockResolvedValueOnce(
      detail([{ role: "assistant", content: "stopped reply" }]) as never,
    );

    renderPanel({});
    await screen.findByText("stopped reply");
    dispatch({ type: "SET_INTERRUPTED", sessionId: "sess-int", interrupted: true });
    dispatch({ type: "SET_WAS_INTERRUPTED", sessionId: "sess-int", wasInterrupted: true });

    expect(screen.queryByText(NOTICE)).toBeNull();
  });

  it("stays hidden while a turn is active or streaming", async () => {
    vi.mocked(api.getSession).mockResolvedValueOnce(
      detail([{ role: "assistant", content: "thinking" }]) as never,
    );
    renderPanel({});
    await screen.findByText("thinking");
    dispatch({ type: "SET_INTERRUPTED", sessionId: "sess-int", interrupted: true });

    dispatch({ type: "SET_TURN_STATE", sessionId: "sess-int", turnActive: true });
    expect(screen.queryByText(NOTICE)).toBeNull();

    dispatch({ type: "SET_TURN_STATE", sessionId: "sess-int", turnActive: false });
    dispatch({ type: "SET_STREAMING", sessionId: "sess-int", isStreaming: true });
    expect(screen.queryByText(NOTICE)).toBeNull();
  });

  it("stays hidden while live streaming parts are present", async () => {
    vi.mocked(api.getSession).mockResolvedValueOnce(
      detail([{ role: "assistant", content: "streaming" }]) as never,
    );
    renderPanel({});
    await screen.findByText("streaming");
    dispatch({ type: "SET_INTERRUPTED", sessionId: "sess-int", interrupted: true });
    dispatch({ type: "LIVE_NOTICE", sessionId: "sess-int", text: "working…" });

    expect(screen.queryByText(NOTICE)).toBeNull();
  });

  it("stays hidden while a question or permission ask is pending", async () => {
    vi.mocked(api.getSession).mockResolvedValueOnce(
      detail([{ role: "assistant", content: "asked" }]) as never,
    );
    renderPanel({});
    await screen.findByText("asked");
    dispatch({ type: "SET_INTERRUPTED", sessionId: "sess-int", interrupted: true });

    dispatch({ type: "QUESTION_REQUEST", sessionId: "sess-int", question: { request_id: "q1", questions: [] } });
    expect(screen.queryByText(NOTICE)).toBeNull();

    dispatch({ type: "QUESTION_RESOLVED", sessionId: "sess-int", requestId: "q1" });
    dispatch({ type: "PERMISSION_REQUEST", sessionId: "sess-int", permission: { tool: "bash", request_id: "p1" } });
    expect(screen.queryByText(NOTICE)).toBeNull();
  });

  it("stays hidden for an empty transcript and for a draft (new-*) tab", async () => {
    vi.mocked(api.getSession).mockResolvedValueOnce(detail([]) as never);
    const first = renderPanel({});
    await screen.findByText("Start a conversation");
    dispatch({ type: "SET_INTERRUPTED", sessionId: "sess-int", interrupted: true });
    expect(screen.queryByText(NOTICE)).toBeNull();
    first.unmount();

    // A draft tab never fetches, so inject a transcript + the flag directly.
    const second = renderPanel({ sessionId: "new-1" });
    await waitFor(() => expect(dispatchRef).not.toBeNull());
    dispatch({
      type: "MERGE_SNAPSHOT",
      sessionId: "new-1",
      messages: [{ role: "assistant", content: "draft tab" } as Message],
      total: 1,
    });
    dispatch({ type: "SET_INTERRUPTED", sessionId: "new-1", interrupted: true });
    expect(screen.queryByText(NOTICE)).toBeNull();
    second.unmount();
  });

  it("stays hidden when the initial load failed", async () => {
    vi.mocked(api.getSession).mockRejectedValueOnce(new Error("remote connect failed: 502"));
    renderPanel({});
    await screen.findByRole("alert");
    dispatch({ type: "SET_INTERRUPTED", sessionId: "sess-int", interrupted: true });
    expect(screen.queryByText(NOTICE)).toBeNull();
  });
});
