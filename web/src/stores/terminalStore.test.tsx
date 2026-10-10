import { render, screen, act, cleanup } from "@testing-library/react";
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

  it("activate() with nothing persisted goes live with ZERO terminals and spawns none", () => {
    // Behavior change (2026-09-30): this used to mint one fresh terminal, which
    // made "user closed them all" indistinguishable from "never had one" — both
    // persist as an absent key — so a reload that restored the terminal view
    // resurrected a shell the user had closed. The dedicated regression test
    // for that reload boundary lives in the "no minimum-one terminal" describe
    // below; this one pins the branch itself.
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("activate").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("count").textContent).toBe("0");
    expect(screen.getByTestId("active-id").textContent).toBe("");
  });

  it("openTerminal() is what actually creates a terminal on a never-activated project", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("open").click());
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("count").textContent).toBe("1");
    expect(screen.getByTestId("active-id").textContent).not.toBe("");
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
    // open() twice: activate() no longer seeds a terminal for us.
    act(() => screen.getByText("open").click());
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
    act(() => screen.getByText("open").click());
    const id = screen.getByTestId("active-id").textContent!;
    expect(screen.getByTestId("alerted").textContent).toBe("");
    act(() => screen.getByText(`mark-${id}`).click());
    expect(screen.getByTestId("alerted").textContent).toBe(id);
    act(() => screen.getByText(`clear-${id}`).click());
    expect(screen.getByTestId("alerted").textContent).toBe("");
  });

  // closeTerminal reports the POST-close remaining count, read from the store
  // after the synchronous removal. That is what lets shouldLeaveTerminalView
  // decide "did this close empty the project?" without a caller-supplied
  // pre-close snapshot, which could be one commit stale when a cross-client
  // `terminal_tabs_changed` refetch lands between a render and a click.
  describe("closeTerminal reports the post-close remaining count", () => {
    it("returns 1 when a terminal remains, and 0 when the close empties the project", () => {
      // Asserting the return value needs a probe, so a second component reads
      // the store and records what closeTerminal reported.
      const seen: (number | null)[] = [];
      function Probe() {
        const { state, closeTerminal } = useTerminalState();
        const { terminals, activeId } = getProjectTerminals(state, "/proj");
        return (
          <div>
            <div data-testid="probe-count">{terminals.length}</div>
            <button
              onClick={() => {
                if (activeId) seen.push(closeTerminal("/proj", activeId));
              }}
            >
              close-and-report
            </button>
            <button onClick={() => seen.push(closeTerminal("/proj", "term-does-not-exist"))}>
              close-missing
            </button>
          </div>
        );
      }
      render(
        <TerminalProvider>
          <Harness projectPath="/proj" />
          <Probe />
        </TerminalProvider>,
      );
      act(() => screen.getByText("open").click());
      act(() => screen.getByText("open").click());
      // The Probe's own count — `count` is the shared Harness's testid.
      expect(screen.getByTestId("probe-count").textContent).toBe("2");

      // Two open: the close leaves one, so the report must be 1 — NOT 2, which
      // is what a pre-close read would have returned.
      act(() => screen.getByText("close-and-report").click());
      expect(seen).toEqual([1]);

      // Last one: 0, meaning the project is now empty.
      act(() => screen.getByText("close-and-report").click());
      expect(seen).toEqual([1, 0]);
      expect(screen.getByTestId("probe-count").textContent).toBe("0");

      // Nothing removed: null, distinct from a count of 0. A caller that
      // treated these the same would hand the user away from a view that still
      // has a live terminal in it.
      act(() => screen.getByText("close-missing").click());
      expect(seen).toEqual([1, 0, null]);
    });

    it("returns null on a repeated close instead of removing a neighbour", () => {
      // The guard the old boolean contract carried: two synchronous closes in
      // one tick must not take out two terminals.
      const seen: (number | null)[] = [];
      function DoubleClose() {
        const { state, closeTerminal } = useTerminalState();
        const { activeId } = getProjectTerminals(state, "/proj");
        return (
          <button
            onClick={() => {
              if (!activeId) return;
              seen.push(closeTerminal("/proj", activeId));
              seen.push(closeTerminal("/proj", activeId));
            }}
          >
            close-twice
          </button>
        );
      }
      render(
        <TerminalProvider>
          <Harness projectPath="/proj" />
          <DoubleClose />
        </TerminalProvider>,
      );
      act(() => screen.getByText("open").click());
      act(() => screen.getByText("open").click());
      act(() => screen.getByText("close-twice").click());
      // First close reports 1 left; the repeat must not touch the neighbour.
      expect(seen).toEqual([1, null]);
      expect(screen.getByTestId("count").textContent).toBe("1");
    });
  });

  it("closing an alerted terminal drops its alert flag along with the terminal", () => {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    act(() => screen.getByText("open").click());
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
    act(() => screen.getByText("open").click());
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

// "No minimum-one terminal" regression.
//
// Reported as: the desktop app enforces that at least one terminal tab is
// always open. The cause was NOT a guard but a collapsed persisted state:
// closing the last terminal DELETES the project from both persistence layers
// (saveProjectTerminals deletes the mirror key on an empty list; the server
// treats an empty PUT as a delete), so "the user closed them all" and "this
// project never had one" were byte-identical. App.tsx persists focusedKind
// per project, so a restart that restored the terminal view re-ran activate(),
// found nothing persisted, and minted a fresh shell — a terminal the user had
// deliberately closed.
//
// The in-session case already worked (a live-but-empty entry short-circuits
// activate's guard), so a test that only closes a terminal would pass both
// before and after the fix. This one crosses the RELOAD boundary, which is
// the only place the bug lived.
describe("no minimum-one terminal", () => {
  /** Long enough for both debounced writes to land: the 200ms mirror save and
   *  the 400ms server push. Asserting before these flush would read the
   *  pre-close state and let the test pass for the wrong reason. */
  async function settlePersistence() {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 600));
    });
  }

  async function mountAndSettle() {
    render(
      <TerminalProvider>
        <Harness projectPath="/proj" />
      </TerminalProvider>,
    );
    // Let the one-time server restore settle before driving the store.
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
  }

  it("does not resurrect a terminal on reload after the user closed every one", async () => {
    // ── Session 1: open one terminal, then close it. ───────────────────────
    await mountAndSettle();
    await act(async () => {
      screen.getByText("open").click();
    });
    expect(screen.getByTestId("count").textContent).toBe("1");
    await act(async () => {
      screen.getByText("close-active").click();
    });
    expect(screen.getByTestId("count").textContent).toBe("0");
    await settlePersistence();

    // Both layers now hold nothing for this project — identical to a project
    // that never had a terminal. Pinned so a future change that starts
    // recording the empty state explicitly (a "visited" marker) is a visible,
    // intentional difference rather than a silent one.
    expect(window.localStorage.getItem("ocode.ui.terminals.project.v1")).not.toContain("term-");
    expect(serverProjects.has("/proj")).toBe(false);

    // ── Session 2: a reload. cleanup() tears down the provider, so the fresh
    //    mount builds a brand-new (empty) store against the SAME persisted
    //    state — exactly what an app restart or webview reload does. App then
    //    restores focusedKind="terminal", so TerminalTabs mounts active and
    //    calls activate() again. ─────────────────────────────────────────────
    cleanup();
    await mountAndSettle();
    expect(screen.getByTestId("live").textContent).toBe("false");
    expect(screen.getByTestId("count").textContent).toBe("0");

    await act(async () => {
      screen.getByText("activate").click();
    });

    // THE assertion. Pre-fix this was "1": activate() minted a replacement.
    expect(screen.getByTestId("count").textContent).toBe("0");
    expect(screen.getByTestId("active-id").textContent).toBe("");
  });

  it("does not seed a terminal for a project that never had one", async () => {
    // The same collapsed state, reached without any close at all — so the
    // first-ever visit to a project's terminal view shows the empty panel
    // rather than opening a shell the user did not ask for.
    await mountAndSettle();
    expect(screen.getByTestId("count").textContent).toBe("0");

    await act(async () => {
      screen.getByText("activate").click();
    });
    expect(screen.getByTestId("live").textContent).toBe("true");
    expect(screen.getByTestId("count").textContent).toBe("0");
  });

  it("an explicit open still creates one after an empty activation", async () => {
    // Guards the other direction: B must not have broken the actual way a
    // user gets a shell (the ⌨️+ button / Cmd+T).
    await mountAndSettle();
    await act(async () => {
      screen.getByText("activate").click();
    });
    expect(screen.getByTestId("count").textContent).toBe("0");

    await act(async () => {
      screen.getByText("open").click();
    });
    expect(screen.getByTestId("count").textContent).toBe("1");
    expect(screen.getByTestId("active-id").textContent).not.toBe("");
  });
});
