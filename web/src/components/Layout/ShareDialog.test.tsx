import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const mockIsRemoteSession = vi.hoisted(() => vi.fn(() => false));
const mockIsDesktopShell = vi.hoisted(() => vi.fn(() => false));
const mockAuthedFetch = vi.hoisted(() => vi.fn());
vi.mock("../../api/client", () => ({
  authToken: () => "test-token",
  isRemoteSession: mockIsRemoteSession,
  authedFetch: mockAuthedFetch,
  apiPath: (p: string) => p,
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

function mockShareResponses(
  opts: {
    tailscale?: string;
    lan?: string;
    // undefined/null = the route is absent (a plain `ocode serve`), which is
    // what makes the dialog fall back to the launch token.
    shareToken?: string | null;
    resetToken?: string;
    resetStatus?: number;
  } = {},
) {
  mockAuthedFetch.mockImplementation(async (path: string) => {
    if (path === "/api/tailscale-url") {
      return {
        ok: true,
        json: async () => ({ url: opts.tailscale ?? "", available: !!opts.tailscale, hint: "" }),
      };
    }
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
      const status = opts.resetStatus ?? 200;
      return {
        ok: status < 400,
        status,
        json: async () => (status < 400 ? { token: opts.resetToken ?? "rotated-token" } : null),
      };
    }
    return { ok: false, status: 404, json: async () => null };
  });
}

describe("ShareDialog", () => {
  beforeEach(() => {
    mockIsRemoteSession.mockReturnValue(false);
    mockIsDesktopShell.mockReturnValue(false);
    // Clear call history between tests: the "never POST a reset" assertions
    // below would otherwise see the POST from an earlier test, which really
    // does reset the token.
    mockAuthedFetch.mockReset();
    mockShareResponses({ lan: "192.168.1.5" });
  });

  it("shows the desktop share link for a non-remote session", async () => {
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.queryByTestId("share-dialog-loading")).toBeNull());
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toContain("token=test-token");
    expect(screen.queryByTestId("share-dialog-remote-unavailable")).toBeNull();
  });

  it("prefers the tailscale URL when available", async () => {
    mockShareResponses({ tailscale: "https://host.tailnet.ts.net/desktop", lan: "192.168.1.5" });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-tailscale")).toBeInTheDocument());
    const boxes = screen.getAllByRole("textbox") as HTMLInputElement[];
    expect(boxes[0].value).toContain(
      "https://host.tailnet.ts.net/desktop",
    );
    expect((screen.getByTestId("share-dialog-lan-url") as HTMLInputElement).value).toContain("192.168.1.5");
  });

  it("falls back to the LAN URL and never localhost when tailscale is unavailable", async () => {
    mockShareResponses({ lan: "192.168.1.5" });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-lan")).toBeInTheDocument());
    const value = (screen.getByRole("textbox") as HTMLInputElement).value;
    expect(value).toContain("192.168.1.5");
    expect(value).not.toContain("localhost");
  });

  it("shows an unavailable state instead of a localhost URL when nothing resolves", async () => {
    mockShareResponses({});
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-unavailable")).toBeInTheDocument());
    expect(screen.queryByRole("textbox")).toBeNull();
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

    mockShareResponses({ tailscale: "https://host.tailnet.ts.net/desktop" });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() =>
      expect(screen.getByTestId("share-dialog-copy")).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByTestId("share-dialog-copy"));

    await waitFor(() => expect(execCommand).toHaveBeenCalled());
    expect(copied).toEqual([
      "https://host.tailnet.ts.net/desktop/?token=test-token",
    ]);
    // A false "Copied" must not be reported when nothing was written.
    expect(screen.queryByTestId("share-dialog-copy-failed")).toBeNull();
  });

  // ---- durable share token (desktop shell) ----

  it("uses the durable share token in place of the launch token in the desktop shell", async () => {
    mockIsDesktopShell.mockReturnValue(true);
    mockShareResponses({ lan: "192.168.1.5", shareToken: "durable-token" });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-copy")).toBeInTheDocument());
    const value = (screen.getByRole("textbox") as HTMLInputElement).value;
    // The launch token dies with the process, so handing it out would make
    // every shared link expire on the next restart.
    expect(value).toContain("token=durable-token");
    expect(value).not.toContain("test-token");
    expect(screen.getByTestId("share-dialog-persistent-note")).toBeInTheDocument();
  });

  it("falls back to the launch token and offers no reset when the desktop has no durable token", async () => {
    mockIsDesktopShell.mockReturnValue(true);
    mockShareResponses({ lan: "192.168.1.5", shareToken: null });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-copy")).toBeInTheDocument());
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toContain("token=test-token");
    expect(screen.queryByTestId("share-dialog-reset")).toBeNull();
  });

  it("offers no reset outside the desktop shell", async () => {
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-lan")).toBeInTheDocument());
    expect(screen.queryByTestId("share-dialog-reset")).toBeNull();
    expect(screen.queryByTestId("share-dialog-persistent-note")).toBeNull();
  });

  it("resets the share token and rewrites the displayed URL", async () => {
    mockIsDesktopShell.mockReturnValue(true);
    mockShareResponses({ lan: "192.168.1.5", shareToken: "old-token", resetToken: "rotated-token" });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-reset-start")).toBeInTheDocument());
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toContain("token=old-token");

    fireEvent.click(screen.getByTestId("share-dialog-reset-start"));
    fireEvent.click(screen.getByTestId("share-dialog-reset-confirm-yes"));

    await waitFor(() =>
      expect((screen.getByRole("textbox") as HTMLInputElement).value).toContain("token=rotated-token"),
    );
    expect(screen.queryByTestId("share-dialog-reset-confirm")).toBeNull();
    expect(screen.queryByTestId("share-dialog-reset-error")).toBeNull();
  });

  it("cancelling the confirmation leaves the token alone", async () => {
    mockIsDesktopShell.mockReturnValue(true);
    mockShareResponses({ lan: "192.168.1.5", shareToken: "old-token", resetToken: "rotated-token" });
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
    mockShareResponses({ lan: "192.168.1.5", shareToken: "old-token", resetStatus: 500 });
    render(<ShareDialog />);
    openShareDialog();
    await waitFor(() => expect(screen.getByTestId("share-dialog-reset-start")).toBeInTheDocument());

    fireEvent.click(screen.getByTestId("share-dialog-reset-start"));
    fireEvent.click(screen.getByTestId("share-dialog-reset-confirm-yes"));

    await waitFor(() => expect(screen.getByTestId("share-dialog-reset-error")).toBeInTheDocument());
    // The old token is still the live one, so the dialog must not advertise a
    // URL it knows is revoked.
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toContain("token=old-token");
  });

  it("arms the confirmation from the Share menu event without revoking anything", async () => {
    mockIsDesktopShell.mockReturnValue(true);
    mockShareResponses({ lan: "192.168.1.5", shareToken: "old-token" });
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
    mockShareResponses({ lan: "192.168.1.5", shareToken: "durable-token" });
    render(<ShareDialog />);
    openShareDialog();
    expect(screen.getByTestId("share-dialog-remote-unavailable")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.queryByTestId("share-dialog-reset")).toBeNull();
  });
});
