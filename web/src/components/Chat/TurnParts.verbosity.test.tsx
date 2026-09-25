import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ChatDisplayPolicy, ChatVerbosityConfig, ChatDisplayOverride } from "../../api/types";
import {
  ChatDisplayContext,
  type ChatDisplayContextValue,
  type ChatDisclosureStore,
} from "./chatDisplayContext";
import { NoticeGroupBlock, ThinkingBlock, ToolBlock } from "./TurnParts";

function makeStore(): ChatDisclosureStore {
  const values = new Map<string, boolean>();
  const listeners = new Map<string, Set<() => void>>();
  return {
    get: (key, fallback) => (values.has(key) ? values.get(key)! : fallback),
    set: (key, open) => {
      values.set(key, open);
      listeners.get(key)?.forEach((listener) => listener());
    },
    subscribe: (key, listener) => {
      const set = listeners.get(key) ?? new Set<() => void>();
      set.add(listener);
      listeners.set(key, set);
      return () => set.delete(listener);
    },
    clear: () => {
      values.clear();
      listeners.forEach((set) => set.forEach((listener) => listener()));
    },
  };
}

function displayValue(
  preset: ChatVerbosityConfig["preset"],
  overrides: Partial<ChatVerbosityConfig["overrides"]> = {},
  disclosure: ChatDisclosureStore = makeStore(),
): ChatDisplayContextValue {
  const withOverride = (value: ChatDisplayOverride | undefined) => value ?? "preset";
  const base = {
    full: { older_thinking: "expanded", tool_calls: "expanded", tool_output: "expanded", notices: "expanded" },
    balanced: { older_thinking: "collapsed", tool_calls: "expanded", tool_output: "expanded", notices: "expanded" },
    quiet: { older_thinking: "collapsed", tool_calls: "collapsed", tool_output: "collapsed", notices: "collapsed" },
  }[preset];
  const policy: ChatDisplayPolicy = {
    older_thinking: (withOverride(overrides.older_thinking) === "preset" ? base.older_thinking : withOverride(overrides.older_thinking)) as ChatDisplayPolicy["older_thinking"],
    latest_thinking: "expanded",
    tool_calls: (withOverride(overrides.tool_calls) === "preset" ? base.tool_calls : withOverride(overrides.tool_calls)) as ChatDisplayPolicy["tool_calls"],
    tool_output: (withOverride(overrides.tool_output) === "preset" ? base.tool_output : withOverride(overrides.tool_output)) as ChatDisplayPolicy["tool_output"],
    notices: (withOverride(overrides.activity_notices) === "preset" ? base.notices : withOverride(overrides.activity_notices)) as ChatDisplayPolicy["notices"],
    status: "expanded",
  };
  return {
    config: {
      preset,
      overrides: {
        older_thinking: withOverride(overrides.older_thinking),
        tool_calls: withOverride(overrides.tool_calls),
        tool_output: withOverride(overrides.tool_output),
        activity_notices: withOverride(overrides.activity_notices),
      },
    },
    policy,
    disclosure,
  };
}

describe("controlled thinking disclosure", () => {
  it("shows older thinking only when the policy expands it", () => {
    const store = makeStore();
    const { rerender } = render(
      <ChatDisplayContext.Provider value={displayValue("balanced", {}, store)}>
        <ThinkingBlock text="older reasoning" blockKey="m1" isLatest={false} />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.queryByText("older reasoning")).not.toBeInTheDocument();

    rerender(
      <ChatDisplayContext.Provider value={displayValue("full", {}, store)}>
        <ThinkingBlock text="older reasoning" blockKey="m1" isLatest={false} />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.getByText("older reasoning")).toBeInTheDocument();
  });

  it("keeps the latest thinking expanded in every preset including Quiet", () => {
    for (const preset of ["full", "balanced", "quiet"] as const) {
      const store = makeStore();
      const { unmount } = render(
        <ChatDisplayContext.Provider value={displayValue(preset, {}, store)}>
          <ThinkingBlock text="latest reasoning" blockKey="m-latest" isLatest />
        </ChatDisplayContext.Provider>,
      );
      expect(screen.getByText("latest reasoning")).toBeInTheDocument();
      unmount();
    }
  });

  it("keeps a manual disclosure choice across an unmount/remount", () => {
    const store = makeStore();
    const value = displayValue("quiet", {}, store);
    const { unmount } = render(
      <ChatDisplayContext.Provider value={value}>
        <ThinkingBlock text="manual reasoning" blockKey="m-manual" isLatest={false} />
      </ChatDisplayContext.Provider>,
    );
    fireEvent.click(screen.getByRole("button", { name: /🧠 Thinking/ }));
    expect(screen.getByText("manual reasoning")).toBeInTheDocument();
    unmount();

    render(
      <ChatDisplayContext.Provider value={value}>
        <ThinkingBlock text="manual reasoning" blockKey="m-manual" isLatest={false} />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.getByText("manual reasoning")).toBeInTheDocument();
  });
});

describe("controlled tool disclosure", () => {
  it("keeps tool call details and tool output as independent gates", () => {
    const store = makeStore();
    render(
      <ChatDisplayContext.Provider
        value={displayValue("quiet", { tool_calls: "expanded", tool_output: "collapsed" }, store)}
      >
        <ToolBlock
          tool="bash"
          command={'{"command":"echo hello"}'}
          output={"one\ntwo"}
          callKey="call-1"
          outputKey="output-1"
        />
      </ChatDisplayContext.Provider>,
    );

    expect(screen.getByText(/\$ echo hello/)).toBeInTheDocument();
    expect(screen.queryByText(/one\s+two/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /show output/i }));
    expect(screen.getByText(/one/)).toBeInTheDocument();
    expect(screen.getByText(/two/)).toBeInTheDocument();
  });

  it("forces a search-hit block open without discarding the manual choice", () => {
    const store = makeStore();
    const value = displayValue("quiet", {}, store);
    const { rerender } = render(
      <ChatDisplayContext.Provider value={value}>
        <ToolBlock tool="grep" command={'{"pattern":"needle"}'} output="MATCH" callKey="call-2" outputKey="output-2" />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.queryByText("MATCH")).not.toBeInTheDocument();

    rerender(
      <ChatDisplayContext.Provider value={value}>
        <ToolBlock
          tool="grep"
          command={'{"pattern":"needle"}'}
          output="MATCH"
          callKey="call-2"
          outputKey="output-2"
          forceOpen
        />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.getByText("MATCH")).toBeInTheDocument();

    rerender(
      <ChatDisplayContext.Provider value={value}>
        <ToolBlock tool="grep" command={'{"pattern":"needle"}'} output="MATCH" callKey="call-2" outputKey="output-2" />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.queryByText("MATCH")).not.toBeInTheDocument();
  });
});

describe("controlled notice grouping", () => {
  it("keeps a run collapsed behind one disclosure and remembers the manual open", () => {
    const store = makeStore();
    const value = displayValue("quiet", {}, store);
    const { unmount } = render(
      <ChatDisplayContext.Provider value={value}>
        <NoticeGroupBlock notices={["Indexing: a", "Indexing: b"]} groupKey="live:0:notices" />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.getByText("2 activity notices")).toBeInTheDocument();
    expect(screen.queryByText("Indexing: a")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /2 activity notices/ }));
    expect(screen.getByText("Indexing: a")).toBeInTheDocument();
    expect(screen.getByText("Indexing: b")).toBeInTheDocument();
    unmount();

    render(
      <ChatDisplayContext.Provider value={value}>
        <NoticeGroupBlock notices={["Indexing: a", "Indexing: b"]} groupKey="live:0:notices" />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.getByText("Indexing: a")).toBeInTheDocument();
  });
});
