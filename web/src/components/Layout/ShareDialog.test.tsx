import { act, render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const mockIsRemoteSession = vi.hoisted(() => vi.fn(() => false));
const mockAuthedFetch = vi.hoisted(() => vi.fn());
vi.mock("../../api/client", () => ({
  authToken: () => "test-token",
  isRemoteSession: mockIsRemoteSession,
  authedFetch: mockAuthedFetch,
  apiPath: (p: string) => p,
}));

import ShareDialog from "./ShareDialog";

function openShareDialog() {
  act(() => {
    window.dispatchEvent(new CustomEvent("ocode:share-desktop"));
  });
}

function mockShareResponses(opts: { tailscale?: string; lan?: string } = {}) {
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
    return { ok: false, json: async () => null };
  });
}

describe("ShareDialog", () => {
  beforeEach(() => {
    mockIsRemoteSession.mockReturnValue(false);
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
});
