import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { PrintPreviewButton } from "./PrintPreviewButton";

const buildPrintDocument = vi.hoisted(() => vi.fn());

vi.mock("./printPreview", async () => ({
  ...(await vi.importActual<typeof import("./printPreview")>("./printPreview")),
  buildPrintDocument,
}));

beforeEach(() => {
  buildPrintDocument.mockReset();
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

  it("opens a preview dialog with the built document on click", async () => {
    buildPrintDocument.mockResolvedValue("<article>printed</article>");
    const load = vi.fn(() => "# Hi");
    render(<PrintPreviewButton kind="markdown" title="a.md" load={load} />);

    fireEvent.click(screen.getByRole("button", { name: "Print" }));

    // The click handler is async: wait for the dialog and the rendered iframe.
    await waitFor(() =>
      expect(screen.getByRole("dialog")).toBeInTheDocument(),
    );
    expect(await screen.findByText("a.md")).toBeInTheDocument();
    const dialog = screen.getByRole("dialog");
    const iframe = dialog.querySelector("iframe");
    expect(iframe).toBeInTheDocument();
    expect(load).toHaveBeenCalledTimes(1);
    expect(buildPrintDocument).toHaveBeenCalledWith("markdown", "# Hi", "a.md");
  });

  it("closes the dialog when the close button is clicked", async () => {
    buildPrintDocument.mockResolvedValue("<article>printed</article>");
    render(<PrintPreviewButton kind="markdown" title="a.md" load={() => "# Hi"} />);

    fireEvent.click(screen.getByRole("button", { name: "Print" }));
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("button", { name: "Close preview" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("shows Print failed and logs when building the document rejects", async () => {
    buildPrintDocument.mockRejectedValue(new Error("boom"));
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    render(<PrintPreviewButton kind="markdown" title="a.md" load={() => "# Hi"} />);

    fireEvent.click(screen.getByRole("button", { name: "Print" }));

    expect(await screen.findByText("Print failed")).toBeInTheDocument();
    expect(error).toHaveBeenCalledWith("print preview failed", { kind: "markdown", title: "a.md" }, expect.any(Error));
  });

  it("shows the warning icon and the reason in icon-only mode when building rejects", async () => {
    buildPrintDocument.mockRejectedValue(new Error("print: document is 9 bytes, over the 8-byte print limit"));
    render(<PrintPreviewButton kind="markdown" title="a.md" load={() => "# Hi"} iconOnly className="rounded" />);

    fireEvent.click(screen.getByRole("button", { name: "Print" }));

    // In icon-only mode the label text is hidden; assert via aria-label + title.
    const btn = await screen.findByRole("button", { name: "Print failed" });
    expect(btn).toHaveAttribute("title", "Print failed: print: document is 9 bytes, over the 8-byte print limit");
  });
});
