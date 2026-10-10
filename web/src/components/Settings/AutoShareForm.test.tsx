import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import AutoShareForm from "./AutoShareForm";

const getAutoShareConfig = vi.fn();
const setAutoShareConfig = vi.fn();
const startShare = vi.fn();
const stopShare = vi.fn();

vi.mock("../../api/client", () => ({
  api: {
    getAutoShareConfig: (...a: unknown[]) => getAutoShareConfig(...a),
    setAutoShareConfig: (...a: unknown[]) => setAutoShareConfig(...a),
    startShare: (...a: unknown[]) => startShare(...a),
    stopShare: (...a: unknown[]) => stopShare(...a),
  },
}));

function toggle() {
  return screen.getByTestId("auto-share-toggle") as HTMLInputElement;
}

describe("AutoShareForm", () => {
  beforeEach(() => {
    getAutoShareConfig.mockReset();
    setAutoShareConfig.mockReset();
    startShare.mockReset();
    stopShare.mockReset();
    getAutoShareConfig.mockResolvedValue({ enabled: false, available: true, running: false });
    setAutoShareConfig.mockResolvedValue({ enabled: false, available: true, running: false });
    startShare.mockResolvedValue({ running: true, available: true, kind: "serve", url: "https://host.ts.net/desktop" });
    stopShare.mockResolvedValue({ running: false, available: true });
  });

  // The default is the whole safety story: a fresh install must show OFF, and
  // the copy must state that enabling makes the instance reachable.
  it("renders OFF by default and warns that it publishes to the tailnet", async () => {
    render(<AutoShareForm />);
    await waitFor(() => expect(toggle()).toBeTruthy());

    expect(toggle().checked).toBe(false);
    expect(screen.getByText(/other devices on your tailnet can reach/i)).toBeTruthy();
    expect(screen.getByText(/not published to the public internet/i)).toBeTruthy();
  });

  it("reflects a persisted ON value and a live running share from the server", async () => {
    getAutoShareConfig.mockResolvedValue({
      enabled: true,
      available: true,
      running: true,
      kind: "serve",
      url: "https://host.ts.net/desktop",
    });
    render(<AutoShareForm />);
    await waitFor(() => expect(toggle().checked).toBe(true));
    expect(screen.getByTestId("auto-share-status-text").textContent).toContain("Sharing");
    expect(screen.getByTestId("auto-share-kind").textContent).toContain("Tailnet only");
    expect(screen.getByTestId("auto-share-url").textContent).toContain("https://host.ts.net/desktop");
  });

  // Saving must send the server's STORED value back into the checkbox rather
  // than the locally requested one, so the UI can't claim a state that failed
  // to persist.
  it("adopts the server's stored value after saving", async () => {
    render(<AutoShareForm />);
    await waitFor(() => expect(toggle()).toBeTruthy());

    setAutoShareConfig.mockResolvedValue({ enabled: false, available: true, running: false });
    fireEvent.click(toggle()); // request ON locally
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(setAutoShareConfig).toHaveBeenCalledWith(true));
    await waitFor(() => expect(toggle().checked).toBe(false));
  });

  it("starts a share from the Start button and reflects it", async () => {
    render(<AutoShareForm />);
    await waitFor(() => expect(screen.getByTestId("auto-share-start")).toBeTruthy());

    fireEvent.click(screen.getByTestId("auto-share-start"));

    await waitFor(() => expect(startShare).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.getByTestId("auto-share-status-text").textContent).toContain("Sharing"));
    expect(screen.getByTestId("auto-share-url").textContent).toContain("https://host.ts.net/desktop");
  });

  it("stops a running share", async () => {
    getAutoShareConfig.mockResolvedValue({ enabled: true, available: true, running: true, url: "https://host.ts.net/desktop" });
    render(<AutoShareForm />);
    await waitFor(() => expect(screen.getByTestId("auto-share-stop")).toBeTruthy());

    fireEvent.click(screen.getByTestId("auto-share-stop"));

    await waitFor(() => expect(stopShare).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.getByTestId("auto-share-status-text").textContent).toContain("Not sharing"));
    // Stopping must not change the persisted "start at launch" preference.
    expect(toggle().checked).toBe(true);
  });

  it("warns that an enabled auto-share restarts after a manual stop", async () => {
    getAutoShareConfig.mockResolvedValue({ enabled: true, available: true, running: false });
    render(<AutoShareForm />);
    await waitFor(() => expect(toggle()).toBeTruthy());
    expect(screen.getByTestId("auto-share-stopped").textContent).toContain("start again");
  });

  it("explains the no-tailscale case instead of implying the share worked", async () => {
    getAutoShareConfig.mockResolvedValue({
      enabled: true,
      available: false,
      running: false,
      hint: "serve is not enabled on your tailnet",
    });
    render(<AutoShareForm />);
    await waitFor(() => expect(toggle().checked).toBe(true));

    expect(screen.getByTestId("auto-share-unavailable").textContent).toContain(
      "serve is not enabled on your tailnet",
    );
    expect(screen.queryByTestId("auto-share-url")).toBeNull();
    // Nothing to start when tailscale is unavailable.
    expect(screen.getByTestId("auto-share-start")).toBeDisabled();
  });

  it("surfaces a load failure instead of silently showing OFF", async () => {
    getAutoShareConfig.mockRejectedValue(new Error("boom"));
    render(<AutoShareForm />);
    await waitFor(() => expect(screen.getByText("boom")).toBeTruthy());
  });
});
