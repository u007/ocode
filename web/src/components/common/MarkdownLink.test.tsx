import { render, fireEvent } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import MarkdownLink from "./MarkdownLink";
import { OPEN_FILE_EVENT, type OpenFileDetail } from "../../lib/fileLinks";
import { URLOpenOwnerProvider } from "../../lib/urlOpenContext";

const requestURL = vi.hoisted(() => vi.fn());
vi.mock("../../lib/externalLinks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../lib/externalLinks")>();
  return { ...actual, requestURLOpen: requestURL };
});

beforeEach(() => requestURL.mockClear());

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("MarkdownLink", () => {
  it("requests the chooser for an http(s) click and prevents default", () => {
    const { getByText } = render(<MarkdownLink href="https://example.com/x">link</MarkdownLink>);
    const anchor = getByText("link");

    // fireEvent.click returns false when the event's default was prevented.
    const notPrevented = fireEvent.click(anchor);

    expect(notPrevented).toBe(false);
    expect(requestURL).toHaveBeenCalledWith("https://example.com/x", undefined);
  });

  it("carries the owning project into the chooser request", () => {
    const { getByText } = render(
      <URLOpenOwnerProvider projectPath="/remote" host="dev@example.com">
        <MarkdownLink href="http://localhost:3000">link</MarkdownLink>
      </URLOpenOwnerProvider>,
    );
    fireEvent.click(getByText("link"));
    expect(requestURL).toHaveBeenCalledWith("http://localhost:3000", {
      projectPath: "/remote",
      host: "dev@example.com",
    });
  });

  it("routes middle-click through the chooser instead of native navigation", () => {
    const { getByText } = render(<MarkdownLink href="https://example.com/x">link</MarkdownLink>);
    const event = new MouseEvent("auxclick", { button: 1, bubbles: true, cancelable: true });
    expect(fireEvent(getByText("link"), event)).toBe(false);
    expect(requestURL).toHaveBeenCalledWith("https://example.com/x", undefined);
  });

  it("keeps href/target/rel so the link stays copyable and middle-clickable", () => {
    const { getByText } = render(<MarkdownLink href="https://example.com">link</MarkdownLink>);
    const anchor = getByText("link") as HTMLAnchorElement;
    expect(anchor.getAttribute("href")).toBe("https://example.com");
    expect(anchor.getAttribute("target")).toBe("_blank");
    expect(anchor.getAttribute("rel")).toBe("noopener noreferrer");
  });

  it("does not intercept non-http schemes that hand off to the OS", () => {
    const { getByText } = render(<MarkdownLink href="mailto:a@b.com">mail</MarkdownLink>);
    // Default not prevented (returns true) and the bridge untouched.
    expect(fireEvent.click(getByText("mail"))).toBe(true);
    expect(requestURL).not.toHaveBeenCalled();
  });

  it("keeps an in-page hash anchor on the default handler", () => {
    const { getByText } = render(<MarkdownLink href="#section">jump</MarkdownLink>);
    expect(fireEvent.click(getByText("jump"))).toBe(true);
  });

  // Regression: gating preventDefault on isHTTPURL let a relative link in
  // assistant prose — [foo.go](internal/x/foo.go) — fall through to the
  // browser. In the desktop webview that resolves against ocode's own origin,
  // so the SPA fallback replaced the whole app with index.html.
  it("prevents default on a relative link so it cannot navigate the app away", () => {
    const { getByText } = render(<MarkdownLink href="internal/x/foo.go">foo.go</MarkdownLink>);

    expect(fireEvent.click(getByText("foo.go"))).toBe(false);
    // …and it is not pushed at the OS either: it is not a resolvable link.
    expect(requestURL).not.toHaveBeenCalled();
  });

  it("opens a home-directory file link in the Files tab, not the OS browser", () => {
    const opened: OpenFileDetail[] = [];
    const listener = (e: Event) => opened.push((e as CustomEvent<OpenFileDetail>).detail);
    window.addEventListener(OPEN_FILE_EVENT, listener);
    try {
      const { getByText } = render(
        <MarkdownLink href="~/.agent/diagrams/fastbrowse-for-ocode.src.html">diagram</MarkdownLink>,
      );
      expect(fireEvent.click(getByText("diagram"))).toBe(false);
      expect(opened).toEqual([{ path: "~/.agent/diagrams/fastbrowse-for-ocode.src.html" }]);
      expect(requestURL).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener(OPEN_FILE_EVENT, listener);
    }
  });

  it("prevents default on a root-relative link", () => {
    const { getByText } = render(<MarkdownLink href="/settings">settings</MarkdownLink>);
    expect(fireEvent.click(getByText("settings"))).toBe(false);
  });

  it("prevents default on a protocol-relative link", () => {
    const { getByText } = render(
      <MarkdownLink href="//evil.example/x">proto-rel</MarkdownLink>,
    );
    expect(fireEvent.click(getByText("proto-rel"))).toBe(false);
  });

  it("still lets a caller's own onClick cancel the interception", () => {
    const { getByText } = render(
      <MarkdownLink href="https://example.com" onClick={(e) => e.preventDefault()}>
        link
      </MarkdownLink>,
    );
    expect(fireEvent.click(getByText("link"))).toBe(false);
    expect(requestURL).not.toHaveBeenCalled();
  });

  it("never leaks react-markdown's `node` prop onto the DOM", () => {
    const { getByText } = render(
      <MarkdownLink href="https://example.com" node={{ type: "element" } as unknown}>
        link
      </MarkdownLink>,
    );
    expect(getByText("link").getAttribute("node")).toBeNull();
  });
});
