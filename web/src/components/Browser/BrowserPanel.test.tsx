import { render, waitFor, act, fireEvent, screen } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const mockBypass = vi.hoisted(() => vi.fn(async () => {}));
const mockHtrNotice = vi.hoisted(() => vi.fn(() => ""));

const state = {
  url: "https://example.com/",
  history: ["https://example.com/"],
  historyIndex: 0,
  panelOpen: true,
  collapsed: false,
  status: 200,
  loading: false,
  mode: "chrome" as const,
  userMode: null as "local" | "chrome" | null,
  error: "",
  consoleEvents: [],
  networkEvents: [],
  responseBodies: {},
  perfMetrics: {},
  perfRecording: false,
};
const actions = {
  navigate: vi.fn(), back: vi.fn(), forward: vi.fn(),
  pushConsole: vi.fn(), pushNetwork: vi.fn(),
  clearConsole: vi.fn(), clearNetwork: vi.fn(),
  setCollapsed: vi.fn(), close: vi.fn(),
  setError: vi.fn(), setUserMode: vi.fn(),
  syncPageURL: vi.fn((_key: string, _url: string, _kind: string): { url: string; idx: number } | null => null),
};
vi.mock("../../lib/browserStore", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../lib/browserStore")>();
  return {
    ...actual,
    useBrowserStore: (_key: string) => ({ ...state }),
    useBrowserActions: () => actions,
  };
});
const mockIsRemoteSession = vi.hoisted(() => vi.fn(() => false));
const mockBrowseRemoteMode = vi.hoisted(() => vi.fn(() => false));
const mockStreamViaMain = vi.hoisted(() => vi.fn((_host: string) => false));
const mockGetBrowseBase = vi.hoisted(() => vi.fn(async (_host: string) => "http://127.0.0.1:54321"));
// Default: this client cannot use a root-mounted origin, so the panel loads in prefix mode.
const mockOpenTabOrigin = vi.hoisted(() => vi.fn(async (_key: string, _url: string, _host: string): Promise<unknown> => ({ kind: "unavailable" })));
const mockMintGrant = vi.hoisted(() => vi.fn(async () => "GRANT123"));
vi.mock("../../api/client", () => ({
  getBrowseBase: (host: string) => mockGetBrowseBase(host),
  getBrowseHTRNotice: mockHtrNotice,
  getBrowseRemoteMode: mockBrowseRemoteMode,
  getBrowseStreamViaMain: mockStreamViaMain,
  mintBrowseGrant: mockMintGrant,
  openBrowseTabOrigin: (key: string, url: string, host: string) => mockOpenTabOrigin(key, url, host),
  bypassBrowseTLS: (...args: unknown[]) => (mockBypass as unknown as (...a: unknown[]) => unknown)(...args),
  normalizeBrowseURL: (u: string) => u,
  isRemoteSession: mockIsRemoteSession,
  // Reconciled to Part 08's real signature: (base, grant, stateKey, url).
  browseSrc: (base: string, grant: string | null, key: string, _url: string) =>
    `${base}/b/${key}/https/example.com/${grant ? "?__grant=" + grant : ""}`,
}));

import { BrowserPanel } from "./BrowserPanel";

// Mock ChromeViewport to a stub so surface-selection is testable without the
// socket stack.
vi.mock("./ChromeViewport", () => ({
  ChromeViewport: (props: Record<string, unknown>) => (
    <div data-testid="chrome-viewport" data-url={String(props.url)} data-host={String(props.host)}>
      <input data-testid="chrome-focus-target" />
    </div>
  ),
}));

describe("BrowserPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetBrowseBase.mockImplementation(async () => "http://127.0.0.1:54321");
    mockOpenTabOrigin.mockImplementation(async () => ({ kind: "unavailable" }));
    mockMintGrant.mockImplementation(async () => "GRANT123");
    mockHtrNotice.mockReturnValue("");
    mockIsRemoteSession.mockReturnValue(false);
    mockBrowseRemoteMode.mockReturnValue(false);
    mockStreamViaMain.mockReturnValue(false);
    state.error = "";
    state.url = "https://example.com/";
    state.status = 200;
    state.loading = false;
    (state as unknown as Record<string, unknown>).mode = "chrome";
    (state as unknown as Record<string, unknown>).userMode = null;
    (state as unknown as Record<string, unknown>).collapsed = false;
    state.history = ["https://example.com/"];
    state.historyIndex = 0;
  });

  it("chrome mode mounts ChromeViewport and no iframe", async () => {
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    await waitFor(() => expect(container.querySelector("[data-testid='chrome-viewport']")).toBeTruthy());
    expect(container.querySelector("iframe")).toBeNull();
  });

  it("renders a localhost page in Chrome (streamed) when the client is on another device", async () => {
    mockStreamViaMain.mockReturnValue(true);
    state.url = "https://localhost:3510/";
    (state as unknown as Record<string, unknown>).mode = "local";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    await waitFor(() => expect(container.querySelector("[data-testid='chrome-viewport']")).toBeTruthy());
    expect(container.querySelector("iframe")).toBeNull();
  });

  it("hands the panel's project host to ChromeViewport when the host streams over the main origin", async () => {
    mockStreamViaMain.mockImplementation((h: string) => h === "ssh:box");
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="ssh:box" />);
    await waitFor(() => expect(container.querySelector("[data-testid='chrome-viewport']")).toBeTruthy());
    expect(container.querySelector("[data-testid='chrome-viewport']")?.getAttribute("data-host")).toBe("ssh:box");
  });

  it("blurs a backgrounded viewport so stale browser focus cannot capture input", async () => {
    const view = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" active host="" />);
    const input = await screen.findByTestId("chrome-focus-target");
    input.focus();
    expect(document.activeElement).toBe(input);

    view.rerender(<BrowserPanel stateKey={"tab:abc" as any} mode="full" active={false} host="" />);
    await waitFor(() => expect(document.activeElement).not.toBe(input));
  });

  it("keeps an HTR startup failure visible while browsing continues", async () => {
    mockHtrNotice.mockReturnValue("daemon failed to start");
    render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    expect(await screen.findByTestId("htr-fallback-banner")).toHaveTextContent("daemon failed to start");
    expect(screen.getByTestId("chrome-viewport")).toBeTruthy();
  });

  it("local mode mounts the iframe and no chrome viewport", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    await waitFor(() => expect(container.querySelector("iframe")).toBeTruthy());
    expect(container.querySelector("[data-testid='chrome-viewport']")).toBeNull();
  });

  it("mode null falls back on the url host: public → chrome, private → local", async () => {
    (state as unknown as Record<string, unknown>).mode = null;
    const pub = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    await waitFor(() => expect(pub.container.querySelector("[data-testid='chrome-viewport']")).toBeTruthy());
    pub.unmount();
    (state as unknown as Record<string, unknown>).url = "http://localhost:3000/";
    const priv = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    await waitFor(() => expect(priv.container.querySelector("iframe")).toBeTruthy());
    expect(priv.container.querySelector("[data-testid='chrome-viewport']")).toBeNull();
  });

  it("collapsed mounts neither surface", () => {
    (state as unknown as Record<string, unknown>).collapsed = true;
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    expect(container.querySelector("iframe")).toBeNull();
    expect(container.querySelector("[data-testid='chrome-viewport']")).toBeNull();
  });

  it("mounts the iframe with the grant only on first load", async () => {
    // Local mode: the iframe path (chrome mode mounts the viewport instead).
    (state as unknown as Record<string, unknown>).mode = "local";
    (state as unknown as Record<string, unknown>).url = "http://localhost:3000/";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    const iframe = await waitFor(() => container.querySelector("iframe")!);
    expect(iframe).toBeTruthy();
    await waitFor(() => expect(iframe.getAttribute("src")).toContain("__grant=GRANT123"));
    expect(iframe.getAttribute("sandbox")).toBe("allow-scripts allow-forms allow-same-origin");
  });

  it("does not mount an iframe when collapsed", () => {
    state.collapsed = true;
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="side" host="" />);
    expect(container.querySelector("iframe")).toBeNull();
  });

  it("shows TLS bypass banner when error is TLS certificate", async () => {
    state.error = "TLS certificate not trusted — x509: certificate signed by unknown authority";
    (state as unknown as Record<string, unknown>).url = "https://192.168.0.99:3000/";
    (state as unknown as Record<string, unknown>).mode = "local";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    expect(container.querySelector('[data-testid="tls-bypass-banner"]')).toBeTruthy();
    expect(container.textContent).toContain("Certificate isn’t trusted");
    expect(container.textContent).toContain("192.168.0.99:3000");
  });

  it("does not show TLS banner for non-TLS errors", async () => {
    state.error = "connection refused";
    (state as unknown as Record<string, unknown>).mode = "local";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    expect(container.querySelector('[data-testid="tls-bypass-banner"]')).toBeNull();
  });

  it("never shows the TLS bypass banner for a public host in chrome mode", async () => {
    state.error = "TLS certificate not trusted — net::ERR_CERT_AUTHORITY_INVALID";
    (state as unknown as Record<string, unknown>).mode = "chrome";
    (state as unknown as Record<string, unknown>).url = "https://example.com/";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    expect(container.querySelector('[data-testid="tls-bypass-banner"]')).toBeNull();
  });

  it("offers the bypass for a private host in chrome mode and reloads through the socket", async () => {
    state.error = "TLS certificate not trusted — net::ERR_CERT_AUTHORITY_INVALID";
    (state as unknown as Record<string, unknown>).mode = "chrome";
    (state as unknown as Record<string, unknown>).url = "https://192.168.0.99:3000/page";
    const cdpSends: unknown[] = [];
    const onSend = (e: Event) => cdpSends.push((e as CustomEvent).detail);
    window.addEventListener("cdp:send", onSend);
    try {
      const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
      // Let the initial (errored) load settle first: a load that starts while
      // the bypass POST is in flight trips handleBypass's stale-load guard.
      await waitFor(() => expect(container.querySelector("iframe")?.getAttribute("src")).toContain("__grant="));
      const btn = container.querySelector('[data-testid="tls-bypass-banner"] button') as HTMLButtonElement;
      expect(btn).toBeTruthy();
      const { fireEvent } = await import("@testing-library/react");
      await act(async () => {
        fireEvent.click(btn);
      });
      await waitFor(() => expect(mockBypass).toHaveBeenCalledWith("tab:abc", "192.168.0.99:3000", ""));
      await waitFor(() => expect(cdpSends).toEqual([{ t: "reload", stateKey: "tab:abc" }]));
      expect(actions.navigate).toHaveBeenCalledWith("tab:abc", "https://192.168.0.99:3000/page");
    } finally {
      window.removeEventListener("cdp:send", onSend);
    }
  });

  it("shows TLS banner for IPv6 loopback with TLS error", async () => {
    state.error = "TLS certificate not trusted — x509: unknown authority";
    (state as unknown as Record<string, unknown>).mode = "local";
    (state as unknown as Record<string, unknown>).url = "https://[::1]:8443/";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    expect(container.querySelector('[data-testid="tls-bypass-banner"]')).toBeTruthy();
    expect(container.textContent).toContain("[::1]:8443");
  });

  it("calls bypassBrowseTLS and reloads on Continue anyway", async () => {
    state.error = "TLS certificate not trusted — x509: unknown authority";
    (state as unknown as Record<string, unknown>).url = "https://192.168.0.99:3000/page";
    (state as unknown as Record<string, unknown>).mode = "local";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    const btn = container.querySelector('[data-testid="tls-bypass-banner"] button') as HTMLButtonElement;
    expect(btn).toBeTruthy();
    const { fireEvent } = await import("@testing-library/react");
    await act(async () => {
      fireEvent.click(btn);
    });
    await waitFor(() => expect(mockBypass).toHaveBeenCalledWith("tab:abc", "192.168.0.99:3000", ""));
  });

  it("routes console messages only from the browse origin", async () => {
    // Local mode registers the capture-script postMessage listener.
    (state as unknown as Record<string, unknown>).mode = "local";
    (state as unknown as Record<string, unknown>).url = "http://localhost:3000/";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    // The listener registers once browseBase resolves; the iframe src being
    // set is the same-effect-batch signal that the message listener is live.
    await waitFor(() => {
      const f = container.querySelector("iframe");
      expect(f?.getAttribute("src")).toContain("__grant=GRANT123");
    });
    const frame = container.querySelector("iframe")!.contentWindow;
    act(() => {
      window.dispatchEvent(new MessageEvent("message", {
        origin: "http://127.0.0.1:54321",
        source: frame,
        data: { type: "ocode:browse:console", stateKey: "tab:abc", level: "log", args: ["hi"], ts: 1 },
      }));
    });
    expect(actions.pushConsole).toHaveBeenCalled();
    act(() => {
      window.dispatchEvent(new MessageEvent("message", {
        origin: "https://evil.example",
        source: frame,
        data: { type: "ocode:browse:console", stateKey: "tab:abc", level: "log", args: ["x"], ts: 2 },
      }));
    });
    expect(actions.pushConsole).toHaveBeenCalledTimes(1); // foreign origin dropped
    act(() => {
      window.dispatchEvent(new MessageEvent("message", {
        origin: "http://127.0.0.1:54321",
        source: window,
        data: { type: "ocode:browse:console", stateKey: "tab:abc", level: "log", args: ["y"], ts: 3 },
      }));
    });
    expect(actions.pushConsole).toHaveBeenCalledTimes(1); // right origin, wrong window: dropped
  });

  it("shows the chrome-mode banner with an exit when userMode is chrome", async () => {
    (state as unknown as Record<string, unknown>).userMode = "chrome";
    const { getByTestId } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    await waitFor(() => expect(getByTestId("chrome-mode-banner")).toBeTruthy());
    fireEvent.click(getByTestId("chrome-mode-banner").querySelector("button")!);
    expect(actions.setUserMode).toHaveBeenCalledWith("tab:abc", null);
  });

  it("userMode chrome forces the chrome viewport even for a private-host URL", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    (state as unknown as Record<string, unknown>).userMode = "chrome";
    state.url = "https://localhost:3510/admin";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    await waitFor(() => expect(container.querySelector("[data-testid='chrome-viewport']")).toBeTruthy());
    expect(container.querySelector("iframe")).toBeNull();
  });

  it("returns to the local iframe (with src) after exiting Chrome mode, even after CDP nav events set mode=chrome", async () => {
    // local dev error → Open in Chrome → CDP target emits server nav events
    // with mode=chrome → user exits the override → the private-host rule must
    // flip the surface back to the local proxy AND the remounted iframe must
    // actually get a src (not mount blank).
    state.url = "https://localhost:3510/admin";
    (state as unknown as Record<string, unknown>).mode = "local";
    (state as unknown as Record<string, unknown>).userMode = null;
    const tree = () => <BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />;
    const { container, rerender } = render(tree());
    await waitFor(() => expect(container.querySelector("iframe")).toBeTruthy());

    // User opens Chrome mode → CDP canvas mounts, iframe unmounts.
    (state as unknown as Record<string, unknown>).userMode = "chrome";
    rerender(tree());
    await waitFor(() => expect(container.querySelector("[data-testid='chrome-viewport']")).toBeTruthy());
    expect(container.querySelector("iframe")).toBeNull();

    // The CDP target announces its navigation with mode=chrome (server event).
    (state as unknown as Record<string, unknown>).mode = "chrome";

    // User exits the override.
    (state as unknown as Record<string, unknown>).userMode = null;
    rerender(tree());
    await waitFor(() => expect(container.querySelector("iframe")).toBeTruthy());
    expect(container.querySelector("[data-testid='chrome-viewport']")).toBeNull();
    // The remounted iframe got its src re-issued (not blank).
    expect((container.querySelector("iframe") as HTMLIFrameElement).src).toContain("/b/tab:abc/");
  });

  it("shows the indeterminate loading bar + connecting overlay on first load", () => {
    state.loading = true;
    state.status = 0;
    (state as unknown as Record<string, unknown>).mode = "local";
    render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    expect(screen.getByTestId("browser-loading-bar")).toBeInTheDocument();
    expect(screen.getByTestId("browser-loading-overlay")).toBeInTheDocument();
    expect(screen.getByText(/Connecting to example\.com/i)).toBeInTheDocument();
  });

  it("chrome mode shows only the top bar on first load — ChromeViewport owns the centered spinner", () => {
    // Default mock mode is chrome; the panel must NOT stack its connecting
    // overlay on top of ChromeViewport's own first-frame spinner.
    state.loading = true;
    state.status = 0;
    render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    expect(screen.getByTestId("browser-loading-bar")).toBeInTheDocument();
    expect(screen.queryByTestId("browser-loading-overlay")).not.toBeInTheDocument();
  });

  it("keeps only the top bar after the first successful load", async () => {
    state.loading = false;
    state.status = 200;
    const { rerender } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    // First load completes → everLoaded flips; then a later navigation shows
    // the progress bar again but never the full-page overlay.
    await waitFor(() => expect(screen.queryByTestId("browser-loading-overlay")).not.toBeInTheDocument());
    (state as unknown as Record<string, unknown>).loading = true;
    (state as unknown as Record<string, unknown>).status = 0;
    rerender(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    expect(screen.getByTestId("browser-loading-bar")).toBeInTheDocument();
    expect(screen.queryByTestId("browser-loading-overlay")).not.toBeInTheDocument();
  });

  it("shows no loading indicators while idle", () => {
    render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    expect(screen.queryByTestId("browser-loading-bar")).not.toBeInTheDocument();
    expect(screen.queryByTestId("browser-loading-overlay")).not.toBeInTheDocument();
  });

  it("renders the normal panel in a remote session (browse origin is tunneled)", () => {
    mockIsRemoteSession.mockReturnValue(true);
    render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    expect(screen.queryByTestId("browser-full-remote-unavailable")).not.toBeInTheDocument();
    expect(screen.getByTestId("browser-full")).toBeInTheDocument();
  });

  it("an unknown project host loads nothing and reports it, never falling back to local", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    (state as unknown as Record<string, unknown>).url = "http://localhost:3000/";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host={null} />);
    await waitFor(() => expect(actions.setError).toHaveBeenCalledWith("tab:abc", expect.stringContaining("host is unknown")));
    expect(mockGetBrowseBase).not.toHaveBeenCalled();
    expect(container.querySelector("iframe")?.getAttribute("src")).toBeFalsy();
  });

  it("a failed origin read becomes the tab's visible error", async () => {
    mockGetBrowseBase.mockImplementation(async () => { throw new Error("browse config: 502"); });
    render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="user@box" />);
    await waitFor(() => expect(actions.setError).toHaveBeenCalledWith("tab:abc", "Browse origin unavailable: browse config: 502"));
  });

  it("a slow origin read for a previous host never installs that host's origin", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    (state as unknown as Record<string, unknown>).url = "http://localhost:3000/";
    let resolveLocal: ((base: string) => void) | null = null;
    mockGetBrowseBase.mockImplementation((host: string) => {
      if (host === "") return new Promise<string>((resolve) => { resolveLocal = resolve; });
      return Promise.resolve("http://localhost:40263");
    });
    const view = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    view.rerender(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="user@box" />);
    const iframe = await waitFor(() => view.container.querySelector("iframe")!);
    await waitFor(() => expect(iframe.getAttribute("src")).toContain("http://localhost:40263/b/"));
    await act(async () => { resolveLocal!("http://127.0.0.1:1"); });
    expect(iframe.getAttribute("src")).toContain("http://localhost:40263/b/");
    expect(iframe.getAttribute("src")).not.toContain("127.0.0.1:1");
  });

  it("loads a local tab through its root-mounted origin, with the grant in that URL", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    (state as unknown as Record<string, unknown>).url = "http://localhost:3000/app/dashboard";
    mockOpenTabOrigin.mockImplementation(async () => ({
      kind: "tab",
      origin: "http://localhost:41001",
      url: "http://localhost:41001/app/dashboard?__grant=TABGRANT",
    }));
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />);
    const iframe = await waitFor(() => container.querySelector("iframe")!);
    await waitFor(() => expect(iframe.getAttribute("src")).toBe("http://localhost:41001/app/dashboard?__grant=TABGRANT"));
    expect(mockOpenTabOrigin).toHaveBeenCalledWith("tab:abc", "http://localhost:3000/app/dashboard", "");
    expect(mockMintGrant).not.toHaveBeenCalled();
    // The tab's own origin gates messages; the prefix base no longer does.
    act(() => {
      window.dispatchEvent(new MessageEvent("message", {
        origin: "http://127.0.0.1:54321",
        source: iframe.contentWindow,
        data: { type: "ocode:browse:console", stateKey: "tab:abc", level: "log", args: ["old"], ts: 1 },
      }));
    });
    expect(actions.pushConsole).not.toHaveBeenCalled();
    act(() => {
      window.dispatchEvent(new MessageEvent("message", {
        origin: "http://localhost:41001",
        source: iframe.contentWindow,
        data: { type: "ocode:browse:console", stateKey: "tab:abc", level: "log", args: ["tab"], ts: 2 },
      }));
    });
    expect(actions.pushConsole).toHaveBeenCalledTimes(1);
  });

  it("surfaces a tab-origin failure as the tab's error and never loads prefix mode", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    mockOpenTabOrigin.mockImplementation(async () => { throw new Error("tab origin: host is not a saved project"); });
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="user@box" />);
    await waitFor(() => expect(actions.setError).toHaveBeenCalledWith("tab:abc", "tab origin: host is not a saved project"));
    expect(mockMintGrant).not.toHaveBeenCalled();
    expect(container.querySelector("iframe")?.getAttribute("src")).toBeFalsy();
  });

  it("retries a connecting remote host after Retry-After, then loads the tab", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    mockOpenTabOrigin
      .mockImplementationOnce(async () => ({ kind: "connecting", retryAfterMs: 5 }))
      .mockImplementationOnce(async () => ({ kind: "tab", origin: "http://localhost:41002", url: "http://localhost:41002/?__grant=G2" }));
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="user@box" />);
    const iframe = await waitFor(() => container.querySelector("iframe")!);
    await waitFor(() => expect(iframe.getAttribute("src")).toBe("http://localhost:41002/?__grant=G2"));
    expect(mockOpenTabOrigin).toHaveBeenCalledTimes(2);
    expect(actions.setError).not.toHaveBeenCalled();
  });

  it("re-reads a remote host's origin for every load", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    (state as unknown as Record<string, unknown>).url = "http://localhost:3000/";
    mockGetBrowseBase.mockImplementation(async () => "http://localhost:40263");
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" host="user@box" />);
    const iframe = await waitFor(() => container.querySelector("iframe")!);
    await waitFor(() => expect(iframe.getAttribute("src")).toContain("__grant=GRANT123"));
    // One read on mount, one more inside the load itself.
    expect(mockGetBrowseBase.mock.calls.filter(([h]) => h === "user@box").length).toBeGreaterThanOrEqual(2);
  });

  it("retries a failed load when the effect runs again for the same index", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    state.url = "http://localhost:3000/a";
    mockOpenTabOrigin
      .mockImplementationOnce(async () => { throw new Error("tab origin down"); })
      .mockImplementationOnce(async () => ({ kind: "tab", origin: "http://localhost:41001", url: "http://localhost:41001/b?__grant=G" }));
    const tree = () => <BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />;
    const { rerender } = render(tree());
    await waitFor(() => expect(actions.setError).toHaveBeenCalledWith("tab:abc", "tab origin down"));
    // Same index and origin, new path: the effect runs again and must retry, not skip.
    state.url = "http://localhost:3000/b";
    rerender(tree());
    await waitFor(() => expect(mockOpenTabOrigin).toHaveBeenCalledTimes(2));
    expect(mockOpenTabOrigin).toHaveBeenLastCalledWith("tab:abc", "http://localhost:3000/b", "");
  });

  it("re-loads the page at the same index when the browse base changes", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    state.url = "http://localhost:3000/app";
    mockGetBrowseBase.mockImplementation(async (h: string) => (h === "user@box" ? "http://localhost:40263" : "http://127.0.0.1:54321"));
    mockOpenTabOrigin.mockImplementation(async () => ({ kind: "tab", origin: "http://localhost:41001", url: "http://localhost:41001/app?__grant=G" }));
    const tree = (host: string) => <BrowserPanel stateKey={"tab:abc" as any} mode="full" host={host} />;
    const { rerender, container } = render(tree(""));
    await waitFor(() => expect(container.querySelector("iframe")?.getAttribute("src")).toContain("__grant=G"));
    expect(mockOpenTabOrigin).toHaveBeenCalledTimes(1);
    // Same tab, same index, same page origin: only the host (and so the base) changed.
    rerender(tree("user@box"));
    await waitFor(() => expect(mockOpenTabOrigin).toHaveBeenCalledTimes(2));
    expect(mockOpenTabOrigin).toHaveBeenLastCalledWith("tab:abc", "http://localhost:3000/app", "user@box");
  });

  it("a page nav hint is applied and the iframe it came from is not reloaded", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    state.url = "http://localhost:3000/app";
    mockOpenTabOrigin.mockImplementation(async () => ({ kind: "tab", origin: "http://localhost:41001", url: "http://localhost:41001/app?__grant=G" }));
    actions.syncPageURL.mockReturnValueOnce({ url: "http://localhost:3000/app/next", idx: 1 });
    const tree = () => <BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />;
    const { container, rerender } = render(tree());
    await waitFor(() => expect(container.querySelector("iframe")?.getAttribute("src")).toContain("__grant=G"));
    const iframe = container.querySelector("iframe")!;
    const post = (data: Record<string, unknown>) => act(() => {
      window.dispatchEvent(new MessageEvent("message", { origin: "http://localhost:41001", source: iframe.contentWindow, data: { stateKey: "tab:abc", ...data } }));
    });
    post({ type: "ocode:browse:nav", url: "http://localhost:3000/app/next", kind: "push", ts: 1 });
    expect(actions.syncPageURL).toHaveBeenCalledWith("tab:abc", "http://localhost:3000/app/next", "push");
    // A popstate hint reaches the store as a traverse; an unknown kind is a replace.
    post({ type: "ocode:browse:nav", url: "http://localhost:3000/app", kind: "traverse", ts: 2 });
    expect(actions.syncPageURL).toHaveBeenLastCalledWith("tab:abc", "http://localhost:3000/app", "traverse");
    post({ type: "ocode:browse:nav", url: "http://localhost:3000/app", kind: "bogus", ts: 3 });
    expect(actions.syncPageURL).toHaveBeenLastCalledWith("tab:abc", "http://localhost:3000/app", "replace");
    // The store now reflects the page's own URL at the new index: no reload.
    state.url = "http://localhost:3000/app/next";
    state.history = ["http://localhost:3000/app", "http://localhost:3000/app/next"];
    state.historyIndex = 1;
    rerender(tree());
    expect(mockOpenTabOrigin).toHaveBeenCalledTimes(1);
  });

  it("a nav hint that lands while a load awaits its grant supersedes that load", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    state.url = "http://localhost:3000/a";
    mockOpenTabOrigin.mockImplementationOnce(async () => ({ kind: "tab", origin: "http://localhost:41001", url: "http://localhost:41001/a?__grant=GA" }));
    // The load for /b stays pending until the test releases it.
    let releaseB: (tab: unknown) => void = () => {};
    mockOpenTabOrigin.mockImplementationOnce(() => new Promise((resolve) => { releaseB = resolve; }));
    actions.syncPageURL.mockReturnValueOnce({ url: "http://localhost:3000/c", idx: 2 });
    const tree = () => <BrowserPanel stateKey={"tab:abc" as any} mode="full" host="" />;
    const { container, rerender } = render(tree());
    await waitFor(() => expect(container.querySelector("iframe")?.getAttribute("src")).toContain("__grant=GA"));
    const iframe = container.querySelector("iframe")!;
    // The user navigates to /b. The iframe still shows /a.
    state.url = "http://localhost:3000/b";
    state.history = ["http://localhost:3000/a", "http://localhost:3000/b"];
    state.historyIndex = 1;
    rerender(tree());
    await waitFor(() => expect(mockOpenTabOrigin).toHaveBeenCalledTimes(2));
    // The page the iframe still shows reports a same-origin pushState to /c.
    act(() => {
      window.dispatchEvent(new MessageEvent("message", { origin: "http://localhost:41001", source: iframe.contentWindow, data: { stateKey: "tab:abc", type: "ocode:browse:nav", url: "http://localhost:3000/c", kind: "push", ts: 1 } }));
    });
    expect(actions.syncPageURL).toHaveBeenCalledWith("tab:abc", "http://localhost:3000/c", "push");
    // The store now holds /c at index 2, and the record matches it: no reissue.
    state.url = "http://localhost:3000/c";
    state.history = ["http://localhost:3000/a", "http://localhost:3000/b", "http://localhost:3000/c"];
    state.historyIndex = 2;
    rerender(tree());
    expect(mockOpenTabOrigin).toHaveBeenCalledTimes(2);
    // The superseded /b load now resolves. It must not assign its grant to the iframe.
    await act(async () => {
      releaseB({ kind: "tab", origin: "http://localhost:41001", url: "http://localhost:41001/b?__grant=GB" });
    });
    expect(iframe.getAttribute("src")).toContain("__grant=GA");
    expect(iframe.getAttribute("src")).not.toContain("GB");
  });
});
