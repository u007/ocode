import { render, screen, waitFor, act } from "@testing-library/react";
import { describe, expect, it, beforeEach, vi } from "vitest";
import ProcessesPanel from "./ProcessesPanel";
import { saveProjectTerminals } from "./terminalPersistence";
import { browserActions, browserStore } from "@/lib/browserStore";

// The real eventBus auto-starts a live EventSource to the server the moment
// ProcessesPanel's terminal_processes effect subscribes. jsdom has no server
// to connect to, so that open handle never resolves and vitest hangs at
// teardown. Stub it with the same private `handlers` Map<string, Set> shape
// the tests drive via the eventBus["handlers"] escape hatch. vi.hoisted runs
// the map factory before the hoisted vi.mock, so the mock can close over it.
const eventBusState = vi.hoisted(() => {
  const handlers = new Map<string, Set<(env: unknown) => void>>();
  return {
    handlers,
    on: (event: string, handler: (env: unknown) => void) => {
      const set = handlers.get(event) ?? new Set();
      set.add(handler);
      handlers.set(event, set);
      return () => void set.delete(handler);
    },
  };
});
vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    handlers: eventBusState.handlers,
    on: eventBusState.on,
  },
}));

// Fakes mutated per-test, read by the hoisted vi.mock factories below. They
// MUST live in vi.hoisted: factories run during collection, when a plain
// module-level `let` is still in the temporal dead zone — referencing it there
// throws and stalls module load silently. Tests reassign the fields in
// beforeEach/at test start.
const fakes = vi.hoisted(() => ({
  project: {
    state: {
      tabsByProject: {} as Record<string, { id: string; projectPath: string; title: string; activeSubTab: "chat" }[]>,
      activeTabByProject: {} as Record<string, string | null>,
    },
  },
  browserTabs: [] as { id: string; title: string }[],
}));

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({ state: fakes.project.state }),
}));

vi.mock("../../stores/browserTabsStore", () => ({
  useBrowserTabs: () => ({ tabs: fakes.browserTabs }),
}));

// Shape the estimator reads: messages (+ live). Full SessionSlice is not
// required by estimateSessionSliceBytes.

// chatSessionsFake is read by the hoisted vi.mock factory at chatStore import
// time (during collection), so it must live in vi.hoisted — a plain module
// `let` is still in the temporal dead zone when the hoisted factory first
// evaluates, crashing collection silently. Tests assign it in beforeEach.
const chatSessionsFakeState = vi.hoisted(() => ({
  sessions: {} as Record<
    string,
    {
      messages: { role: string; content: string; reasoning_content?: string; tool_calls?: { function: { arguments: string } }[] }[];
      live: { kind: string; text?: string; stream?: string; output?: string }[];
    }
  >,
}));

vi.mock("../../stores/chatStore", () => ({
  useChatStateRef: () => ({ current: { sessions: chatSessionsFakeState.sessions } }),
}));

describe("ProcessesPanel", () => {
  const projectPath = "/project";

  beforeEach(() => {
    window.localStorage.clear();
    fakes.project.state = {
      tabsByProject: {
        [projectPath]: [{ id: "sess-1", projectPath, title: "My session", activeSubTab: "chat" }],
      },
      activeTabByProject: { [projectPath]: "sess-1" },
    };
    fakes.browserTabs = [];
    chatSessionsFakeState.sessions = {};
    browserStore.setState((s) => ({ ...s, byKey: {} }));
  });

  it("shows nothing until a terminal_processes envelope arrives", () => {
    // No chat tabs and no terminals — the empty state should show.
    fakes.project.state = { tabsByProject: {}, activeTabByProject: {} };
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
      eventBusState.handlers
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
      eventBusState.handlers
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
      eventBusState.handlers
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
      const row = screen.getAllByRole("row").find((r) => r.textContent?.includes("Terminal 1"));
      expect(row).toBeDefined();
      const cells = row!.querySelectorAll("td");
      // Command is the second column (after Name).
      const commandCell = cells[1];
      expect(commandCell).toHaveTextContent("npm run dev");
      const truncated = commandCell.querySelector("div.truncate");
      expect(truncated).not.toBeNull();
      expect(truncated?.classList.contains("truncate")).toBe(true);
    });
  });

  it("renders an estimated Memory row for the active chat session", async () => {
    chatSessionsFakeState.sessions = {
      "sess-1": { messages: [{ role: "user", content: "x".repeat(5000) }], live: [] },
    };

    render(<ProcessesPanel projectPath={projectPath} />);

    await waitFor(() => {
      const row = screen.getByText("My session").closest("tr");
      expect(row).not.toBeNull();
      expect(row).toHaveTextContent("1 message");
      // estimateSessionSliceBytes(5000 chars) → formatBytes → "5 KB", shown
      // with the "estimated" style.
      expect(row).toHaveTextContent("~5 KB");
      // Group header for estimated frontend surfaces
      expect(screen.getByText(/Frontend surfaces \(estimated\)/)).toBeInTheDocument();
    });
  });

  it("renders a row for every open chat session, marking the active one", async () => {
    fakes.project.state = {
      tabsByProject: {
        [projectPath]: [
          { id: "sess-1", projectPath, title: "Sess 1", activeSubTab: "chat" },
          { id: "sess-2", projectPath, title: "Sess 2", activeSubTab: "chat" },
        ],
      },
      activeTabByProject: { [projectPath]: "sess-2" },
    };
    chatSessionsFakeState.sessions = {
      "sess-1": { messages: [{ role: "user", content: "x".repeat(9000) }], live: [] },
      "sess-2": { messages: [{ role: "user", content: "short" }], live: [] },
    };

    render(<ProcessesPanel projectPath={projectPath} />);

    await waitFor(() => {
      const row1 = screen.getByText("Sess 1").closest("tr");
      const row2 = screen.getByText("Sess 2").closest("tr");
      expect(row1).not.toBeNull();
      expect(row2).not.toBeNull();
      // Both sessions are shown; the active one is marked.
      expect(row1).toHaveTextContent("1 message");
      expect(row1).toHaveTextContent("~9 KB");
      expect(row1).not.toHaveTextContent("active");
      expect(row2).toHaveTextContent("1 message");
      expect(row2).toHaveTextContent("~0 KB"); // 5 chars → <1 KB rounds to 0
      expect(row2).toHaveTextContent("active");
    });
  });

  it("renders an estimated Memory row per open browser tab", async () => {
    fakes.browserTabs = [{ id: "b1", title: "Docs" }];
    // Seed the module-singleton browserStore surface for tab:b1 (what the
    // UnifiedTabBar does when a browser tab is opened).
    browserActions.open("tab:b1", "https://example.com/docs");
    for (let i = 0; i < 4; i++) {
      browserActions.pushConsole("tab:b1", { level: "log", text: "hello ".repeat(100), ts: i });
    }
    for (let i = 0; i < 2; i++) {
      browserActions.pushNetwork("tab:b1", { method: "GET", url: "https://example.com/api", status: 200, durationMs: 12, ts: i });
    }

    render(<ProcessesPanel projectPath={projectPath} />);

    await waitFor(() => {
      const row = screen.getByText("Docs").closest("tr");
      expect(row).not.toBeNull();
      expect(row).toHaveTextContent("https://example.com/docs");
      expect(row).toHaveTextContent("4 console · 2 network");
      expect(row).toHaveTextContent("~"); // estimated-style memory value
    });
  });

  it("drops closed terminals from the table when the server publishes an empty snapshot", async () => {
    saveProjectTerminals(projectPath, [{ id: "term-a", title: "Terminal 1" }], "term-a");
    render(<ProcessesPanel projectPath={projectPath} />);

    act(() => {
      eventBusState.handlers
        .get("terminal_processes")
        ?.forEach((h) =>
          h({
            event: "terminal_processes",
            project: projectPath,
            seq: 1,
            data: [{ id: "term-a", pid: 111, cpu_percent: 12.5, mem_bytes: 1024 * 1024 }],
          }),
        );
    });
    await waitFor(() => expect(screen.getByText("12.5%")).toBeInTheDocument());

    // The emitter publishes a transition-to-empty envelope for the project
    // after its last terminal is closed; the table must drop the stale row.
    act(() => {
      eventBusState.handlers
        .get("terminal_processes")
        ?.forEach((h) => h({ event: "terminal_processes", project: projectPath, seq: 2, data: [] }));
    });

    await waitFor(() => {
      expect(screen.queryByText("12.5%")).not.toBeInTheDocument();
      expect(screen.queryByText("Terminal 1")).not.toBeInTheDocument();
    });
  });
});