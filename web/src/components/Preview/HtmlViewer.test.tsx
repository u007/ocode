import { act, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import HtmlViewer from "./HtmlViewer";
import { api } from "../../api/client";

vi.mock("../../api/client", () => ({ api: { getFileContent: vi.fn() } }));

beforeEach(() => {
  vi.mocked(api.getFileContent).mockReset();
});

// jsdom fires the iframe's load event asynchronously, and onLoad bumps the find
// agent's frame generation — a real async update, so let it settle inside act
// instead of warning after the test body ends.
async function settleFrame(): Promise<void> {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

describe("HtmlViewer", () => {
  it("renders an iframe with srcDoc", async () => {
    render(<HtmlViewer path="doc.html" content="<h1>Hello</h1>" />);
    const iframe = screen.getByTitle("HTML preview");
    expect(iframe.tagName).toBe("IFRAME");
    expect(iframe.getAttribute("srcDoc")).toContain("<h1>Hello</h1>");
    // The find agent is appended to the srcDoc (see htmlFindAgent.ts).
    expect(iframe.getAttribute("srcDoc")).toContain("__ocodeFind");
    await settleFrame();
  });

  it("uses sandbox=allow-scripts (no allow-same-origin)", async () => {
    render(<HtmlViewer path="doc.html" content="<p>safe</p>" />);
    const iframe = screen.getByTitle("HTML preview");
    expect(iframe).toHaveAttribute("sandbox", "allow-scripts");
    expect(iframe.getAttribute("sandbox")).not.toContain("allow-same-origin");
    await settleFrame();
  });

  it("updates srcDoc when content changes", async () => {
    const { rerender } = render(<HtmlViewer path="doc.html" content="<p>one</p>" />);
    expect(screen.getByTitle("HTML preview").getAttribute("srcDoc")).toContain("<p>one</p>");
    rerender(<HtmlViewer path="doc.html" content="<p>two</p>" />);
    const srcDoc = screen.getByTitle("HTML preview").getAttribute("srcDoc") ?? "";
    expect(srcDoc).toContain("<p>two</p>");
    expect(srcDoc).not.toContain("<p>one</p>");
    await settleFrame();
  });

  // An EMPTY string is still controlled: `content !== undefined` decides the
  // mode, never truthiness, or an empty file would trigger a pointless fetch.
  it("treats an empty content string as controlled and does not fetch", async () => {
    render(<HtmlViewer path="doc.html" content="" />);
    expect(screen.getByTitle("HTML preview")).toBeTruthy();
    expect(api.getFileContent).not.toHaveBeenCalled();
    await settleFrame();
  });

  // The sidebar PreviewHost / PreviewTabPage pass only the path: without an
  // editor there is no live `content`, so the viewer must read the file itself
  // or the pane renders nothing at all.
  it("fetches the document when no content prop is supplied", async () => {
    vi.mocked(api.getFileContent).mockResolvedValue({ content: "<h1>Fetched</h1>", is_binary: false });
    render(<HtmlViewer path="mockups/page.html" projectRoot="/proj" />);
    expect(screen.getByText("Loading file…")).toBeTruthy();
    await act(async () => {
      await Promise.resolve();
    });
    expect(api.getFileContent).toHaveBeenCalledWith("mockups/page.html", "/proj", undefined);
    expect(screen.getByTitle("HTML preview").getAttribute("srcDoc")).toContain("<h1>Fetched</h1>");
  });

  it("refetches when the sidebar bumps revision", async () => {
    vi.mocked(api.getFileContent)
      .mockResolvedValueOnce({ content: "<p>v1</p>", is_binary: false })
      .mockResolvedValueOnce({ content: "<p>v2</p>", is_binary: false });
    const { rerender } = render(<HtmlViewer path="page.html" revision={0} />);
    await act(async () => {
      await Promise.resolve();
    });
    rerender(<HtmlViewer path="page.html" revision={1} />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(api.getFileContent).toHaveBeenCalledTimes(2);
    expect(screen.getByTitle("HTML preview").getAttribute("srcDoc")).toContain("<p>v2</p>");
  });

  it("reports a failed fetch instead of an empty frame", async () => {
    vi.mocked(api.getFileContent).mockRejectedValue(new Error("boom"));
    render(<HtmlViewer path="page.html" />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByText("Load failed: boom")).toBeTruthy();
    expect(screen.queryByTitle("HTML preview")).toBeNull();
  });

  // A viewer mounted with a non-zero revision must not fetch twice: the mount
  // effect already covers that revision.
  it("fetches once when mounted with a non-zero revision", async () => {
    vi.mocked(api.getFileContent).mockResolvedValue({ content: "<p>v</p>", is_binary: false });
    render(<HtmlViewer path="page.html" revision={3} />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(api.getFileContent).toHaveBeenCalledTimes(1);
    expect(screen.getByTitle("HTML preview").getAttribute("srcDoc")).toContain("<p>v</p>");
  });

  it("ignores a response for a path that is no longer shown", async () => {
    let resolveOld: (v: { content: string; is_binary: boolean }) => void = () => {};
    vi.mocked(api.getFileContent)
      .mockImplementationOnce(
        () =>
          new Promise((res) => {
            resolveOld = res;
          }),
      )
      .mockResolvedValueOnce({ content: "<p>new</p>", is_binary: false });
    const { rerender } = render(<HtmlViewer path="a.html" />);
    rerender(<HtmlViewer path="b.html" />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByTitle("HTML preview").getAttribute("srcDoc")).toContain("<p>new</p>");
    // The superseded fetch resolving late must not overwrite the newer document.
    await act(async () => {
      resolveOld({ content: "<p>old</p>", is_binary: false });
      await Promise.resolve();
    });
    const srcDoc = screen.getByTitle("HTML preview").getAttribute("srcDoc") ?? "";
    expect(srcDoc).toContain("<p>new</p>");
    expect(srcDoc).not.toContain("<p>old</p>");
  });

  it("recovers from a failed load when a later revision refetches", async () => {
    vi.mocked(api.getFileContent)
      .mockRejectedValueOnce(new Error("boom"))
      .mockResolvedValueOnce({ content: "<p>back</p>", is_binary: false });
    const { rerender } = render(<HtmlViewer path="page.html" revision={0} />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByText("Load failed: boom")).toBeTruthy();
    rerender(<HtmlViewer path="page.html" revision={1} />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.queryByText("Load failed: boom")).toBeNull();
    expect(screen.getByTitle("HTML preview").getAttribute("srcDoc")).toContain("<p>back</p>");
  });
});
