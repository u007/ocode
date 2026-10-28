import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import JsonViewer from "./JsonViewer";

describe("JsonViewer", () => {
  it("renders a collapsible tree for valid JSON", () => {
    const { container } = render(<JsonViewer content='"hello"' />);
    expect(container.textContent).toContain("hello");
  });

  it("expands the first level by default", () => {
    // Root and its direct children are open, so `a` and its leaves are visible.
    render(<JsonViewer content='{"a":{"b":1,"c":2}}' />);
    const buttons = screen.getAllByRole("button");
    expect(buttons[0]).toHaveAttribute("aria-expanded", "true"); // root
    expect(buttons[1]).toHaveAttribute("aria-expanded", "true"); // a
    expect(screen.getByText("b:")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
  });

  it("keeps deeper levels collapsed until toggled", () => {
    render(<JsonViewer content='{"a":{"b":{"c":1}}}' />);
    // root (open) -> a (open, depth 1) -> b (collapsed, depth 2)
    const buttons = screen.getAllByRole("button");
    expect(buttons[2]).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("c:")).not.toBeInTheDocument();
    fireEvent.click(buttons[2]);
    expect(screen.getByText("c:")).toBeInTheDocument();
  });

  it("shows an inline error for invalid JSON without throwing", () => {
    render(<JsonViewer content='{"broken":' />);
    expect(screen.getByText("Invalid JSON")).toBeInTheDocument();
  });

  it("renders arrays with item count preview", () => {
    render(<JsonViewer content='[1,2,3]' />);
    expect(screen.getByText(/3 items/)).toBeInTheDocument();
  });

  it("handles empty containers, scalars and long strings without throwing", () => {
    const { container: empty } = render(<JsonViewer content='{"a":{},"b":[]}' />);
    expect(empty.textContent).toContain("a:");
    expect(empty.textContent).toContain("b:");

    const { container: scalar } = render(<JsonViewer content='"just a string"' />);
    expect(scalar.textContent).toContain("just a string");

    const long = "x".repeat(5000);
    const { container: longRender } = render(
      <JsonViewer content={JSON.stringify({ long })} />,
    );
    expect(longRender.textContent).toContain("x".repeat(20));

    const { container: emptyRoot } = render(<JsonViewer content="{}" />);
    expect(emptyRoot.textContent).toContain("0 keys");
  });

  it("collapses and re-expands on click", () => {
    render(<JsonViewer content='{"a":{"b":1}}' />);
    const button = screen.getAllByRole("button")[0];
    expect(button).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(button);
    expect(screen.getAllByRole("button")[0]).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(screen.getAllByRole("button")[0]);
    expect(screen.getAllByRole("button")[0]).toHaveAttribute("aria-expanded", "true");
  });
});
