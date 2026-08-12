import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import LogPanel from "./LogPanel";

// LogPanel consumes live logs from the shared event bus (the single
// /api/events connection); the old per-panel EventSource is gone. Mock the
// bus module and drive `logs` envelopes through it.

const busHandlers = new Map<string, Set<(env: unknown) => void>>();

vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    on: (event: string, handler: (env: unknown) => void) => {
      let set = busHandlers.get(event);
      if (!set) {
        set = new Set();
        busHandlers.set(event, set);
      }
      set.add(handler);
      return () => {
        set.delete(handler);
      };
    },
    onReconnect: () => () => {},
  },
}));

function emitLog(message: string, kind = "TOOL") {
  act(() => {
    busHandlers.get("logs")?.forEach((h) =>
      h({ event: "logs", project: "", session_id: "", seq: 1, data: { kind, message } }),
    );
  });
}

/** Give the scroll container controllable metrics (jsdom has no layout). */
function makeScrollable(el: HTMLElement, scrollHeight: number, clientHeight: number) {
  let top = 0;
  Object.defineProperty(el, "scrollHeight", { configurable: true, value: scrollHeight });
  Object.defineProperty(el, "clientHeight", { configurable: true, value: clientHeight });
  Object.defineProperty(el, "scrollTop", {
    configurable: true,
    get: () => top,
    set: (v: number) => {
      top = v;
    },
  });
  return {
    setScrollHeight: (h: number) => {
      Object.defineProperty(el, "scrollHeight", { configurable: true, value: h });
    },
  };
}

function getScroller(container: HTMLElement): HTMLElement {
  const el = container.querySelector<HTMLElement>(".overflow-y-auto");
  if (!el) throw new Error("scroller not found");
  return el;
}

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [],
    }),
  );
  busHandlers.clear();
  vi.stubGlobal("requestAnimationFrame", (cb: () => void) => {
    cb();
    return 1;
  });
  vi.stubGlobal("cancelAnimationFrame", () => {});
  // scrollTo is used by the "jump to bottom" button; make it a no-op spy.
  Element.prototype.scrollTo = vi.fn() as unknown as typeof Element.prototype.scrollTo;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("LogPanel scroll behavior", () => {
  it("jumps to the bottom the first time the tab is opened", async () => {
    const { rerender, container } = render(<LogPanel active={false} sessionId="test-session" />);
    await act(async () => {}); // flush the initial logs fetch
    const scroller = getScroller(container);
    makeScrollable(scroller, 1000, 100);

    act(() => rerender(<LogPanel active={true} sessionId="test-session" />));

    expect(scroller.scrollTop).toBe(1000);
  });

  it("retains the scroll position when the user scrolled up and new logs arrive", async () => {
    const { rerender, container } = render(<LogPanel active={false} sessionId="test-session" />);
    await act(async () => {});
    const scroller = getScroller(container);
    makeScrollable(scroller, 1000, 100);

    act(() => rerender(<LogPanel active={true} sessionId="test-session" />)); // first open -> bottom
    expect(scroller.scrollTop).toBe(1000);

    // User scrolls up to read older entries.
    scroller.scrollTop = 400;
    fireEvent.scroll(scroller);
    expect(scroller.scrollTop).toBe(400);

    // New logs must not yank the viewport.
    emitLog("new entry");
    expect(scroller.scrollTop).toBe(400);
  });

  it("follows new logs when the scrollbar is near the bottom", async () => {
    const { rerender, container } = render(<LogPanel active={false} sessionId="test-session" />);
    await act(async () => {});
    const scroller = getScroller(container);
    const { setScrollHeight } = makeScrollable(scroller, 1000, 100);

    act(() => rerender(<LogPanel active={true} sessionId="test-session" />));
    expect(scroller.scrollTop).toBe(1000);

    // Scroll to within 30px of the bottom (distance 20): still "near bottom".
    scroller.scrollTop = 880;
    fireEvent.scroll(scroller);

    // Content grows and a new log arrives: the viewport follows to the bottom.
    setScrollHeight(1200);
    emitLog("tail entry");
    expect(scroller.scrollTop).toBe(1200);
  });

  it("restores the saved position when the tab is re-opened after reading", async () => {
    const { rerender, container } = render(<LogPanel active={false} sessionId="test-session" />);
    await act(async () => {});
    const scroller = getScroller(container);
    makeScrollable(scroller, 1000, 100);

    act(() => rerender(<LogPanel active={true} sessionId="test-session" />)); // open #1 -> bottom
    expect(scroller.scrollTop).toBe(1000);

    // Read older logs: scroll up (disables auto-scroll, saves the position).
    scroller.scrollTop = 300;
    fireEvent.scroll(scroller);

    // Leave the tab. In a real browser display:none resets scrollTop to 0.
    act(() => rerender(<LogPanel active={false} sessionId="test-session" />));
    scroller.scrollTop = 0;

    // Re-open: the saved position is restored, not forced to the bottom.
    act(() => rerender(<LogPanel active={true} sessionId="test-session" />));
    expect(scroller.scrollTop).toBe(300);
  });

  it("catches up to the latest logs when re-opening while following", async () => {
    const { rerender, container } = render(<LogPanel active={false} sessionId="test-session" />);
    await act(async () => {});
    const scroller = getScroller(container);
    const { setScrollHeight } = makeScrollable(scroller, 1000, 100);

    act(() => rerender(<LogPanel active={true} sessionId="test-session" />)); // open #1 -> bottom (following)
    expect(scroller.scrollTop).toBe(1000);

    // Leave while pinned to the bottom, and let logs stream in while hidden.
    act(() => rerender(<LogPanel active={false} sessionId="test-session" />));
    scroller.scrollTop = 0;
    setScrollHeight(2000);
    emitLog("hidden entry");

    // Re-open: jumps to the new bottom to catch up.
    act(() => rerender(<LogPanel active={true} sessionId="test-session" />));
    expect(scroller.scrollTop).toBe(2000);
  });

  it("re-enabling auto-scroll from the toolbar jumps to the bottom", async () => {
    const { rerender, container } = render(<LogPanel active={false} sessionId="test-session" />);
    await act(async () => {});
    const scroller = getScroller(container);
    makeScrollable(scroller, 1000, 100);

    act(() => rerender(<LogPanel active={true} sessionId="test-session" />));

    // Disable auto-scroll, then scroll up.
    fireEvent.click(screen.getByTitle("Disable auto-scroll"));
    scroller.scrollTop = 300;
    fireEvent.scroll(scroller);
    expect(screen.getByTitle("Enable auto-scroll")).toBeInTheDocument();

    // Re-enable: smooth-scrolls to the bottom and resumes following.
    fireEvent.click(screen.getByTitle("Enable auto-scroll"));
    emitLog("after enable");
    expect(scroller.scrollTop).toBe(1000);
  });
});
