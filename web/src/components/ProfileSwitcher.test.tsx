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

// The pill reads the active project's binding so it can explain when the
// window-level choice does not apply.
const projectFake = vi.hoisted(() => ({
  activeProject: null as { profile?: string } | null,
}));
vi.mock("../stores/projectStore", () => ({
  useProjectState: () => ({ state: { activeProject: projectFake.activeProject } }),
}));

function response(data: unknown) {
  return Promise.resolve({ json: () => Promise.resolve(data) });
}

beforeEach(() => {
  vi.clearAllMocks();
  projectFake.activeProject = null;
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

describe("ProfileSwitcher project binding hint", () => {
  it("names the project's bound profile and says the window choice does not apply", async () => {
    projectFake.activeProject = { profile: "work" };
    render(<ProfileSwitcher />);

    const trigger = await screen.findByRole("button", { name: /Base.*project: work/ });
    expect(trigger.getAttribute("title")).toContain("bound to this project");

    fireEvent.click(trigger);
    expect(screen.getByText(/its sessions ignore this choice/)).toBeInTheDocument();
  });

  it("shows no project hint when the active project has no binding", async () => {
    projectFake.activeProject = { profile: "" };
    render(<ProfileSwitcher />);

    const trigger = await screen.findByRole("button", { name: /Base/ });
    expect(trigger.textContent).not.toContain("project:");
    fireEvent.click(trigger);
    expect(screen.queryByText(/its sessions ignore this choice/)).toBeNull();
  });
});
