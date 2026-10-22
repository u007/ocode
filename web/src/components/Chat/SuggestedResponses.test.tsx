import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import SuggestedResponses from "./SuggestedResponses";

describe("SuggestedResponses", () => {
  it("renders a labelled toolbar of the proposed options", () => {
    render(
      <SuggestedResponses
        suggestions={["Run the tests", "Open a PR"]}
        onSelect={vi.fn()}
      />,
    );
    expect(screen.getByRole("toolbar", { name: "Suggested responses" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Run the tests" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Open a PR" })).toBeInTheDocument();
  });

  it("renders nothing when there are no suggestions", () => {
    const { container } = render(
      <SuggestedResponses suggestions={[]} onSelect={vi.fn()} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("reports the chosen option", () => {
    const onSelect = vi.fn();
    render(
      <SuggestedResponses
        suggestions={["Run the tests", "Open a PR"]}
        onSelect={onSelect}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Open a PR" }));
    expect(onSelect).toHaveBeenCalledExactlyOnceWith("Open a PR");
  });

  it("renders the option text as plain text, never as markup", () => {
    // The options are model-authored. If they were rendered as markdown or
    // HTML, a suggestion could inject formatting into the transcript.
    const nasty = "<img src=x onerror=alert(1)>";
    const { container } = render(
      <SuggestedResponses suggestions={[nasty]} onSelect={vi.fn()} />,
    );
    expect(container.querySelector("img")).toBeNull();
    expect(container.textContent).toContain(nasty);
  });
});
