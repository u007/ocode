import { render, waitFor, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const mockBypass = vi.hoisted(() => vi.fn(async () => {}));

const state = {
  url: "https://example.com/",
  history: ["https://example.com/"],
  historyIndex: 0,
  panelOpen: true,
  collapsed: false,
  status: 200,
  mode: "chrome" as const,
  error: "",
  consoleEvents: [],
  networkEvents: [],
};
const actions = {
  navigate: vi.fn(), back: vi.fn(), forward: vi.fn(),
  pushConsole: vi.fn(), pushNetwork: vi.fn(),
  clearConsole: vi.fn(), clearNetwork: vi.fn(),
  setCollapsed: vi.fn(), close: vi.fn(),
  setError: vi.fn(),
};
vi.mock("../../lib/browserStore", () => ({
  useBrowserStore: (_key: string) => ({ ...state }),
  useBrowserActions: () => actions,
}));
vi.mock("../../api/client", () => ({
  getBrowseBase: vi.fn(async () => "http://127.0.0.1:54321"),
  mintBrowseGrant: vi.fn(async () => "GRANT123"),
  bypassBrowseTLS: (...args: unknown[]) => (mockBypass as unknown as (...a: unknown[]) => unknown)(...args),
  normalizeBrowseURL: (u: string) => u,
  // Reconciled to Part 08's real signature: (base, grant, stateKey, url).
  browseSrc: (base: string, grant: string | null, key: string, _url: string) =>
    `${base}/b/${key}/https/example.com/${grant ? "?__grant=" + grant : ""}`,
}));

import { BrowserPanel } from "./BrowserPanel";

describe("BrowserPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    state.error = "";
    state.url = "https://example.com/";
    state.status = 200;
    (state as unknown as Record<string, unknown>).mode = "chrome";
    (state as unknown as Record<string, unknown>).collapsed = false;
  });

  it("mounts the iframe with the grant only on first load", async () => {
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    const iframe = await waitFor(() => container.querySelector("iframe")!);
    expect(iframe).toBeTruthy();
    await waitFor(() => expect(iframe.getAttribute("src")).toContain("__grant=GRANT123"));
    expect(iframe.getAttribute("sandbox")).toBe("allow-scripts allow-forms allow-same-origin");
  });

  it("does not mount an iframe when collapsed", () => {
    state.collapsed = true;
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="side" />);
    expect(container.querySelector("iframe")).toBeNull();
  });

  it("shows TLS bypass banner when error is TLS certificate", async () => {
    state.error = "TLS certificate not trusted — x509: certificate signed by unknown authority";
    (state as unknown as Record<string, unknown>).url = "https://192.168.0.99:3000/";
    (state as unknown as Record<string, unknown>).mode = "local";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    expect(container.querySelector('[data-testid="tls-bypass-banner"]')).toBeTruthy();
    expect(container.textContent).toContain("Certificate isn’t trusted");
    expect(container.textContent).toContain("192.168.0.99:3000");
  });

  it("does not show TLS banner for non-TLS errors", async () => {
    state.error = "connection refused";
    (state as unknown as Record<string, unknown>).mode = "local";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    expect(container.querySelector('[data-testid="tls-bypass-banner"]')).toBeNull();
  });

  it("does not show TLS bypass banner for chrome mode even with TLS error", async () => {
    state.error = "TLS certificate not trusted — x509: unknown authority";
    (state as unknown as Record<string, unknown>).mode = "chrome";
    (state as unknown as Record<string, unknown>).url = "https://192.168.0.99:3000/";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    expect(container.querySelector('[data-testid="tls-bypass-banner"]')).toBeNull();
  });

  it("shows TLS banner for IPv6 loopback with TLS error", async () => {
    state.error = "TLS certificate not trusted — x509: unknown authority";
    (state as unknown as Record<string, unknown>).mode = "local";
    (state as unknown as Record<string, unknown>).url = "https://[::1]:8443/";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    expect(container.querySelector('[data-testid="tls-bypass-banner"]')).toBeTruthy();
    expect(container.textContent).toContain("[::1]:8443");
  });

  it("calls bypassBrowseTLS and reloads on Continue anyway", async () => {
    state.error = "TLS certificate not trusted — x509: unknown authority";
    (state as unknown as Record<string, unknown>).url = "https://192.168.0.99:3000/page";
    (state as unknown as Record<string, unknown>).mode = "local";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    const btn = container.querySelector('[data-testid="tls-bypass-banner"] button') as HTMLButtonElement;
    expect(btn).toBeTruthy();
    const { fireEvent } = await import("@testing-library/react");
    await act(async () => {
      fireEvent.click(btn);
    });
    await waitFor(() => expect(mockBypass).toHaveBeenCalledWith("tab:abc", "192.168.0.99:3000"));
  });

  it("routes console messages only from the browse origin", async () => {
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    // The listener registers once browseBase resolves; the iframe src being
    // set is the same-effect-batch signal that the message listener is live.
    await waitFor(() => {
      const f = container.querySelector("iframe");
      expect(f?.getAttribute("src")).toContain("__grant=GRANT123");
    });
    act(() => {
      window.dispatchEvent(new MessageEvent("message", {
        origin: "http://127.0.0.1:54321",
        data: { type: "ocode:browse:console", stateKey: "tab:abc", level: "log", args: ["hi"], ts: 1 },
      }));
    });
    expect(actions.pushConsole).toHaveBeenCalled();
    act(() => {
      window.dispatchEvent(new MessageEvent("message", {
        origin: "https://evil.example",
        data: { type: "ocode:browse:console", stateKey: "tab:abc", level: "log", args: ["x"], ts: 2 },
      }));
    });
    expect(actions.pushConsole).toHaveBeenCalledTimes(1); // foreign origin dropped
  });
});
