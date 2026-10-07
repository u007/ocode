import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const mockIsRemoteSession = vi.hoisted(() => vi.fn(() => false));
const mockIsDesktopShell = vi.hoisted(() => vi.fn(() => false));
const mockAuthedFetch = vi.hoisted(() => vi.fn());
const mockApi = vi.hoisted(() => ({
  getAutoShareConfig: vi.fn(),
  startShare: vi.fn(),
  stopShare: vi.fn(),
}));
vi.mock("../../api/client", () => ({
  authToken: () => "test-token",
  isRemoteSession: mockIsRemoteSession,
  authedFetch: mockAuthedFetch,
  apiPath: (p: string) => p,
  api: mockApi,
}));
vi.mock("../../lib/desktopShell", () => ({
  isDesktopShell: () => mockIsDesktopShell(),
}));

import ShareDialog from "./ShareDialog";

function openShareDialog() {
  act(() => {
    window.dispatchEvent(new CustomEvent("ocode:share-desktop"));
  });
}

interface ShareOpts {
  tailscale?: string; // ""/undefined => not sharing over tailscale
  running?: boolean; // default: !!tailscale
  available?: boolean; // default: running || !!tailscale
  kind?: "funnel" | "serve";
  lan?: string;
  // undefined/null = the route is absent (a plain `ocode serve`), which is
  // what makes the dialog fall back to the launch token.
  shareToken?: string | null;
  resetToken?: string;
  resetStatus?: number;
  autoShare?: boolean;
}

function statusFor(opts: ShareOpts) {
  const running = opts.running ?? !!opts.tailscale;
  const available = opts.available ?? (running || !!opts.tailscale);
  return {
    running,
    available,
    kind: running ? (opts.kind ?? "serve") : undefined,
    url: running ? opts.tailscale : undefined,
    hint: "",
  };
}

function mockShareResponses(opts: ShareOpts = {}) {
  const status = statusFor(opts);
  mockApi.getAutoShareConfig.mockResolvedValue({ enabled: opts.autoShare ?? false, ...status });
  mockApi.startShare.mockResolvedValue({
    ...status,
    running: true,
    available: true,
    kind: opts.kind ?? "serve",
    url: opts.tailscale ?? "https://host.tailnet.ts.net/desktop",
  });
  mockApi.stopShare.mockResolvedValue({ running: false, available: true, hint: "" });

  mockAuthedFetch.mockImplementation(async (path: string) => {
    if (path === "/api/network-ip") {
      return { ok: true, json: async () => ({ ip: opts.lan ?? "" }) };
    }
    if (path === "/api/desktop/share-token") {
      if (opts.shareToken == null) {
        return { ok: false, status: 404, json: async () => null };
      }
      return { ok: true, status: 200, json: async () => ({ token: opts.shareToken }) };
    }
    if (path === "/api/desktop/share-token/reset") {
      const code = opts.resetStatus ?? 200;
      return {
        ok: code < 400,
        status: code,
        json: async () => (code < 400 ? { token: opts.resetToken ?? "rotated-token" } : null),
      };
    }
    return { ok: false, status: 404, json: async () => null };
  });
}

describe("ShareDialog", () => {
  beforeEach(() => {
    mockIsRemoteSession.mockReturnValue(false);
    mockIsDesktopShell.mockReturnValue(false);
    mockAuthedFetch.mockReset();
    mockApi.getAutoShareConfig.mockReset();
    mockApi.startShare.mockReset();
    mockApi.stopShare.mockReset();
    // Default: already sharing over tailscale (serve) with a LAN fallback.
    mockShareResponses({ tailscale: "https://host.tailnet.ts.net/desktop", lan: "192.168.1.5" });
  });

  it("shows the live share link when already sharing", async () => {
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.queryByTestId("share-dialog-loading")).toBeNull());
    expect(screen.getByTestId("share-dialog-status-text")).toHaveTextContent("Sharing");
    expect((screen.getAllByRole("textbox")[0] as HTMLInputElement).value).toContain("token=test-token");
    expect(screen.queryByTestId("share-dialog-remote-unavailable")).toBeNull();
  });

  it("never starts an exposure merely by opening the dialog", async () => {
    mockShareResponses({ running: false, available: true, lan: "192.168.1.5" });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-not-sharing")).toBeInTheDocument());
    expect(mockApi.startShare).not.toHaveBeenCalled();
    expect(mockApi.getAutoShareConfig).toHaveBeenCalled();
  });

  it("starts sharing from the Start button and reveals the URL", async () => {
    mockShareResponses({ running: false, available: true, tailscale: "https://host.tailnet.ts.net/desktop" });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-start")).toBeInTheDocument());

    fireEvent.click(screen.getByTestId("share-dialog-start"));

    await waitFor(() => expect(screen.getByTestId("share-dialog-status-text")).toHaveTextContent("Sharing"));
    expect(mockApi.startShare).toHaveBeenCalledTimes(1);
    expect((screen.getAllByRole("textbox")[0] as HTMLInputElement).value).toContain(
      "https://host.tailnet.ts.net/desktop",
    );
  });

  it("stops sharing only after an explicit confirmation", async () => {
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-stop")).toBeInTheDocument());

    // First click only arms the confirmation; nothing is stopped yet.
    fireEvent.click(screen.getByTestId("share-dialog-stop"));
    expect(screen.getByTestId("share-dialog-stop-confirm")).toBeInTheDocument();
    expect(mockApi.stopShare).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("share-dialog-stop-confirm"));

    await waitFor(() => expect(screen.getByTestId("share-dialog-status-text")).toHaveTextContent("Not sharing"));
    expect(mockApi.stopShare).toHaveBeenCalledTimes(1);
  });

  it("warns that a funnel share is public, and labels a serve share tailnet-only", async () => {
    mockShareResponses({ tailscale: "https://host.tailnet.ts.net/desktop", kind: "funnel" });
    const first = render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-public-warning")).toBeInTheDocument());
    first.unmount();

    mockShareResponses({ tailscale: "https://host.tailnet.ts.net/desktop", kind: "serve" });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-tailscale")).toBeInTheDocument());
    expect(screen.getByTestId("share-dialog-kind")).toHaveTextContent("Tailnet only");
  });

  it("shows the unavailable state with a LAN fallback when tailscale is not available", async () => {
    mockShareResponses({ tailscale: "", running: false, available: false, lan: "192.168.1.5" });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-unavailable")).toBeInTheDocument());
    const lan = screen.getByTestId("share-dialog-lan-url") as HTMLInputElement;
    expect(lan.value).toContain("192.168.1.5");
    expect(lan.value).not.toContain("localhost");
    // Start must be disabled: there is nothing to start.
    expect(screen.getByTestId("share-dialog-start")).toBeDisabled();
  });

  it("warns that an enabled auto-share will restart after a manual stop", async () => {
    mockShareResponses({ running: false, available: true, autoShare: true });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-auto-restart")).toBeInTheDocument());
  });

  it("shows an explanatory message instead of a token-bearing link in a remote session", () => {
    mockIsRemoteSession.mockReturnValue(true);
    render(<ShareDialog />);
    openShareDialog();
    expect(screen.getByTestId("share-dialog-remote-unavailable")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText(/isn't available for a remote session/i)).toBeInTheDocument();
  });

  it("does not copy the token-bearing URL to the clipboard via the desktop-copy shortcut in a remote session", async () => {
    mockIsRemoteSession.mockReturnValue(true);
    const writeText = vi.fn(async () => {});
    Object.assign(navigator, { clipboard: { writeText } });
    render(<ShareDialog />);
    await act(async () => {
      window.dispatchEvent(new CustomEvent("ocode:copy-desktop-url"));
    });
    expect(writeText).not.toHaveBeenCalled();
    expect(mockApi.startShare).not.toHaveBeenCalled();
  });

  it("starts the share before copying when the Copy Desktop URL menu item fires while stopped", async () => {
    mockShareResponses({ running: false, available: true, tailscale: "https://host.tailnet.ts.net/desktop" });
    const writeText = vi.fn(async () => {});
    Object.assign(navigator, { clipboard: { writeText } });
    render(<ShareDialog />);
    await act(async () => {
      window.dispatchEvent(new CustomEvent("ocode:copy-desktop-url"));
    });
    await waitFor(() => expect(mockApi.startShare).toHaveBeenCalled());
    expect(writeText).toHaveBeenCalledWith(
      expect.stringContaining("https://host.tailnet.ts.net/desktop"),
    );
  });

  it("copies the URL via the execCommand fallback from inside the modal focus scope", async () => {
    // The dialog is a Radix modal, so a scratch textarea appended to
    // document.body sits OUTSIDE the FocusScope: Radix bounces focus back into
    // the dialog, the scratch field is never the active selection, and
    // document.execCommand("copy") reports true while copying nothing. That
    // made the button show "Copied" with an empty clipboard.
    //
    // Emulate WKWebView: execCommand always returns true, but the copied value
    // only exists when the active element really is the scratch textarea.
    const writeText = vi.fn(async () => {
      throw new Error("clipboard denied");
    });
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });

    const copied: string[] = [];
    const execCommand = vi.fn((command: string) => {
      if (command === "copy") {
        const active = document.activeElement as HTMLElement | null;
        if (active && active.tagName === "TEXTAREA") {
          copied.push((active as HTMLTextAreaElement).value);
        }
      }
      return true;
    });
    (document as unknown as { execCommand: unknown }).execCommand = execCommand;

    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-copy")).toBeInTheDocument());

    fireEvent.click(screen.getByTestId("share-dialog-copy"));

    await waitFor(() => expect(execCommand).toHaveBeenCalled());
    expect(copied).toEqual(["https://host.tailnet.ts.net/desktop/?token=test-token"]);
    // A false "Copied" must not be reported when nothing was written.
    expect(screen.queryByTestId("share-dialog-copy-failed")).toBeNull();
  });

  // ---- durable share token (desktop shell) ----

  it("uses the durable share token in place of the launch token in the desktop shell", async () => {
    mockIsDesktopShell.mockReturnValue(true);
    mockShareResponses({ tailscale: "https://host.tailnet.ts.net/desktop", lan: "192.168.1.5", shareToken: "durable-token" });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-copy")).toBeInTheDocument());
    const value = (screen.getAllByRole("textbox")[0] as HTMLInputElement).value;
    // The launch token dies with the process, so handing it out would make
    // every shared link expire on the next restart.
    expect(value).toContain("token=durable-token");
    expect(value).not.toContain("test-token");
    expect(screen.getByTestId("share-dialog-persistent-note")).toBeInTheDocument();
  });

  it("falls back to the launch token and offers no reset when the desktop has no durable token", async () => {
    mockIsDesktopShell.mockReturnValue(true);
    mockShareResponses({ tailscale: "https://host.tailnet.ts.net/desktop", lan: "192.168.1.5", shareToken: null });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-copy")).toBeInTheDocument());
    expect((screen.getAllByRole("textbox")[0] as HTMLInputElement).value).toContain("token=test-token");
    expect(screen.queryByTestId("share-dialog-reset")).toBeNull();
  });

  it("offers no reset outside the desktop shell", async () => {
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-status-text")).toBeInTheDocument());
    expect(screen.queryByTestId("share-dialog-reset")).toBeNull();
    expect(screen.queryByTestId("share-dialog-persistent-note")).toBeNull();
  });

  it("resets the share token and rewrites the displayed URL", async () => {
    mockIsDesktopShell.mockReturnValue(true);
    mockShareResponses({ tailscale: "https://host.tailnet.ts.net/desktop", lan: "192.168.1.5", shareToken: "old-token", resetToken: "rotated-token" });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-reset-start")).toBeInTheDocument());
    expect((screen.getAllByRole("textbox")[0] as HTMLInputElement).value).toContain("token=old-token");

    fireEvent.click(screen.getByTestId("share-dialog-reset-start"));
    fireEvent.click(screen.getByTestId("share-dialog-reset-confirm-yes"));

    await waitFor(() =>
      expect((screen.getAllByRole("textbox")[0] as HTMLInputElement).value).toContain("token=rotated-token"),
    );
    expect(screen.queryByTestId("share-dialog-reset-confirm")).toBeNull();
    expect(screen.queryByTestId("share-dialog-reset-error")).toBeNull();
  });

  it("cancelling the confirmation leaves the token alone", async () => {
    mockIsDesktopShell.mockReturnValue(true);
    mockShareResponses({ tailscale: "https://host.tailnet.ts.net/desktop", lan: "192.168.1.5", shareToken: "old-token", resetToken: "rotated-token" });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-reset-start")).toBeInTheDocument());

    fireEvent.click(screen.getByTestId("share-dialog-reset-start"));
    fireEvent.click(screen.getByTestId("share-dialog-reset-cancel"));

    await waitFor(() => expect(screen.getByTestId("share-dialog-reset-start")).toBeInTheDocument());
    expect(mockAuthedFetch).not.toHaveBeenCalledWith(
      "/api/desktop/share-token/reset",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("keeps the previous link and explains the failure when the reset does not land", async () => {
    mockIsDesktopShell.mockReturnValue(true);
    mockShareResponses({ tailscale: "https://host.tailnet.ts.net/desktop", lan: "192.168.1.5", shareToken: "old-token", resetStatus: 500 });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-reset-start")).toBeInTheDocument());

    fireEvent.click(screen.getByTestId("share-dialog-reset-start"));
    fireEvent.click(screen.getByTestId("share-dialog-reset-confirm-yes"));

    await waitFor(() => expect(screen.getByTestId("share-dialog-reset-error")).toBeInTheDocument());
    // The old token is still the live one, so the dialog must not advertise a
    // URL it knows is revoked.
    expect((screen.getAllByRole("textbox")[0] as HTMLInputElement).value).toContain("token=old-token");
  });

  it("arms the confirmation from the Share menu event without revoking anything", async () => {
    mockIsDesktopShell.mockReturnValue(true);
    mockShareResponses({ tailscale: "https://host.tailnet.ts.net/desktop", lan: "192.168.1.5", shareToken: "old-token" });
    render(<ShareDialog />);
    await act(async () => {
      window.dispatchEvent(new CustomEvent("ocode:reset-share-token"));
    });
    await waitFor(() => expect(screen.getByTestId("share-dialog-reset-confirm")).toBeInTheDocument());
    expect(mockAuthedFetch).not.toHaveBeenCalledWith(
      "/api/desktop/share-token/reset",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("keeps refusing to share in a remote session even with a durable token", async () => {
    mockIsDesktopShell.mockReturnValue(true);
    mockIsRemoteSession.mockReturnValue(true);
    mockShareResponses({ tailscale: "https://host.tailnet.ts.net/desktop", lan: "192.168.1.5", shareToken: "durable-token" });
    render(<ShareDialog />);
    openShareDialog();
    expect(screen.getByTestId("share-dialog-remote-unavailable")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.queryByTestId("share-dialog-reset")).toBeNull();
  });
});
