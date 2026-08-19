import { render, screen, waitFor, act } from "@testing-library/react";
import { describe, expect, it, beforeEach } from "vitest";
import ProcessesPanel from "./ProcessesPanel";
import { eventBus } from "@/lib/eventBus";
import { saveSessionTerminals } from "./terminalPersistence";

describe("ProcessesPanel", () => {
  const sessionId = "sess-1";

  beforeEach(() => {
    window.localStorage.clear();
  });

  it("shows nothing until a terminal_processes envelope arrives", () => {
    render(<ProcessesPanel sessionId={sessionId} />);
    expect(screen.getByText(/No terminal process data yet/)).toBeInTheDocument();
  });

  it("renders rows sorted by CPU% descending, using persisted terminal titles", async () => {
    saveSessionTerminals(
      sessionId,
      [
        { id: "term-a", title: "Terminal 1" },
        { id: "term-b", title: "Terminal 2" },
      ],
      "term-a",
    );

    render(<ProcessesPanel sessionId={sessionId} />);

    act(() => {
      eventBus["handlers"]
        .get("terminal_processes")
        ?.forEach((h) =>
          h({
            event: "terminal_processes",
            seq: 1,
            data: [
              { id: "term-a", pid: 111, cpu_percent: 2.5, mem_bytes: 1024 * 1024 },
              { id: "term-b", pid: 222, cpu_percent: 87.4, mem_bytes: 10 * 1024 * 1024 },
            ],
          }),
        );
    });

    await waitFor(() => {
      const rows = screen.getAllByRole("row").slice(1); // skip header row
      expect(rows[0]).toHaveTextContent("Terminal 2");
      expect(rows[0]).toHaveTextContent("87.4%");
      expect(rows[1]).toHaveTextContent("Terminal 1");
    });
  });
});
