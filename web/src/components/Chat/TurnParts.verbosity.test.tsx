import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ChatDisplayPolicy, ChatVerbosityConfig, ChatLatestThinkingOverride, ChatDisplayOverride } from "../../api/types";
import {
  ChatDisplayContext,
  type ChatDisplayContextValue,
  type ChatDisclosureStore,
} from "./chatDisplayContext";
import { NoticeGroupBlock, ThinkingBlock, ToolBlock } from "./TurnParts";
import { ChatDisplayTestProvider } from "./chatDisplayTestUtils";

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
  const withOverride = (value: ChatLatestThinkingOverride | undefined) => value ?? "preset";
  const base = {
    full: { older_thinking: "expanded", latest_thinking: "expanded", tool_calls: "expanded", tool_output: "expanded", notices: "expanded" },
    balanced: { older_thinking: "collapsed", latest_thinking: "expanded", tool_calls: "collapsed", tool_output: "expanded", notices: "expanded" },
    quiet: { older_thinking: "collapsed", latest_thinking: "auto", tool_calls: "collapsed", tool_output: "collapsed", notices: "collapsed" },
  }[preset];
  const policy: ChatDisplayPolicy = {
    older_thinking: (withOverride(overrides.older_thinking) === "preset" ? base.older_thinking : withOverride(overrides.older_thinking)) as ChatDisplayPolicy["older_thinking"],
    latest_thinking: (withOverride(overrides.latest_thinking) === "preset" ? base.latest_thinking : withOverride(overrides.latest_thinking)) as ChatDisplayPolicy["latest_thinking"],
    tool_calls: (withOverride(overrides.tool_calls) === "preset" ? base.tool_calls : withOverride(overrides.tool_calls)) as ChatDisplayPolicy["tool_calls"],
    tool_output: (withOverride(overrides.tool_output) === "preset" ? base.tool_output : withOverride(overrides.tool_output)) as ChatDisplayPolicy["tool_output"],
    notices: (withOverride(overrides.activity_notices) === "preset" ? base.notices : withOverride(overrides.activity_notices)) as ChatDisplayPolicy["notices"],
    status: "expanded",
  };
  return {
    config: {
      preset,
      overrides: {
        // Only latest_thinking accepts "auto"; the other categories stay
        // ChatDisplayOverride, so cast their shared helper's result back down.
        older_thinking: withOverride(overrides.older_thinking) as ChatDisplayOverride,
        latest_thinking: withOverride(overrides.latest_thinking),
        tool_calls: withOverride(overrides.tool_calls) as ChatDisplayOverride,
        tool_output: withOverride(overrides.tool_output) as ChatDisplayOverride,
        activity_notices: withOverride(overrides.activity_notices) as ChatDisplayOverride,
        plan_panel: "5",
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
        <ThinkingBlock text="older reasoning" blockKey="m1" isLatest={false} answerStarted={false} />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.queryByText("older reasoning")).not.toBeInTheDocument();

    rerender(
      <ChatDisplayContext.Provider value={displayValue("full", {}, store)}>
        <ThinkingBlock text="older reasoning" blockKey="m1" isLatest={false} answerStarted={false} />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.getByText("older reasoning")).toBeInTheDocument();
  });

  it("keeps the latest thinking expanded under Full and Balanced", () => {
    for (const preset of ["full", "balanced"] as const) {
      const store = makeStore();
      const { unmount } = render(
        <ChatDisplayContext.Provider value={displayValue(preset, {}, store)}>
          <ThinkingBlock text="latest reasoning" blockKey="m-latest" isLatest answerStarted />
        </ChatDisplayContext.Provider>,
      );
      expect(screen.getByText("latest reasoning")).toBeInTheDocument();
      unmount();
    }
  });

  it("under auto, opens the latest block with no answer and collapses it once the answer starts", () => {
    const store = makeStore();
    const value = displayValue("quiet", {}, store);
    const { rerender } = render(
      <ChatDisplayContext.Provider value={value}>
        <ThinkingBlock text="auto reasoning" blockKey="m-auto" isLatest answerStarted={false} />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.getByText("auto reasoning")).toBeInTheDocument();

    rerender(
      <ChatDisplayContext.Provider value={value}>
        <ThinkingBlock text="auto reasoning" blockKey="m-auto" isLatest answerStarted />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.queryByText("auto reasoning")).not.toBeInTheDocument();
  });

  it("under auto, a committed block whose answer already started renders collapsed", () => {
    render(
      <ChatDisplayContext.Provider value={displayValue("quiet", {}, makeStore())}>
        <ThinkingBlock text="committed reasoning" blockKey="m-committed" isLatest answerStarted />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.queryByText("committed reasoning")).not.toBeInTheDocument();
  });

  it("under auto, a block with only a tool call and no text stays open", () => {
    // A tool-call follow-up contributes no answer text, so the caller passes
    // answerStarted=false and the block keeps its open default.
    render(
      <ChatDisplayContext.Provider value={displayValue("quiet", {}, makeStore())}>
        <ThinkingBlock text="tool reasoning" blockKey="m-tool" isLatest answerStarted={false} />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.getByText("tool reasoning")).toBeInTheDocument();
  });

  it("keeps explicit expanded and collapsed latest overrides fixed whatever the answer state", () => {
    const expanded = displayValue("quiet", { latest_thinking: "expanded" }, makeStore());
    const collapsed = displayValue("full", { latest_thinking: "collapsed" }, makeStore());
    const { unmount } = render(
      <ChatDisplayContext.Provider value={expanded}>
        <ThinkingBlock text="fixed open" blockKey="m-fixed-open" isLatest answerStarted />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.getByText("fixed open")).toBeInTheDocument();
    unmount();

    render(
      <ChatDisplayContext.Provider value={collapsed}>
        <ThinkingBlock text="fixed closed" blockKey="m-fixed-closed" isLatest answerStarted={false} />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.queryByText("fixed closed")).not.toBeInTheDocument();
  });

  it("keeps a manual collapse of the latest block until a policy revision clears it", () => {
    const store = makeStore();
    const value = displayValue("quiet", {}, store);
    const { rerender } = render(
      <ChatDisplayContext.Provider value={value}>
        <ThinkingBlock text="latest manual reasoning" blockKey="m-latest-manual" isLatest answerStarted={false} />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.getByText("latest manual reasoning")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /🧠 Thinking/ }));
    expect(screen.queryByText("latest manual reasoning")).not.toBeInTheDocument();

    rerender(
      <ChatDisplayContext.Provider value={value}>
        <ThinkingBlock text="latest manual reasoning" blockKey="m-latest-manual" isLatest answerStarted={false} />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.queryByText("latest manual reasoning")).not.toBeInTheDocument();

    act(() => store.clear());
    expect(screen.getByText("latest manual reasoning")).toBeInTheDocument();
  });

  it("lets a manual choice win over the auto default across the answer flip", () => {
    const store = makeStore();
    const value = displayValue("quiet", {}, store);
    const { rerender } = render(
      <ChatDisplayContext.Provider value={value}>
        <ThinkingBlock text="manual auto reasoning" blockKey="m-auto-manual" isLatest answerStarted={false} />
      </ChatDisplayContext.Provider>,
    );
    // The user collapses the open block before the answer arrives...
    fireEvent.click(screen.getByRole("button", { name: /🧠 Thinking/ }));
    rerender(
      <ChatDisplayContext.Provider value={value}>
        <ThinkingBlock text="manual auto reasoning" blockKey="m-auto-manual" isLatest answerStarted />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.queryByText("manual auto reasoning")).not.toBeInTheDocument();

    // ...and re-opens it after the answer started. The flip must not override that.
    fireEvent.click(screen.getByRole("button", { name: /🧠 Thinking/ }));
    rerender(
      <ChatDisplayContext.Provider value={value}>
        <ThinkingBlock text="manual auto reasoning" blockKey="m-auto-manual" isLatest answerStarted />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.getByText("manual auto reasoning")).toBeInTheDocument();
  });

  it("keeps a manual disclosure choice across an unmount/remount", () => {
    const store = makeStore();
    const value = displayValue("quiet", {}, store);
    const { unmount } = render(
      <ChatDisplayContext.Provider value={value}>
        <ThinkingBlock text="manual reasoning" blockKey="m-manual" isLatest={false} answerStarted={false} />
      </ChatDisplayContext.Provider>,
    );
    fireEvent.click(screen.getByRole("button", { name: /🧠 Thinking/ }));
    expect(screen.getByText("manual reasoning")).toBeInTheDocument();
    unmount();

    render(
      <ChatDisplayContext.Provider value={value}>
        <ThinkingBlock text="manual reasoning" blockKey="m-manual" isLatest={false} answerStarted={false} />
      </ChatDisplayContext.Provider>,
    );
    expect(screen.getByText("manual reasoning")).toBeInTheDocument();
  });
});

describe("controlled tool disclosure", () => {
  it("collapses tool call details under the balanced preset but keeps the tool output", () => {
    // Uses the REAL resolver (not the local base-map fixture) so this pins the
    // shipped §9 matrix: balanced collapses tool_calls and expands tool_output.
    render(
      <ChatDisplayTestProvider preset="balanced">
        <ToolBlock
          tool="bash"
          // Longer than BASH_COMMAND_INLINE_BUDGET so the command gets its own
          // code block, which renders only while the call gate is open. (A short
          // command is shown in the header hint either way, so it cannot prove
          // the gate state.)
          command={JSON.stringify({
            command:
              "echo hello && echo world && echo again && echo more && echo yet-another-long-line",
          })}
          output={"one\ntwo"}
          callKey="call-balanced"
          outputKey="output-balanced"
        />
      </ChatDisplayTestProvider>,
    );

    // The command code block only renders while the call gate is open, so its
    // absence IS the collapsed assertion.
    const commandBlock = () => screen.queryByText(/^\$ echo hello/, { selector: "pre" });
    expect(commandBlock()).not.toBeInTheDocument();
    // …while the separate output gate stays open.
    expect(screen.getByText(/one/)).toBeInTheDocument();

    // Disclosure is still user-controlled: opening the call reveals it.
    fireEvent.click(screen.getByRole("button", { name: /🔧/ }));
    expect(commandBlock()).toBeInTheDocument();
  });

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
