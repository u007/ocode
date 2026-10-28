import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ToolBlock } from "./TurnParts";
import { ChatDisplayTestProvider } from "./chatDisplayTestUtils";

const PNG_B64 = "iVBORw0KGgo=";

describe("ToolBlock inline tool-result images", () => {
  // Screenshots come back as agent.Message.Images on the tool message. They
  // render inside the output region, so the preset's default-open output
  // disclosure (the "full" preset used here) shows them.
  it("renders an inline-safe PNG as a data: image", () => {
    render(
      <ChatDisplayTestProvider preset="full">
        <ToolBlock
          tool="browser"
          command='{"action":"screenshot"}'
          output="Screenshot taken [image shown below]"
          images={[{ mime_type: "image/png", data: PNG_B64 }]}
          callKey="call-img"
          outputKey="call-img:out"
        />
      </ChatDisplayTestProvider>,
    );

    const img = screen.getByAltText("browser result image 1");
    expect(img.getAttribute("src")).toBe(`data:image/png;base64,${PNG_B64}`);
  });

  // The MIME type comes from the server, so anything outside the raster
  // allowlist (here SVG, which can carry script) must never become an <img>.
  it("drops an image whose MIME type is not inline-safe", () => {
    render(
      <ChatDisplayTestProvider preset="full">
        <ToolBlock
          tool="browser"
          output="Screenshot taken [image shown below]"
          images={[{ mime_type: "image/svg+xml", data: PNG_B64 }]}
          callKey="call-svg"
          outputKey="call-svg:out"
        />
      </ChatDisplayTestProvider>,
    );

    expect(screen.queryByRole("img")).toBeNull();
  });
});
