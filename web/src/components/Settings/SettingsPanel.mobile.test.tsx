import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { useEffect } from "react";
import SettingsPanel from "./SettingsPanel";

// SettingsPanel's default group is ModelDefaultsForm; stub the two groups this
// suite drives so the nav assertions don't drag in the chat/config stores.
const mounts = vi.hoisted(() => ({ count: 0 }));

vi.mock("./ModelDefaultsForm", () => ({
  default: () => {
    useEffect(() => {
      mounts.count += 1;
    }, []);
    return <div data-testid="model-defaults-form">ModelDefaultsForm</div>;
  },
}));
vi.mock("./ChatDisplayForm", () => ({
  default: () => <div data-testid="chat-display-form">ChatDisplayForm</div>,
}));

// The shared test setup evaluates `(max-width: Npx)` against window.innerWidth
// on every read, so choosing the viewport before mount selects the branch.
function setViewportWidth(width: number) {
  Object.defineProperty(window, "innerWidth", {
    configurable: true,
    writable: true,
    value: width,
  });
}

function drawer(): HTMLElement {
  const el = document.getElementById("settings-nav-drawer");
  if (!el) throw new Error("settings drawer not rendered");
  return el;
}

describe("SettingsPanel mobile layout", () => {
  beforeEach(() => setViewportWidth(390));
  afterEach(() => setViewportWidth(1024));

  // The regression: <=md used to render the two <NavSection> stacks side by
  // side in a horizontal flex row across the full width, which pushed the
  // selected form below the viewport. On a phone the section list must be an
  // overlay, never a column that shares the screen with the form.
  it("puts the section list in an off-canvas overlay, not a split view", () => {
    render(<SettingsPanel />);

    // No inline resizer/handle column on mobile.
    expect(screen.queryByRole("separator", { name: "Resize settings navigation" })).toBeNull();

    const nav = drawer();
    expect(nav.className).toContain("fixed");
    expect(nav.className).toContain("inset-y-0");
    expect(nav.className).toContain("-translate-x-full");
    // Closed drawer is out of the tab order and the a11y tree.
    expect(nav.hasAttribute("inert")).toBe(true);
    expect(nav.getAttribute("aria-hidden")).toBe("true");

    // The form owns the screen; the nav buttons are not reachable.
    expect(screen.getByTestId("model-defaults-form")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Chat display" })).toBeNull();
    expect(screen.getByRole("button", { name: "Open settings sections" })).toBeTruthy();
  });

  it("opens the drawer, selects a section, and dismisses itself", () => {
    render(<SettingsPanel />);

    fireEvent.click(screen.getByRole("button", { name: "Open settings sections" }));

    const openButton = screen.getByRole("button", { name: "Open settings sections" });
    expect(openButton.getAttribute("aria-expanded")).toBe("true");
    expect(drawer().className).toContain("translate-x-0");
    expect(drawer().hasAttribute("inert")).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: "Chat display" }));

    // The chosen form rendered AND the drawer closed (a stuck drawer would
    // leave the form behind an overlay the user cannot dismiss).
    expect(screen.getByTestId("chat-display-form")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Open settings sections" }).getAttribute("aria-expanded"),
    ).toBe("false");
    expect(drawer().className).toContain("-translate-x-full");
    expect(drawer().hasAttribute("inert")).toBe(true);
    // The compact header names the active section so the choice is visible
    // (scoped to the header: the closed drawer still holds a "Chat display"
    // button, which getByText would also match).
    expect(screen.getByTestId("settings-active-group").textContent).toBe("Chat display");
  });

  it("closes the drawer on Escape and on a backdrop tap", () => {
    render(<SettingsPanel />);

    fireEvent.click(screen.getByRole("button", { name: "Open settings sections" }));
    expect(drawer().className).toContain("translate-x-0");
    fireEvent.keyDown(window, { key: "Escape" });
    expect(drawer().className).toContain("-translate-x-full");

    fireEvent.click(screen.getByRole("button", { name: "Open settings sections" }));
    const backdrop = document.querySelector(".z-40");
    expect(backdrop).toBeTruthy();
    fireEvent.click(backdrop as Element);
    expect(drawer().className).toContain("-translate-x-full");

    // The explicit close button is also wired.
    fireEvent.click(screen.getByRole("button", { name: "Open settings sections" }));
    fireEvent.click(screen.getByRole("button", { name: "Close settings sections" }));
    expect(drawer().className).toContain("-translate-x-full");
  });

  it("keeps the resizable split view on desktop", () => {
    setViewportWidth(1280);
    render(<SettingsPanel />);

    expect(screen.getByRole("separator", { name: "Resize settings navigation" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Open settings sections" })).toBeNull();
    // Desktop keeps the inline nav column with the persisted width var.
    const nav = document.querySelector("nav");
    expect(nav?.className).toContain("w-[var(--nav-width)]");
    expect(screen.getByRole("button", { name: "Chat display" })).toBeTruthy();
    expect(screen.getByTestId("model-defaults-form")).toBeTruthy();
  });

  // The active form owns unsaved input, so crossing the breakpoint (rotation,
  // split-screen, window resize) must not unmount and remount it.
  it("keeps the active form mounted when the viewport crosses the breakpoint", () => {
    mounts.count = 0;
    render(<SettingsPanel />);
    const form = screen.getByTestId("model-defaults-form");
    expect(mounts.count).toBe(1);

    setViewportWidth(1280);
    act(() => {
      window.dispatchEvent(new Event("resize"));
    });

    expect(screen.queryByRole("button", { name: "Open settings sections" })).toBeNull();
    expect(screen.getByTestId("model-defaults-form")).toBe(form);
    expect(mounts.count).toBe(1);
  });
});
