import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, act } from "@testing-library/react";
import { PulseShellSignal } from "./PulseShellSignal";

/** The desktop shell reaches the page with window.ExecJS dispatching a plain
 *  DOM CustomEvent (see cmd/ocode-desktop/main.go buildAppMenu — the webview
 *  loads a plain http:// URL, so window.EmitEvent is structurally unavailable). */
function fireShellEvent(name: string) {
  act(() => {
    window.dispatchEvent(new CustomEvent(name));
  });
}

describe("PulseShellSignal", () => {
  let onOpen: ReturnType<typeof vi.fn<() => void>>;
  let onFocus: ReturnType<typeof vi.fn<() => void>>;

  function mount() {
    return render(
      <PulseShellSignal
        onOpen={() => onOpen()}
        onFocus={() => onFocus()}
      />,
    );
  }

  beforeEach(() => {
    onOpen = vi.fn(() => {});
    onFocus = vi.fn(() => {});
  });

  afterEach(() => vi.restoreAllMocks());

  it("calls onOpen when the shell dispatches ocode:open-pulse", () => {
    mount();
    fireShellEvent("ocode:open-pulse");
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it("also focuses the window, so the dashboard is not opened behind another app", () => {
    // The user clicked a tray/app-menu item while ocode was in the background;
    // switching the view without raising the window looks like nothing happened.
    mount();
    fireShellEvent("ocode:open-pulse");
    expect(onFocus).toHaveBeenCalledTimes(1);
  });

  it("ignores the unrelated shell events it shares a namespace with", () => {
    mount();
    fireShellEvent("ocode:open-settings");
    fireShellEvent("ocode:quit-blocked");
    expect(onOpen).not.toHaveBeenCalled();
    expect(onFocus).not.toHaveBeenCalled();
  });

  it("stops listening after unmount", () => {
    // A listener left on window survives the view and would re-open the
    // dashboard on a later dispatch, long after the user left.
    const { unmount } = mount();
    unmount();
    fireShellEvent("ocode:open-pulse");
    expect(onOpen).not.toHaveBeenCalled();
  });

  it("is safe in a browser where the shell never dispatches anything", () => {
    expect(() => {
      const { unmount } = mount();
      unmount();
    }).not.toThrow();
  });
});
