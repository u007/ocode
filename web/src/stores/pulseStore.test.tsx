import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, act, waitFor } from "@testing-library/react";
import { PulseProvider, usePulse, pulseEventSink, sortPulseRows } from "./pulseStore";
import { api } from "../api/client";
import type { PulseRow } from "../api/types";

vi.mock("../api/client", () => ({
  api: { getPulse: vi.fn() },
}));

// Fake the bus so the reconnect reseed can be driven deterministically (the
// real bus opens a live SSE stream, which jsdom has no server for).
const reconnectHandlers = new Set<() => void>();
vi.mock("../lib/eventBus", () => ({
  eventBus: {
    onReconnect: (h: () => void) => {
      reconnectHandlers.add(h);
      return () => reconnectHandlers.delete(h);
    },
  },
}));

const getPulse = vi.mocked(api.getPulse);

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
function emit(event: string, sessionId: string, data: unknown) {
  act(() => pulseEventSink(event, sessionId, data));
}

beforeEach(() => {
  getPulse.mockReset();
  getPulse.mockResolvedValue(page([]));
  reconnectHandlers.clear();
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
    expect(getPulse).toHaveBeenCalledWith("live", null, 50);
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
    expect(getPulse).toHaveBeenCalledWith("all", null, 50);
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

  it("loadMore appends using the previous cursor and reports hasMore", async () => {
    getPulse.mockResolvedValue(page([row({ session_id: "a" })], "cur1"));
    mount();
    await waitFor(() => expect(latest.rows).toHaveLength(1));

    getPulse.mockResolvedValue(page([row({ session_id: "b" })], "cur2"));
    await act(async () => {
      latest.loadMore();
    });
    expect(getPulse).toHaveBeenLastCalledWith("live", "cur1", 50);
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
