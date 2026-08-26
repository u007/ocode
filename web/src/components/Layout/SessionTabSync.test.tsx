import { act, render } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { ChatProvider, useChatState } from "../../stores/chatStore";
import { RECONCILE_PAGE_SIZE, ROUTABLE_EVENTS, LIVE_DELTA_FLUSH_MS } from "../../lib/sessionEvents";
import SessionTabSync from "./SessionTabSync";

const mockGetSessionState = vi.fn();
const mockGetSession = vi.fn();
vi.mock("../../api/client", () => ({
  api: {
    getSessionState: (...a: unknown[]) => mockGetSessionState(...a),
    getSession: (...a: unknown[]) => mockGetSession(...a),
  },
}));

// SessionTabSync is the only place live chat events reach the store (see
// useChat.ts). This test guards against a regression where the component
// subscribed to the literal string "envelope" — the SSE *frame* name the
// server sets, never a value that appears in an envelope's `event` field
// (see eventBus.ts) — which silently dropped every live text/turn event and
// left the UI to update only via the slow reconcile/watchdog fallback.

let tabsByProject: Record<string, { id: string; title: string }[]> = {};

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({
    state: { tabsByProject },
    dispatch: vi.fn(),
  }),
}));

const subscribed = new Map<string, (env: unknown) => void>();
vi.mock("../../lib/eventBus", () => ({
  eventBus: {
    on: (event: string, handler: (env: unknown) => void) => {
      subscribed.set(event, handler);
      return () => subscribed.delete(event);
    },
    onReconnect: () => () => {},
  },
}));

function Probe({ sessionId }: { sessionId: string }) {
  const state = useChatState();
  const live = state.sessions[sessionId]?.live ?? [];
  return <div data-testid="text">{live.map((p) => ("text" in p ? p.text : "")).join("")}</div>;
}

function MessagesProbe({ sessionId }: { sessionId: string }) {
  const state = useChatState();
  const slice = state.sessions[sessionId];
  return (
    <div data-testid="messages">
      {`turnActive=${slice?.turnActive ?? false};count=${slice?.messages.length ?? 0};${slice?.messages
        .map((m) => m.content)
        .join("|")}`}
    </div>
  );
}

describe("SessionTabSync", () => {
  beforeEach(() => {
    subscribed.clear();
    tabsByProject = {};
    mockGetSessionState.mockReset();
    mockGetSession.mockReset();
    mockGetSessionState.mockResolvedValue({ bootstrap_stage: "ready", turn_active: false, last_seq: 1 });
    mockGetSession.mockResolvedValue({ messages: [], total: 0 });
  });

  it("subscribes to every real event type routeBusEnvelope handles, never the SSE frame name 'envelope'", () => {
    render(
      <ChatProvider>
        <SessionTabSync />
      </ChatProvider>,
    );
    expect(subscribed.has("envelope")).toBe(false);
    for (const event of ROUTABLE_EVENTS) {
      expect(subscribed.has(event)).toBe(true);
    }
    expect(subscribed.has("text")).toBe(true);
    expect(subscribed.has("turn_started")).toBe(true);
  });

  it("routes a live 'text' envelope into the session's store slice", () => {
    vi.useFakeTimers();
    tabsByProject = { "/proj": [{ id: "s1", title: "t" }] };
    const { getByTestId } = render(
      <ChatProvider>
        <SessionTabSync />
        <Probe sessionId="s1" />
      </ChatProvider>,
    );
    act(() => {
      subscribed.get("text")?.({
        event: "text",
        session_id: "s1",
        seq: 1,
        data: { delta: "hello" },
      });
      // Deltas are coalesced (see LIVE_DELTA_FLUSH_MS) instead of dispatched
      // per SSE frame — advance past the flush interval to observe it.
      vi.advanceTimersByTime(LIVE_DELTA_FLUSH_MS);
    });
    expect(getByTestId("text").textContent).toBe("hello");
    vi.useRealTimers();
  });

  it("routes events for a tab opened after the initial mount (regression: openSessionIdsRef must not be swapped for a new Set)", () => {
    // No tabs open yet at mount — matches the real app on startup.
    const { getByTestId, rerender } = render(
      <ChatProvider>
        <SessionTabSync />
        <Probe sessionId="s2" />
      </ChatProvider>,
    );

    // A new session tab opens after mount, triggering a re-render with an
    // updated tabsByProject — but the effect that installed the router only
    // runs once, so it must observe this via the same mutated Set instance.
    tabsByProject = { "/proj": [{ id: "s2", title: "t" }] };
    rerender(
      <ChatProvider>
        <SessionTabSync />
        <Probe sessionId="s2" />
      </ChatProvider>,
    );

    vi.useFakeTimers();
    act(() => {
      subscribed.get("text")?.({
        event: "text",
        session_id: "s2",
        seq: 1,
        data: { delta: "hello" },
      });
      vi.advanceTimersByTime(LIVE_DELTA_FLUSH_MS);
    });
    expect(getByTestId("text").textContent).toBe("hello");
    vi.useRealTimers();
  });

  it("load-time reconcile fetches state + transcript once restored tabs appear", async () => {
    tabsByProject = { "/proj": [{ id: "s1", title: "t" }] };
    render(
      <ChatProvider>
        <SessionTabSync />
      </ChatProvider>,
    );
    await act(async () => {}); // flush the reconcile promise chain

    expect(mockGetSessionState).toHaveBeenCalledTimes(1);
    expect(mockGetSessionState).toHaveBeenCalledWith("s1");
    expect(mockGetSession).toHaveBeenCalledWith("s1", { limit: RECONCILE_PAGE_SIZE });
  });

  it("refreshing mid-turn populates the transcript from disk despite turn_active=true", async () => {
    // The exact refresh-mid-turn sequence: fresh page (empty store) →
    // restored tab → load reconcile reports an active turn → the disk
    // snapshot must still populate the slice (nothing in memory is newer).
    tabsByProject = { "/proj": [{ id: "s1", title: "t" }] };
    mockGetSessionState.mockResolvedValue({
      bootstrap_stage: "ready",
      turn_active: true,
      last_seq: 99,
    });
    mockGetSession.mockResolvedValue({
      messages: [
        { role: "user", content: "earlier question" },
        { role: "assistant", content: "earlier answer" },
        { role: "user", content: "follow-up" },
      ],
      total: 3,
    });
    const { getByTestId } = render(
      <ChatProvider>
        <SessionTabSync />
        <MessagesProbe sessionId="s1" />
      </ChatProvider>,
    );
    await act(async () => {}); // flush the reconcile promise chain

    expect(getByTestId("messages").textContent).toBe(
      "turnActive=true;count=3;earlier question|earlier answer|follow-up",
    );
  });

  it("load-time reconcile runs once per page load, not on later tab changes", async () => {
    tabsByProject = { "/proj": [{ id: "s1", title: "t" }] };
    const view = render(
      <ChatProvider>
        <SessionTabSync />
      </ChatProvider>,
    );
    await act(async () => {});
    expect(mockGetSessionState).toHaveBeenCalledTimes(1);

    // Another tab opens after the load reconcile — it must not refetch.
    tabsByProject = { "/proj": [{ id: "s1", title: "t" }, { id: "s9", title: "n" }] };
    view.rerender(
      <ChatProvider>
        <SessionTabSync />
      </ChatProvider>,
    );
    await act(async () => {});
    expect(mockGetSessionState).toHaveBeenCalledTimes(1);
    expect(mockGetSessionState).not.toHaveBeenCalledWith("s9");
  });
});
