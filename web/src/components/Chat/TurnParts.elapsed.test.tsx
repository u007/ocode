import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ToolBlock } from "./TurnParts";
import { ChatDisplayTestProvider } from "./chatDisplayTestUtils";

describe("ToolBlock elapsed time", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-16T12:00:00Z"));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows the humanised duration once the result is in", () => {
    render(
      <ChatDisplayTestProvider preset="full">
        <ToolBlock tool="bash" command="go test ./..." output="ok" durationMs={4200} callKey="e1" outputKey="e1:out" />
      </ChatDisplayTestProvider>,
    );
    expect(screen.getByText("4.2s")).toBeTruthy();
  });

  it("ticks a live elapsed while pending when startedAt is known", () => {
    render(
      <ChatDisplayTestProvider preset="full">
        <ToolBlock
          tool="bash"
          command="make desktop"
          startedAt={Date.now() - 2500}
          callKey="e2"
          outputKey="e2:out"
        />
      </ChatDisplayTestProvider>,
    );
    expect(screen.getByText(/running…/).textContent).toContain("2.5s");
  });

  it("shows no duration at all for a completed call without timing data", () => {
    render(
      <ChatDisplayTestProvider preset="full">
        <ToolBlock tool="read" command="x" output="data" callKey="e3" outputKey="e3:out" />
      </ChatDisplayTestProvider>,
    );
    expect(screen.queryByText(/\d+(\.\d+)?(ms|s)$/)).toBeNull();
  });
});
