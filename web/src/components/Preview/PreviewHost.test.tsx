import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { api } from "../../api/client";
import PreviewHost from "./PreviewHost";

vi.mock("../../api/client", () => ({
  api: { openFileWithOS: vi.fn(), fetchFileRaw: vi.fn(), getFileContent: vi.fn(), saveFileContent: vi.fn() },
}));

vi.mock("../Browser/BrowserPanel", () => ({
  BrowserPanel: () => <div data-testid="browser-panel" />,
}));

vi.mock("../Files/FileEditor", () => ({ default: () => <div data-testid="file-editor" /> }));

describe("PreviewHost legacy fallback", () => {
  it("opens legacy .doc with the request's own project root, not the pane default", async () => {
    vi.mocked(api.openFileWithOS).mockResolvedValue({ path: "/proj/old.doc", status: "opened" });
    render(
      <PreviewHost
        stateKey="side:chat:test"
        projectRoot="/default"
        request={{ path: "old.doc", kind: "text", page: 1, projectRoot: "/proj" }}
        nonce={1}
      />,
    );

    expect(await screen.findByText("old.doc")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Open in app" }));
    expect(api.openFileWithOS).toHaveBeenCalledWith("old.doc", "/proj");
  });
});

describe("PreviewHost regression (after PreviewSurface extraction)", () => {
  it("renders sidebar shell with browser/preview tabs", () => {
    const { container } = render(<PreviewHost stateKey="tab:test" request={null} nonce={0} />);
    expect(container.textContent).toContain("Browser");
    expect(container.textContent).toContain("Preview");
  });
});
