import { render as rtlRender, screen } from "@testing-library/react";
import type { ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";

vi.mock("../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api/client")>();
  return {
    ...actual,
    api: {
      ...actual.api,
      fetchMcpAppResource: vi.fn(() => new Promise(() => {})),
      callMcpAppTool: vi.fn(),
    },
  };
});

import { ToolBlock } from "./TurnParts";
import { ChatDisplayTestProvider } from "./chatDisplayTestUtils";

function render(ui: ReactElement) {
  return rtlRender(<ChatDisplayTestProvider>{ui}</ChatDisplayTestProvider>);
}

describe("ToolBlock with an MCP Apps widget", () => {
  it("renders the widget container and its view below the block", () => {
    const { container } = render(
      <ToolBlock
        tool="show_screen"
        command='{"screen":1}'
        output="shown"
        toolCallId="call-mcp-1"
        sessionId="sess-mcp-1"
        mcpApp={{ server: "atlas", tool: "show_screen", resource_uri: "ui://atlas/screen" }}
        mcpAppActive={false}
      />,
    );
    const wrapper = container.querySelector(".w-full.basis-full.min-w-0");
    expect(wrapper).not.toBeNull();
    // The older-card path: the view offers Open view and does not fetch.
    expect(wrapper?.contains(screen.getByRole("button", { name: "Open view" }))).toBe(true);
  });

  it("renders the plain block without a widget container when mcpApp is absent", () => {
    const { container } = render(
      <ToolBlock tool="show_screen" command='{"screen":1}' output="shown" toolCallId="call-plain-1" sessionId="sess-plain-1" />,
    );
    expect(container.querySelector(".basis-full")).toBeNull();
    expect(screen.queryByRole("button", { name: "Open view" })).toBeNull();
  });
});
