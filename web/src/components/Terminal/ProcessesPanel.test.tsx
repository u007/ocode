import { render, screen, waitFor, act, fireEvent } from "@testing-library/react";
import { describe, expect, it, beforeEach } from "vitest";
import ProcessesPanel from "./ProcessesPanel";
import { eventBus } from "@/lib/eventBus";
import { saveProjectTerminals } from "./terminalPersistence";

describe("ProcessesPanel", () => {
  const projectPath = "/project";

  beforeEach(() => {
    window.localStorage.clear();
  });

  it("shows nothing until a terminal_processes envelope arrives", () => {
    render(<ProcessesPanel projectPath={projectPath} />);
    expect(screen.getByText(/No terminal process data yet/)).toBeInTheDocument();
  });

  it("renders rows sorted by CPU% descending, using persisted terminal titles", async () => {
    saveProjectTerminals(
      projectPath,
      [
        { id: "term-a", title: "Terminal 1" },
        { id: "term-b", title: "Terminal 2" },
      ],
      "term-a",
    );

    render(<ProcessesPanel projectPath={projectPath} />);

    act(() => {
      eventBus["handlers"]
        .get("terminal_processes")
        ?.forEach((h) =>
          h({
            event: "terminal_processes",
            project: projectPath,
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

  it("ignores process snapshots for another project", async () => {
    saveProjectTerminals(projectPath, [{ id: "term-a", title: "Terminal 1" }], "term-a");
    render(<ProcessesPanel projectPath={projectPath} />);

    act(() => {
      eventBus["handlers"]
        .get("terminal_processes")
        ?.forEach((h) => h({ event: "terminal_processes", project: "/other", seq: 1, data: [
          { id: "term-a", pid: 111, cpu_percent: 99, mem_bytes: 1024 },
        ] }));
    });

    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(screen.queryByText("99.0%")).not.toBeInTheDocument();
  });

  it("renders the running command in its own column with the truncate class", async () => {
    saveProjectTerminals(
      projectPath,
      [{ id: "term-a", title: "Terminal 1" }],
      "term-a",
    );

    render(<ProcessesPanel projectPath={projectPath} />);

    act(() => {
      eventBus["handlers"]
        .get("terminal_processes")
        ?.forEach((h) =>
          h({
            event: "terminal_processes",
            project: projectPath,
            seq: 1,
            data: [
              {
                id: "term-a",
                pid: 111,
                cpu_percent: 2.5,
                mem_bytes: 1024 * 1024,
                command: "npm run dev -- --port 4096 --host 0.0.0.0",
              },
            ],
          }),
        );
    });

    await waitFor(() => {
      const row = screen.getAllByRole("row").slice(1)[0];
      const cells = row.querySelectorAll("td");
      // Command is the second column (after Name).
      const commandCell = cells[1];
      expect(commandCell).toHaveTextContent("npm run dev");
      const truncated = commandCell.querySelector("div.truncate");
      expect(truncated).not.toBeNull();
      expect(truncated?.classList.contains("truncate")).toBe(true);
    });
  });

  it("persists resized column widths per project", async () => {
    saveProjectTerminals(
      projectPath,
      [{ id: "term-a", title: "T1" }],
      "term-a",
    );

    render(<ProcessesPanel projectPath={projectPath} />);

    act(() => {
      eventBus["handlers"]
        .get("terminal_processes")
        ?.forEach((h) =>
          h({
            event: "terminal_processes",
            project: projectPath,
            seq: 1,
            data: [{ id: "term-a", pid: 1, cpu_percent: 1, mem_bytes: 1, command: "x" }],
          }),
        );
    });

    await waitFor(() => expect(screen.getByText("x")).toBeInTheDocument());

    const commandTh = screen.getAllByRole("columnheader")[1];
    const handle = commandTh.querySelector('[aria-label="Resize column"]') as HTMLElement;
    expect(handle).not.toBeNull();

    // PID / CPU / Memory are fixed-width and must NOT expose a resize handle.
    const pidTh = screen.getAllByRole("columnheader")[2];
    const pidHandle = pidTh.querySelector('[aria-label="Resize column"]');
    expect(pidHandle).toBeNull();
    const cpuTh = screen.getAllByRole("columnheader")[3];
    expect(cpuTh.querySelector('[aria-label="Resize column"]')).toBeNull();
    const memTh = screen.getAllByRole("columnheader")[4];
    expect(memTh.querySelector('[aria-label="Resize column"]')).toBeNull();

    // Dragging the Command handle persists the new width, clamped to bounds.
    fireEvent.mouseDown(handle, { clientX: 300 });
    fireEvent.mouseMove(document, { clientX: 360 });
    fireEvent.mouseUp(document, { clientX: 360 });

    await waitFor(() => {
      const raw = window.localStorage.getItem(`ocode.ui.processes.colwidths.v1.${projectPath}`);
      expect(raw).not.toBeNull();
      const parsed = JSON.parse(raw as string);
      // Default command width is 360; a +60 drag must be persisted and clamped
      // within bounds.
      expect(parsed.command).toBeGreaterThan(360);
      expect(parsed.command).toBeLessThanOrEqual(600);
      // Fixed columns keep their default widths.
      expect(parsed.pid).toBe(70);
      expect(parsed.cpu).toBe(70);
      expect(parsed.mem).toBe(90);
    });
  });
});
