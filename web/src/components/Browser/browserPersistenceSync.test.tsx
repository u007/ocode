import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import type { ServerProjectBrowsers } from "../../api/types";
import { BrowserTabsProvider, useBrowserTabs } from "../../stores/browserTabsStore";
import { __setRevoker, browserActions, browserStore } from "../../lib/browserStore";
import { useBrowserPersistence } from "./browserPersistence";

/** In-memory stand-in for GET/PUT /api/browser-tabs, with the server's rules: a
 *  write carries the revision it was read at, a stale one is refused with 409
 *  and nothing is written, and an accepted write gets a new revision. `puts`
 *  records every write this client sends, so an echo sent back again shows up as
 *  an extra entry. `failNextPut` makes the next PUT fail as a network error. */
const fake = {
  projects: {} as Record<string, ServerProjectBrowsers>,
  puts: [] as Record<string, ServerProjectBrowsers>[],
  failNextPut: false,
};

const busHandlers = new Map<string, Set<() => void>>();

vi.mock("../../api/client", async () => {
  const actual = await vi.importActual<typeof import("../../api/client")>("../../api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getBrowserTabs: () => Promise.resolve({ projects: structuredClone(fake.projects) }),
      setBrowserTabs: (projects: Record<string, ServerProjectBrowsers>) => {
        fake.puts.push(structuredClone(projects));
        if (fake.failNextPut) {
          fake.failNextPut = false;
          return Promise.reject(new Error("network down"));
        }
        for (const [k, v] of Object.entries(projects)) {
          const stored = fake.projects[k];
          const current = stored ? stored.revision : 0;
          if (v.revision !== current) {
            return Promise.reject(
              new actual.ApiError(`stale revision for ${k}`, 409, {
                error: `stale revision for ${k}`,
                projects: structuredClone(fake.projects),
              }),
            );
          }
        }
        for (const [k, v] of Object.entries(projects)) {
          const stored = fake.projects[k];
          const current = stored ? stored.revision : 0;
          if (v.tabs.length === 0) delete fake.projects[k];
          else fake.projects[k] = { ...structuredClone(v), revision: current + 1 };
        }
        return Promise.resolve({ status: "ok" });
      },
    },
  };
});

vi.mock("../../lib/eventBus", () => ({
  eventBus: {
    on: (event: string, handler: () => void) => {
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

const revoke = vi.fn(async (_key: string, _host: string): Promise<void> => {});

function fireBusEvent(event: string) {
  for (const handler of busHandlers.get(event) ?? []) handler();
}

function wrap({ children }: { children: ReactNode }) {
  return <BrowserTabsProvider>{children}</BrowserTabsProvider>;
}

const PROJ = "/proj/sync";

type ServerState = Omit<ServerProjectBrowsers, "revision">;

function serverTab(id: string, url: string, history: string[], index: number): ServerState {
  return {
    tabs: [{ id, title: "Shared" }],
    active_id: id,
    surfaces: { [id]: { url, history, history_index: index } },
  };
}

/** Another client's write: the server stores it under the next revision. */
function serverWrites(state: ServerState): void {
  const stored = fake.projects[PROJ];
  const current = stored ? stored.revision : 0;
  fake.projects[PROJ] = { ...structuredClone(state), revision: current + 1 };
}

async function settle() {
  await advance(1000);
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

async function microtasks() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function renderProject() {
  return renderHook(
    () => ({ browser: useBrowserTabs(PROJ), persist: useBrowserPersistence(PROJ) }),
    { wrapper: wrap },
  );
}

beforeEach(() => {
  localStorage.clear();
  fake.projects = {};
  fake.puts = [];
  fake.failNextPut = false;
  busHandlers.clear();
  browserStore.setState(() => ({ byKey: {} }));
  revoke.mockClear();
  __setRevoker(revoke);
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("useBrowserPersistence sync with the server", () => {
  it("applies the server's state on restore and sends nothing back", async () => {
    serverWrites(serverTab("b1", "https://a.example/", ["https://a.example/"], 0));
    const { result } = renderProject();
    await settle();

    expect(result.current.browser.tabs.map((t) => t.id)).toEqual(["b1"]);
    expect(browserStore.state.byKey["tab:b1"]?.url).toBe("https://a.example/");
    expect(fake.puts).toEqual([]);
  });

  it("sends a local navigation once, and does not send its echo again", async () => {
    serverWrites(serverTab("b1", "https://a.example/", ["https://a.example/"], 0));
    renderProject();
    await settle();

    act(() => browserActions.navigate("tab:b1", "https://b.example/"));
    await settle();
    expect(fake.puts).toHaveLength(1);
    expect(fake.projects[PROJ].surfaces.b1.url).toBe("https://b.example/");
    expect(fake.projects[PROJ].surfaces.b1.history).toEqual(["https://a.example/", "https://b.example/"]);

    // The server announces the write back to this client.
    act(() => fireBusEvent("browser_tabs_changed"));
    await settle();
    expect(fake.puts).toHaveLength(1);
    expect(browserStore.state.byKey["tab:b1"]?.history).toEqual(["https://a.example/", "https://b.example/"]);
  });

  it("applies another client's navigation without growing history or sending it back", async () => {
    serverWrites(serverTab("b1", "https://a.example/", ["https://a.example/"], 0));
    renderProject();
    await settle();

    // The other client navigates the same tab.
    serverWrites(serverTab("b1", "https://c.example/", ["https://a.example/", "https://c.example/"], 1));
    act(() => fireBusEvent("browser_tabs_changed"));
    await settle();

    const tab = browserStore.state.byKey["tab:b1"];
    expect(tab?.url).toBe("https://c.example/");
    expect(tab?.history).toEqual(["https://a.example/", "https://c.example/"]);
    expect(tab?.historyIndex).toBe(1);
    expect(fake.puts).toEqual([]);
  });

  it("does not overwrite a local navigation that has not been sent yet", async () => {
    serverWrites(serverTab("b1", "https://a.example/", ["https://a.example/"], 0));
    renderProject();
    await settle();

    act(() => browserActions.navigate("tab:b1", "https://b.example/"));
    // The other client's write lands before this client's debounce fires.
    serverWrites(serverTab("b1", "https://c.example/", ["https://c.example/"], 0));
    act(() => fireBusEvent("browser_tabs_changed"));
    await microtasks();
    expect(browserStore.state.byKey["tab:b1"]?.url).toBe("https://b.example/");

    // The local edit goes out with the revision the read just recorded, so it is
    // accepted rather than refused.
    await settle();
    expect(fake.projects[PROJ].surfaces.b1.url).toBe("https://b.example/");
  });

  it("closes here a tab that another client closed, without revoking its grant", async () => {
    serverWrites({
      tabs: [
        { id: "b1", title: "One" },
        { id: "b2", title: "Two" },
      ],
      active_id: "b1",
      surfaces: {
        b1: { url: "https://a.example/", history: ["https://a.example/"], history_index: 0 },
        b2: { url: "https://b.example/", history: ["https://b.example/"], history_index: 0 },
      },
    });
    const { result } = renderProject();
    await settle();
    expect(result.current.browser.tabs.map((t) => t.id)).toEqual(["b1", "b2"]);

    serverWrites({
      tabs: [{ id: "b1", title: "One" }],
      active_id: "b1",
      surfaces: { b1: { url: "https://a.example/", history: ["https://a.example/"], history_index: 0 } },
    });
    act(() => fireBusEvent("browser_tabs_changed"));
    await settle();

    expect(result.current.browser.tabs.map((t) => t.id)).toEqual(["b1"]);
    expect(browserStore.state.byKey["tab:b2"]).toBeUndefined();
    expect(fake.puts).toEqual([]);
    expect(revoke).not.toHaveBeenCalled();
  });

  it("revokes the grant when this client closes a tab itself", async () => {
    serverWrites(serverTab("b1", "https://a.example/", ["https://a.example/"], 0));
    renderProject();
    await settle();

    act(() => browserActions.close("tab:b1", ""));
    expect(revoke).toHaveBeenCalledWith("tab:b1", "");
  });

  it("closing the last tab sends an explicit empty strip, which deletes the project", async () => {
    serverWrites(serverTab("b1", "https://a.example/", ["https://a.example/"], 0));
    const { result } = renderProject();
    await settle();

    act(() => result.current.browser.closeBrowserTab("b1"));
    await settle();

    expect(fake.puts[fake.puts.length - 1][PROJ].tabs).toEqual([]);
    expect(fake.projects[PROJ]).toBeUndefined();
  });

  it("keeps scroll offsets on this viewer and does not send them", async () => {
    serverWrites(serverTab("b1", "https://a.example/", ["https://a.example/"], 0));
    renderProject();
    await settle();

    act(() => browserActions.setScrollY("tab:b1", 240, "https://a.example/"));
    await settle();

    expect(fake.puts).toEqual([]);
    expect(JSON.stringify(fake.projects)).not.toContain("240");
    expect(JSON.parse(localStorage.getItem("ocode.ui.browser.scroll.v1") ?? "{}").tabs.b1).toEqual({
      "https://a.example/": 240,
    });
  });

  it("a nav event that reports the URL already applied sends nothing", async () => {
    serverWrites(serverTab("b1", "https://a.example/", ["https://a.example/"], 0));
    renderProject();
    await settle();

    act(() =>
      browserActions.applyNavEvent("tab:b1", {
        state_key: "tab:b1",
        url: "https://a.example/",
        status: 200,
        mode: "local",
      }),
    );
    await settle();
    expect(fake.puts).toEqual([]);
  });

  it("a save the server refuses with 409 is dropped, and the server's state is applied", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    serverWrites(serverTab("b1", "https://a.example/", ["https://a.example/"], 0));
    renderProject();
    await settle();

    // Another client writes, and this client has not read it yet.
    serverWrites(serverTab("b1", "https://c.example/", ["https://c.example/"], 0));
    act(() => browserActions.navigate("tab:b1", "https://b.example/"));
    await settle();

    expect(fake.puts).toHaveLength(1);
    expect(fake.projects[PROJ].surfaces.b1.url).toBe("https://c.example/");
    expect(browserStore.state.byKey["tab:b1"]?.url).toBe("https://c.example/");
    expect(warn).toHaveBeenCalledTimes(1);

    // The adopted state is what this client holds, so it sends nothing back.
    await settle();
    expect(fake.puts).toHaveLength(1);
  });

  it("a failed save is retried after 5 s, and a later read from another client still applies", async () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    serverWrites(serverTab("b1", "https://a.example/", ["https://a.example/"], 0));
    renderProject();
    await settle();

    fake.failNextPut = true;
    act(() => browserActions.navigate("tab:b1", "https://b.example/"));
    await advance(750);
    expect(fake.puts).toHaveLength(1);
    expect(error).toHaveBeenCalledTimes(1);
    expect(fake.projects[PROJ].surfaces.b1.url).toBe("https://a.example/");

    await advance(5000);
    expect(fake.puts).toHaveLength(2);
    expect(fake.projects[PROJ].surfaces.b1.url).toBe("https://b.example/");

    // After the retry lands, this client still applies another client's read.
    serverWrites(serverTab("b1", "https://c.example/", ["https://c.example/"], 0));
    act(() => fireBusEvent("browser_tabs_changed"));
    await settle();
    expect(browserStore.state.byKey["tab:b1"]?.url).toBe("https://c.example/");
    expect(fake.puts).toHaveLength(2);
  });

  it("a project another client deleted closes its synced tabs here, without revoking", async () => {
    serverWrites({
      tabs: [
        { id: "b1", title: "One" },
        { id: "b2", title: "Two" },
      ],
      active_id: "b1",
      surfaces: {
        b1: { url: "https://a.example/", history: ["https://a.example/"], history_index: 0 },
        b2: { url: "https://b.example/", history: ["https://b.example/"], history_index: 0 },
      },
    });
    const { result } = renderProject();
    await settle();

    delete fake.projects[PROJ];
    act(() => fireBusEvent("browser_tabs_changed"));
    await settle();

    expect(result.current.browser.tabs).toEqual([]);
    expect(browserStore.state.byKey["tab:b1"]).toBeUndefined();
    expect(browserStore.state.byKey["tab:b2"]).toBeUndefined();
    expect(revoke).not.toHaveBeenCalled();
    expect(fake.puts).toEqual([]);
  });

  // KNOWN OPEN, see TODO.md ("shared browser tabs"). When the same URL redirects
  // differently in two clients (cookies live in each client's own browser), each
  // round trip re-sends one PUT: this client's nav event reports the redirect target
  // and shares it, the other client shares its own redirect back, and this client
  // applies that and shares the target again. This test pins the cycle as it
  // stands. Flip the expectation when the product decision on sharing
  // user-initiated navigations (or suppressing re-share after a remote apply)
  // lands.
  it("KNOWN OPEN: a redirect that differs per client sends one PUT per round trip", async () => {
    const X = "https://a.example/";
    const Y = "https://a.example/login";
    serverWrites(serverTab("b1", X, [X], 0));
    renderProject();
    await settle();

    for (let round = 1; round <= 3; round++) {
      // The other client's copy is back on X.
      serverWrites(serverTab("b1", X, [X], 0));
      act(() => fireBusEvent("browser_tabs_changed"));
      await settle();
      // This client's proxy redirects X to Y.
      act(() =>
        browserActions.applyNavEvent("tab:b1", { state_key: "tab:b1", url: Y, status: 200, mode: "local" }),
      );
      await settle();
      expect(fake.puts).toHaveLength(round);
      expect(fake.projects[PROJ].surfaces.b1.url).toBe(Y);
    }
  });
});
