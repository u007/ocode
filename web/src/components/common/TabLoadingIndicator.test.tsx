import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TabLoadingIndicator } from "./TabLoadingIndicator";

describe("TabLoadingIndicator", () => {
  it("renders a reduced-motion-safe live spinner only while active", () => {
    const { rerender } = render(<TabLoadingIndicator active label="Loading Git" />);
    const status = screen.getByRole("status");
    expect(status).toHaveAttribute("aria-live", "polite");
    expect(status).toHaveTextContent("Loading Git");
    expect(status.querySelector("svg")).toHaveClass("motion-reduce:animate-none");

    rerender(<TabLoadingIndicator active={false} label="Loading Git" />);
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("can expose an initial-load error dot", () => {
    render(<TabLoadingIndicator active={false} error label="Loading Cron" />);
    const status = screen.getByRole("status");
    expect(status).toHaveTextContent("Loading Cron");
    expect(status).toHaveAttribute("data-error", "true");
  });
});
