import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import RecentInputsStrip from "./RecentInputsStrip";

describe("RecentInputsStrip", () => {
  it("renders one small line per input, oldest first", () => {
    render(<RecentInputsStrip inputs={["earlier ask", "latest ask"]} />);
    const strip = screen.getByTestId("recent-inputs");
    // `title` carries the canonical text; the visible line also holds a `›`
    // marker, so reading `textContent` would assert on the decoration.
    const lines = Array.from(strip.querySelectorAll("li")).map((li) => li.getAttribute("title"));
    expect(lines).toEqual(["earlier ask", "latest ask"]);
  });

  it("puts the full text in a hover tooltip even though the line is truncated", () => {
    const long = "a".repeat(400);
    render(<RecentInputsStrip inputs={[long]} />);
    expect(screen.getByTitle(long)).toBeInTheDocument();
  });

  it("exposes an accessible name so the strip is not unlabelled text", () => {
    render(<RecentInputsStrip inputs={["an ask"]} />);
    expect(screen.getByLabelText(/recent inputs/i)).toBeInTheDocument();
  });

  it("labels itself as sent so it is not read as the queued list", () => {
    render(<RecentInputsStrip inputs={["an ask"]} />);
    expect(screen.getByText(/sent — not queued/i)).toBeInTheDocument();
    // The list is *described by* that hint, so assistive tech gets it too.
    expect(screen.getByLabelText(/recent inputs/i)).toHaveAccessibleDescription(
      /sent — not queued/i,
    );
  });

  it("marks every line with a send glyph instead of a queue number", () => {
    render(<RecentInputsStrip inputs={["earlier ask", "latest ask"]} />);
    const rows = Array.from(screen.getByTestId("recent-inputs").querySelectorAll("li"));
    expect(rows).toHaveLength(2);
    for (const row of rows) {
      expect(row.querySelector("svg")).not.toBeNull();
      // Queue rows begin "1.", "2."; these never do.
      expect(row.textContent ?? "").not.toMatch(/^\s*\d+\./);
    }
  });

  it("renders nothing when there are no inputs, so it reserves no chrome", () => {
    const { container } = render(<RecentInputsStrip inputs={[]} />);
    expect(container.firstChild).toBeNull();
  });

  it("is read-only: no buttons, inputs or links to click", () => {
    render(<RecentInputsStrip inputs={["an ask"]} />);
    const strip = screen.getByTestId("recent-inputs");
    expect(strip.querySelectorAll("button, a, input, textarea")).toHaveLength(0);
  });

  it("collapses newlines in a multi-line input rather than growing the band", () => {
    render(<RecentInputsStrip inputs={["line one\nline two"]} />);
    const line = screen.getByTestId("recent-inputs").querySelector("li");
    expect(line?.getAttribute("title")).toBe("line one line two");
    // And the rendered row itself is the collapsed text, not the raw newline.
    expect(line?.lastElementChild?.textContent).toBe("line one line two");
  });
});
