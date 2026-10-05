import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ToolBlock } from "./TurnParts";
import { ChatDisplayTestProvider } from "./chatDisplayTestUtils";

// Must match tool.ToolCancelledResult in internal/tool/misc.go. The Go side
// writes this as the tool result for any call the user interrupted, so the
// block has a defined output and must stop reading as in-flight.
const TOOL_CANCELLED_RESULT =
  "Cancelled by the user before this tool call finished. It did not complete; do not assume any result.";

describe("ToolBlock pending state after a cancelled turn", () => {
  // Regression guard for "when the loop stops on main chat ... also for any
  // tool calling": an interrupted tool call kept rendering a pulsing "running…"
  // forever. ToolBlock derives `pending` purely from `output === undefined`, so
  // the symptom had exactly one cause — an assistant tool_call with no tool
  // message, which is what Agent.Step used to leave behind on cancel.
  // This pins the rendering half: a tool call WITH the cancellation result is
  // never "pending", and the cancelled text is shown instead.
  it("does not show running… once a cancelled result has arrived", () => {
    render(
      <ChatDisplayTestProvider preset="full">
        <ToolBlock
          tool="bash"
          command="sleep 300"
          output={TOOL_CANCELLED_RESULT}
          callKey="call-1"
          outputKey="call-1:out"
        />
      </ChatDisplayTestProvider>,
    );

    expect(screen.queryByText(/running…/)).toBeNull();
    expect(screen.getByText(TOOL_CANCELLED_RESULT)).toBeTruthy();
  });

  // The inverse must stay true, otherwise this test would pass for the wrong
  // reason: a call with no result at all is genuinely still running and must
  // keep its indicator.
  it("still shows running… for a call with no result yet", () => {
    render(
      <ChatDisplayTestProvider preset="full">
        <ToolBlock tool="bash" command="sleep 300" callKey="call-2" outputKey="call-2:out" />
      </ChatDisplayTestProvider>,
    );

    expect(screen.getByText(/running…/)).toBeTruthy();
  });
});