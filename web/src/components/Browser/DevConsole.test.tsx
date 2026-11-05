import { act, render, screen, fireEvent } from "@testing-library/react";
import { afterEach, describe, it, expect, vi } from "vitest";
import { DevConsole } from "./DevConsole";

const copyTextToClipboard = vi.hoisted(() => vi.fn(async (_text: string) => true));
vi.mock("../../lib/clipboard", () => ({ copyTextToClipboard }));

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

  it("switches to the Network tab and expands the collapsed panel", () => {
    render(<DevConsole {...base} />);
    expect(screen.queryByText("hello world")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: /network/i }));
    expect(screen.getByText("https://a.dev/x")).toBeInTheDocument();
    expect(screen.getByLabelText("Collapse console")).toBeInTheDocument();
    // Clicking the already-active tab does not collapse the panel.
    fireEvent.click(screen.getByRole("tab", { name: /network/i }));
    expect(screen.getByLabelText("Collapse console")).toBeInTheDocument();
  });

  it("expands a local network row without requesting a CDP body", () => {
    const onRequestBody = vi.fn();
    const localEvents = [
      { requestId: "local-1-1", method: "POST", url: "https://api.dev/collect", status: 204, durationMs: 5, ts: 1, requestHeaders: { "content-type": "application/json" }, responseHeaders: { "x-req": "1" } },
    ];
    render(
      <DevConsole
        {...base}
        networkEvents={localEvents}
        responseBodies={{ "local-1-1": { body: "{\"ok\":true}", base64Encoded: false, truncated: false } }}
        onRequestBody={onRequestBody}
      />,
    );
    fireEvent.click(screen.getByRole("tab", { name: /network/i }));
    fireEvent.click(screen.getByText("https://api.dev/collect"));
    expect(screen.getByText("Request Headers")).toBeInTheDocument();
    expect(screen.getByText("Response Headers")).toBeInTheDocument();
    expect(onRequestBody).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: /body/i }));
    expect(screen.getByText("{\"ok\":true}")).toBeInTheDocument();
  });

  it("keeps an expanded local row open when its body arrives later", () => {
    // A fetch body is a SEPARATE message posted after the row. It must update
    // the row's detail in place, never re-mount the list and collapse the row
    // the user just opened.
    const row = { requestId: "local-2-1", method: "POST", url: "https://api.dev/late", status: 200, durationMs: 9, ts: 2 };
    const { rerender } = render(<DevConsole {...base} networkEvents={[row]} responseBodies={{}} />);
    fireEvent.click(screen.getByRole("tab", { name: /network/i }));
    fireEvent.click(screen.getByText("https://api.dev/late"));
    fireEvent.click(screen.getByRole("button", { name: /body/i }));
    expect(screen.getByText("Body not captured in local mode")).toBeInTheDocument();

    rerender(
      <DevConsole
        {...base}
        networkEvents={[row]}
        responseBodies={{ "local-2-1": { body: "{\"late\":true}", base64Encoded: false, truncated: false } }}
      />,
    );
    // Still expanded, still on the Body sub-tab, and the body is now visible.
    expect(screen.getByRole("button", { name: /headers/i })).toBeInTheDocument();
    expect(screen.getByText("{\"late\":true}")).toBeInTheDocument();
  });

  it("requests a body for a CDP row that has none yet", () => {
    const onRequestBody = vi.fn();
    render(
      <DevConsole
        {...base}
        networkEvents={[
          { requestId: "1234.56", method: "GET", url: "https://a.dev/cdp", status: 200, durationMs: 4, ts: 9 },
        ]}
        responseBodies={{}}
        onRequestBody={onRequestBody}
      />,
    );
    fireEvent.click(screen.getByRole("tab", { name: /network/i }));
    fireEvent.click(screen.getByText("https://a.dev/cdp"));
    expect(onRequestBody).toHaveBeenCalledWith("1234.56");
    // Collapsing the row fetches nothing.
    fireEvent.click(screen.getByText("https://a.dev/cdp"));
    expect(onRequestBody).toHaveBeenCalledTimes(1);
  });

  it("expands a legacy row with no requestId and never asks CDP for one", () => {
    const onRequestBody = vi.fn();
    render(
      <DevConsole
        {...base}
        networkEvents={[{ requestId: "", method: "GET", url: "https://a.dev/legacy", status: 200, durationMs: 2, ts: 7 }]}
        responseBodies={{}}
        onRequestBody={onRequestBody}
      />,
    );
    fireEvent.click(screen.getByRole("tab", { name: /network/i }));
    fireEvent.click(screen.getByText("https://a.dev/legacy"));
    expect(screen.getByText("No headers available")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /body/i }));
    expect(screen.getByText("Body not captured in local mode")).toBeInTheDocument();
    expect(onRequestBody).not.toHaveBeenCalled();
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

  describe("copy output", () => {
    afterEach(() => {
      copyTextToClipboard.mockClear();
      copyTextToClipboard.mockResolvedValue(true);
      vi.useRealTimers();
    });

    it("copies only the filtered console rows as LEVEL text lines", async () => {
      render(<DevConsole {...base} />);
      fireEvent.click(screen.getByLabelText("Expand console"));
      fireEvent.change(screen.getByLabelText("Filter"), { target: { value: "boom" } });
      await act(async () => {
        fireEvent.click(screen.getByLabelText("Copy console output"));
      });
      expect(copyTextToClipboard).toHaveBeenCalledWith("ERROR boom failure");
      expect(screen.getByLabelText("Copy console output").dataset.state).toBe("copied");
    });

    it("copies network rows with the fields the row shows", async () => {
      render(<DevConsole {...base} />);
      fireEvent.click(screen.getByLabelText("Expand console"));
      fireEvent.click(screen.getByRole("tab", { name: /network/i }));
      await act(async () => {
        fireEvent.click(screen.getByLabelText("Copy network output"));
      });
      expect(copyTextToClipboard).toHaveBeenCalledWith("200 GET https://a.dev/x 12ms —");
    });

    it("copies performance metrics with their display labels and values", async () => {
      render(<DevConsole {...base} perfMetrics={{ Nodes: 42, JSHeapUsedSize: 2048 }} />);
      fireEvent.click(screen.getByLabelText("Expand console"));
      fireEvent.click(screen.getByRole("tab", { name: /performance/i }));
      await act(async () => {
        fireEvent.click(screen.getByLabelText("Copy performance output"));
      });
      // Same order as the table: sorted by raw metric key.
      expect(copyTextToClipboard).toHaveBeenCalledWith("JS Heap Used: 2.0 KB\nDOM Nodes: 42");
    });

    it("shows the failure state when the clipboard write is refused", async () => {
      copyTextToClipboard.mockResolvedValue(false);
      render(<DevConsole {...base} />);
      fireEvent.click(screen.getByLabelText("Expand console"));
      await act(async () => {
        fireEvent.click(screen.getByLabelText("Copy console output"));
      });
      expect(screen.getByLabelText("Copy console output").dataset.state).toBe("failed");
    });

    it("returns to the idle state after the confirmation expires", async () => {
      vi.useFakeTimers();
      render(<DevConsole {...base} />);
      fireEvent.click(screen.getByLabelText("Expand console"));
      await act(async () => {
        fireEvent.click(screen.getByLabelText("Copy console output"));
      });
      expect(screen.getByLabelText("Copy console output").dataset.state).toBe("copied");
      await act(async () => {
        vi.advanceTimersByTime(1500);
      });
      expect(screen.getByLabelText("Copy console output").dataset.state).toBe("idle");
    });

    it("is disabled when the active tab has no rows", () => {
      render(<DevConsole {...base} />);
      fireEvent.click(screen.getByLabelText("Expand console"));
      fireEvent.change(screen.getByLabelText("Filter"), { target: { value: "no-such-text" } });
      expect(screen.getByLabelText("Copy console output")).toBeDisabled();
    });
  });

  describe("selectable output", () => {
    afterEach(() => {
      window.getSelection()?.removeAllRanges();
    });

    it.each([
      ["console", "hello world"],
      ["network", "https://a.dev/x"],
      ["performance", "DOM Nodes"],
    ] as const)("makes the %s output selectable", (tab, marker) => {
      const { container } = render(<DevConsole {...base} perfMetrics={{ Nodes: 42 }} />);
      fireEvent.click(screen.getByLabelText("Expand console"));
      fireEvent.click(screen.getByRole("tab", { name: new RegExp(tab, "i") }));
      expect((container.firstChild as HTMLElement).className).not.toContain("select-none");
      expect(screen.getByText(marker).closest(".overflow-auto")).toHaveClass("select-text");
    });

    it("keeps the tab bar out of the selection", () => {
      render(<DevConsole {...base} />);
      fireEvent.click(screen.getByLabelText("Expand console"));
      expect(screen.getByRole("tab", { name: /console/i }).parentElement).toHaveClass("select-none");
    });

    it("does not toggle a network row when the click ends a text selection", () => {
      render(<DevConsole {...base} />);
      fireEvent.click(screen.getByLabelText("Expand console"));
      fireEvent.click(screen.getByRole("tab", { name: /network/i }));
      const url = screen.getByText("https://a.dev/x");
      const range = document.createRange();
      range.selectNodeContents(url);
      window.getSelection()!.removeAllRanges();
      window.getSelection()!.addRange(range);
      fireEvent.click(url);
      expect(screen.queryByText("No headers available")).not.toBeInTheDocument();
    });

    it("still expands a network row on a plain click", () => {
      render(<DevConsole {...base} />);
      fireEvent.click(screen.getByLabelText("Expand console"));
      fireEvent.click(screen.getByRole("tab", { name: /network/i }));
      fireEvent.click(screen.getByText("https://a.dev/x"));
      expect(screen.getByText("No headers available")).toBeInTheDocument();
    });
  });
});
