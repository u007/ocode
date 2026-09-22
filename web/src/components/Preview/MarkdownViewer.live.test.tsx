import { describe, expect, it, vi, beforeEach } from "vitest";
import { act, render } from "@testing-library/react";
import MarkdownViewer from "./MarkdownViewer";
import { api } from "../../api/client";

// Same heavy-dependency isolation as the other Preview tests: FileEditor pulls
// Monaco (unresolvable in vitest) and MermaidViewer pulls mermaid.
vi.mock("../../api/client", () => ({ api: { getFileContent: vi.fn() } }));
vi.mock("../Files/FileEditor", () => ({ default: () => null }));
vi.mock("./MermaidViewer", () => ({ default: () => null }));

/**
 * jsdom has no layout engine: scrollHeight/clientHeight are 0 and assignment
 * to scrollTop is a no-op. The tests stub the geometry with
 * Object.defineProperty and assert the CONTRACT: a revision bump refetches,
 * the old content stays visible while the refresh is in flight (no loading
 * flash), and the follow effect pins scrollTop to scrollHeight when the
 * reader is pinned/following.
 */

function stubGeometry(el: HTMLElement, opts: { height: number; scroll: number }) {
  Object.defineProperty(el, "clientHeight", { configurable: true, value: opts.height });
  Object.defineProperty(el, "scrollHeight", { configurable: true, value: opts.scroll });
  let top = 0;
  Object.defineProperty(el, "scrollTop", {
    configurable: true,
    get: () => top,
    set: (v: number) => {
      top = v;
    },
  });
}

beforeEach(() => {
  vi.mocked(api.getFileContent).mockReset();
});

describe("MarkdownViewer live refresh + tail-follow", () => {
  it("pins the scroller to the tail when a revision lands while the reader is at the bottom", async () => {
    vi.mocked(api.getFileContent)
      .mockResolvedValueOnce({ content: "# v1\n\nfirst", is_binary: false })
      .mockResolvedValueOnce({ content: "# v2\n\n" + "para ".repeat(200), is_binary: false });

    const { container, rerender } = render(
      <MarkdownViewer path="doc.md" onOpenFile={() => {}} revision={0} followTail={false} />,
    );
    await act(async () => {
      await Promise.resolve();
    });
    const prose = container.querySelector(".prose") as HTMLElement;
    stubGeometry(prose, { height: 100, scroll: 200 });
    expect(prose.textContent).toContain("v1");
    expect(vi.mocked(api.getFileContent)).toHaveBeenCalledTimes(1);

    // Reader stays at the bottom (atBottomRef defaults true) → the pin must run.
    rerender(<MarkdownViewer path="doc.md" onOpenFile={() => {}} revision={1} followTail={true} />);
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(vi.mocked(api.getFileContent)).toHaveBeenCalledTimes(2);
    expect(prose.textContent).toContain("v2");
    expect(prose.scrollTop).toBe(200); // = stubbed scrollHeight
  });

  it("does NOT follow the tail when the reader scrolled up (lock) and followTail is off", async () => {
    vi.mocked(api.getFileContent)
      .mockResolvedValueOnce({ content: "# v1\n\nfirst", is_binary: false })
      .mockResolvedValueOnce({ content: "# v2\n\nmore", is_binary: false });

    const { container, rerender } = render(
      <MarkdownViewer path="doc.md" onOpenFile={() => {}} revision={0} followTail={false} />,
    );
    await act(async () => {
      await Promise.resolve();
    });
    const prose = container.querySelector(".prose") as HTMLElement;
    stubGeometry(prose, { height: 100, scroll: 500 });
    // Reader scrolls up: fire the scroll listener with a large distance.
    prose.scrollTop = 10;
    prose.dispatchEvent(new Event("scroll"));
    expect(prose.scrollTop).toBe(10);

    rerender(<MarkdownViewer path="doc.md" onOpenFile={() => {}} revision={1} followTail={false} />);
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    // Refetched, but position preserved (no pin: the reader scrolled away).
    expect(vi.mocked(api.getFileContent)).toHaveBeenCalledTimes(2);
    expect(prose.scrollTop).toBe(10);
  });

  it("followTail=true forces the tail even after the reader scrolled up (streaming re-arm)", async () => {
    vi.mocked(api.getFileContent)
      .mockResolvedValueOnce({ content: "# v1\n\nfirst", is_binary: false })
      .mockResolvedValueOnce({ content: "# v2\n\nmore", is_binary: false });

    const { container, rerender } = render(
      <MarkdownViewer path="doc.md" onOpenFile={() => {}} revision={0} followTail={true} />,
    );
    await act(async () => {
      await Promise.resolve();
    });
    const prose = container.querySelector(".prose") as HTMLElement;
    stubGeometry(prose, { height: 100, scroll: 500 });
    prose.scrollTop = 5;
    prose.dispatchEvent(new Event("scroll"));

    rerender(<MarkdownViewer path="doc.md" onOpenFile={() => {}} revision={1} followTail={true} />);
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(prose.scrollTop).toBe(500);
  });

  it("pins to the tail of the refetched content even when the turn ends before it lands", async () => {
    vi.mocked(api.getFileContent)
      .mockResolvedValueOnce({ content: "# v1\n\nshort", is_binary: false })
      .mockResolvedValueOnce({ content: "# v2\n\n" + "para ".repeat(200), is_binary: false });

    const { container, rerender } = render(
      <MarkdownViewer path="doc.md" onOpenFile={() => {}} revision={0} followTail={true} />,
    );
    await act(async () => {
      await Promise.resolve();
    });
    const prose = container.querySelector(".prose") as HTMLElement;
    // Geometry tracks the rendered document: the short v1 is 200px, the long
    // v2 is 4000px. A pin against the stale (v1) content would land at 200.
    Object.defineProperty(prose, "clientHeight", { configurable: true, value: 100 });
    Object.defineProperty(prose, "scrollHeight", {
      configurable: true,
      get: () => (prose.textContent?.includes("v2") ? 4000 : 200),
    });
    let top = 0;
    Object.defineProperty(prose, "scrollTop", {
      configurable: true,
      get: () => top,
      set: (v: number) => {
        top = v;
      },
    });

    // Revision bump, then the turn ends (followTail false) BEFORE the refetch
    // resolves — the follow must survive until the new content lands and pin to
    // the document actually on screen, not the stale one.
    rerender(<MarkdownViewer path="doc.md" onOpenFile={() => {}} revision={1} followTail={true} />);
    rerender(<MarkdownViewer path="doc.md" onOpenFile={() => {}} revision={1} followTail={false} />);
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(prose.textContent).toContain("v2");
    expect(prose.scrollTop).toBe(4000);
  });

  it("refetches on revision change without clearing the old content (no loading flash)", async () => {
    vi.mocked(api.getFileContent).mockResolvedValueOnce({ content: "# v1\n\nfirst", is_binary: false });
    const { container, rerender } = render(<MarkdownViewer path="doc.md" onOpenFile={() => {}} revision={0} />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(container.querySelector(".prose")?.textContent).toContain("v1");

    // Revision bump: refetch in flight. Old content must stay visible (no
    // setMd(null) reset → no "Loading…" flash).
    let resolveSecond: ((v: { content: string; is_binary: boolean }) => void) | undefined;
    vi.mocked(api.getFileContent).mockImplementationOnce(
      () =>
        new Promise((res) => {
          resolveSecond = res;
        }),
    );
    rerender(<MarkdownViewer path="doc.md" onOpenFile={() => {}} revision={1} />);
    await act(async () => {
      await Promise.resolve();
    });
    expect(container.querySelector(".prose")?.textContent).toContain("v1");
    expect(container.textContent).not.toContain("Loading…");

    await act(async () => {
      resolveSecond?.({ content: "# v2\n\nupdated", is_binary: false });
      await Promise.resolve();
    });
    expect(container.querySelector(".prose")?.textContent).toContain("v2");
  });

  it("does not refetch in controlled mode (Files-tab split) on revision change", async () => {
    let fetched = 0;
    vi.mocked(api.getFileContent).mockImplementation(async () => {
      fetched += 1;
      return { content: "x", is_binary: false };
    });
    const { rerender } = render(
      <MarkdownViewer path="doc.md" onOpenFile={() => {}} content={"# controlled"} revision={0} />,
    );
    await act(async () => {});
    rerender(<MarkdownViewer path="doc.md" onOpenFile={() => {}} content={"# controlled"} revision={5} />);
    await act(async () => {});
    expect(fetched).toBe(0);
  });
});