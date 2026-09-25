import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { TabLoadingOverlay } from "./TabLoadingOverlay";
import { emitTabLoadEvent, tabLoadKey, useKeyedLoad, useTabLoadingStore } from "@/hooks/useKeyedLoad";

describe("TabLoadingOverlay", () => {
  it("announces visible loading text and exposes busy state", () => {
    render(<TabLoadingOverlay active label="Loading Files" />);
    const status = screen.getByRole("status");
    expect(status).toHaveAttribute("aria-live", "polite");
    expect(status).toHaveAttribute("aria-busy", "true");
    expect(status).toHaveTextContent("Loading Files");
    expect(status.querySelector("svg")).toHaveClass("motion-reduce:animate-none");
  });

  it("renders a focusable retry action for an initial error without a spinner", () => {
    const retry = vi.fn();
    render(<TabLoadingOverlay active={false} error="Network unavailable" onRetry={retry} />);
    const status = screen.getByRole("status");
    expect(status).toHaveTextContent("Network unavailable");
    expect(status.querySelector(".animate-spin")).toBeNull();
    const button = screen.getByRole("button", { name: "Retry" });
    button.focus();
    expect(button).toHaveFocus();
    fireEvent.click(button);
    expect(retry).toHaveBeenCalledTimes(1);
  });

  it("wires the store retry callback back into a successful load", async () => {
    const key = tabLoadKey("", "/project", "git");
    let attempt = 0;
    function Harness() {
      const run = useKeyedLoad(key, emitTabLoadEvent);
      const state = useTabLoadingStore().get(key);
      return (
        <>
          <button
            onClick={() => {
              attempt += 1;
              void run(() => attempt === 1 ? Promise.reject(new Error("offline")) : Promise.resolve("ready"));
            }}
          >
            Load
          </button>
          <TabLoadingOverlay
            active={state?.phase === "initial"}
            error={state?.phase === "error" ? state.error : undefined}
            onRetry={state?.retry}
          />
        </>
      );
    }
    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Load" }));
    expect(await screen.findByRole("button", { name: "Retry" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Retry" })).toBeNull());
  });
});
