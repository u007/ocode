import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../../../api/client";
import type { McpAppResource } from "../../../api/types";
import { McpAppView, type McpAppViewProps } from "./McpAppView";

const hoisted = vi.hoisted(() => ({
  fetchMcpAppResource: vi.fn(),
  callMcpAppTool: vi.fn(),
  openExternalURL: vi.fn(),
}));

vi.mock("../../../lib/externalLinks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../../lib/externalLinks")>();
  return { ...actual, openExternalURL: hoisted.openExternalURL };
});

vi.mock("../../../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../../api/client")>();
  return {
    ...actual,
    api: {
      fetchMcpAppResource: hoisted.fetchMcpAppResource,
      callMcpAppTool: hoisted.callMcpAppTool,
    },
  };
});

vi.mock("../../../lib/appearanceTheme", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../../lib/appearanceTheme")>();
  return {
    ...actual,
    useAppearanceTheme: () => ({ theme: { kind: "dark" }, colorThemeId: "test", setColorThemeId: vi.fn() }),
  };
});

const RESOURCE: McpAppResource = {
  html: "<!doctype html><html><head><title>Atlas</title></head><body>screen</body></html>",
  csp: { connectDomains: ["https://api.example.com"] },
  prefers_border: false,
};

// The resource cache is module-level, so every test gets its own session id.
let sessionSeq = 0;
function props(overrides: Partial<McpAppViewProps> = {}): McpAppViewProps {
  return {
    sessionId: `sess-${++sessionSeq}`,
    toolCallId: "call-1",
    mcpApp: { server: "atlas", tool: "show_screen", resource_uri: "ui://atlas/screen" },
    toolArguments: '{"screen":1}',
    output: "shown",
    active: true,
    ...overrides,
  };
}

beforeEach(() => {
  hoisted.fetchMcpAppResource.mockReset();
  hoisted.callMcpAppTool.mockReset();
  hoisted.openExternalURL.mockReset();
});

describe("McpAppView", () => {
  it("renders only an Open view button for an inactive card and fetches nothing", () => {
    render(<McpAppView {...props({ active: false, toolCallId: "older-card" })} />);
    expect(screen.getByRole("button", { name: "Open view" })).toBeInTheDocument();
    expect(document.querySelector("iframe")).toBeNull();
    expect(hoisted.fetchMcpAppResource).not.toHaveBeenCalled();
  });

  it("starts an inactive card when Open view is clicked", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    const p = props({ active: false, toolCallId: "click-card" });
    render(<McpAppView {...p} />);
    fireEvent.click(screen.getByRole("button", { name: "Open view" }));
    await screen.findByTitle("show_screen");
    expect(hoisted.fetchMcpAppResource).toHaveBeenCalledWith(
      p.sessionId,
      "atlas",
      "ui://atlas/screen",
      undefined,
      expect.any(AbortSignal),
    );
  });

  it("fetches an active card and renders a sandboxed iframe with only allow-scripts", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    const p = props({ toolCallId: "sandbox-card", host: "box" });
    render(<McpAppView {...p} />);
    const frame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    expect(frame.tagName).toBe("IFRAME");
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame.getAttribute("srcdoc")).toContain("Content-Security-Policy");
    expect(frame.getAttribute("srcdoc")).toContain("connect-src https://api.example.com");
    expect(hoisted.fetchMcpAppResource).toHaveBeenCalledWith(
      p.sessionId,
      "atlas",
      "ui://atlas/screen",
      "box",
      expect.any(AbortSignal),
    );
  });

  it("ignores a message whose source is not the widget frame", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    render(<McpAppView {...props({ toolCallId: "source-card" })} />);
    const frame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    const post = vi.spyOn(frame.contentWindow as Window, "postMessage");
    const ping = { jsonrpc: "2.0", id: 42, method: "ping" };

    act(() => {
      window.dispatchEvent(new MessageEvent("message", { data: ping, source: window }));
    });
    expect(post).not.toHaveBeenCalled();

    // Positive control: the same message from the frame's own window is answered.
    act(() => {
      window.dispatchEvent(new MessageEvent("message", { data: ping, source: frame.contentWindow, origin: "null" }));
    });
    expect(post).toHaveBeenCalledWith({ jsonrpc: "2.0", id: 42, result: {} }, "*");
  });

  it("shows the live-session hint on a 409", async () => {
    hoisted.fetchMcpAppResource.mockRejectedValue(new ApiError("session not loaded", 409, { error: "session not loaded" }));
    render(<McpAppView {...props({ toolCallId: "conflict-card" })} />);
    expect(
      await screen.findByText("This view needs a live session. Send a message in this chat, then retry."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(document.querySelector("iframe")).toBeNull();
  });

  it("refetches after Retry when the first attempt failed", async () => {
    hoisted.fetchMcpAppResource
      .mockRejectedValueOnce(new ApiError("boom", 502, { error: "boom" }))
      .mockResolvedValueOnce(RESOURCE);
    render(<McpAppView {...props({ toolCallId: "retry-card" })} />);
    fireEvent.click(await screen.findByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.getByTitle("show_screen")).toBeInTheDocument());
    expect(hoisted.fetchMcpAppResource).toHaveBeenCalledTimes(2);
  });
});

// Sends a protocol message to the window as if it came from `frame`.
function fromFrame(frame: HTMLIFrameElement, data: object) {
  act(() => {
    window.dispatchEvent(new MessageEvent("message", { data, source: frame.contentWindow, origin: "null" }));
  });
}

function requestDisplayMode(frame: HTMLIFrameElement, id: number, mode: "inline" | "fullscreen") {
  fromFrame(frame, { jsonrpc: "2.0", id, method: "ui/request-display-mode", params: { mode } });
}

// Opens fullscreen through the widget's own request and waits for the Close button.
async function openFullscreen(frame: HTMLIFrameElement): Promise<HTMLElement> {
  requestDisplayMode(frame, 1, "fullscreen");
  return screen.findByRole("button", { name: "Close" });
}

describe("McpAppView fullscreen", () => {
  it("portals the fullscreen frame to document.body and keeps an inline placeholder", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    const p = props();
    const { container } = render(<McpAppView {...p} />);
    const inlineFrame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;

    const close = await openFullscreen(inlineFrame);
    // Close takes focus on open, so Escape works without clicking the SPA first.
    expect(close).toHaveFocus();

    const fsFrame = screen.getByTitle("show_screen") as HTMLIFrameElement;
    expect(fsFrame).not.toBe(inlineFrame);
    expect(document.body.contains(fsFrame)).toBe(true);
    expect(container.contains(fsFrame)).toBe(false);
    expect(fsFrame.parentElement?.className).toContain("fixed inset-0");
    expect(fsFrame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(container.textContent).toContain("Shown fullscreen");
  });

  it("Close returns to the inline view", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    const { container } = render(<McpAppView {...props()} />);
    const inlineFrame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    await openFullscreen(inlineFrame);

    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Close" })).toBeNull());
    expect(container.textContent).not.toContain("Shown fullscreen");
    expect(container.querySelector("iframe")).not.toBeNull();
  });

  it("Escape returns to the inline view", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    render(<McpAppView {...props()} />);
    const inlineFrame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    await openFullscreen(inlineFrame);

    fireEvent.keyDown(window, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("button", { name: "Close" })).toBeNull());
  });

  it("re-initializes the fullscreen frame with displayMode fullscreen and the container size", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    render(<McpAppView {...props()} />);
    const inlineFrame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    await openFullscreen(inlineFrame);

    const fsFrame = screen.getByTitle("show_screen") as HTMLIFrameElement;
    const post = vi.spyOn(fsFrame.contentWindow as Window, "postMessage").mockImplementation(() => {});
    fromFrame(fsFrame, { jsonrpc: "2.0", id: 7, method: "ui/initialize", params: { protocolVersion: "2026-01-26" } });

    expect(post).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 7,
        result: expect.objectContaining({
          hostContext: expect.objectContaining({
            displayMode: "fullscreen",
            containerDimensions: { width: expect.any(Number), height: expect.any(Number) },
          }),
        }),
      }),
      "*",
    );
  });

  it("re-initializes the inline frame with displayMode inline after closing", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    render(<McpAppView {...props()} />);
    const inlineFrame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    await openFullscreen(inlineFrame);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Close" })).toBeNull());

    const again = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    const post = vi.spyOn(again.contentWindow as Window, "postMessage").mockImplementation(() => {});
    fromFrame(again, { jsonrpc: "2.0", id: 8, method: "ui/initialize" });

    expect(post).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 8,
        result: expect.objectContaining({
          hostContext: expect.objectContaining({ displayMode: "inline", containerDimensions: { maxHeight: 800 } }),
        }),
      }),
      "*",
    );
  });

  it("posts the request-display-mode response before the mode switches", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    render(<McpAppView {...props()} />);
    const inlineFrame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;

    let closeVisibleAtResponse: boolean | null = null;
    const post = vi.spyOn(inlineFrame.contentWindow as Window, "postMessage").mockImplementation((msg: unknown) => {
      if ((msg as { id?: unknown }).id === 3) {
        closeVisibleAtResponse = screen.queryByRole("button", { name: "Close" }) !== null;
      }
    });

    requestDisplayMode(inlineFrame, 3, "fullscreen");
    expect(post).toHaveBeenCalledWith({ jsonrpc: "2.0", id: 3, result: { mode: "fullscreen" } }, "*");
    // The response went out while the view was still inline.
    expect(closeVisibleAtResponse).toBe(false);
    await screen.findByRole("button", { name: "Close" });
  });

  it("a remount after unmounting while fullscreen starts inline", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    const p = props();
    const first = render(<McpAppView {...p} />);
    const inlineFrame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    await openFullscreen(inlineFrame);

    first.unmount();
    expect(document.querySelector("iframe")).toBeNull();

    render(<McpAppView {...p} />);
    const again = await screen.findByTitle("show_screen");
    expect(screen.queryByRole("button", { name: "Close" })).toBeNull();
    expect(again.parentElement?.className).toContain("relative");
  });
});

describe("McpAppView width", () => {
  it("caps the inline wrapper at 880px and leaves the fullscreen wrapper uncapped", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    render(<McpAppView {...props()} />);
    const inlineFrame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    const inlineWrapper = inlineFrame.parentElement as HTMLElement;
    expect(inlineWrapper.className).toContain("w-full");
    expect(inlineWrapper.className).toContain("max-w-[880px]");

    await openFullscreen(inlineFrame);
    const fsFrame = screen.getByTitle("show_screen") as HTMLIFrameElement;
    const fsWrapper = fsFrame.parentElement as HTMLElement;
    expect(fsWrapper.className).toContain("fixed inset-0");
    expect(fsWrapper.className).not.toContain("max-w-");
  });
});

// Opens a link request from the frame. The row appears; nothing opens yet.
function requestLink(frame: HTMLIFrameElement, id: number, url: string) {
  fromFrame(frame, { jsonrpc: "2.0", id, method: "ui/open-link", params: { url } });
}

describe("McpAppView navigation guard", () => {
  it("closes the view on a second load, ignores the old window, and reloads from the cache", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    const { container } = render(<McpAppView {...props()} />);
    const frame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    const oldWindow = frame.contentWindow as Window;

    // jsdom fires the srcdoc document's own load once, on its own; that first
    // load keeps the view up. Let it land before the self-navigation below.
    await new Promise((resolve) => setTimeout(resolve, 100));
    expect(container.querySelector("iframe")).toBe(frame);

    // A second load on the same element is a self-navigation: the view closes.
    fireEvent.load(frame);
    expect(screen.getByText("This view navigated away from its document and was closed.")).toBeInTheDocument();
    expect(document.querySelector("iframe")).toBeNull();

    // The old window can still dispatch; nothing reaches the tool bridge.
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          data: { jsonrpc: "2.0", id: 9, method: "tools/call", params: { name: "stop", arguments: {} } },
          source: oldWindow,
          origin: "null",
        }),
      );
    });
    expect(hoisted.callMcpAppTool).not.toHaveBeenCalled();

    // Reload mounts a fresh iframe from the cached resource (no refetch).
    fireEvent.click(screen.getByRole("button", { name: "Reload view" }));
    const fresh = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    expect(fresh).not.toBe(frame);
    expect(screen.queryByText("This view navigated away from its document and was closed.")).toBeNull();
    expect(hoisted.fetchMcpAppResource).toHaveBeenCalledTimes(1);
  });

  it("ignores a message whose origin is not null even with the correct source", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    render(<McpAppView {...props()} />);
    const frame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    const post = vi.spyOn(frame.contentWindow as Window, "postMessage");

    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          data: { jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "stop", arguments: {} } },
          source: frame.contentWindow,
          origin: "http://evil.example",
        }),
      );
    });
    expect(hoisted.callMcpAppTool).not.toHaveBeenCalled();
    expect(post).not.toHaveBeenCalled();
  });
});

describe("McpAppView open-link confirmation", () => {
  it("shows a confirmation row and does not open the URL", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    render(<McpAppView {...props()} />);
    const frame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    const post = vi.spyOn(frame.contentWindow as Window, "postMessage");

    requestLink(frame, 4, "https://example.com/docs");

    // The request is accepted for confirmation, not refused.
    expect(post).toHaveBeenCalledWith({ jsonrpc: "2.0", id: 4, result: {} }, "*");
    expect(hoisted.openExternalURL).not.toHaveBeenCalled();
    expect(screen.getByText("This view wants to open")).toBeInTheDocument();
    expect(screen.getByText("https://example.com/docs")).toHaveAttribute("title", "https://example.com/docs");
  });

  it("Open calls openExternalURL once and removes the row", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    render(<McpAppView {...props()} />);
    const frame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    requestLink(frame, 4, "https://example.com/docs");

    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(hoisted.openExternalURL).toHaveBeenCalledTimes(1);
    expect(hoisted.openExternalURL).toHaveBeenCalledWith("https://example.com/docs");
    expect(screen.queryByText("This view wants to open")).toBeNull();
  });

  it("Cancel removes the row without opening", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    render(<McpAppView {...props()} />);
    const frame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    requestLink(frame, 4, "https://example.com/docs");

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(hoisted.openExternalURL).not.toHaveBeenCalled();
    expect(screen.queryByText("This view wants to open")).toBeNull();
  });

  it("a javascript: URL renders nothing and is refused", async () => {
    hoisted.fetchMcpAppResource.mockResolvedValue(RESOURCE);
    render(<McpAppView {...props()} />);
    const frame = (await screen.findByTitle("show_screen")) as HTMLIFrameElement;
    const post = vi.spyOn(frame.contentWindow as Window, "postMessage");

    requestLink(frame, 5, "javascript:alert(1)");

    expect(post).toHaveBeenCalledWith(expect.objectContaining({ id: 5, error: expect.objectContaining({ code: -32602 }) }), "*");
    expect(screen.queryByText("This view wants to open")).toBeNull();
    expect(hoisted.openExternalURL).not.toHaveBeenCalled();
  });
});
