import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";

const fetchFileRaw = vi.fn();

// Same heavy-dependency isolation as the other Markdown tests.
vi.mock("../../api/client", () => ({
  api: { getFileContent: vi.fn().mockResolvedValue({ content: "", is_binary: false }), fetchFileRaw: (...args: unknown[]) => fetchFileRaw(...args) },
}));
vi.mock("../Files/FileEditor", () => ({ default: () => null }));
vi.mock("./MermaidViewer", () => ({ default: () => null }));

import MarkdownViewer from "./MarkdownViewer";
import { localImagePath } from "./localImagePath";

const PNG = new Uint8Array([0x89, 0x50, 0x4e, 0x47]).buffer;

describe("localImagePath", () => {
  it("resolves a relative source against the markdown file's directory", () => {
    expect(localImagePath("preview.md", "./preview-screenshots/home_1440.png")).toBe("preview-screenshots/home_1440.png");
    expect(localImagePath("docs/report.md", "images/a.png")).toBe("docs/images/a.png");
    expect(localImagePath("docs/report.md", "../shots/a.png")).toBe("shots/a.png");
  });

  it("passes absolute, home and Windows sources through", () => {
    expect(localImagePath("docs/report.md", "/tmp/shots/a.png")).toBe("/tmp/shots/a.png");
    expect(localImagePath("docs/report.md", "~/shots/a.png")).toBe("~/shots/a.png");
    expect(localImagePath("docs/report.md", "C:\\shots\\a.png")).toBe("C:\\shots\\a.png");
  });

  it("leaves sources the browser can load alone", () => {
    expect(localImagePath("docs/report.md", "https://example.com/a.png")).toBeNull();
    expect(localImagePath("docs/report.md", "data:image/png;base64,AAA")).toBeNull();
    expect(localImagePath("docs/report.md", "blob:http://localhost/abc")).toBeNull();
    expect(localImagePath("docs/report.md", "//cdn.example.com/a.png")).toBeNull();
    expect(localImagePath("docs/report.md", "#fragment")).toBeNull();
  });

  it("resolves a relative source against a Windows document's directory", () => {
    expect(localImagePath("docs\\report.md", "images/a.png")).toBe("docs/images/a.png");
  });

  it("drops a query/fragment and decodes the URL escapes a file name arrives with", () => {
    expect(localImagePath("docs/report.md", "./shots/a%20b.png")).toBe("docs/shots/a b.png");
    expect(localImagePath("docs/report.md", "./shots/a.png?v=2#top")).toBe("docs/shots/a.png");
    // A malformed escape is used as written rather than guessed at.
    expect(localImagePath("docs/report.md", "./shots/100%.png")).toBe("docs/shots/100%.png");
  });
});

describe("MarkdownViewer images", () => {
  let createObjectURL: ReturnType<typeof vi.fn>;
  let revokeObjectURL: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchFileRaw.mockReset();
    fetchFileRaw.mockResolvedValue(PNG);
    createObjectURL = vi.fn(() => "blob:preview/1");
    revokeObjectURL = vi.fn();
    Object.defineProperty(URL, "createObjectURL", { configurable: true, value: createObjectURL });
    Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: revokeObjectURL });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    delete (URL as unknown as Record<string, unknown>).createObjectURL;
    delete (URL as unknown as Record<string, unknown>).revokeObjectURL;
  });

  it("fetches a relative local image against the markdown file and renders its bytes", async () => {
    const view = render(
      <MarkdownViewer path="preview.md" projectRoot="/proj" content={"![Home](./preview-screenshots/home_1440.png)"} onOpenFile={() => {}} />,
    );

    await waitFor(() => expect(view.container.querySelector("img")?.getAttribute("src")).toBe("blob:preview/1"));
    expect(fetchFileRaw).toHaveBeenCalledWith("preview-screenshots/home_1440.png", "/proj", undefined);
    expect(view.container.querySelector("img")?.getAttribute("alt")).toBe("Home");
  });

  it("opens an absolute local image from the project root anchor", async () => {
    const view = render(
      <MarkdownViewer path="preview.md" projectRoot="/proj" projectHost="ci.local" content={"![Shot](/proj/shots/a.png)"} onOpenFile={() => {}} />,
    );

    await waitFor(() => expect(view.container.querySelector("img")?.getAttribute("src")).toBe("blob:preview/1"));
    expect(fetchFileRaw).toHaveBeenCalledWith("/proj/shots/a.png", "/proj", "ci.local");
  });

  it("leaves an http image to the browser", async () => {
    const view = render(<MarkdownViewer path="preview.md" content={"![Remote](https://example.com/a.png)"} onOpenFile={() => {}} />);

    expect(view.container.querySelector("img")?.getAttribute("src")).toBe("https://example.com/a.png");
    expect(fetchFileRaw).not.toHaveBeenCalled();
  });

  it("renders a raw HTML img, keeping its width", async () => {
    const view = render(
      <MarkdownViewer path="preview.md" content={'<img src="./shots/home_390.png" width="260" alt="Mobile">'} onOpenFile={() => {}} />,
    );

    await waitFor(() => expect(view.container.querySelector("img")?.getAttribute("src")).toBe("blob:preview/1"));
    const img = view.container.querySelector("img");
    expect(img?.getAttribute("width")).toBe("260");
    expect(fetchFileRaw).toHaveBeenCalledWith("shots/home_390.png", undefined, undefined);
  });

  it("sanitizes raw HTML instead of trusting the document", async () => {
    const view = render(
      <MarkdownViewer path="preview.md" content={'<script>alert(1)</script>\n\n<img src="javascript:alert(2)" onerror="alert(3)">'} onOpenFile={() => {}} />,
    );

    expect(view.container.querySelector("script")).toBeNull();
    expect(view.container.textContent).not.toContain("alert");
    expect(fetchFileRaw).not.toHaveBeenCalled();
  });

  it("keeps GFM table alignment through the sanitizer", () => {
    const view = render(
      <MarkdownViewer path="preview.md" content={"| a | b |\n|:--|--:|\n| 1 | 2 |"} onOpenFile={() => {}} />,
    );

    // React renders the GFM `align` property as an inline text-align style.
    const headers = view.container.querySelectorAll("th");
    expect(headers[0]?.style.textAlign).toBe("left");
    expect(headers[1]?.style.textAlign).toBe("right");
  });

  it("shows the failure instead of a silent gap", async () => {
    fetchFileRaw.mockRejectedValue(new Error("path is outside the project root"));
    const view = render(<MarkdownViewer path="preview.md" content={"![Gone](./missing.png)"} onOpenFile={() => {}} />);

    await waitFor(() => expect(view.container.textContent).toContain("could not load: path is outside the project root"));
    expect(view.container.querySelector("img")).toBeNull();
  });

  it("waits until the image nears the viewport before fetching", async () => {
    const observers: Array<(entry: { isIntersecting: boolean }) => void> = [];
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        private readonly cb: IntersectionObserverCallback;
        constructor(cb: IntersectionObserverCallback) {
          this.cb = cb;
        }
        observe(): void {
          observers.push((entry) =>
            this.cb([entry as IntersectionObserverEntry], {} as IntersectionObserver),
          );
        }
        unobserve(): void {}
        disconnect(): void {}
      },
    );

    const view = render(<MarkdownViewer path="preview.md" content={"![A](./a.png)"} onOpenFile={() => {}} />);
    await waitFor(() => expect(observers.length).toBe(1));
    expect(fetchFileRaw).not.toHaveBeenCalled();

    observers[0]({ isIntersecting: true });
    await waitFor(() => expect(view.container.querySelector("img")?.getAttribute("src")).toBe("blob:preview/1"));
    expect(fetchFileRaw).toHaveBeenCalledTimes(1);
  });

  it("revokes the blob URL when the viewer unmounts", async () => {
    const view = render(<MarkdownViewer path="preview.md" content={"![A](./a.png)"} onOpenFile={() => {}} />);
    await waitFor(() => expect(view.container.querySelector("img")?.getAttribute("src")).toBe("blob:preview/1"));

    view.unmount();
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:preview/1");
  });

  it("does not refetch when the host re-renders with a new onOpenFile", async () => {
    const view = render(
      <MarkdownViewer path="preview.md" projectRoot="/proj" content={"![Home](./shots/home.png)"} onOpenFile={() => {}} />,
    );
    await waitFor(() => expect(view.container.querySelector("img")?.getAttribute("src")).toBe("blob:preview/1"));

    // A fresh callback identity is what PreviewHost produces on every render.
    // A changed component TYPE would remount the image and revoke its blob.
    view.rerender(
      <MarkdownViewer path="preview.md" projectRoot="/proj" content={"![Home](./shots/home.png)"} onOpenFile={() => {}} />,
    );

    await waitFor(() => expect(view.container.querySelector("img")?.getAttribute("src")).toBe("blob:preview/1"));
    expect(fetchFileRaw).toHaveBeenCalledTimes(1);
    expect(revokeObjectURL).not.toHaveBeenCalled();
  });
});
