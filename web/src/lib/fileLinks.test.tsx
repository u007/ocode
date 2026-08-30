import { render, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import ReactMarkdown from "react-markdown";
import { rehypeFileLinks, FileLinkFromNode, linkifyPlainText, OPEN_FILE_EVENT, splitPathToken } from "./fileLinks";

describe("fileLinks", () => {
  describe("splitPathToken", () => {
    it("splits path:line:col", () => {
      expect(splitPathToken("src/foo.ts:12:3")).toEqual({ path: "src/foo.ts", line: 12 });
      expect(splitPathToken("src/foo.ts:42")).toEqual({ path: "src/foo.ts", line: 42 });
      expect(splitPathToken("src/foo.ts")).toEqual({ path: "src/foo.ts" });
    });
  });

  describe("FileLink dispatch", () => {
    beforeEach(() => vi.restoreAllMocks());

    it("click dispatches ocode:open-file with path and line", async () => {
      const handler = vi.fn();
      window.addEventListener(OPEN_FILE_EVENT, handler as EventListener);
      const nodes = linkifyPlainText("see src/app.ts:10 for details");
      const { container } = render(<div>{nodes}</div>);
      const link = container.querySelector('[role="link"]') as HTMLElement;
      expect(link).toBeTruthy();
      fireEvent.click(link);
      expect(handler).toHaveBeenCalledTimes(1);
      const detail = (handler.mock.calls[0][0] as CustomEvent).detail;
      expect(detail).toEqual({ path: "src/app.ts", line: 10 });
      window.removeEventListener(OPEN_FILE_EVENT, handler as EventListener);
    });

    it("Enter key dispatches same event", () => {
      const handler = vi.fn();
      window.addEventListener(OPEN_FILE_EVENT, handler as EventListener);
      const nodes = linkifyPlainText("open web/src/App.tsx:99");
      const { container } = render(<div>{nodes}</div>);
      const link = container.querySelector('[role="link"]') as HTMLElement;
      fireEvent.keyDown(link, { key: "Enter" });
      expect(handler).toHaveBeenCalledTimes(1);
      const detail = (handler.mock.calls[0][0] as CustomEvent).detail;
      expect(detail.path).toBe("web/src/App.tsx");
      expect(detail.line).toBe(99);
      window.removeEventListener(OPEN_FILE_EVENT, handler as EventListener);
    });

    it("Space key dispatches same event", () => {
      const handler = vi.fn();
      window.addEventListener(OPEN_FILE_EVENT, handler as EventListener);
      const nodes = linkifyPlainText("see internal/server/handler.go:123");
      const { container } = render(<div>{nodes}</div>);
      const link = container.querySelector('[role="link"]') as HTMLElement;
      fireEvent.keyDown(link, { key: " " });
      expect(handler).toHaveBeenCalledTimes(1);
      window.removeEventListener(OPEN_FILE_EVENT, handler as EventListener);
    });

    it("markdown rendered filelink dispatches event", () => {
      const handler = vi.fn();
      window.addEventListener(OPEN_FILE_EVENT, handler as EventListener);
      render(
        // @ts-expect-error custom hast element
        <ReactMarkdown rehypePlugins={[rehypeFileLinks]} components={{ filelink: FileLinkFromNode }}>
          {"check `src/foo.ts:5` now"}
        </ReactMarkdown>,
      );
      // markdown inline code is still linkified per fileLinks spec
      const link = document.querySelector('[role="link"]') as HTMLElement;
      expect(link).toBeTruthy();
      fireEvent.click(link);
      expect(handler).toHaveBeenCalledTimes(1);
      window.removeEventListener(OPEN_FILE_EVENT, handler as EventListener);
    });

    it("file links carry the stable `file-link` class (surface-override hook)", () => {
      const nodes = linkifyPlainText("see src/app.ts:10");
      const { container } = render(<div>{nodes}</div>);
      const link = container.querySelector('[role="link"]') as HTMLElement;
      // The class is what colored surfaces (inline-code chips, user bubbles)
      // target to swap text-link for their own foreground token.
      expect(link.className).toContain("file-link");
      expect(link.className).toContain("text-link");
    });

    it("linkifies .ocode/uploads paths containing spaces (incl. U+202F)", () => {
      const handler = vi.fn();
      window.addEventListener(OPEN_FILE_EVENT, handler as EventListener);
      // Exact shape of a macOS screenshot name: regular spaces plus a
      // narrow no-break space (U+202F) before "PM".
      const name = "Screenshot 2026-08-30 at 1.58.00\u202fPM.png";
      const nodes = linkifyPlainText(`uploaded .ocode/uploads/${name} to show`);
      const { container } = render(<div>{nodes}</div>);
      const link = container.querySelector('[role="link"]') as HTMLElement;
      expect(link).toBeTruthy();
      expect(link.textContent).toBe(`.ocode/uploads/${name}`);
      fireEvent.click(link);
      expect(handler).toHaveBeenCalledTimes(1);
      const detail = (handler.mock.calls[0][0] as CustomEvent).detail;
      expect(detail).toEqual({ path: `.ocode/uploads/${name}`, line: undefined });
      window.removeEventListener(OPEN_FILE_EVENT, handler as EventListener);
    });

    it("does not linkify image extensions outside the uploads anchor", () => {
      // Image extensions are matched only by the .ocode/uploads branch, so
      // bare prose like "world.png" must NOT become a link.
      const nodes = linkifyPlainText("see hello world.png here");
      const { container } = render(<div>{nodes}</div>);
      expect(container.querySelectorAll('[role="link"]')).toHaveLength(0);
    });

    it("handler in App would switch Files tab - event contract", async () => {
      // Simulate the App listener: on event, it calls openFileAndShow which sets activeView="files"
      let activeView = "sessions";
      const openFileAndShow = vi.fn(async (path: string, line?: number) => {
        void path; void line;
        activeView = "files";
      });
      const appHandler = (e: Event) => {
        const detail = (e as CustomEvent).detail as { path: string; line?: number };
        if (!detail?.path) return;
        void openFileAndShow(detail.path, detail.line);
      };
      window.addEventListener(OPEN_FILE_EVENT, appHandler as EventListener);
      const nodes = linkifyPlainText("see pkg/foo/bar.go:7");
      const { container } = render(<div>{nodes}</div>);
      fireEvent.click(container.querySelector('[role="link"]') as HTMLElement);
      // allow microtask
      await new Promise((r) => setTimeout(r, 0));
      expect(openFileAndShow).toHaveBeenCalledWith("pkg/foo/bar.go", 7);
      expect(activeView).toBe("files");
      window.removeEventListener(OPEN_FILE_EVENT, appHandler as EventListener);
    });
  });
});
