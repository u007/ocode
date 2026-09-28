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

  it("does not intercept non-http schemes", () => {
    const invoke = vi.fn();
    (window as WailsWindow)._wails = { invoke };
    const { getByText } = render(<MarkdownLink href="mailto:a@b.com">mail</MarkdownLink>);
    // Default not prevented (returns true) and the bridge untouched.
    expect(fireEvent.click(getByText("mail"))).toBe(true);
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
