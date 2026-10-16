// Preview tab refresh: the refresh button remounts the previewed surface so the
// viewer refetches the file from disk. It only appears once a file is selected.
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import PreviewTabPage from "./PreviewTabPage";
import type { PreviewOpenRequest } from "../../lib/previewKind";

const mockMount = vi.hoisted(() => vi.fn());

vi.mock("../Files/FilePicker", () => ({ default: () => null }));
vi.mock("./PreviewSurface", async () => {
  const React = await import("react");
  return {
    default: ({ path }: { path: string }) => {
      React.useEffect(() => {
        mockMount(path);
      }, [path]);
      return <div data-testid="preview-surface" data-path={path} />;
    },
  };
});

function req(over: Partial<PreviewOpenRequest> = {}): PreviewOpenRequest {
  return { path: "docs/spec.md", kind: "markdown", page: 1, ...over };
}

describe("PreviewTabPage refresh", () => {
  beforeEach(() => {
    mockMount.mockClear();
  });

  it("hides the refresh button until a file is selected", () => {
    render(<PreviewTabPage nonce={0} request={null} />);
    expect(screen.queryByRole("button", { name: "Refresh preview" })).toBeNull();
  });

  it("remounts the previewed surface on click so the file is refetched", async () => {
    render(<PreviewTabPage nonce={1} request={req()} />);
    await screen.findByTestId("preview-surface");
    expect(mockMount).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: "Refresh preview" }));

    expect(mockMount).toHaveBeenCalledTimes(2);
    expect(mockMount).toHaveBeenLastCalledWith("docs/spec.md");
    expect(screen.getByTestId("preview-surface").getAttribute("data-path")).toBe("docs/spec.md");
  });
});
