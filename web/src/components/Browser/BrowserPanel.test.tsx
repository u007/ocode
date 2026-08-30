import { render, screen, waitFor, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const state = {
  url: "https://example.com/",
  history: ["https://example.com/"],
  historyIndex: 0,
  panelOpen: true,
  collapsed: false,
  status: 200,
  mode: "proxied" as const,
  error: "",
  consoleEvents: [],
  networkEvents: [],
};
const actions = {
  navigate: vi.fn(), back: vi.fn(), forward: vi.fn(),
  pushConsole: vi.fn(), pushNetwork: vi.fn(),
  clearConsole: vi.fn(), clearNetwork: vi.fn(),
  setCollapsed: vi.fn(), close: vi.fn(),
};
vi.mock("../../lib/browserStore", () => ({
  useBrowserStore: (key: string) => ({ ...state }),
  useBrowserActions: () => actions,
}));
vi.mock("../../api/client", () => ({
  getBrowseBase: vi.fn(async () => "http://127.0.0.1:54321"),
  mintBrowseGrant: vi.fn(async () => "GRANT123"),
  // Reconciled to Part 08's real signature: (base, grant, stateKey, url).
  browseSrc: (base: string, grant: string | null, key: string, _url: string) =>
    `${base}/b/${key}/https/example.com/${grant ? "?__grant=" + grant : ""}`,
}));

import { BrowserPanel } from "./BrowserPanel";

describe("BrowserPanel", () => {
  beforeEach(() => vi.clearAllMocks());

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
    state.collapsed = false;
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
