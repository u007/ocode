// Mobile preview activation: the side pane is hidden at the mobile breakpoint,
// so App routes a preview activation into this full-width Preview sub-tab. The
// page must show the requested file and acknowledge the one-shot activation.
import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import PreviewTabPage from "./PreviewTabPage";
import type { PreviewOpenRequest } from "../../lib/previewKind";

vi.mock("../Files/FilePicker", () => ({ default: () => null }));
vi.mock("./PreviewSurface", () => ({
  default: ({
    path,
    kind,
    page,
    projectRoot,
    projectHost,
  }: {
    path: string;
    kind: string;
    page?: number;
    projectRoot?: string;
    projectHost?: string;
  }) => (
    <div
      data-testid="preview-surface"
      data-path={path}
      data-kind={kind}
      data-page={String(page ?? "")}
      data-root={projectRoot ?? ""}
      data-host={projectHost ?? ""}
    />
  ),
}));

function req(over: Partial<PreviewOpenRequest> = {}): PreviewOpenRequest {
  return { path: "docs/spec.md", kind: "markdown", page: 1, ...over };
}

describe("PreviewTabPage activation", () => {
  it("shows the activated file, applies page + fallback project, and consumes the activation", async () => {
    const consume = vi.fn();
    const { rerender } = render(
      <PreviewTabPage projectRoot="/proj" nonce={0} request={null} onConsumeActivation={consume} />,
    );
    expect(screen.queryByTestId("preview-surface")).toBeNull();

    rerender(
      <PreviewTabPage projectRoot="/proj" nonce={1} request={req({ page: 3 })} onConsumeActivation={consume} />,
    );
    const surface = await screen.findByTestId("preview-surface");
    expect(surface.getAttribute("data-path")).toBe("docs/spec.md");
    expect(surface.getAttribute("data-kind")).toBe("markdown");
    expect(surface.getAttribute("data-page")).toBe("3");
    expect(surface.getAttribute("data-root")).toBe("/proj");
    expect(consume).toHaveBeenCalledTimes(1);
  });

  it("prefers the request's own project root/host over the fallback", async () => {
    render(
      <PreviewTabPage
        projectRoot="/fallback"
        projectHost="fallback-host"
        nonce={1}
        request={req({ projectRoot: "/remote", projectHost: "remote-host" })}
      />,
    );
    const surface = await screen.findByTestId("preview-surface");
    expect(surface.getAttribute("data-root")).toBe("/remote");
    expect(surface.getAttribute("data-host")).toBe("remote-host");
  });

  it("re-applies when the nonce bumps even for the same path", async () => {
    const consume = vi.fn();
    const { rerender } = render(<PreviewTabPage nonce={1} request={req()} onConsumeActivation={consume} />);
    await screen.findByTestId("preview-surface");
    rerender(<PreviewTabPage nonce={2} request={req()} onConsumeActivation={consume} />);
    await waitFor(() => expect(consume).toHaveBeenCalledTimes(2));
  });

  it("does nothing without an activation (keeps the file-picker placeholder)", () => {
    render(<PreviewTabPage nonce={0} request={null} />);
    expect(screen.queryByTestId("preview-surface")).toBeNull();
    expect(screen.getByText(/select a file to preview or edit/i)).toBeTruthy();
  });
});
