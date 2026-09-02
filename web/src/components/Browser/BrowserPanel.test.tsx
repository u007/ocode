import { render, waitFor, act, fireEvent, screen } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const mockBypass = vi.hoisted(() => vi.fn(async () => {}));

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
};
const actions = {
  navigate: vi.fn(), back: vi.fn(), forward: vi.fn(),
  pushConsole: vi.fn(), pushNetwork: vi.fn(),
  clearConsole: vi.fn(), clearNetwork: vi.fn(),
  setCollapsed: vi.fn(), close: vi.fn(),
  setError: vi.fn(), setUserMode: vi.fn(),
};
vi.mock("../../lib/browserStore", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../lib/browserStore")>();
  return {
    ...actual,
    useBrowserStore: (_key: string) => ({ ...state }),
    useBrowserActions: () => actions,
  };
});
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

// Mock ChromeViewport to a stub so surface-selection is testable without the
// socket stack.
vi.mock("./ChromeViewport", () => ({
  ChromeViewport: (props: Record<string, unknown>) => (
    <div data-testid="chrome-viewport" data-url={String(props.url)} />
  ),
}));

describe("BrowserPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    state.error = "";
    state.url = "https://example.com/";
    state.status = 200;
    state.loading = false;
    (state as unknown as Record<string, unknown>).mode = "chrome";
    (state as unknown as Record<string, unknown>).userMode = null;
    (state as unknown as Record<string, unknown>).collapsed = false;
  });

  it("chrome mode mounts ChromeViewport and no iframe", async () => {
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    await waitFor(() => expect(container.querySelector("[data-testid='chrome-viewport']")).toBeTruthy());
    expect(container.querySelector("iframe")).toBeNull();
  });

  it("local mode mounts the iframe and no chrome viewport", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    await waitFor(() => expect(container.querySelector("iframe")).toBeTruthy());
    expect(container.querySelector("[data-testid='chrome-viewport']")).toBeNull();
  });

  it("mode null falls back on the url host: public → chrome, private → local", async () => {
    (state as unknown as Record<string, unknown>).mode = null;
    const pub = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    await waitFor(() => expect(pub.container.querySelector("[data-testid='chrome-viewport']")).toBeTruthy());
    pub.unmount();
    (state as unknown as Record<string, unknown>).url = "http://localhost:3000/";
    const priv = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    await waitFor(() => expect(priv.container.querySelector("iframe")).toBeTruthy());
    expect(priv.container.querySelector("[data-testid='chrome-viewport']")).toBeNull();
  });

  it("collapsed mounts neither surface", () => {
    (state as unknown as Record<string, unknown>).collapsed = true;
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    expect(container.querySelector("iframe")).toBeNull();
    expect(container.querySelector("[data-testid='chrome-viewport']")).toBeNull();
  });

  it("mounts the iframe with the grant only on first load", async () => {
    // Local mode: the iframe path (chrome mode mounts the viewport instead).
    (state as unknown as Record<string, unknown>).mode = "local";
    (state as unknown as Record<string, unknown>).url = "http://localhost:3000/";
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
    // Local mode registers the capture-script postMessage listener.
    (state as unknown as Record<string, unknown>).mode = "local";
    (state as unknown as Record<string, unknown>).url = "http://localhost:3000/";
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

  it("shows the dev-server banner when error carries the module-graph token, and offers Chrome mode", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    state.url = "https://localhost:3510/admin";
    state.error = "dev-server-module-graph: this dev server's module graph can't be served by the embedded proxy — open externally or switch to Chrome mode";
    const { container, getByTestId } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    await waitFor(() => expect(getByTestId("dev-server-banner")).toBeTruthy());
    expect(container.textContent).toContain("Dev server can't render in the embedded proxy");
    fireEvent.click(getByTestId("dev-server-banner").querySelector("button")!);
    expect(actions.setUserMode).toHaveBeenCalledWith("tab:abc", "chrome");
  });

  it("does not show the dev-server banner for unrelated errors", async () => {
    state.error = "TLS certificate not trusted — x509: self-signed";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    expect(container.querySelector("[data-testid='dev-server-banner']")).toBeNull();
  });

  it("shows the chrome-mode banner with an exit when userMode is chrome", async () => {
    (state as unknown as Record<string, unknown>).userMode = "chrome";
    const { getByTestId } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    await waitFor(() => expect(getByTestId("chrome-mode-banner")).toBeTruthy());
    fireEvent.click(getByTestId("chrome-mode-banner").querySelector("button")!);
    expect(actions.setUserMode).toHaveBeenCalledWith("tab:abc", null);
  });

  it("userMode chrome forces the chrome viewport even for a private-host URL", async () => {
    (state as unknown as Record<string, unknown>).mode = "local";
    (state as unknown as Record<string, unknown>).userMode = "chrome";
    state.url = "https://localhost:3510/admin";
    const { container } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
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
    const tree = () => <BrowserPanel stateKey={"tab:abc" as any} mode="full" />;
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
    render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    expect(screen.getByTestId("browser-loading-bar")).toBeInTheDocument();
    expect(screen.getByTestId("browser-loading-overlay")).toBeInTheDocument();
    expect(screen.getByText(/Connecting to example\.com/i)).toBeInTheDocument();
  });

  it("chrome mode shows only the top bar on first load — ChromeViewport owns the centered spinner", () => {
    // Default mock mode is chrome; the panel must NOT stack its connecting
    // overlay on top of ChromeViewport's own first-frame spinner.
    state.loading = true;
    state.status = 0;
    render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    expect(screen.getByTestId("browser-loading-bar")).toBeInTheDocument();
    expect(screen.queryByTestId("browser-loading-overlay")).not.toBeInTheDocument();
  });

  it("keeps only the top bar after the first successful load", async () => {
    state.loading = false;
    state.status = 200;
    const { rerender } = render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    // First load completes → everLoaded flips; then a later navigation shows
    // the progress bar again but never the full-page overlay.
    await waitFor(() => expect(screen.queryByTestId("browser-loading-overlay")).not.toBeInTheDocument());
    (state as unknown as Record<string, unknown>).loading = true;
    (state as unknown as Record<string, unknown>).status = 0;
    rerender(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    expect(screen.getByTestId("browser-loading-bar")).toBeInTheDocument();
    expect(screen.queryByTestId("browser-loading-overlay")).not.toBeInTheDocument();
  });

  it("shows no loading indicators while idle", () => {
    render(<BrowserPanel stateKey={"tab:abc" as any} mode="full" />);
    expect(screen.queryByTestId("browser-loading-bar")).not.toBeInTheDocument();
    expect(screen.queryByTestId("browser-loading-overlay")).not.toBeInTheDocument();
  });
});
