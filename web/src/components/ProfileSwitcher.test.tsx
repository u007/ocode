import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ProfileSwitcher } from "./ProfileSwitcher";

const hoisted = vi.hoisted(() => ({
  authedFetch: vi.fn(),
}));

vi.mock("../api/client", () => ({ authedFetch: hoisted.authedFetch }));
vi.mock("../lib/eventBus", () => ({
  eventBus: { on: vi.fn(() => () => {}) },
}));
vi.mock("../lib/windowId", () => ({ getWindowId: () => "window-1" }));

function response(data: unknown) {
  return Promise.resolve({ json: () => Promise.resolve(data) });
}

beforeEach(() => {
  vi.clearAllMocks();
  hoisted.authedFetch.mockImplementation((url: string, init?: RequestInit) => {
    if (url === "/api/profiles") {
      return response({
        profiles: [
          { name: "alpha", displayName: "Alpha", overrideCount: 2, credentialCount: 1 },
          { name: "beta", displayName: "Beta", overrideCount: 1, credentialCount: 0 },
        ],
      });
    }
    if (url.includes("activeProfile")) {
      const body = init?.body ? JSON.parse(String(init.body)) : null;
      return response({ activeProfile: body?.profile ?? "" });
    }
    return response({});
  });
});

describe("ProfileSwitcher keyboard navigation", () => {
  it("focuses the active profile, navigates, and selects with Enter", async () => {
    render(<ProfileSwitcher />);
    await waitFor(() => expect(screen.getByRole("button", { name: /Base/ })).toBeInTheDocument());
    const trigger = screen.getByRole("button", { name: /Base/ });
    fireEvent.click(trigger);

    const defaultOption = screen.getByRole("button", { name: /Default \(base\)/ });
    await waitFor(() => expect(document.activeElement).toBe(defaultOption));
    const alpha = screen.getByRole("button", { name: /alpha/ });
    fireEvent.keyDown(defaultOption, { key: "ArrowDown" });
    expect(document.activeElement).toBe(alpha);

    fireEvent.keyDown(alpha, { key: "Enter" });
    await waitFor(() => {
      expect(hoisted.authedFetch).toHaveBeenCalledWith(
        "/api/window/window-1/activeProfile",
        expect.objectContaining({ method: "PUT", body: JSON.stringify({ profile: "alpha" }) }),
      );
    });
    expect(screen.queryByRole("button", { name: /Default \(base\)/ })).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("closes on Escape and Tab while restoring trigger focus on Escape", async () => {
    render(<ProfileSwitcher />);
    const trigger = await screen.findByRole("button", { name: /Base/ });
    fireEvent.click(trigger);
    const alpha = screen.getByRole("button", { name: /alpha/ });

    fireEvent.keyDown(alpha, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("button", { name: /alpha/ })).toBeNull());
    expect(document.activeElement).toBe(trigger);

    fireEvent.click(trigger);
    fireEvent.keyDown(screen.getByRole("button", { name: /alpha/ }), { key: "Tab" });
    await waitFor(() => expect(screen.queryByRole("button", { name: /alpha/ })).toBeNull());
  });
});
