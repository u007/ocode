import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, act, waitFor } from "@testing-library/react";
import {
  PulseProvider,
  usePulse,
  pulseEventSink,
  sortPulseRows,
  __resetPulseSavedProjectsForTests,
} from "./pulseStore";
import { api } from "../api/client";
import type { PulseRow } from "../api/types";

vi.mock("../api/client", () => ({
  api: { getPulse: vi.fn(), listProjects: vi.fn() },
}));

// Fake the bus so the reconnect reseed can be driven deterministically (the
// real bus opens a live SSE stream, which jsdom has no server for).
const reconnectHandlers = new Set<() => void>();
const hostsChangeHandlers = new Set<() => void>();
/** The relayed host set the fake bus reports, and the fake `setHosts` mutator. */
const bus: { hosts: string[] } = { hosts: [] };
vi.mock("../lib/eventBus", () => ({
  eventBus: {
    onReconnect: (h: () => void) => {
      reconnectHandlers.add(h);
      return () => reconnectHandlers.delete(h);
    },
    onHostsChange: (h: () => void) => {
      hostsChangeHandlers.add(h);
      return () => hostsChangeHandlers.delete(h);
    },
    currentHosts: () => [...bus.hosts],
  },
}));

const getPulse = vi.mocked(api.getPulse);
const listProjects = vi.mocked(api.listProjects);

/** Fixture default: a fixed PAST timestamp. Anything a live event stamps
 *  (new Date().toISOString()) is then genuinely newer, so recency assertions
 *  test the sort rule instead of today's wall clock. */
const OLD = "2020-01-01T00:00:00Z";

function row(over: Partial<PulseRow> & { session_id: string }): PulseRow {
  return {
    project_path: "/p",
    title: `t ${over.session_id}`,
    status: "idle",
    current_task: null,
    todo: null,
    pending_ask: null,
    turn_started_at: "",
    updated_at: OLD,
    child_count: 0,
    host: "",
    ...over,
  };
}

function page(items: PulseRow[], next: string | null = null) {
  return { items, next_cursor: next };
}

let latest: ReturnType<typeof usePulse>;

function Probe() {
  latest = usePulse();
  return null;
}

function mount() {
  return render(
    <PulseProvider>
      <Probe />
    </PulseProvider>,
  );
}

/** Drive the module-level sink the SSE router calls. */
function emit(event: string, sessionId: string, data: unknown, host = "") {
  act(() => pulseEventSink(event, sessionId, data, host));
}

beforeEach(() => {
  getPulse.mockReset();
  listProjects.mockReset();
  __resetPulseSavedProjectsForTests();  listProjects.mockResolvedValue([]);
  getPulse.mockResolvedValue(page([]));
  bus.hosts = [];
  reconnectHandlers.clear();
  hostsChangeHandlers.clear();
  vi.useRealTimers();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("pulseStore seeding", () => {
  it("seeds from getPulse('live', null, 50) on mount", async () => {
    getPulse.mockResolvedValue(page([row({ session_id: "a" })]));
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));
    expect(getPulse).toHaveBeenCalledWith("live", null, 50, undefined);
    expect(latest.rows[0].session_id).toBe("a");
  });

  it("preserves the order the server sent on seed", async () => {
    // The client trusts the server's ordering for the initial page and only
    // re-sorts after it applies an event itself. Feeding a deliberately
    // mis-ordered page and expecting a fix here would pin behavior the spec
    // does not ask for; the rule itself is pinned by the sortPulseRows suite.
    getPulse.mockResolvedValue(
      page([
        row({ session_id: "a", status: "needs_permission" }),
        row({ session_id: "z", status: "running" }),
      ]),
    );
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    expect(latest.rows.map((r) => r.session_id)).toEqual(["a", "z"]);
  });

  it("reports running and needsYou counts for the header badge", async () => {
    getPulse.mockResolvedValue(
      page([
        row({ session_id: "a", status: "running" }),
        row({ session_id: "b", status: "needs_permission" }),
        row({ session_id: "c", status: "needs_question" }),
        row({ session_id: "d", status: "idle" }),
      ]),
    );
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(4));
    expect(latest.counts).toEqual({ running: 1, needsYou: 2 });
  });

  it("surfaces a fetch failure with context and leaves rows untouched", async () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    getPulse.mockResolvedValue(page([row({ session_id: "keep" })]));
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));

    getPulse.mockRejectedValue(new Error("boom"));
    await act(async () => {
      latest.retry();
    });
    await waitFor(() => expect(latest.error).toBeTruthy());
    // A stale list is better than an empty one, and the plan forbids
    // substituting a fabricated one.
    expect(latest.rows.map((r) => r.session_id)).toEqual(["keep"]);
    expect(spy).toHaveBeenCalled();
    const logged = spy.mock.calls.map((c) => c.join(" ")).join("\n");
    expect(logged).toContain("boom");
  });

  it("survives a malformed page body without crashing the tree", async () => {
    // Regression: an earlier version consumed `res.items` blindly, so a body
    // that was not a PulsePage threw inside a setState updater — a render-phase
    // throw that unmounted the ENTIRE app (every App.*.test.tsx suite went
    // blank) instead of degrading this one view. Several real test doubles
    // answer `{}`, and a mis-deployed/older server would too.
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    getPulse.mockResolvedValue(page([row({ session_id: "keep" })]));
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));

    getPulse.mockResolvedValue({} as never);
    await act(async () => {
      latest.retry();
    });

    await waitFor(() => expect(latest.error).toMatch(/malformed/i));
    // Degraded, not blanked, and emphatically not a crash.
    expect(latest.rows.map((r) => r.session_id)).toEqual(["keep"]);
    expect(latest.hasMore).toBe(false);
    const logged = spy.mock.calls.map((c) => c.join(" ")).join("\n");
    expect(logged).toMatch(/malformed/);
  });

  it("rejects a non-array items field and a non-string cursor", async () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    for (const bad of [
      { items: "nope", next_cursor: null },
      { items: [], next_cursor: 42 },
      { items: [], next_cursor: { oops: true } },
    ]) {
      getPulse.mockResolvedValue(bad as never);
      const { unmount } = mount();
      await waitFor(() => expect(latest.error).toMatch(/malformed/i));
      unmount();
    }
    expect(spy).toHaveBeenCalled();
  });

  it("retry() clears the previous error on success", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    getPulse.mockRejectedValueOnce(new Error("first"));
    mount();
    await waitFor(() => expect(latest.error).toBeTruthy());

    getPulse.mockResolvedValue(page([row({ session_id: "ok" })]));
    await act(async () => {
      latest.retry();
    });
    await waitFor(() => expect(latest.error).toBeNull());
    expect(latest.rows).toHaveLength(1);
  });
});

describe("pulseStore live event updates", () => {
  beforeEach(() => {
    getPulse.mockResolvedValue(
      page([
        row({ session_id: "a", status: "idle" }),
        row({ session_id: "b", status: "running" }),
      ]),
    );
  });

  it("turn_started moves a known row to running and to the Running rank", async () => {
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    emit("turn_started", "a", {});
    // b was already running and is older by id; a must sort first now.
    await waitFor(() => expect(latest.rows[0].session_id).toBe("a"));
    expect(latest.rows[0].status).toBe("running");
    expect(latest.counts.running).toBe(2);
  });

  it("permission marks needs_permission with the command as the summary", async () => {
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    emit("permission", "a", { tool: "bash", command: "rm -rf /tmp/x" });
    await waitFor(() => expect(latest.rows[0].status).toBe("needs_permission"));
    expect(latest.rows[0].pending_ask).toEqual({
      kind: "permission",
      summary: "rm -rf /tmp/x",
    });
    // The ask is the headline; a task line would duplicate it.
    expect(latest.rows[0].current_task).toBeNull();
    expect(latest.counts.needsYou).toBe(1);
  });

  it("permission falls back to tool name, then summary, when there is no command", async () => {
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    emit("permission", "a", { tool: "write" });
    await waitFor(() =>
      expect(latest.rows[0].pending_ask?.summary).toBe("allow write?"),
    );
    emit("permission_resolved", "a", {});
    emit("permission", "a", { tool: "write", summary: "writes a config file" });
    await waitFor(() =>
      expect(latest.rows[0].pending_ask?.summary).toBe("writes a config file"),
    );
  });

  it("question marks needs_question from the first prompt", async () => {
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    emit("question", "a", { questions: [{ question: "Which branch?" }] });
    await waitFor(() => expect(latest.rows[0].status).toBe("needs_question"));
    expect(latest.rows[0].pending_ask).toEqual({
      kind: "question",
      summary: "Which branch?",
    });
  });

  it("permission_resolved returns the row to running", async () => {
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    emit("permission", "a", { tool: "bash", command: "ls" });
    await waitFor(() => expect(latest.rows[0].status).toBe("needs_permission"));
    emit("permission_resolved", "a", {});
    await waitFor(() => expect(latest.rows.find((r) => r.session_id === "a")?.status).toBe("running"));
  });

  it("question_resolved returns the row to running", async () => {
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    emit("question", "a", { questions: [{ question: "?" }] });
    await waitFor(() => expect(latest.rows[0].status).toBe("needs_question"));
    emit("question_resolved", "a", {});
    await waitFor(() =>
      expect(latest.rows.find((r) => r.session_id === "a")?.status).toBe("running"),
    );
  });

  it("turn_done makes the row idle and turn_done after turn_error is still idle", async () => {
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    emit("turn_error", "a", { error: "llm 500" });
    await waitFor(() =>
      expect(latest.rows.find((r) => r.session_id === "a")?.status).toBe("error"),
    );
    emit("turn_done", "a", {});
    await waitFor(() =>
      expect(latest.rows.find((r) => r.session_id === "a")?.status).toBe("idle"),
    );
  });

  it("todo_updated sets the todo and promotes the task line to the todo item", async () => {
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    emit("todo_updated", "b", {
      session_id: "b",
      done: 1,
      total: 3,
      current: "wire the store",
      items: [{ text: "wire the store", state: "in_progress" }],
    });
    const b = latest.rows.find((r) => r.session_id === "b");
    expect(b?.todo).toMatchObject({ done: 1, total: 3, current: "wire the store" });
    expect(b?.current_task).toEqual({ kind: "todo", text: "wire the store" });
  });

  it("todo_updated drops a frame older than the plan the row holds", async () => {
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    const frame = (revision: number, current: string) => ({
      session_id: "b",
      revision,
      done: 0,
      total: 1,
      current,
      items: [{ text: current, state: "in_progress" }],
    });
    emit("todo_updated", "b", frame(4, "four"));
    emit("todo_updated", "b", frame(3, "stale"));
    expect(latest.rows.find((r) => r.session_id === "b")?.todo?.current).toBe("four");
    emit("todo_updated", "b", frame(5, "five"));
    expect(latest.rows.find((r) => r.session_id === "b")?.todo?.current).toBe("five");
  });

  it("todo_updated does not add a task line to a needs-you row", async () => {
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    emit("permission", "a", { tool: "bash", command: "ls" });
    await waitFor(() => expect(latest.rows[0].status).toBe("needs_permission"));
    emit("todo_updated", "a", {
      session_id: "a",
      done: 0,
      total: 1,
      current: "next step",
      items: [{ text: "next step", state: "in_progress" }],
    });
    const a = latest.rows.find((r) => r.session_id === "a");
    expect(a?.current_task).toBeNull();
    expect(a?.todo?.current).toBe("next step");
  });

  it("session_rekeyed re-keys the row in place", async () => {
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    emit("session_rekeyed", "a", { session_id: "new", old_id: "a" });
    // Both rows are rank 2 with equal updated_at, so the tiebreak is
    // session_id ascending: "b" before "new".
    await waitFor(() => expect(latest.rows.map((r) => r.session_id)).toEqual(["b", "new"]));
    expect(getPulse).toHaveBeenCalledTimes(1); // re-keyed locally, not refetched
  });
});

describe("pulseStore unknown sessions and reconnect", () => {
  it("refetches once for a burst of events on an unknown session, synthesizing no row", async () => {
    vi.useFakeTimers();
    getPulse.mockResolvedValue(page([row({ session_id: "a" })]));
    mount();
    await act(async () => {
      await Promise.resolve();
    });
    getPulse.mockClear();

    emit("turn_started", "ghost", {});
    emit("turn_done", "ghost", {});
    emit("text", "ghost", {});
    // Nothing is invented: a row for a session we have never seen would be a
    // card the user cannot open and cannot explain.
    expect(latest.rows.map((r) => r.session_id)).toEqual(["a"]);
    expect(getPulse).not.toHaveBeenCalled();

    await act(async () => {
      vi.advanceTimersByTime(400);
    });
    expect(getPulse).toHaveBeenCalledTimes(1);
  });

  it("reseeds on reconnect", async () => {
    getPulse.mockResolvedValue(page([row({ session_id: "a" })]));
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));
    getPulse.mockClear();
    getPulse.mockResolvedValue(page([row({ session_id: "a" }), row({ session_id: "z" })]));

    act(() => {
      for (const h of reconnectHandlers) h();
    });
    await waitFor(() => expect(latest.rows).toHaveLength(2));
  });
});

describe("pulseStore scope and paging", () => {
  it("setScope('all') refetches from the first page and drops the old cursor", async () => {
    getPulse.mockResolvedValue(page([row({ session_id: "a" })], "cur1"));
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));
    expect(latest.hasMore).toBe(true);

    getPulse.mockClear();
    getPulse.mockResolvedValue(page([row({ session_id: "old" })], "cur2"));
    await act(async () => {
      latest.setScope("all");
    });
    expect(getPulse).toHaveBeenCalledWith("all", null, 50, undefined);
    expect(latest.rows.map((r) => r.session_id)).toEqual(["old"]);
    // The active scope must be recorded, not just used for this one request:
    // the no-op guard in setScope and the reconnect reseed both read it back.
    expect(latest.scope).toBe("all");
  });

  it("setScope back to 'live' refetches and re-arms the no-op guard", async () => {
    getPulse.mockResolvedValue(page([row({ session_id: "a" })]));
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));

    await act(async () => {
      latest.setScope("all");
    });
    await waitFor(() => expect(latest.scope).toBe("all"));
    await act(async () => {
      latest.setScope("live");
    });
    await waitFor(() => expect(latest.scope).toBe("live"));

    getPulse.mockClear();
    // Re-selecting the scope already active must not hit the network.
    await act(async () => {
      latest.setScope("live");
    });
    expect(getPulse).not.toHaveBeenCalled();
  });

  it("setScope toggled back mid-flight is not swallowed as a no-op", async () => {
    // The no-op guard used to compare against the last APPLIED scope, which
    // only updates when a response lands. Toggling live→all→live inside one
    // tick therefore dropped the second click: the "all" response landed last
    // and the UI sat on "all" even though the user had asked for "live".
    let resolveAll: ((v: ReturnType<typeof page>) => void) | undefined;
    getPulse.mockResolvedValue(page([row({ session_id: "a" })]));
    mount();
    await waitFor(() => expect(latest.scope).toBe("live"));

    getPulse.mockClear();
    getPulse.mockImplementationOnce(
      () => new Promise((resolve) => { resolveAll = resolve; }),
    );
    act(() => {
      latest.setScope("all");
      latest.setScope("live"); // same tick, before "all" has resolved
    });
    // Both clicks must reach the network; the second is a real change of mind.
    expect(getPulse).toHaveBeenCalledTimes(2);
    expect(getPulse).toHaveBeenLastCalledWith("live", null, 50, undefined);

    await act(async () => {
      resolveAll?.(page([row({ session_id: "a" })]));
    });
    // The superseded "all" response must not win, and the requested scope does.
    await waitFor(() => expect(latest.scope).toBe("live"));
  });

  it("loadMore appends using the previous cursor and reports hasMore", async () => {
    getPulse.mockResolvedValue(page([row({ session_id: "a" })], "cur1"));
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));

    getPulse.mockResolvedValue(page([row({ session_id: "b" })], "cur2"));
    await act(async () => {
      latest.loadMore();
    });
    expect(getPulse).toHaveBeenLastCalledWith("live", "cur1", 50, undefined);
    // Server pages are disjoint and pre-sorted, so appending preserves order.
    expect(latest.rows.map((r) => r.session_id)).toEqual(["a", "b"]);
    expect(latest.hasMore).toBe(true);

    getPulse.mockResolvedValue(page([row({ session_id: "c" })], null));
    await act(async () => {
      latest.loadMore();
    });
    expect(latest.hasMore).toBe(false);
  });

  it("loadMore is a no-op without a next cursor", async () => {
    getPulse.mockResolvedValue(page([row({ session_id: "a" })], null));
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));
    getPulse.mockClear();
    await act(async () => {
      latest.loadMore();
    });
    expect(getPulse).not.toHaveBeenCalled();
  });
});

describe("sortPulseRows", () => {
  it("orders by rank, then recency, then session id", () => {
    const rows = [
      row({ session_id: "zzz_idle_old", status: "idle" }),
      row({ session_id: "b_running_old", status: "running", updated_at: OLD }),
      row({ session_id: "a_running_new", status: "running", updated_at: "2030-01-01T00:00:00Z" }),
      row({ session_id: "ask", status: "needs_question" }),
      row({ session_id: "ask2", status: "needs_permission" }),
    ];
    expect(sortPulseRows(rows).map((r) => r.session_id)).toEqual([
      "ask", // needs_* share rank 0, tiebreak by id
      "ask2",
      "a_running_new",
      "b_running_old",
      "zzz_idle_old",
    ]);
  });

  it("does not mutate its input", () => {
    const rows = [row({ session_id: "b" }), row({ session_id: "a" })];
    const before = rows.map((r) => r.session_id);
    sortPulseRows(rows);
    expect(rows.map((r) => r.session_id)).toEqual(before);
  });
});

describe("pulseStore known-session no-refetch", () => {
  it("does not refetch for events from a host it is not fanning out to", async () => {
    vi.useFakeTimers();
    getPulse.mockResolvedValue(page([row({ session_id: "a" })]));
    mount();
    await act(async () => {
      await Promise.resolve();
    });
    getPulse.mockClear();

    // A host with no open tab is not in the relayed set, so the dashboard does
    // not ask it either (dialing an unsaved or disconnected host is the
    // cold-connect hazard). Its frames therefore describe a session no fetch
    // could ever learn about, and each one must not re-arm the 300ms debounce —
    // that was ~3 wasted fetches per second during a streaming remote turn.
    for (let i = 0; i < 20; i++) {
      emit("text", "ses_remote", { delta: "x" }, "james@example.com");
      emit("turn_heartbeat", "ses_remote", {}, "james@example.com");
    }

    await act(async () => {
      vi.advanceTimersByTime(2000);
    });
    expect(getPulse).not.toHaveBeenCalled();
  });

  it("still refetches for an unknown session on the LOCAL host", async () => {
    // Control for the test above: the unknown-session refetch is not disabled,
    // only withheld for hosts this list can never describe.
    vi.useFakeTimers();
    getPulse.mockResolvedValue(page([row({ session_id: "a" })]));
    mount();
    await act(async () => {
      await Promise.resolve();
    });
    getPulse.mockClear();

    emit("turn_started", "ses_local", { model: "m" });
    await act(async () => {
      vi.advanceTimersByTime(400);
    });
    expect(getPulse).toHaveBeenCalledTimes(1);
  });

  it("does not refetch for a known session's unhandled event", async () => {
    vi.useFakeTimers();
    getPulse.mockResolvedValue(page([row({ session_id: "a" }), row({ session_id: "b" })]));
    mount();
    await act(async () => {
      await Promise.resolve();
    });
    getPulse.mockClear();

    // Streaming frames for a row we already have must not be mistaken for an
    // unknown session; before the fix each re-armed the 300ms debounce and
    // polled GET /api/pulse continuously during a turn.
    emit("text", "a", { delta: "hi" });
    emit("thinking", "a", { delta: "..." });
    emit("tool_output", "a", {});
    emit("turn_heartbeat", "a", {});

    await act(async () => {
      vi.advanceTimersByTime(400);
    });
    expect(getPulse).not.toHaveBeenCalled();
    vi.useRealTimers();
  });
});

describe("sortPulseRows across timestamp formats", () => {
  it("orders by instant, not lexical string, across offset vs Z", () => {
    // Server rows carry Go local-offset stamps; live patches carry "...Z".
    // "2026-09-28T13:00:00Z" is LATER than "2026-09-28T20:00:00+08:00"
    // (12:00Z) but sorts lexically BEFORE it.
    const olderServer = row({
      session_id: "server_plus08",
      status: "running",
      updated_at: "2026-09-28T20:00:00+08:00",
    });
    const newerLive = row({
      session_id: "live_Z",
      status: "running",
      updated_at: "2026-09-28T13:00:00Z",
    });
    expect(sortPulseRows([olderServer, newerLive]).map((r) => r.session_id)).toEqual([
      "live_Z",
      "server_plus08",
    ]);
  });
});

// ── Remote fan-out ───────────────────────────────────────────────────────────
//
// The dashboard asks the local server AND every remote host the SSE stream
// relays, so a remote project's sessions get cards. These cover the three
// things a multi-source list can get wrong: which hosts are asked, what happens
// to one that cannot answer, and that a live frame patches the right card.

describe("pulseStore remote fan-out", () => {
  const HOST = "james@217.216.72.49";

  function remoteProject(path: string) {
    return { path, name: path, added_at: "", last_used_at: "", order: 0, group: "", host: HOST };
  }

  it("asks each relayed host alongside the local server and stamps the host on its rows", async () => {
    bus.hosts = [HOST];
    getPulse.mockImplementation(async (_scope, _cursor, _limit, host?: string) =>
      host === HOST
        ? page([row({ session_id: "r1", project_path: "/home/james/www/aimsai2", host })])
        : page([row({ session_id: "l1" })]),
    );
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));

    // Once for the local source, once for the remote one.
    expect(getPulse.mock.calls.map((c) => c[3])).toEqual([undefined, HOST]);
    const remote = latest.rows.find((r) => r.session_id === "r1");
    expect(remote?.host).toBe(HOST);
    const local = latest.rows.find((r) => r.session_id === "l1");
    expect(local?.host).toBe("");
  });

  it("resolves a remote project path to the locally-registered form so a jump finds it", async () => {
    // The remote server reports the path it expanded; this server registered the
    // tilde form. Without the resolve the card renders but its click is refused.
    bus.hosts = [HOST];
    listProjects.mockResolvedValue([remoteProject("~/www/aimsai2")]);
    getPulse.mockImplementation(async (_scope, _cursor, _limit, host?: string) =>
      host === HOST
        ? page([row({ session_id: "r1", project_path: "/home/james/www/aimsai2", host })])
        : page([]),
    );
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));
    expect(latest.rows[0].project_path).toBe("~/www/aimsai2");
  });

  it("keeps the local board and the host's previous rows when that host fails", async () => {
    // Seed both sources, then drop only the remote one: the dashboard must dim,
    // never blank, and the local rows must be untouched.
    bus.hosts = [HOST];
    getPulse.mockImplementation(async (_scope, _cursor, _limit, host?: string) =>
      host === HOST ? page([row({ session_id: "r1", host })]) : page([row({ session_id: "l1" })]),
    );
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));

    getPulse.mockImplementation(async (_scope, _cursor, _limit, host?: string) => {
      if (host === HOST) throw new Error("502 remote connect failed");
      return page([row({ session_id: "l1" })]);
    });
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    await act(async () => {
      latest.retry();
    });
    await waitFor(() => expect(latest.hostErrors).toEqual([HOST]));
    // Not a fatal error — the local board is healthy.
    expect(latest.error).toBeNull();
    expect(latest.rows.map((r) => r.session_id).sort()).toEqual(["l1", "r1"]);
    expect(spy).toHaveBeenCalled();
  });

  it("merges every source's ordering into one rank-ordered list", async () => {
    bus.hosts = [HOST];
    getPulse.mockImplementation(async (_scope, _cursor, _limit, host?: string) =>
      host === HOST
        ? page([row({ session_id: "r_idle", status: "idle", host })])
        : page([row({ session_id: "l_run", status: "running" }), row({ session_id: "l_ask", status: "needs_permission" })]),
    );
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(3));
    expect(latest.rows.map((r) => r.session_id)).toEqual(["l_ask", "l_run", "r_idle"]);
  });

  it("pages every source independently, so one host's cursor is not the board's", async () => {
    bus.hosts = [HOST];
    getPulse.mockImplementation(async (_scope, cursor, _limit, host?: string) =>
      host === HOST
        ? page([row({ session_id: cursor ? "r2" : "r1", host })], cursor ? null : "rcur")
        : page([row({ session_id: cursor ? "l2" : "l1" })], cursor ? null : "lcur"),
    );
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    expect(latest.hasMore).toBe(true);
    expect(latest.nextCursor).toBe("lcur");

    await act(async () => {
      latest.loadMore();
    });
    await waitFor(() => expect(latest.rows).toHaveLength(4));
    // Both cursors were used, not just the local one.
    expect(getPulse.mock.calls.map((c) => c[1])).toEqual([null, null, "lcur", "rcur"]);
    expect(latest.hasMore).toBe(false);
  });

  it("patches a remote card from its own host's event, leaving the local card alone", async () => {
    bus.hosts = [HOST];
    getPulse.mockImplementation(async (_scope, _cursor, _limit, host?: string) =>
      host === HOST ? page([row({ session_id: "r1", host })]) : page([row({ session_id: "l1" })]),
    );
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));

    emit("permission", "r1", { tool: "bash", command: "ls" }, HOST);
    await waitFor(() => expect(latest.rows.find((r) => r.session_id === "r1")?.status).toBe("needs_permission"));
    // The local row is untouched — identity is (session_id, host), not id alone.
    expect(latest.rows.find((r) => r.session_id === "l1")?.status).toBe("idle");
    expect(latest.counts.needsYou).toBe(1);
  });

  it("reseeds when a remote project's first tab changes the relayed host set", async () => {
    getPulse.mockImplementation(async (_scope, _cursor, _limit, host?: string) =>
      host === HOST ? page([row({ session_id: "r1", host })]) : page([row({ session_id: "l1" })]),
    );
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));
    expect(getPulse).toHaveBeenCalledTimes(1);

    bus.hosts = [HOST];
    act(() => hostsChangeHandlers.forEach((h) => h()));
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    expect(latest.rows.find((r) => r.session_id === "r1")?.host).toBe(HOST);
  });

  it("reports an unresolvable remote project path ONCE, not on every refresh", async () => {
    // A remote host can report a project this server never saved. The row still
    // renders (its jump is refused rather than pointed at the wrong machine), so
    // the only thing that must NOT happen is silence — and the second time it
    // must not spam 50 identical lines.
    bus.hosts = [HOST];
    listProjects.mockResolvedValue([]);
    getPulse.mockImplementation(async (_scope, _cursor, _limit, host?: string) =>
      host === HOST ? page([row({ session_id: "r1", project_path: "/srv/unregistered", host })]) : page([]),
    );
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));

    await act(async () => {
      latest.retry();
    });
    await waitFor(() => expect(latest.rows).toHaveLength(1));

    const lines = spy.mock.calls.map((c) => c.join(" ")).filter((l) => l.includes("not a project saved"));
    expect(lines).toHaveLength(1);
    expect(lines[0]).toContain("/srv/unregistered");
    expect(lines[0]).toContain(HOST);
    // The row keeps the host's own path, so nothing is silently rewritten.
    expect(latest.rows[0].project_path).toBe("/srv/unregistered");
  });

  it("patches a local card whose row arrived without a host field", async () => {
    // The Go row has no `host` key, so a real local page lands as `undefined`.
    // The fixture's `host: ""` hid this: the store's strict `=== ""` lookups
    // never matched, every frame looked like an unknown session, and the board
    // polled /api/pulse ~3x/s for a whole turn instead of patching the card.
    const serverRow = { ...row({ session_id: "l1" }) } as Partial<PulseRow>;
    delete serverRow.host;
    getPulse.mockResolvedValue(page([serverRow as PulseRow]));
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));
    expect(latest.rows[0].host).toBe("");
    getPulse.mockClear();

    vi.useFakeTimers();
    emit("turn_started", "l1", {});
    expect(latest.rows[0].status).toBe("running");
    emit("text", "l1", { text: "hi" });
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(getPulse).not.toHaveBeenCalled();
  });

  it("loadMore never re-asks a source whose cursor is exhausted", async () => {
    // Local has one page, remote has two. The second load-more must ask only
    // the remote host; re-asking local fetched page one again, got its cursor
    // back, and `hasMore` cycled between the sources forever.
    bus.hosts = [HOST];
    getPulse.mockImplementation(async (_scope, cursor, _limit, host?: string) =>
      host === HOST
        ? page([row({ session_id: cursor ? "r2" : "r1", host })], cursor ? null : "rcur")
        : page([row({ session_id: "l1" })], null),
    );
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));
    expect(latest.hasMore).toBe(true);
    getPulse.mockClear();

    await act(async () => {
      latest.loadMore();
    });
    await waitFor(() => expect(latest.rows).toHaveLength(3));
    expect(getPulse.mock.calls.map((c) => [c[1], c[3]])).toEqual([["rcur", HOST]]);
    expect(latest.hasMore).toBe(false);
  });

  it("still replaces a remote host's rows with its fresh page when the local fetch fails", async () => {
    bus.hosts = [HOST];
    getPulse.mockImplementation(async (_scope, _cursor, _limit, host?: string) =>
      host === HOST ? page([row({ session_id: "r1", status: "running", host })]) : page([row({ session_id: "l1" })]),
    );
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));

    vi.spyOn(console, "error").mockImplementation(() => {});
    getPulse.mockImplementation(async (_scope, _cursor, _limit, host?: string) => {
      if (host === HOST) return page([row({ session_id: "r1", status: "needs_permission", host })]);
      throw new Error("local 500");
    });
    await act(async () => {
      latest.retry();
    });
    await waitFor(() => expect(latest.error).toBe("local 500"));
    // Local rows are held over; the remote page that DID land wins over its
    // stale copy rather than being dropped by a first-occurrence dedupe.
    expect(latest.rows.map((r) => [r.session_id, r.status])).toEqual([
      ["r1", "needs_permission"],
      ["l1", "idle"],
    ]);
    expect(latest.counts).toEqual({ running: 0, needsYou: 1 });
  });

  it("does not cache a failed project list", async () => {
    bus.hosts = [HOST];
    listProjects.mockRejectedValueOnce(new Error("projects timeout"));
    getPulse.mockImplementation(async (_scope, _cursor, _limit, host?: string) =>
      host === HOST ? page([row({ session_id: "r1", project_path: "/home/james/www/aimsai2", host })]) : page([]),
    );
    vi.spyOn(console, "error").mockImplementation(() => {});
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));
    expect(latest.rows[0].project_path).toBe("/home/james/www/aimsai2");

    listProjects.mockResolvedValue([remoteProject("~/www/aimsai2")]);
    await act(async () => {
      latest.retry();
    });
    await waitFor(() => expect(latest.rows[0].project_path).toBe("~/www/aimsai2"));
    expect(listProjects).toHaveBeenCalledTimes(2);
  });

  it("reseeds against a fresh project list on reconnect", async () => {
    bus.hosts = [HOST];
    listProjects.mockResolvedValueOnce([]);
    getPulse.mockImplementation(async (_scope, _cursor, _limit, host?: string) =>
      host === HOST ? page([row({ session_id: "r1", project_path: "/home/james/www/aimsai2", host })]) : page([]),
    );
    vi.spyOn(console, "error").mockImplementation(() => {});
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));
    expect(latest.rows[0].project_path).toBe("/home/james/www/aimsai2");

    // The project was added while the stream was down; the reconnect reseed
    // must read the new list, not the one it cached before the outage.
    listProjects.mockResolvedValue([remoteProject("~/www/aimsai2")]);
    act(() => reconnectHandlers.forEach((h) => h()));
    await waitFor(() => expect(latest.rows[0].project_path).toBe("~/www/aimsai2"));
  });

  it("drops a host's rows once its last tab closes", async () => {
    bus.hosts = [HOST];
    getPulse.mockImplementation(async (_scope, _cursor, _limit, host?: string) =>
      host === HOST ? page([row({ session_id: "r1", host })]) : page([row({ session_id: "l1" })]),
    );
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(2));

    bus.hosts = [];
    act(() => hostsChangeHandlers.forEach((h) => h()));
    await waitFor(() => expect(latest.rows.map((r) => r.session_id)).toEqual(["l1"]));
    expect(latest.hostErrors).toEqual([]);
  });
});
