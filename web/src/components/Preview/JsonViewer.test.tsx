import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import JsonViewer from "./JsonViewer";

describe("JsonViewer", () => {
  it("renders a collapsible tree for valid JSON", () => {
    const { container } = render(<JsonViewer content='"hello"' />);
    expect(container.textContent).toContain("hello");
  });

  it("renders nested objects with count preview", () => {
    render(<JsonViewer content='{"a":{"b":1,"c":2}}' />);
    const button = screen.getAllByRole("button")[0];
    fireEvent.click(button);
    expect(screen.getAllByRole("button")[1]).toHaveAttribute("aria-expanded", "false");
  });

  it("shows an inline error for invalid JSON without throwing", () => {
    render(<JsonViewer content='{"broken":' />);
    expect(screen.getByText("Invalid JSON")).toBeInTheDocument();
  });

  it("renders arrays with item count preview", () => {
    render(<JsonViewer content='[1,2,3]' />);
    expect(screen.getByText(/3 items/)).toBeInTheDocument();
  });

  it("expands and collapses on click", () => {
    render(<JsonViewer content='{"a":{"b":1}}' />);
    const button = screen.getAllByRole("button")[0];
    expect(button).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(button);
    expect(screen.getAllByRole("button")[0]).toHaveAttribute("aria-expanded", "true");
  });
});
