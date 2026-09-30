import { render, fireEvent } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import MarkdownLink from "./MarkdownLink";
import { OPEN_EXTERNAL_MESSAGE_PREFIX } from "../../lib/externalLinks";

type WailsWindow = Window & { _wails?: { invoke?: (message: string) => void } };

afterEach(() => {
  delete (window as WailsWindow)._wails;
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("MarkdownLink", () => {
  it("routes an http(s) click through the desktop bridge and prevents default", () => {
    const invoke = vi.fn();
    (window as WailsWindow)._wails = { invoke };
    const { getByText } = render(<MarkdownLink href="https://example.com/x">link</MarkdownLink>);
    const anchor = getByText("link");

    // fireEvent.click returns false when the event's default was prevented.
    const notPrevented = fireEvent.click(anchor);

    expect(notPrevented).toBe(false);
    expect(invoke).toHaveBeenCalledWith(OPEN_EXTERNAL_MESSAGE_PREFIX + "https://example.com/x");
  });

  it("keeps href/target/rel so the link stays copyable and middle-clickable", () => {
    const { getByText } = render(<MarkdownLink href="https://example.com">link</MarkdownLink>);
    const anchor = getByText("link") as HTMLAnchorElement;
    expect(anchor.getAttribute("href")).toBe("https://example.com");
    expect(anchor.getAttribute("target")).toBe("_blank");
    expect(anchor.getAttribute("rel")).toBe("noopener noreferrer");
  });

  it("does not intercept non-http schemes that hand off to the OS", () => {
    const invoke = vi.fn();
    (window as WailsWindow)._wails = { invoke };
    const { getByText } = render(<MarkdownLink href="mailto:a@b.com">mail</MarkdownLink>);
    // Default not prevented (returns true) and the bridge untouched.
    expect(fireEvent.click(getByText("mail"))).toBe(true);
    expect(invoke).not.toHaveBeenCalled();
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
    const invoke = vi.fn();
    (window as WailsWindow)._wails = { invoke };
    const { getByText } = render(<MarkdownLink href="internal/x/foo.go">foo.go</MarkdownLink>);

    expect(fireEvent.click(getByText("foo.go"))).toBe(false);
    // …and it is not pushed at the OS either: it is not a resolvable link.
    expect(invoke).not.toHaveBeenCalled();
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
    const invoke = vi.fn();
    (window as WailsWindow)._wails = { invoke };
    const { getByText } = render(
      <MarkdownLink href="https://example.com" onClick={(e) => e.preventDefault()}>
        link
      </MarkdownLink>,
    );
    expect(fireEvent.click(getByText("link"))).toBe(false);
    expect(invoke).not.toHaveBeenCalled();
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
