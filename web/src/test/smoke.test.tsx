import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("test infrastructure smoke test", () => {
  it("renders a component and asserts on the DOM", () => {
    render(<div>hello agents tab</div>);
    expect(screen.getByText("hello agents tab")).toBeInTheDocument();
  });
});
