import { render, screen, act } from "@testing-library/react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { TerminalProvider, useTerminalState, getProjectTerminals, terminalDisplayTitle, PROCESSES_TAB_ID } from "./terminalStore";

/** Fake terminal-tab server. The tab LIST is server state now, so anything that
 *  changes it from "another window" has to arrive through here rather than a
 *  localStorage `storage` event (which never crossed browser profiles anyway). */
const serverProjects = new Map<string, { terminals: { id: string; title: string }[] }>();
const busHandlers = new Map<string, Set<(env: unknown) => void>>();

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ...actual,
    remoteApiBase: () => "",
    authedFetch: () => Promise.resolve({ ok: true, status: 200 }),
    api: {
      ...actual.api,
      getTerminalTabs: () =>
        Promise.resolve({
          projects: Object.fromEntries([...serverProjects].map(([k, v]) => [k, v])),
        }),
      setTerminalTabs: (projects: Record<string, { terminals: { id: string; title: string }[] }>) => {
        for (const [k, v] of Object.entries(projects)) {
          if (v.terminals.length === 0) serverProjects.delete(k);
          else serverProjects.set(k, v);
        }
        return Promise.resolve({ status: "ok" });
      },
    },
  };
});

vi.mock("../lib/eventBus", () => ({
  eventBus: {
    on: (event: string, handler: (env: unknown) => void) => {
      let set = busHandlers.get(event);
      if (!set) {
        set = new Set();
        busHandlers.set(event, set);
      }
      set.add(handler);
      return () => set!.delete(handler);
    },
    onReconnect: () => () => {},
  },
}));

/** The server publishes this after every PUT; the store refetches on it. */
async function publishTerminalTabsChanged() {
  for (const handler of busHandlers.get("terminal_tabs_changed") ?? []) {
    handler({ event: "terminal_tabs_changed", seq: 1, data: null });
  }
}

function Harness({ projectPath }: { projectPath: string }) {
  const { state, activate, openTerminal, closeTerminal, setActiveId, renameTerminal, setOscTitle, markAlerted, clearAlert, attachTerminal } =
    useTerminalState();
  const { terminals, activeId, live } = getProjectTerminals(state, projectPath);
  return (
    <div>
      <div data-testid="live">{String(live)}</div>
      <div data-testid="active-id">{activeId}</div>
      <div data-testid="count">{terminals.length}</div>
      <div data-testid="titles">{terminals.map((t) => t.title).join(",")}</div>
      <div data-testid="display-titles">{terminals.map(terminalDisplayTitle).join(",")}</div>
      <div data-testid="alerted">
        {terminals.map((t) => (t.alerted ? t.id : "")).filter(Boolean).join(",")}
      </div>
      <div data-testid="raw-alert-count">
        {Object.values(state.byProject[projectPath]?.alerts ?? {}).filter(Boolean).length}
      </div>
      <button onClick={() => activate(projectPath)}>activate</button>
      <button onClick={() => openTerminal(projectPath)}>open</button>
      <button onClick={() => activeId && closeTerminal(projectPath, activeId)}>close-active</button>
      <button onClick={() => setActiveId(projectPath, PROCESSES_TAB_ID)}>focus-processes</button>
      <button onClick={() => attachTerminal(projectPath, undefined, "remote-1", "Remote shell")}>attach</button>
      {terminals.map((t) => (
        <span key={t.id}>
          <button onClick={() => markAlerted(projectPath, t.id)}>{`mark-${t.id}`}</button>
          <button onClick={() => clearAlert(projectPath, t.id)}>{`clear-${t.id}`}</button>
          <button onClick={() => renameTerminal(projectPath, t.id, "Mine")}>{`rename-${t.id}`}</button>
          <button onClick={() => setOscTitle(projectPath, t.id, "  ⦿ ocode —  fix\nbug  ")}>{`osc-${t.id}`}</button>
          <button onClick={() => setOscTitle(projectPath, t.id, "")}>{`osc-clear-${t.id}`}</button>
        </span>
      ))}
    </div>
  );
}

beforeEach(() => {
  window.localStorage.clear();
  serverProjects.clear();
  busHandlers.clear();
});

function seedPersisted(projectPath: string, terminals: { id: string; title: string }[], activeId: string) {
  window.localStorage.setItem(
    "ocode.ui.terminals.project.v1",
    JSON.stringify({ version: 1, projects: { [projectPath]: { terminals, activeId } } }),
  );
}

describe("terminalStore", () => {
  it("getProjectTerminals peeks persisted metadata without going live", () => {
    seedPersisted("/proj", [{ id: "term-1-1", title: "Terminal 1" }], "term-1-1");
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    expect(screen.getByTestId("live").textContent).toBe("false");
    expect(screen.getByTestId("count").textContent).toBe("1");
    expect(screen.getByTestId("active-id").textContent).toBe("term-1-1");
  });

  it("activate() with nothing persisted creates one fresh terminal and goes live", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("count").textContent).toBe("1");
  });

  it("activate() with persisted terminals restores them and goes live", () => {
    seedPersisted("/proj", [{ id: "term-9-1", title: "Terminal 9" }], "term-9-1");
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("titles").textContent).toBe("Terminal 9");
  });

  it("openTerminal() on a never-activated project seeds from persisted terminals then appends a new one", () => {
    seedPersisted("/proj", [{ id: "term-9-1", title: "Terminal 9" }], "term-9-1");
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("open").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("count").textContent).toBe("2");
  });

  it("closeTerminal() falls back the active id to the last remaining terminal", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    act(() => screen.getByText("open").click());
    expect(screen.getByTestId("count").textContent).toBe("2");
    act(() => screen.getByText("close-active").click());
    expect(screen.getByTestId("count").textContent).toBe("1");
  });

  it("setActiveId() to the Processes sentinel activates a never-activated project first", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("focus-processes").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("active-id").textContent).toBe(PROCESSES_TAB_ID);
  });

  it("markAlerted surfaces an alert on the terminal and clearAlert removes it", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    const id = screen.getByTestId("active-id").textContent!;
    expect(screen.getByTestId("alerted").textContent).toBe("");
    act(() => screen.getByText(`mark-${id}`).click());
    expect(screen.getByTestId("alerted").textContent).toBe(id);
    act(() => screen.getByText(`clear-${id}`).click());
    expect(screen.getByTestId("alerted").textContent).toBe("");
  });

  it("closing an alerted terminal drops its alert flag along with the terminal", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    const id = screen.getByTestId("active-id").textContent!;
    act(() => screen.getByText(`mark-${id}`).click());
    expect(screen.getByTestId("alerted").textContent).toBe(id);
    act(() => screen.getByText("close-active").click());
    expect(screen.getByTestId("alerted").textContent).toBe("");
  });

  it("prunes alerts for terminals another client removed", async () => {
    // Regression: SET_PROJECT_TERMINALS used to carry the whole `alerts` map
    // over to the new terminal list, so an alert for a terminal closed in
    // another window stayed truthy forever. The project sidebar counts every
    // truthy entry, and no tab exists to focus, so its attention bell could
    // never be cleared. Alerts must be scoped to the terminals that survive.
    seedPersisted(
      "/proj",
      [
        { id: "term-A", title: "A" },
        { id: "term-B", title: "B" },
      ],
      "term-A",
    );
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    act(() => screen.getByText("mark-term-A").click());
    act(() => screen.getByText("mark-term-B").click());
    expect(screen.getByTestId("raw-alert-count").textContent).toBe("2");
    // Another client closes term-B: the server holds the shrunken list and
    // announces it, and this window re-seeds from that.
    serverProjects.set("/proj", { terminals: [{ id: "term-A", title: "A" }] });
    await act(async () => {
      await publishTerminalTabsChanged();
    });
    expect(screen.getByTestId("count").textContent).toBe("1");
    expect(screen.getByTestId("raw-alert-count").textContent).toBe("1");
  });

  it("alert state is ephemeral and is never persisted to disk", async () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    const id = screen.getByTestId("active-id").textContent!;
    act(() => screen.getByText(`mark-${id}`).click());
    expect(screen.getByTestId("alerted").textContent).toBe(id);
    // Let the debounced persistence (200ms) flush.
    await new Promise((r) => setTimeout(r, 300));
    const raw = window.localStorage.getItem("ocode.ui.terminals.project.v1");
    expect(raw).not.toBeNull();
    expect(raw).not.toContain("alerted");
  });

  describe("OSC titles", () => {
    function mountLive(id = "term-1-1") {
      seedPersisted("/proj", [{ id, title: "Terminal 1" }], id);
      const { unmount } = render(
        <TerminalProvider>
          <Harness projectPath="/proj" />
        </TerminalProvider>,
      );
      act(() => screen.getByText("activate").click());
      return { id, unmount };
    }

    it("shows the program-set title, normalised, until cleared", () => {
      const { id } = mountLive();
      expect(screen.getByTestId("display-titles").textContent).toBe("Terminal 1");
      act(() => screen.getByText(`osc-${id}`).click());
      expect(screen.getByTestId("display-titles").textContent).toBe("⦿ ocode — fix bug");
      expect(screen.getByTestId("titles").textContent).toBe("Terminal 1");
      act(() => screen.getByText(`osc-clear-${id}`).click());
      expect(screen.getByTestId("display-titles").textContent).toBe("Terminal 1");
    });

    it("a manual rename beats the OSC title", () => {
      const { id } = mountLive();
      act(() => screen.getByText(`rename-${id}`).click());
      act(() => screen.getByText(`osc-${id}`).click());
      expect(screen.getByTestId("display-titles").textContent).toBe("Mine");
    });

    it("persists the OSC title so a reload keeps it", () => {
      const { id, unmount } = mountLive();
      act(() => screen.getByText(`osc-${id}`).click());
      unmount(); // flushes the debounced save synchronously
      const saved = JSON.parse(window.localStorage.getItem("ocode.ui.terminals.project.v1")!);
      expect(saved.projects["/proj"].terminals[0].oscTitle).toBe("⦿ ocode — fix bug");
    });
  });
});

describe("attachTerminal", () => {
  it("adds a tab with the given id once and ignores a second call", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );

    act(() => screen.getByText("attach").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("count").textContent).toBe("1");
    expect(screen.getByTestId("titles").textContent).toBe("Remote shell");
    expect(screen.getByTestId("active-id").textContent).toBe("remote-1");

    act(() => screen.getByText("attach").click());
    expect(screen.getByTestId("count").textContent).toBe("1");
  });
});
