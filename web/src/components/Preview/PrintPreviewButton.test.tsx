import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { PrintPreviewButton } from "./PrintPreviewButton";

const printPreview = vi.hoisted(() => vi.fn());

vi.mock("./printPreview", async () => ({
  ...(await vi.importActual<typeof import("./printPreview")>("./printPreview")),
  printPreview,
}));

beforeEach(() => {
  printPreview.mockReset();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("PrintPreviewButton", () => {
  it("renders nothing for a kind that cannot be printed", () => {
    const { container } = render(<PrintPreviewButton kind="pdf" title="a.pdf" load={() => ""} />);

    expect(container).toBeEmptyDOMElement();
  });

  it("loads the source on click and prints it", async () => {
    printPreview.mockResolvedValue(undefined);
    const load = vi.fn(() => "# Hi");
    render(<PrintPreviewButton kind="markdown" title="a.md" load={load} />);

    fireEvent.click(screen.getByRole("button", { name: "Print" }));

    await waitFor(() =>
      expect(printPreview).toHaveBeenCalledWith({ kind: "markdown", title: "a.md", content: "# Hi" }),
    );
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("shows Print failed and logs when printing rejects", async () => {
    printPreview.mockRejectedValue(new Error("boom"));
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    render(<PrintPreviewButton kind="markdown" title="a.md" load={() => "# Hi"} />);

    fireEvent.click(screen.getByRole("button", { name: "Print" }));

    expect(await screen.findByText("Print failed")).toBeInTheDocument();
    expect(error).toHaveBeenCalledWith("print preview failed", { kind: "markdown", title: "a.md" }, expect.any(Error));
  });

  it("shows the warning icon and the reason in icon-only mode when printing rejects", async () => {
    printPreview.mockRejectedValue(new Error("print: document is 9 bytes, over the 8-byte print limit"));
    vi.spyOn(console, "error").mockImplementation(() => {});
    render(<PrintPreviewButton kind="markdown" title="a.md" load={() => "# Hi"} iconOnly className="rounded" />);

    fireEvent.click(screen.getByRole("button", { name: "Print" }));

    const failed = await screen.findByRole("button", { name: "Print failed" });
    expect(failed).toHaveClass("text-destructive", "rounded");
    expect(failed).toHaveAttribute("title", "Print failed: print: document is 9 bytes, over the 8-byte print limit");
    expect(screen.queryByText("Print failed")).toBeNull();
  });
});
