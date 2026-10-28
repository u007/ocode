import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import RunNode from "./RunNode";
import type { AgentRun, AgentRunMessage } from "../../api/types";

const baseRun: AgentRun = {
  id: "run-1",
  name: "code-reviewer",
  status: "done",
  model: "sonnet-5",
  startedAt: "2026-08-06T10:00:00.000Z",
  endedAt: "2026-08-06T10:00:02.000Z",
  inputTokens: 10,
  outputTokens: 20,
  messages: [{ role: "assistant", content: "looks good" }],
  children: [],
};

describe("RunNode", () => {
  it("renders the run name, status, and toggles messages on row click", () => {
    render(<RunNode run={baseRun} depth={0} />);
    expect(screen.getByText("code-reviewer")).toBeInTheDocument();
    expect(screen.getByText("done")).toBeInTheDocument();
    expect(screen.getByText("looks good")).toBeInTheDocument();

    fireEvent.click(screen.getByText("code-reviewer"));
    expect(screen.queryByText("looks good")).not.toBeInTheDocument();
  });

  it("renders nested child runs recursively", () => {
    const withChild: AgentRun = {
      ...baseRun,
      children: [{ ...baseRun, id: "run-2", name: "sub-agent" }],
    };
    render(<RunNode run={withChild} depth={0} />);
    expect(screen.getByText("sub-agent")).toBeInTheDocument();
  });

  it("calls onOpenDetail when the name is clicked, without toggling the row", () => {
    const onOpenDetail = vi.fn();
    render(<RunNode run={baseRun} depth={0} onOpenDetail={onOpenDetail} />);

    fireEvent.click(screen.getByText("code-reviewer"));

    expect(onOpenDetail).toHaveBeenCalledWith("run-1");
    // row click was not triggered, so messages stay visible (still open)
    expect(screen.getByText("looks good")).toBeInTheDocument();
  });

  it("starts collapsed with defaultOpen={false} and reveals messages on row click", () => {
    render(<RunNode run={baseRun} depth={0} defaultOpen={false} />);
    expect(screen.getByText("code-reviewer")).toBeInTheDocument();
    expect(screen.queryByText("looks good")).not.toBeInTheDocument();

    fireEvent.click(screen.getByText("code-reviewer"));
    expect(screen.getByText("looks good")).toBeInTheDocument();
  });

  it("propagates defaultOpen={false} to nested child runs", () => {
    const child: AgentRun = {
      ...baseRun,
      id: "run-2",
      name: "sub-agent",
      messages: [{ role: "assistant", content: "child says hi" }],
    };
    const withChild: AgentRun = { ...baseRun, children: [child] };
    render(<RunNode run={withChild} depth={0} defaultOpen={false} />);

    // parent collapsed: neither parent nor child details are visible
    expect(screen.queryByText("looks good")).not.toBeInTheDocument();
    expect(screen.queryByText("sub-agent")).not.toBeInTheDocument();

    // expand the parent: the child row appears but is itself collapsed
    fireEvent.click(screen.getByText("code-reviewer"));
    expect(screen.getByText("sub-agent")).toBeInTheDocument();
    expect(screen.queryByText("child says hi")).not.toBeInTheDocument();

    // expand the child independently
    fireEvent.click(screen.getByText("sub-agent"));
    expect(screen.getByText("child says hi")).toBeInTheDocument();
  });

  it("propagates onOpenDetail to nested child runs", () => {
    const child: AgentRun = {
      ...baseRun,
      id: "run-2",
      name: "sub-agent",
      messages: [{ role: "assistant", content: "child says hi" }],
    };
    const withChild: AgentRun = { ...baseRun, children: [child] };
    const onOpenDetail = vi.fn();
    render(<RunNode run={withChild} depth={0} onOpenDetail={onOpenDetail} />);

    fireEvent.click(screen.getByText("sub-agent"));

    expect(onOpenDetail).toHaveBeenCalledWith("run-2");
    // The name click is the drill-in, not the expand toggle, so the child's
    // transcript stays open rather than collapsing.
    expect(screen.getByText("child says hi")).toBeInTheDocument();
  });

  it("badges a run whose output contract was checked and not satisfied", () => {
    const contractFailed: AgentRun = {
      ...baseRun,
      contract: { checked: true, satisfied: false, deficiency: "missing file list" },
    };
    render(<RunNode run={contractFailed} depth={0} />);
    const badge = screen.getByText("contract ✗");
    expect(badge).toBeInTheDocument();
    expect(badge).toHaveAttribute("title", expect.stringContaining("missing file list"));
  });

  it("badges a failed verification as 'not verified', not as a contract failure", () => {
    const contractUnverified: AgentRun = {
      ...baseRun,
      contract: {
        checked: true,
        satisfied: false,
        checkFailed: true,
        deficiency: "verification failed: timed out",
      },
    };
    render(<RunNode run={contractUnverified} depth={0} />);
    const badge = screen.getByText("contract ?");
    expect(badge).toBeInTheDocument();
    expect(badge).toHaveAttribute("title", expect.stringContaining("not verified"));
    expect(screen.queryByText("contract ✗")).not.toBeInTheDocument();
  });

  it("labels a timed-out verification as a timeout, not a generic failure", () => {
    const contractTimeout: AgentRun = {
      ...baseRun,
      contract: {
        checked: true,
        satisfied: false,
        checkFailed: true,
        timedOut: true,
        deficiency: "timed out after 5m0s (Agent.RequestTimeout)",
      },
    };
    render(<RunNode run={contractTimeout} depth={0} />);
    const badge = screen.getByText("contract ?");
    expect(badge).toHaveAttribute("title", expect.stringContaining("timed out"));
    expect(screen.queryByText("contract ✗")).not.toBeInTheDocument();
  });

  it("shows no contract badge when the contract was satisfied", () => {
    const contractOK: AgentRun = {
      ...baseRun,
      contract: { checked: true, satisfied: true },
    };
    render(<RunNode run={contractOK} depth={0} />);
    expect(screen.queryByText("contract ✗")).not.toBeInTheDocument();
  });

  it("shows no contract badge when no contract applies", () => {
    render(<RunNode run={baseRun} depth={0} />);
    expect(screen.queryByText("contract ✗")).not.toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Expanded transcript: bounded scroll box with top/bottom affordances that
// follows the tail while the reader stays at the bottom.
// ---------------------------------------------------------------------------

function msg(content: string): AgentRunMessage {
  return { role: "assistant", content };
}

function liveRun(messages: AgentRunMessage[]): AgentRun {
  return { ...baseRun, status: "running", endedAt: undefined, messages };
}

/** Give the transcript controllable metrics; jsdom has no layout engine. */
function makeScrollable(el: HTMLElement, scrollHeight: number, clientHeight: number) {
  let top = 0;
  let height = scrollHeight;
  Object.defineProperty(el, "scrollHeight", { configurable: true, get: () => height });
  Object.defineProperty(el, "clientHeight", { configurable: true, value: clientHeight });
  Object.defineProperty(el, "scrollTop", {
    configurable: true,
    get: () => top,
    set: (v: number) => {
      top = v;
    },
  });
  // jsdom's Element may or may not expose scrollTo; pin the fallback path the
  // affordances use so a click is observable as a scrollTop write.
  Object.defineProperty(el, "scrollTo", { configurable: true, value: undefined });
  return {
    setTop: (v: number) => {
      top = v;
    },
    getTop: () => top,
    setScrollHeight: (h: number) => {
      height = h;
    },
  };
}

function transcript(): HTMLElement {
  return screen.getByTestId("agent-transcript");
}

function scrollToBottomButton() {
  return screen.queryByRole("button", { name: "Scroll to bottom" });
}

function scrollToTopButton() {
  return screen.queryByRole("button", { name: "Scroll to top" });
}

describe("RunNode transcript scrolling", () => {
  it("pins the expanded transcript to the newest entry and follows appends", () => {
    const { rerender } = render(<RunNode run={liveRun([msg("first")])} depth={0} />);
    const m = makeScrollable(transcript(), 600, 200);

    // New content while at the bottom keeps the newest entry in view.
    rerender(<RunNode run={liveRun([msg("first"), msg("second")])} depth={0} />);
    expect(m.getTop()).toBe(600);

    m.setScrollHeight(700);
    rerender(
      <RunNode run={liveRun([msg("first"), msg("second"), msg("third")])} depth={0} />,
    );
    expect(m.getTop()).toBe(700);
  });

  it("stops following once the reader scrolls up, and offers scroll-to-bottom", () => {
    const { rerender } = render(<RunNode run={liveRun([msg("first")])} depth={0} />);
    const m = makeScrollable(transcript(), 600, 200);
    rerender(<RunNode run={liveRun([msg("first"), msg("second")])} depth={0} />);
    expect(m.getTop()).toBe(600);

    m.setTop(120);
    fireEvent.scroll(transcript());

    m.setScrollHeight(700);
    rerender(
      <RunNode run={liveRun([msg("first"), msg("second"), msg("third")])} depth={0} />,
    );

    // The reader's position is respected; the affordance offers the tail back.
    expect(m.getTop()).toBe(120);
    expect(scrollToBottomButton()).toBeInTheDocument();
  });

  it("resumes following the moment scroll-to-bottom is clicked, before the scroll event lands", () => {
    const { rerender } = render(<RunNode run={liveRun([msg("first")])} depth={0} />);
    const m = makeScrollable(transcript(), 600, 200);
    rerender(<RunNode run={liveRun([msg("first"), msg("second")])} depth={0} />);
    m.setTop(120);
    fireEvent.scroll(transcript());

    fireEvent.click(scrollToBottomButton()!);
    expect(m.getTop()).toBe(600);

    // A smooth scroll reports back asynchronously (and jsdom never fires the
    // event at all). The click must re-arm the follow itself, or an update
    // arriving mid-animation is dropped and the reader is left short of the
    // tail with the button still showing.
    m.setScrollHeight(720);
    rerender(
      <RunNode run={liveRun([msg("first"), msg("second"), msg("third")])} depth={0} />,
    );
    expect(m.getTop()).toBe(720);
  });

  it("offers scroll-to-top only after the reader leaves the top", () => {
    render(<RunNode run={liveRun([msg("first")])} depth={0} />);
    const el = transcript();
    makeScrollable(el, 600, 200);
    fireEvent.scroll(el);

    // parked at the top: only the "down" affordance makes sense
    expect(scrollToTopButton()).toBeNull();
    expect(scrollToBottomButton()).toBeInTheDocument();

    const m = makeScrollable(el, 600, 200);
    m.setTop(300);
    fireEvent.scroll(el);
    expect(scrollToTopButton()).toBeInTheDocument();

    fireEvent.click(scrollToTopButton()!);
    expect(m.getTop()).toBe(0);
  });

  it("renders no nav buttons when the transcript fits its box", () => {
    render(<RunNode run={liveRun([msg("first")])} depth={0} />);
    makeScrollable(transcript(), 300, 300);
    fireEvent.scroll(transcript());

    expect(scrollToTopButton()).toBeNull();
    expect(scrollToBottomButton()).toBeNull();
  });

  it("pins to the tail as soon as a collapsed card is expanded", () => {
    // The transcript only exists after the expand click, so the follow effect
    // must observe it in that same commit. jsdom has no layout engine, so the
    // metrics have to be visible on the prototype BEFORE the element mounts —
    // stubbing the node after the fact would let the effect run against zeros
    // and the assertion would pass for the wrong reason.
    const proto = HTMLElement.prototype;
    const dScrollHeight = Object.getOwnPropertyDescriptor(proto, "scrollHeight");
    const dClientHeight = Object.getOwnPropertyDescriptor(proto, "clientHeight");
    const dScrollTop = Object.getOwnPropertyDescriptor(proto, "scrollTop");
    let top = 0;
    Object.defineProperty(proto, "scrollHeight", { configurable: true, get: () => 600 });
    Object.defineProperty(proto, "clientHeight", { configurable: true, get: () => 200 });
    Object.defineProperty(proto, "scrollTop", {
      configurable: true,
      get: () => top,
      set: (v: number) => {
        top = v;
      },
    });
    try {
      render(
        <RunNode run={liveRun([msg("first"), msg("second")])} depth={0} defaultOpen={false} />,
      );
      expect(screen.queryByTestId("agent-transcript")).toBeNull();

      fireEvent.click(screen.getByText("code-reviewer"));

      expect(screen.getByTestId("agent-transcript")).toBeInTheDocument();
      expect(top).toBe(600);
    } finally {
      if (dScrollHeight) Object.defineProperty(proto, "scrollHeight", dScrollHeight);
      else Reflect.deleteProperty(proto, "scrollHeight");
      if (dClientHeight) Object.defineProperty(proto, "clientHeight", dClientHeight);
      else Reflect.deleteProperty(proto, "clientHeight");
      if (dScrollTop) Object.defineProperty(proto, "scrollTop", dScrollTop);
      else Reflect.deleteProperty(proto, "scrollTop");
    }
  });
});
