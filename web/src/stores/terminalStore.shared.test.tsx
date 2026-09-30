import { render, screen, act, waitFor } from "@testing-library/react";
import { it, expect, beforeEach, vi } from "vitest";
import { TerminalProvider, useTerminalState, getProjectTerminals } from "./terminalStore";

/** Handlers registered against the (mocked) bus, so a test can deliver a
 *  server event without a live SSE stream. Mirrors the pattern the other
 *  store suites use. */
const busHandlers = new Map<string, Set<(env: unknown) => void>>();

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

/**
 * A fake terminal-tab server: a plain in-memory map plus the merge semantics the
 * real endpoint has (a provided key replaces it, an EMPTY list deletes it, an
 * absent key is preserved). Two providers in one jsdom window stand in for two
 * clients — they share localStorage, which is exactly why this test has to go
 * through the server: the old localStorage-only store made the two clients
 * indistinguishable and both "passed" while the real second browser saw nothing.
 */
const serverProjects = new Map<string, { terminals: { id: string; title: string; renamed?: boolean; osc_title?: string }[] }>();
let putCount = 0;
let getCalls = 0;

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  // The store calls `api.getTerminalTabs()`, so the methods must be patched ON
  // the exported `api` object — overriding top-level named exports would leave
  // the real ones in place and the test would silently exercise the network
  // path instead of the fake server.
  return {
    ...actual,
    remoteApiBase: () => "",
    authedFetch: () => Promise.resolve({ ok: true, status: 200 }),
    api: {
      ...actual.api,
      getTerminalTabs: () => {
        getCalls += 1;
        const projects: Record<string, { terminals: unknown[] }> = {};
        for (const [k, v] of serverProjects) projects[k] = v;
        return Promise.resolve({ projects });
      },
      setTerminalTabs: (projects: Record<string, { terminals: { id: string; title: string }[] }>) => {
        putCount += 1;
        for (const [k, v] of Object.entries(projects)) {
          if (v.terminals.length === 0) serverProjects.delete(k);
          else serverProjects.set(k, v);
        }
        return Promise.resolve({ status: "ok" });
      },
    },
  };
});

function Client({ id, projectPath, host }: { id: string; projectPath: string; host?: string }) {
  const { state, activate, openTerminal } = useTerminalState();
  const { terminals } = getProjectTerminals(state, projectPath, host);
  return (
    <div>
      <div data-testid={`${id}-count`}>{terminals.length}</div>
      <div data-testid={`${id}-ids`}>{terminals.map((t) => t.id).join(",")}</div>
      <button onClick={() => activate(projectPath, host)}>{`${id}-activate`}</button>
      <button onClick={() => openTerminal(projectPath, host)}>{`${id}-open`}</button>
    </div>
  );
}

function renderTwoClients(projectPath: string) {
  return render(
    <>
      <TerminalProvider>
        <Client id="a" projectPath={projectPath} />
      </TerminalProvider>
      <TerminalProvider>
        <Client id="b" projectPath={projectPath} />
      </TerminalProvider>
    </>,
  );
}

/** The server publishes this after every PUT; clients refetch on it. */
function publishChanged() {
  for (const handler of busHandlers.get("terminal_tabs_changed") ?? []) {
    handler({ event: "terminal_tabs_changed", seq: 1, data: null });
  }
}

beforeEach(() => {
  window.localStorage.clear();
  serverProjects.clear();
  busHandlers.clear();
  putCount = 0;
  getCalls = 0;
});

// THE REPORTED BUG. A terminal started in one client (the desktop app) must
// appear in the other (a second browser) once the server announces the write.
// Before the shared store, client B kept its own localStorage copy and stayed
// empty forever.
it("shows a terminal opened by another client once the server announces it", async () => {
  renderTwoClients("/srv/app");

  await waitFor(() => expect(putCount).toBeGreaterThanOrEqual(0));

  await act(async () => {
    screen.getByText("a-activate").click();
  });
  await act(async () => {
    screen.getByText("a-open").click();
  });

  // Client A's terminal reached the server.
  await waitFor(() => {
    expect(serverProjects.get("/srv/app")?.terminals.length).toBe(2);
  });

  // Client B is told to refetch.
  publishChanged();

  await waitFor(() => {
    expect(screen.getByTestId("b-count").textContent).toBe("2");
  });
  expect(screen.getByTestId("b-ids").textContent).toBe(screen.getByTestId("a-ids").textContent);
});

// A client that restores after another client already opened a terminal must
// adopt the SERVER's list, not manufacture its own fresh `term-1` — that ghost
// tab would also spawn a second shell.
it("adopts the server list on restore instead of minting a fresh terminal", async () => {
  serverProjects.set("/srv/app", { terminals: [{ id: "from-desktop", title: "Desktop shell" }] });

  render(
    <TerminalProvider>
      <Client id="b" projectPath="/srv/app" />
    </TerminalProvider>,
  );

  await waitFor(() => {
    expect(screen.getByTestId("b-ids").textContent).toBe("from-desktop");
  });
  expect(screen.getByTestId("b-count").textContent).toBe("1");
  // No phantom write: the restored list was never re-minted under a new id.
  expect(serverProjects.get("/srv/app")?.terminals.map((t) => t.id)).toEqual(["from-desktop"]);
});

// Closing the last terminal in one client must remove the tab for everyone,
// not just locally.
it("removes a terminal closed by another client", async () => {
  serverProjects.set("/srv/app", { terminals: [{ id: "t1", title: "One" }, { id: "t2", title: "Two" }] });

  render(
    <TerminalProvider>
      <Client id="b" projectPath="/srv/app" />
    </TerminalProvider>,
  );
  await waitFor(() => expect(screen.getByTestId("b-count").textContent).toBe("2"));

  serverProjects.set("/srv/app", { terminals: [{ id: "t1", title: "One" }] });
  publishChanged();

  await waitFor(() => {
    expect(screen.getByTestId("b-ids").textContent).toBe("t1");
  });
});

// The "+ terminal" button is not blocked while the async restore is in flight, so
// a terminal can be minted BEFORE the store has read the server. Nothing about
// that mint re-triggers the debounced write effect (the server never mentioned
// the project, so nothing was adopted), so without an explicit flush after the
// restore settles the terminal would sit in the local mirror until the next
// reload — and no other client would ever see it.
it("sends a terminal opened before hydration finished once the restore settles", async () => {
  render(
    <TerminalProvider>
      <Client id="a" projectPath="/srv/app" />
    </TerminalProvider>,
  );
  // Open IMMEDIATELY, before the restore GET can resolve.
  act(() => {
    screen.getByText("a-open").click();
  });
  await waitFor(() => {
    expect(serverProjects.get("/srv/app")?.terminals.length).toBe(1);
  });
});

// Adopting another client's list must not PUT that same list straight back: the
// state came FROM the server, so echoing it fires another terminal_tabs_changed
// on every client and adds a round-trip per change. It must converge without a
// single write-back.
it("does not echo an adopted server list back to the server", async () => {
  render(
    <TerminalProvider>
      <Client id="b" projectPath="/srv/app" />
    </TerminalProvider>,
  );
  // The store suppresses writes until its initial server read settles, so wait
  // for that GET before activating — otherwise there is nothing to observe.
  await waitFor(() => expect(getCalls).toBeGreaterThanOrEqual(1));
  await act(async () => {
    screen.getByText("b-activate").click();
  });
  await waitFor(() => expect(putCount).toBeGreaterThan(0));
  const putsAfterActivate = putCount;

  // Another client opens a terminal; the server announces it.
  serverProjects.set("/srv/app", {
    terminals: [
      { id: "term-1-1", title: "Terminal 1" },
      { id: "from-desktop", title: "Desktop shell" },
    ],
  });
  publishChanged();

  await waitFor(() => {
    expect(screen.getByTestId("b-ids").textContent).toContain("from-desktop");
  });
  // The mirror→server write is debounced, so wait past the debounce before
  // asserting. Without this the check runs before any PUT could have been
  // issued and passes with or without the echo guard (verified: the guard's
  // removal survives an assertion made here).
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 700));
  });
  expect(putCount).toBe(putsAfterActivate);
});

// A remote project's terminals are keyed `<host>::<path>`, so a local project at
// the same path must not absorb them (and vice versa) — otherwise opening a
// remote terminal would also appear as a tab in the local project.
it("keeps a remote project's terminals out of the same-path local project", async () => {
  serverProjects.set("dev@box::/srv/app", { terminals: [{ id: "remote-1", title: "Remote" }] });

  render(
    <TerminalProvider>
      <Client id="remote" projectPath="/srv/app" host="dev@box" />
      <Client id="local" projectPath="/srv/app" />
    </TerminalProvider>,
  );

  await waitFor(() => {
    expect(screen.getByTestId("remote-ids").textContent).toBe("remote-1");
  });
  expect(screen.getByTestId("local-ids").textContent).not.toContain("remote-1");
});
