import { beforeEach, describe, expect, it, vi } from "vitest";

/**
 * btwStore suite.
 *
 * The store subscribes to the `btw` bus event once at module load. This fake
 * captures that handler so tests drive frames exactly as the SSE relay would.
 */
const { handlers } = vi.hoisted(() => ({
  handlers: [] as ((env: unknown) => void)[],
}));
vi.mock("./eventBus", () => ({
  eventBus: {
    on: (event: string, handler: (env: unknown) => void) => {
      if (event === "btw") handlers.push(handler);
      return () => {};
    },
  },
}));

import {
  __resetBtwStoreForTests,
  closeBtw,
  getBtwState,
  rekeyBtw,
  startBtw,
} from "./btwStore";

function frame(
  sessionId: string,
  data: Record<string, unknown>,
  host?: string,
): void {
  for (const h of handlers) h({ event: "btw", session_id: sessionId, ...(host ? { host } : {}), data });
}

describe("btwStore", () => {
  beforeEach(() => {
    __resetBtwStoreForTests();
  });

  it("reduces started/activity/delta/done into panel state", () => {
    frame("s1", { generation: 1, phase: "started", question: "use tabs?" });
    let st = getBtwState("s1");
    expect(st?.question).toBe("use tabs?");
    expect(st?.loading).toBe(true);
    expect(st?.open).toBe(true);
    expect(st?.activity).toEqual([]);

    frame("s1", { generation: 1, phase: "activity", text: "→ read a.go" });
    frame("s1", { generation: 1, phase: "activity", text: "→ bash ls" });
    frame("s1", { generation: 1, phase: "delta", text: "partial " });
    frame("s1", { generation: 1, phase: "delta", text: "answer" });

    st = getBtwState("s1");
    expect(st?.activity).toEqual(["→ read a.go", "→ bash ls"]);
    expect(st?.answer).toBe("partial answer");
    expect(st?.loading).toBe(true);

    frame("s1", { generation: 1, phase: "done", text: "the final answer" });
    st = getBtwState("s1");
    expect(st?.loading).toBe(false);
    expect(st?.answer).toBe("the final answer");
    expect(st?.error).toBeUndefined();
  });

  it("ignores frames from an older generation", () => {
    frame("s1", { generation: 2, phase: "started", question: "second run" });
    frame("s1", { generation: 2, phase: "delta", text: "second " });
    // A late STARTED frame from the superseded run must NOT reset the panel
    // back to the old run (this is the generation guard's discriminating case).
    frame("s1", { generation: 1, phase: "started", question: "first run" });
    // Nor may the superseded run's delta/done land.
    frame("s1", { generation: 1, phase: "delta", text: "stale " });
    frame("s1", { generation: 1, phase: "done", text: "stale answer" });

    const st = getBtwState("s1");
    expect(st?.generation).toBe(2);
    expect(st?.question).toBe("second run");
    expect(st?.answer).toBe("second ");
    expect(st?.loading).toBe(true);
  });

  it("keys state per host so the same session id does not collide", () => {
    frame("s1", { generation: 1, phase: "started", question: "local" });
    frame("s1", { generation: 1, phase: "started", question: "remote" }, "devbox");
    frame("s1", { generation: 1, phase: "done", text: "remote answer" }, "devbox");

    expect(getBtwState("s1")?.question).toBe("local");
    expect(getBtwState("s1")?.answer).toBe("");
    expect(getBtwState("s1", "devbox")?.question).toBe("remote");
    expect(getBtwState("s1", "devbox")?.answer).toBe("remote answer");
  });

  it("a started frame opens the panel even without a local startBtw call", () => {
    expect(getBtwState("s1")).toBeUndefined();
    frame("s1", { generation: 3, phase: "started", question: "from another tab" });
    expect(getBtwState("s1")?.open).toBe(true);
    expect(getBtwState("s1")?.question).toBe("from another tab");
  });

  it("startBtw opens the panel optimistically and is replaced by the started frame", () => {
    startBtw("s1", undefined, "my aside");
    expect(getBtwState("s1")?.question).toBe("my aside");
    expect(getBtwState("s1")?.loading).toBe(true);

    frame("s1", { generation: 1, phase: "started", question: "my aside" });
    expect(getBtwState("s1")?.generation).toBe(1);
  });

  it("closeBtw drops the state and a stray frame cannot resurrect it", () => {
    frame("s1", { generation: 1, phase: "started", question: "q" });
    closeBtw("s1");
    expect(getBtwState("s1")).toBeUndefined();

    // A late delta for the closed panel must not recreate it.
    frame("s1", { generation: 1, phase: "delta", text: "late" });
    expect(getBtwState("s1")).toBeUndefined();
  });

  it("an error frame stops loading and records the message", () => {
    frame("s1", { generation: 1, phase: "started", question: "q" });
    frame("s1", { generation: 1, phase: "error", error: "could not build a client" });
    const st = getBtwState("s1");
    expect(st?.loading).toBe(false);
    expect(st?.error).toBe("could not build a client");
  });

  it("rekeyBtw moves state to the new id on /reset-id", () => {
    frame("old", { generation: 1, phase: "started", question: "q" });
    rekeyBtw("old", "new");
    expect(getBtwState("old")).toBeUndefined();
    expect(getBtwState("new")?.sessionId).toBe("new");
    expect(getBtwState("new")?.question).toBe("q");
  });

  it("rekeyBtw resets the generation so the new run's frames are not seen as stale", () => {
    // The server DELETES the run entry on /reset-id, so the new id's first run
    // starts at generation 1. Carrying the old generation would reject it.
    frame("old", { generation: 3, phase: "started", question: "q" });
    rekeyBtw("old", "new");
    expect(getBtwState("new")?.generation).toBe(0);
    frame("new", { generation: 1, phase: "started", question: "second" });
    expect(getBtwState("new")?.question).toBe("second");
    expect(getBtwState("new")?.generation).toBe(1);
  });

  it("startBtw does not clobber a terminal frame that arrived first", () => {
    // The server publishes `started` — and a fast `error` — BEFORE it writes
    // the 202, and the SSE stream reaches the browser first. startBtw (called
    // after the POST resolves) must not reset the panel to loading.
    frame("s1", { generation: 1, phase: "started", question: "q" });
    frame("s1", { generation: 1, phase: "error", error: "no client" });
    startBtw("s1", undefined, "q");
    const st = getBtwState("s1");
    expect(st?.loading).toBe(false);
    expect(st?.error).toBe("no client");
    expect(st?.open).toBe(true);
  });

  it("startBtw does not clobber a fast done frame that arrived first", () => {
    frame("s1", { generation: 1, phase: "started", question: "q" });
    frame("s1", { generation: 1, phase: "done", text: "the answer" });
    startBtw("s1", undefined, "q");
    const st = getBtwState("s1");
    expect(st?.loading).toBe(false);
    expect(st?.answer).toBe("the answer");
  });
});
