import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ThinkingBlock } from "./TurnParts";

describe("ThinkingBlock", () => {
  it("renders the Speak button when onSpeak is provided", () => {
    render(<ThinkingBlock text="some reasoning" onSpeak={() => {}} />);
    expect(screen.getByRole("button", { name: /speak thinking/i })).toBeInTheDocument();
  });

  it("calls onSpeak when Speak is clicked", () => {
    const onSpeak = vi.fn();
    render(<ThinkingBlock text="some reasoning" onSpeak={onSpeak} />);
    fireEvent.click(screen.getByRole("button", { name: /speak thinking/i }));
    expect(onSpeak).toHaveBeenCalledTimes(1);
  });

  it("does not render the Speak button when onSpeak is omitted", () => {
    render(<ThinkingBlock text="some reasoning" />);
    expect(screen.queryByRole("button", { name: /speak thinking/i })).not.toBeInTheDocument();
  });
});
