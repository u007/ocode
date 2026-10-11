import { act, render } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { ChatProvider, useChatState } from "../../stores/chatStore";
import { ROUTABLE_EVENTS, LIVE_DELTA_FLUSH_MS } from "../../lib/sessionEvents";
import { getCompactionState, noteCompactionFinishedGeneration, noteCompactionGeneration, resetCompactionGenerations } from "../../lib/compactionState";
import SessionTabSync from "./SessionTabSync";

const mockGetSessionState = vi.fn();
const mockGetSession = vi.fn();
vi.mock("../../api/client", () => ({
  api: {
    getSessionState: (...a: unknown[]) => mockGetSessionState(...a),
    getSession: (...a: unknown[]) => mockGetSession(...a),
  },
  apiPath: (p: string) => p,
}));

// SessionTabSync is the only place live chat events reach the store (see
// useChat.ts). This test guards against a regression where the component
// subscribed to the literal string "envelope" — the SSE *frame* name the
// server sets, never a value that appears in an envelope's `event` field
// (see eventBus.ts) — which silently dropped every live text/turn event and
// left the UI to update only via the slow reconcile/watchdog fallback.

let tabsByProject: Record<string, { id: string; title: string }[]> = {};

vi.mock("../../stores/projectStore", () => ({
  findProjectPathForTab: () => undefined,
  // resolveSessionHost (via useSessionHost) calls this directly, so the REAL
  // implementation runs against the stub state below.
  findTabForSession: () => undefined,
  useProjectState: () => ({
    state: { tabsByProject },
    dispatch: vi.fn(),
  }),
}));

const bus = vi.hoisted(() => ({
  subscribed: new Map<string, (env: unknown) => void>(),
  reconnectHandler: undefined as (() => void) | undefined,
}));
vi.mock("../../lib/eventBus", () => ({
  eventBus: {
    on: (event: string, handler: (env: unknown) => void) => {
      bus.subscribed.set(event, handler);
      return () => bus.subscribed.delete(event);
    },
    onReconnect: (handler: () => void) => {
      bus.reconnectHandler = handler;
      return () => { bus.reconnectHandler = undefined; };
    },
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
    bus.subscribed.clear();
    bus.reconnectHandler = undefined;
    resetCompactionGenerations();
    tabsByProject = {};
    mockGetSessionState.mockReset();
    mockGetSession.mockReset();
    mockGetSessionState.mockResolvedValue({ bootstrap_stage: "ready", turn_active: false, last_seq: 1 });
    mockGetSession.mockResolvedValue({ messages: [], total: 0 });
  });

  it("resets compaction generations when the SSE stream reconnects", () => {
    tabsByProject = { "/proj": [{ id: "s1", title: "t" }] };
    noteCompactionGeneration("s1", 5);
    noteCompactionFinishedGeneration("s1", 5);
    render(
      <ChatProvider>
        <SessionTabSync />
      </ChatProvider>,
    );
    expect(bus.reconnectHandler).toBeDefined();
    act(() => { bus.reconnectHandler?.(); });
    act(() => {
      bus.subscribed.get("compaction_started")?.({
        event: "compaction_started",
        project: "/proj",
        session_id: "s1",
        seq: 1,
        data: { started_at: "2026-09-25T16:00:00Z", generation: 1 },
      });
    });
    expect(getCompactionState("s1")).toMatchObject({ status: "active" });
  });

  it("subscribes to every real event type routeBusEnvelope handles, never the SSE frame name 'envelope'", () => {
    render(
      <ChatProvider>
        <SessionTabSync />
      </ChatProvider>,
    );
    expect(bus.subscribed.has("envelope")).toBe(false);
    for (const event of ROUTABLE_EVENTS) {
      expect(bus.subscribed.has(event)).toBe(true);
    }
    expect(bus.subscribed.has("text")).toBe(true);
    expect(bus.subscribed.has("turn_started")).toBe(true);
    // Explicit pin (beyond the ROUTABLE_EVENTS loop above, which would silently
    // stop checking it if the event were dropped from the set): the headless
    // agent-loop activity feed only reaches the status bar if the SSE transport
    // actually subscribes to it.
    expect(bus.subscribed.has("agent_activity")).toBe(true);
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
      bus.subscribed.get("text")?.({
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
      bus.subscribed.get("text")?.({
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

  it("load-time reconcile fetches state only for restored tabs nobody has opened yet", async () => {
    // Lazy tab hydration: a restored tab's transcript is fetched by its
    // ChatPanel on first activation, never by the boot reconcile — with a
    // dozen restored tabs that was a dozen transcript pages parsed and
    // rendered before the user could click anything.
    tabsByProject = { "/proj": [{ id: "s1", title: "t" }] };
    render(
      <ChatProvider>
        <SessionTabSync />
      </ChatProvider>,
    );
    await act(async () => {}); // flush the reconcile promise chain

    expect(mockGetSessionState).toHaveBeenCalledTimes(1);
    expect(mockGetSessionState).toHaveBeenCalledWith("s1", undefined);
    expect(mockGetSession).not.toHaveBeenCalled();
  });

  it("refreshing mid-turn arms the running state from disk state without a transcript fetch", async () => {
    // Fresh page (empty store) → restored, never-opened tab → the load
    // reconcile reports an active turn: the slice must show the turn running
    // while its transcript waits for the tab's first activation (ChatPanel).
    tabsByProject = { "/proj": [{ id: "s1", title: "t" }] };
    mockGetSessionState.mockResolvedValue({
      bootstrap_stage: "ready",
      turn_active: true,
      last_seq: 99,
    });
    const { getByTestId } = render(
      <ChatProvider>
        <SessionTabSync />
        <MessagesProbe sessionId="s1" />
      </ChatProvider>,
    );
    await act(async () => {}); // flush the reconcile promise chain

    expect(mockGetSession).not.toHaveBeenCalled();
    expect(getByTestId("messages").textContent).toBe("turnActive=true;count=0;");
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
