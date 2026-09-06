import { act, render, screen } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const mockIsRemoteSession = vi.hoisted(() => vi.fn(() => false));
vi.mock("../../api/client", () => ({
  authToken: () => "test-token",
  isRemoteSession: mockIsRemoteSession,
}));

import ShareDialog from "./ShareDialog";

function openShareDialog() {
  act(() => {
    window.dispatchEvent(new CustomEvent("ocode:share-desktop"));
  });
}

describe("ShareDialog", () => {
  beforeEach(() => {
    mockIsRemoteSession.mockReturnValue(false);
  });

  it("shows the desktop share link for a non-remote session", () => {
    render(<ShareDialog />);
    openShareDialog();
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toContain("token=test-token");
    expect(screen.queryByTestId("share-dialog-remote-unavailable")).toBeNull();
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
