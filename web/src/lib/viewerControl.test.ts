import { act, renderHook } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ChatProvider, useChatDispatch } from "../stores/chatStore";
import type { Message } from "../api/types";
import {
  readViewerState,
  registerViewerController,
  resetViewerStates,
  setViewerDoc,
} from "./viewerContext";
import { applyViewerControl, parseViewerControls, useViewerControl } from "./viewerControl";

const directive = (json: object) => `VIEWER_CONTROL:${JSON.stringify(json)}`;
const toolMessage = (content: string): Message => ({ role: "tool", content });

describe("parseViewerControls", () => {
  let warn: ReturnType<typeof vi.spyOn>;
  beforeEach(() => {
    warn = vi.spyOn(console, "warn").mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("returns every directive in transcript order", () => {
    const out = parseViewerControls([
      `moved\n${directive({ id: "vc_1", action: "scroll", page: 3 })}`,
      `found ${directive({ id: "vc_2", action: "find", query: "install" })}`,
    ]);
    expect(out).toEqual([
      { id: "vc_1", action: "scroll", page: 3 },
      { id: "vc_2", action: "find", query: "install" },
    ]);
    expect(warn).not.toHaveBeenCalled();
  });

  it("keeps the first directive for a repeated id", () => {
    const out = parseViewerControls([
      directive({ id: "vc_1", action: "scroll", page: 3 }),
      directive({ id: "vc_1", action: "scroll", page: 9 }),
    ]);
    expect(out).toEqual([{ id: "vc_1", action: "scroll", page: 3 }]);
  });

  it("skips and logs malformed directives, keeping the valid ones", () => {
    const out = parseViewerControls([
      "VIEWER_CONTROL:{not json}",
      directive({ action: "scroll", page: 1 }),
      directive({ id: "vc_3", action: "explode" }),
      directive({ id: "vc_4", action: "find" }),
      directive({ id: "vc_5", action: "find_next" }),
    ]);
    expect(out).toEqual([{ id: "vc_5", action: "find_next" }]);
    expect(warn).toHaveBeenCalledTimes(4);
  });

  it("ignores content without the sentinel", () => {
    expect(parseViewerControls(["plain text", '{"id":"vc_1","action":"scroll"}'])).toEqual([]);
  });
});

describe("applyViewerControl", () => {
  beforeEach(() => {
    resetViewerStates();
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("fails with no preview open when no viewer is registered, and records it", () => {
    const outcome = applyViewerControl("s1", { id: "vc_1", action: "find_close" });
    expect(outcome).toEqual({ id: "vc_1", status: "failed", error: "no preview open" });
    expect(readViewerState("s1")?.control).toEqual(outcome);
  });

  it("fails when the viewer does not support the target", () => {
    registerViewerController("s1", { find: () => true });
    expect(applyViewerControl("s1", { id: "vc_2", action: "scroll", page: 2 })).toEqual({
      id: "vc_2",
      status: "failed",
      error: "not supported by this viewer",
    });
  });

  it("reports the message a handler throws", () => {
    registerViewerController("s1", {
      select: () => {
        throw new Error('no match for "xyz"');
      },
    });
    const outcome = applyViewerControl("s1", { id: "vc_3", action: "select", text: "xyz" });
    expect(outcome).toEqual({ id: "vc_3", status: "failed", error: 'no match for "xyz"' });
    expect(readViewerState("s1")?.control).toEqual(outcome);
  });

  it("applies a supported target, passing its fields through, and records success", () => {
    const scroll = vi.fn(() => true);
    registerViewerController("s1", { scroll });
    expect(applyViewerControl("s1", { id: "vc_4", action: "scroll", page: 3, direction: "down" })).toEqual({
      id: "vc_4",
      status: "applied",
    });
    expect(scroll).toHaveBeenCalledWith({ page: 3, direction: "down" });
    expect(readViewerState("s1")?.control).toEqual({ id: "vc_4", status: "applied" });
  });

  it("maps the find actions onto the find command", () => {
    const find = vi.fn(() => true);
    registerViewerController("s1", { find });
    applyViewerControl("s1", { id: "a", action: "find", query: "install" });
    applyViewerControl("s1", { id: "b", action: "find_next" });
    applyViewerControl("s1", { id: "c", action: "find_prev" });
    applyViewerControl("s1", { id: "d", action: "find_close" });
    expect(find.mock.calls).toEqual([
      [{ op: "open", query: "install" }],
      [{ op: "next" }],
      [{ op: "prev" }],
      [{ op: "close" }],
    ]);
  });

  it("returns the outcome without a record to patch", () => {
    setViewerDoc("s1", null);
    registerViewerController("s1", { scroll: () => true });
    expect(applyViewerControl("s1", { id: "vc_5", action: "scroll", line: 1 })).toEqual({ id: "vc_5", status: "applied" });
    expect(readViewerState("s1")).toBeNull();
  });
});

describe("useViewerControl", () => {
  beforeEach(() => {
    resetViewerStates();
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    setViewerDoc("s2", { path: "b.md", kind: "markdown" });
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  function wrapper({ children }: { children: ReactNode }) {
    return createElement(ChatProvider, null, children);
  }

  function mount(initial: string | null) {
    return renderHook(
      ({ sessionId }: { sessionId: string | null }) => {
        useViewerControl(sessionId);
        return useChatDispatch();
      },
      { wrapper, initialProps: { sessionId: initial } },
    );
  }

  it("seeds a session's existing directives without applying them, then applies only new ones once", () => {
    const scroll = vi.fn(() => true);
    registerViewerController("s1", { scroll });
    const { result, rerender } = mount(null);

    act(() => {
      result.current({
        type: "SET_MESSAGES",
        sessionId: "s1",
        messages: [toolMessage(directive({ id: "vc_old", action: "scroll", page: 1 }))],
      });
    });
    rerender({ sessionId: "s1" });
    expect(scroll).not.toHaveBeenCalled();

    act(() => {
      result.current({
        type: "ADD_MESSAGE",
        sessionId: "s1",
        message: toolMessage(directive({ id: "vc_new", action: "scroll", page: 3 })),
      });
    });
    expect(scroll).toHaveBeenCalledTimes(1);
    expect(scroll).toHaveBeenCalledWith({ page: 3 });
    expect(readViewerState("s1")?.control).toEqual({ id: "vc_new", status: "applied" });

    // A re-render with the same transcript applies nothing again.
    rerender({ sessionId: "s1" });
    expect(scroll).toHaveBeenCalledTimes(1);
  });

  it("re-seeds on a session change, so each session's old directives stay unapplied", () => {
    const scroll1 = vi.fn(() => true);
    const scroll2 = vi.fn(() => true);
    registerViewerController("s1", { scroll: scroll1 });
    registerViewerController("s2", { scroll: scroll2 });
    const { result, rerender } = mount(null);

    act(() => {
      result.current({
        type: "SET_MESSAGES",
        sessionId: "s1",
        messages: [toolMessage(directive({ id: "vc_a", action: "scroll", page: 1 }))],
      });
      result.current({
        type: "SET_MESSAGES",
        sessionId: "s2",
        messages: [toolMessage(directive({ id: "vc_b", action: "scroll", page: 2 }))],
      });
    });
    rerender({ sessionId: "s1" });
    rerender({ sessionId: "s2" });
    expect(scroll1).not.toHaveBeenCalled();
    expect(scroll2).not.toHaveBeenCalled();

    act(() => {
      result.current({
        type: "ADD_MESSAGE",
        sessionId: "s2",
        message: toolMessage(directive({ id: "vc_c", action: "scroll", page: 5 })),
      });
    });
    expect(scroll2).toHaveBeenCalledWith({ page: 5 });
    expect(scroll2).toHaveBeenCalledTimes(1);

    // Back to s1: its old directive is seeded again, not replayed.
    rerender({ sessionId: "s1" });
    expect(scroll1).not.toHaveBeenCalled();
  });

  it("does nothing without a session", () => {
    const scroll = vi.fn(() => true);
    registerViewerController("s1", { scroll });
    const { result } = mount(null);
    act(() => {
      result.current({
        type: "SET_MESSAGES",
        sessionId: "s1",
        messages: [toolMessage(directive({ id: "vc_x", action: "scroll", page: 1 }))],
      });
    });
    expect(scroll).not.toHaveBeenCalled();
  });
});
