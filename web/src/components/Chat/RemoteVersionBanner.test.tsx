import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { RemoteHostStatusState } from "@/hooks/useRemoteHostStatus";
import { RemoteVersionBanner, __resetRemoteVersionBannerForTests } from "./RemoteVersionBanner";

const mockState = vi.fn();
vi.mock("@/hooks/useRemoteHostStatus", () => ({
  useRemoteHostStatus: (...a: unknown[]) => mockState(...a),
}));

const connected = (over: Partial<RemoteHostStatusState["status"]> = {}) => ({
  host: "dev@box",
  connected: true,
  version: "1.2.3",
  local_version: "2.0.0",
  outdated: false,
  pid: 1,
  ...over,
});

function state(over: Partial<RemoteHostStatusState> = {}): RemoteHostStatusState {
  return {
    status: connected({ outdated: true }),
    loading: false,
    error: null,
    busy: "idle",
    refresh: vi.fn(),
    connect: vi.fn(),
    restart: vi.fn(),
    ...over,
  };
}

describe("RemoteVersionBanner", () => {
  beforeEach(() => {
    __resetRemoteVersionBannerForTests();
    mockState.mockReset();
    mockState.mockReturnValue(state());
  });

  it("renders nothing without a host", () => {
    render(<RemoteVersionBanner />);
    expect(screen.queryByTestId("remote-version-banner")).toBeNull();
    expect(mockState).toHaveBeenCalledWith(undefined, false);
  });

  it("renders nothing when the host is not connected", () => {
    mockState.mockReturnValue(state({ status: connected({ connected: false, outdated: true }) }));
    render(<RemoteVersionBanner host="dev@box" />);
    expect(screen.queryByTestId("remote-version-banner")).toBeNull();
  });

  it("renders nothing when the remote server is already up to date", () => {
    mockState.mockReturnValue(state({ status: connected({ outdated: false }) }));
    render(<RemoteVersionBanner host="dev@box" />);
    expect(screen.queryByTestId("remote-version-banner")).toBeNull();
  });

  it("shows the version mismatch with an Update action", () => {
    render(<RemoteVersionBanner host="dev@box" />);
    const banner = screen.getByTestId("remote-version-banner");
    expect(banner.textContent).toContain("v1.2.3");
    expect(banner.textContent).toContain("v2.0.0");
    expect(screen.getByText("Update")).toBeTruthy();
    expect(mockState).toHaveBeenCalledWith("dev@box", true);
  });

  it("is collapsed by default and expands the explanation on demand", () => {
    render(<RemoteVersionBanner host="dev@box" />);
    expect(screen.queryByText(/interrupts running turns and terminals/i)).toBeNull();
    fireEvent.click(screen.getByLabelText("Expand version warning"));
    expect(screen.getByText(/interrupts running turns and terminals/i)).toBeTruthy();
    fireEvent.click(screen.getByLabelText("Collapse version warning"));
    expect(screen.queryByText(/interrupts running turns and terminals/i)).toBeNull();
  });

  it("confirms before restarting and cancelling does not restart", () => {
    const restart = vi.fn();
    mockState.mockReturnValue(state({ restart }));
    render(<RemoteVersionBanner host="dev@box" />);

    fireEvent.click(screen.getByText("Update"));
    expect(screen.getByText("Update remote server?")).toBeTruthy();
    // Destructive confirm: focus/primary default must be Cancel, not Update.
    const cancel = screen.getByText("Cancel");
    expect(cancel.getAttribute("data-dialog-default-action")).not.toBeNull();

    fireEvent.click(cancel);
    expect(restart).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText("Update"));
    fireEvent.click(screen.getByText("Update server"));
    expect(restart).toHaveBeenCalledTimes(1);
  });

  it("shows the updating state and disables the action while restarting", () => {
    mockState.mockReturnValue(state({ busy: "restarting" }));
    render(<RemoteVersionBanner host="dev@box" />);
    const button = screen.getByText("Updating…") as HTMLButtonElement;
    expect(button.disabled).toBe(true);
  });

  it("surfaces a restart failure", () => {
    mockState.mockReturnValue(state({ error: "kill failed (stage: remote-kill)" }));
    render(<RemoteVersionBanner host="dev@box" />);
    expect(screen.getByText(/remote-kill/)).toBeTruthy();
  });
});
