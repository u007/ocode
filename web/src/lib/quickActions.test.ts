import { api } from "@/api/client";
import { eventBus } from "@/lib/eventBus";

vi.mock("@/api/client", () => ({
  api: {
    getQuickActionsConfig: vi.fn(),
    setQuickActionsConfig: vi.fn(),
  },
}));
vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    on: vi.fn(() => () => {}),
    onReconnect: vi.fn(() => () => {}),
  },
}));

import { createElement } from "react";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  QUICK_ACTION_ICONS,
  QUICK_ACTION_RESERVED_IDS,
  QUICK_ACTIONS_MAX,
  SEED_CHIPS,
  __resetQuickActionsForTests,
  chipDispatchKind,
  chipDispatchesCompact,
  chipRequiresHistory,
  isQuickActionMode,
  isQuickActionSeed,
  mintQuickActionId,
  normalizeQuickActionChip,
  normalizeQuickActions,
  quickActionIconComponent,
  quickActionsRevision,
  refreshQuickActions,
  saveQuickActions,
  useQuickActions,
  visibleChips,
} from "./quickActions";
import type { QuickActionChip } from "@/api/types";

const chip = (over: Partial<QuickActionChip> = {}): QuickActionChip => ({
  id: "a",
  label: "A",
  icon: "zap",
  message: "m",
  mode: "send",
  ...over,
});

describe("quick action icon allowlist", () => {
  // The allowlist is deliberately duplicated in Go (validation authority) and
  // TS (render map). These two assertions are what make a divergence loud
  // instead of a silently blank pill.
  it("declares exactly 24 icons", () => {
    expect(QUICK_ACTION_ICONS).toHaveLength(24);
    expect(new Set(QUICK_ACTION_ICONS).size).toBe(24);
  });

  it("covers every seed chip's icon", () => {
    for (const starter of SEED_CHIPS) {
      expect(QUICK_ACTION_ICONS).toContain(starter.icon);
    }
  });

  it("resolves every allowlisted key to a usable React component", () => {
    for (const key of QUICK_ACTION_ICONS) {
      // lucide exports forwardRef objects, so "is a function" would be false
      // for every real icon. createElement throwing on an invalid type is the
      // property that actually matters to a renderer.
      expect(() => createElement(quickActionIconComponent(key), {})).not.toThrow();
    }
  });

  it("does not collapse the whole map onto the default icon", () => {
    expect(quickActionIconComponent("archive")).not.toBe(quickActionIconComponent("zap"));
    expect(quickActionIconComponent("file-text")).not.toBe(quickActionIconComponent("zap"));
  });

  // Review Focus #5: a key that vanishes from lucide must not blank the pill.
  it("falls back instead of throwing for an unknown icon key", () => {
    expect(() => quickActionIconComponent("not-a-real-icon")).not.toThrow();
    expect(quickActionIconComponent("not-a-real-icon")).toBe(quickActionIconComponent("zap"));
  });

  it("falls back for an empty key too", () => {
    expect(quickActionIconComponent("")).toBe(quickActionIconComponent("zap"));
  });
});

describe("normalizeQuickActionChip", () => {
  it("fills a missing icon and mode", () => {
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "m" })).toEqual({
      id: "a",
      label: "A",
      icon: "zap",
      message: "m",
      mode: "send",
    });
  });

  // An ABSENT mode is the common case (a hand-written config, a chip the user
  // never touched) and normalizes to "send", reproducing pre-feature behaviour.
  it("normalizes an absent mode to send", () => {
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "m" })?.mode).toBe("send");
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "m", mode: null })?.mode).toBe("send");
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "m", mode: "" })?.mode).toBe("send");
  });

  // The counterpart: an EXPLICITLY invalid mode is deliberately NOT repaired,
  // so the server rejects it and the user learns why. Repairing it here would
  // turn a 400 into silent divergence between what the user typed and what got
  // saved.
  it("preserves an invalid mode so the server can reject it", () => {
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "m", mode: "sideways" })?.mode).toBe("sideways");
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "m", mode: 7 })?.mode).toBe(7);
  });

  // Review Focus #1: a pasted prompt with a trailing newline must be refused,
  // not turned into a pill that silently does nothing.
  it("returns null for a whitespace-only message", () => {
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "\n  \t " })).toBeNull();
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "" })).toBeNull();
    expect(normalizeQuickActionChip({ id: "a", label: "A" })).toBeNull();
  });

  it("returns null for a whitespace-only label", () => {
    expect(normalizeQuickActionChip({ id: "a", label: "   ", message: "m" })).toBeNull();
  });

  it("returns null for a missing id", () => {
    expect(normalizeQuickActionChip({ label: "A", message: "m" })).toBeNull();
    expect(normalizeQuickActionChip({ id: "  ", label: "A", message: "m" })).toBeNull();
  });

  it("returns null for a non-object entry", () => {
    expect(normalizeQuickActionChip(null)).toBeNull();
    expect(normalizeQuickActionChip("a chip")).toBeNull();
    expect(normalizeQuickActionChip([{ id: "a", label: "A", message: "m" }])).toBeNull();
  });

  it("trims the id and label", () => {
    const normalized = normalizeQuickActionChip({ id: "  a  ", label: "  A  ", message: "m" });
    expect(normalized?.id).toBe("a");
    expect(normalized?.label).toBe("A");
  });

  // The message body is kept verbatim (Go's Normalize does not touch it either).
  // Every derived helper trims defensively, so trimming here would only make
  // those trims dead code and would silently rewrite what the user typed.
  it("keeps the message verbatim, including its whitespace", () => {
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "  m  " })?.message).toBe("  m  ");
  });

  it("keeps the seed when present and drops an invalid one", () => {
    expect(normalizeQuickActionChip({ id: "continue", label: "C", message: "continue", seed: "continue" })?.seed).toBe(
      "continue",
    );
    expect(normalizeQuickActionChip({ id: "x", label: "X", message: "m", seed: "nope" })?.seed).toBeUndefined();
    expect("seed" in (normalizeQuickActionChip({ id: "x", label: "X", message: "m" }) ?? {})).toBe(false);
  });

  it("keeps a valid icon and defaults a blank one", () => {
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "m", icon: "bug" })?.icon).toBe("bug");
    expect(normalizeQuickActionChip({ id: "a", label: "A", message: "m", icon: "" })?.icon).toBe("zap");
  });
});

describe("isQuickActionSeed / isQuickActionMode", () => {
  it("accepts exactly the three seed slugs", () => {
    expect(isQuickActionSeed("compact")).toBe(true);
    expect(isQuickActionSeed("continue")).toBe(true);
    expect(isQuickActionSeed("recap")).toBe(true);
    expect(isQuickActionSeed("compact ")).toBe(false);
    expect(isQuickActionSeed("")).toBe(false);
    expect(isQuickActionSeed(undefined)).toBe(false);
  });

  it("accepts exactly fill and send", () => {
    expect(isQuickActionMode("fill")).toBe(true);
    expect(isQuickActionMode("send")).toBe(true);
    expect(isQuickActionMode("Fill")).toBe(false);
    expect(isQuickActionMode(null)).toBe(false);
  });
});

describe("QUICK_ACTION_RESERVED_IDS", () => {
  it("names the three starter slugs the starters actually occupy", () => {
    expect(QUICK_ACTION_RESERVED_IDS).toEqual(["compact", "continue", "recap"]);
    expect(SEED_CHIPS.map((c) => c.id)).toEqual([...QUICK_ACTION_RESERVED_IDS]);
  });
});

describe("normalizeQuickActions", () => {
  it("drops a non-array chips value and falls back to nothing", () => {
    expect(normalizeQuickActions({ chips: "nope" })).toEqual([]);
    expect(normalizeQuickActions({})).toEqual([]);
    expect(normalizeQuickActions(null)).toEqual([]);
  });

  it("treats a missing chips array as no strip rather than the starters", () => {
    // Go seeds; a client that invented starters here would be a second
    // authority, and a user who deleted every chip would see them return.
    expect(normalizeQuickActions({})).toEqual([]);
  });

  it("drops malformed entries and keeps the good ones", () => {
    const chips = normalizeQuickActions({ chips: [{ id: "a", label: "A", message: "m" }, { id: "b", label: "  " }] });
    expect(chips).toHaveLength(1);
    expect(chips[0].id).toBe("a");
  });

  it("preserves array order, because order is the sort order", () => {
    const chips = normalizeQuickActions({
      chips: [
        { id: "z", label: "Z", message: "z" },
        { id: "a", label: "A", message: "a" },
      ],
    });
    expect(chips.map((c) => c.id)).toEqual(["z", "a"]);
  });

  it("normalizes an empty chips array to no strip", () => {
    expect(normalizeQuickActions({ chips: [] })).toEqual([]);
  });
});

describe("derived chip state", () => {
  it("detects a compaction dispatch so the pill can dim", () => {
    expect(chipDispatchesCompact(chip({ message: "/compact" }))).toBe(true);
    expect(chipDispatchesCompact(chip({ message: "  /compact  " }))).toBe(true);
    expect(chipDispatchesCompact(chip({ message: "/compact --x" }))).toBe(true);
    expect(chipDispatchesCompact(chip({ message: "continue" }))).toBe(false);
    expect(chipDispatchesCompact(chip({ message: "/compacted" }))).toBe(false);
    expect(chipDispatchesCompact(chip({ message: "compact" }))).toBe(false);
  });

  it("marks only seeded chips as needing history", () => {
    expect(chipRequiresHistory(SEED_CHIPS[0])).toBe(true);
    expect(chipRequiresHistory(SEED_CHIPS[2])).toBe(true);
    expect(chipRequiresHistory(chip({ mode: "fill" }))).toBe(false);
    expect(chipRequiresHistory(chip({ seed: undefined }))).toBe(false);
  });

  it("routes a leading slash to the command pipeline", () => {
    expect(chipDispatchKind(chip({ message: "/recap" }))).toBe("command");
    expect(chipDispatchKind(chip({ message: "  /recap" }))).toBe("command");
    expect(chipDispatchKind(chip({ message: "hello" }))).toBe("message");
    expect(chipDispatchKind(chip({ message: "" }))).toBe("message");
  });
});

describe("visibleChips", () => {
  const custom = chip({ id: "x", label: "X", message: "run tests", mode: "fill" });

  it("hides seeded chips on an empty session but keeps custom ones", () => {
    expect(visibleChips([...SEED_CHIPS, custom], false).map((c) => c.id)).toEqual(["x"]);
    expect(visibleChips([...SEED_CHIPS, custom], true)).toHaveLength(4);
  });

  it("returns an empty list when nothing is visible, so the strip unmounts", () => {
    expect(visibleChips([...SEED_CHIPS], false)).toEqual([]);
    expect(visibleChips([], false)).toEqual([]);
  });

  it("preserves relative order", () => {
    const a = chip({ id: "a" });
    const b = chip({ id: "b" });
    expect(visibleChips([a, ...SEED_CHIPS, b], false).map((c) => c.id)).toEqual(["a", "b"]);
  });
});

describe("mintQuickActionId", () => {
  // Review Focus #2: two pills sharing a React key make one vanish silently.
  it("never returns a reserved seed slug", () => {
    expect(["compact", "continue", "recap"]).not.toContain(mintQuickActionId([]));
  });

  it("never collides with an existing id", () => {
    const existing = [chip({ id: "chip-1" })];
    expect(existing.map((c) => c.id)).not.toContain(mintQuickActionId(existing));
  });

  it("skips past a run of taken ids", () => {
    const existing = [chip({ id: "chip-1" }), chip({ id: "chip-2" }), chip({ id: "chip-3" })];
    expect(mintQuickActionId(existing)).toBe("chip-4");
  });

  it("stays unique across repeated mints", () => {
    const seen: string[] = [];
    for (let i = 0; i < 5; i++) {
      seen.push(mintQuickActionId(seen.map((id) => chip({ id }))));
    }
    expect(new Set(seen).size).toBe(5);
  });

  it("never collides with a reserved slug even when the counters line up", () => {
    // 'compact' is reserved; a mint that only compared against `existing` would
    // hand it out as soon as nothing else used it.
    const minted = [mintQuickActionId([])];
    for (let i = 0; i < 5; i++) minted.push(mintQuickActionId(minted.map((id) => chip({ id }))));
    for (const id of minted) {
      expect(QUICK_ACTION_RESERVED_IDS).not.toContain(id);
      expect(new Set(minted).size).toBe(minted.length);
    }
  });
});

describe("cap", () => {
  it("is 20", () => {
    expect(QUICK_ACTIONS_MAX).toBe(20);
  });
});

describe("quickActionsRevision", () => {
  it("changes when any rendered field changes", () => {
    const base = chip();
    expect(quickActionsRevision([base])).not.toBe(quickActionsRevision([chip({ label: "B" })]));
    expect(quickActionsRevision([base])).not.toBe(quickActionsRevision([chip({ icon: "bug" })]));
    expect(quickActionsRevision([base])).not.toBe(quickActionsRevision([chip({ message: "n" })]));
    expect(quickActionsRevision([base])).not.toBe(quickActionsRevision([chip({ mode: "fill" })]));
    expect(quickActionsRevision([base])).not.toBe(quickActionsRevision([chip({ seed: "recap" })]));
  });

  it("is stable for identical chips and distinguishes order", () => {
    expect(quickActionsRevision([chip({ id: "a" }), chip({ id: "b" })])).toBe(
      quickActionsRevision([chip({ id: "a" }), chip({ id: "b" })]),
    );
    expect(quickActionsRevision([chip({ id: "a" }), chip({ id: "b" })])).not.toBe(
      quickActionsRevision([chip({ id: "b" }), chip({ id: "a" })]),
    );
  });
});

const getConfig = vi.mocked(api.getQuickActionsConfig);
const setConfig = vi.mocked(api.setQuickActionsConfig);

beforeEach(() => {
  __resetQuickActionsForTests();
  vi.clearAllMocks();
  vi.spyOn(console, "warn").mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("quick actions store", () => {
  it("publishes the server's chips", async () => {
    getConfig.mockResolvedValue({ chips: [chip({ id: "x", label: "X", message: "m", mode: "fill" })] });
    const state = await refreshQuickActions();
    expect(state.chips.map((c) => c.id)).toEqual(["x"]);
    expect(state.error).toBeNull();
    expect(state.loading).toBe(false);
    expect(state.revision).toBe(quickActionsRevision(state.chips));
  });

  // The spec's degraded path: a failed fetch must fall back to the pre-feature
  // strip, NOT blank the chips out from under the user.
  it("falls back to the starter strip when the first fetch fails", async () => {
    getConfig.mockRejectedValue(new Error("offline"));
    const state = await refreshQuickActions();
    expect(state.chips.map((c) => c.id)).toEqual(["compact", "continue", "recap"]);
    expect(state.error).toBe("offline");
    expect(console.warn).toHaveBeenCalledWith(
      "[quick-actions] config request failed; using the pre-feature default strip",
      expect.objectContaining({ reason: "no-cached-chips" }),
    );
  });

  it("retains the cached chips when a later refresh fails", async () => {
    getConfig.mockResolvedValue({ chips: [chip({ id: "x", label: "X", message: "m", mode: "fill" })] });
    await refreshQuickActions();
    getConfig.mockRejectedValue(new Error("offline"));
    const state = await refreshQuickActions();
    expect(state.chips.map((c) => c.id)).toEqual(["x"]);
    expect(state.error).toBe("offline");
    expect(console.warn).toHaveBeenCalledWith(
      "[quick-actions] config request failed; retaining cached chips",
      expect.objectContaining({ error: expect.any(Error) }),
    );
  });

  it("keeps an explicit empty strip after a later failure, never resurrecting the starters", () => {
    // The user deleted every chip; that state must survive a transient failure
    // exactly like any other cached list.
    return (async () => {
      getConfig.mockResolvedValue({ chips: [] });
      await refreshQuickActions();
      getConfig.mockRejectedValue(new Error("offline"));
      const state = await refreshQuickActions();
      expect(state.chips).toEqual([]);
    })();
  });

  it("deduplicates concurrent refreshes into one request", async () => {
    getConfig.mockResolvedValue({ chips: [] });
    await Promise.all([refreshQuickActions(), refreshQuickActions(), refreshQuickActions()]);
    expect(getConfig).toHaveBeenCalledTimes(1);
  });

  it("allows a new request after the in-flight one settles", async () => {
    getConfig.mockResolvedValue({ chips: [] });
    await refreshQuickActions();
    await refreshQuickActions();
    expect(getConfig).toHaveBeenCalledTimes(2);
  });

  it("saveQuickActions publishes the server's response, not the local draft", async () => {
    setConfig.mockResolvedValue({ chips: [] });
    const state = await saveQuickActions([chip({ id: "x", label: "X", message: "m", mode: "fill" })]);
    // The server is authoritative: if it normalises or rejects, the client
    // must adopt its answer rather than keeping the local draft.
    expect(state.chips).toEqual([]);
  });

  it("saveQuickActions sends the whole list and adopts the reply", async () => {
    const sent = [chip({ id: "x", label: "X", message: "m", mode: "fill" })];
    setConfig.mockResolvedValue({ chips: [chip({ id: "x", label: "X", message: "m", mode: "send" })] });
    const state = await saveQuickActions(sent);
    expect(setConfig).toHaveBeenCalledWith({ chips: sent });
    expect(state.chips[0].mode).toBe("send");
  });

  it("surfaces a server rejection instead of publishing the local draft", async () => {
    getConfig.mockResolvedValue({ chips: [chip({ id: "x" })] });
    await refreshQuickActions();
    setConfig.mockRejectedValue(new Error("at most 20 chips are allowed"));
    // Task 6 renders this message on the form; a silent optimistic publish here
    // would show chips the server never accepted.
    await expect(saveQuickActions([chip({ id: "y" })])).rejects.toThrow("at most 20 chips");
    expect(getConfig).toHaveBeenCalledTimes(1);
  });
});

describe("useQuickActions", () => {
  const busOn = vi.mocked(eventBus.on);
  const busReconnect = vi.mocked(eventBus.onReconnect);

  afterEach(() => {
    cleanup();
  });

  it("fetches once for N mounted session tabs", async () => {
    getConfig.mockResolvedValue({ chips: [chip({ id: "x" })] });
    const a = renderHook(() => useQuickActions());
    const b = renderHook(() => useQuickActions());
    await waitFor(() => {
      expect(a.result.current.chips.map((c) => c.id)).toEqual(["x"]);
      expect(b.result.current.chips.map((c) => c.id)).toEqual(["x"]);
    });
    expect(getConfig).toHaveBeenCalledTimes(1);
  });

  it("subscribes to the global quick_actions_changed bus event and to reconnect", async () => {
    getConfig.mockResolvedValue({ chips: [] });
    const { result } = renderHook(() => useQuickActions());
    // Let the mount fetch settle so the publish lands inside act().
    await waitFor(() => expect(result.current.loading).toBe(false));
    const events = busOn.mock.calls.map((call) => call[0]);
    expect(events).toContain("quick_actions_changed");
    expect(busReconnect).toHaveBeenCalled();
  });

  it("refreshes when the bus event fires", async () => {
    getConfig.mockResolvedValue({ chips: [chip({ id: "x" })] });
    // `on` receives the handler as its second argument and RETURNS an
    // unsubscriber; capture the argument, not the return value.
    let fire: Parameters<typeof eventBus.on>[1] | null = null;
    busOn.mockImplementation((name, cb) => {
      if (name === "quick_actions_changed") fire = cb;
      return () => {};
    });
    const { result } = renderHook(() => useQuickActions());
    await waitFor(() => expect(result.current.chips.map((c) => c.id)).toEqual(["x"]));
    expect(getConfig).toHaveBeenCalledTimes(1);

    getConfig.mockResolvedValue({ chips: [chip({ id: "y" })] });
    await act(async () => {
      expect(fire).toBeTypeOf("function");
      fire?.({} as never);
    });
    await waitFor(() => expect(result.current.chips.map((c) => c.id)).toEqual(["y"]));
    expect(getConfig).toHaveBeenCalledTimes(2);
  });

  it("unsubscribes from the bus on unmount", async () => {
    getConfig.mockResolvedValue({ chips: [] });
    const off = vi.fn();
    busOn.mockImplementation(() => off);
    const { result, unmount } = renderHook(() => useQuickActions());
    await waitFor(() => expect(result.current.loading).toBe(false));
    unmount();
    expect(off).toHaveBeenCalled();
  });

  it("leaves the module cache intact after unmount", async () => {
    getConfig.mockResolvedValue({ chips: [chip({ id: "x" })] });
    const { unmount } = renderHook(() => useQuickActions());
    await waitFor(() => expect(getConfig).toHaveBeenCalled());
    unmount();
    // A later tab reuses the cached chips instead of re-fetching: the strip
    // must not flash empty between tabs.
    const { result } = renderHook(() => useQuickActions());
    expect(result.current.chips.map((c) => c.id)).toEqual(["x"]);
  });
});
