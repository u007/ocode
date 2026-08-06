import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import RunNode from "./RunNode";
import type { AgentRun } from "../../api/types";

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
});
