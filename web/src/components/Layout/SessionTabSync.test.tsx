import { act, render } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { ChatProvider, useChatState } from "../../stores/chatStore";
import { ROUTABLE_EVENTS } from "../../lib/sessionEvents";
import SessionTabSync from "./SessionTabSync";

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

describe("SessionTabSync", () => {
  beforeEach(() => {
    subscribed.clear();
    tabsByProject = {};
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
    });
    expect(getByTestId("text").textContent).toBe("hello");
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

    act(() => {
      subscribed.get("text")?.({
        event: "text",
        session_id: "s2",
        seq: 1,
        data: { delta: "hello" },
      });
    });
    expect(getByTestId("text").textContent).toBe("hello");
  });
});
