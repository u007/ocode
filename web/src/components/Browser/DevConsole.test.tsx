import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { DevConsole } from "./DevConsole";

const consoleEvents = [
  { level: "log", text: "hello world", ts: 1 },
  { level: "error", text: "boom failure", ts: 2 },
];
const networkEvents = [
  { requestId: "req-1", method: "GET", url: "https://a.dev/x", status: 200, durationMs: 12, ts: 1 },
];

describe("DevConsole", () => {
  const base = {
    consoleEvents,
    networkEvents,
    responseBodies: {},
    perfMetrics: {},
    perfRecording: true,
    perfAvailable: true,
    onClearConsole: vi.fn(),
    onClearNetwork: vi.fn(),
    onClearPerformance: vi.fn(),
    onTogglePerfRecording: vi.fn(),
    onRequestBody: vi.fn(),
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

  it("performance tab shows Stop + Recording indicator while recording", () => {
    const onTogglePerfRecording = vi.fn();
    render(<DevConsole {...base} onTogglePerfRecording={onTogglePerfRecording} />);
    fireEvent.click(screen.getByLabelText("Expand console"));
    fireEvent.click(screen.getByRole("tab", { name: /performance/i }));
    expect(screen.getByLabelText("Stop performance recording")).toBeInTheDocument();
    expect(screen.getByLabelText("Recording performance metrics")).toHaveTextContent("Recording");
    fireEvent.click(screen.getByLabelText("Stop performance recording"));
    expect(onTogglePerfRecording).toHaveBeenCalledTimes(1);
  });

  it("performance tab shows Record + Paused indicator when stopped", () => {
    render(<DevConsole {...base} perfRecording={false} perfMetrics={{ Nodes: 42 }} />);
    fireEvent.click(screen.getByLabelText("Expand console"));
    fireEvent.click(screen.getByRole("tab", { name: /performance/i }));
    expect(screen.getByLabelText("Start performance recording")).toBeInTheDocument();
    expect(screen.getByLabelText("Performance recording paused")).toHaveTextContent("Paused");
    // Last snapshot is preserved while stopped.
    expect(screen.getByText("42")).toBeInTheDocument();
  });

  it("performance tab Clear only clears metrics, not recording state", () => {
    const onClearPerformance = vi.fn();
    const onTogglePerfRecording = vi.fn();
    render(<DevConsole {...base} onClearPerformance={onClearPerformance} onTogglePerfRecording={onTogglePerfRecording} />);
    fireEvent.click(screen.getByLabelText("Expand console"));
    fireEvent.click(screen.getByRole("tab", { name: /performance/i }));
    fireEvent.click(screen.getByLabelText("Clear performance metrics"));
    expect(onClearPerformance).toHaveBeenCalledTimes(1);
    expect(onTogglePerfRecording).not.toHaveBeenCalled();
  });

  it("performance toggle is disabled off Chrome mode", () => {
    render(<DevConsole {...base} perfAvailable={false} />);
    fireEvent.click(screen.getByLabelText("Expand console"));
    fireEvent.click(screen.getByRole("tab", { name: /performance/i }));
    expect(screen.getByLabelText("Stop performance recording")).toBeDisabled();
  });
});
