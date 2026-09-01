import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { DevConsole } from "./DevConsole";

const consoleEvents = [
  { level: "log", text: "hello world", ts: 1 },
  { level: "error", text: "boom failure", ts: 2 },
];
const networkEvents = [
  { method: "GET", url: "https://a.dev/x", status: 200, durationMs: 12, ts: 1 },
];

describe("DevConsole", () => {
  const base = {
    consoleEvents,
    networkEvents,
    onClearConsole: vi.fn(),
    onClearNetwork: vi.fn(),
  };

  it("starts collapsed and expands on toggle", () => {
    render(<DevConsole {...base} />);
    expect(screen.queryByText(/hello world/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Expand console"));
    expect(screen.getByText(/hello world/)).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Collapse console"));
    expect(screen.queryByText(/hello world/)).not.toBeInTheDocument();
  });

  it("filters console entries", () => {
    render(<DevConsole {...base} />);
    fireEvent.click(screen.getByLabelText("Expand console"));
    expect(screen.getByText(/hello world/)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Filter"), { target: { value: "boom" } });
    expect(screen.queryByText(/hello world/)).not.toBeInTheDocument();
    expect(screen.getByText(/boom failure/)).toBeInTheDocument();
  });

  it("clears the console", () => {
    const onClearConsole = vi.fn();
    render(<DevConsole {...base} onClearConsole={onClearConsole} />);
    fireEvent.click(screen.getByLabelText("Clear"));
    expect(onClearConsole).toHaveBeenCalled();
  });

  it("switches to the Network tab", () => {
    render(<DevConsole {...base} />);
    fireEvent.click(screen.getByLabelText("Expand console"));
    fireEvent.click(screen.getByRole("tab", { name: /network/i }));
    expect(screen.getByText("https://a.dev/x")).toBeInTheDocument();
  });
});
