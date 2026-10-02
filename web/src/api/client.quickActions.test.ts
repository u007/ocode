import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { api } from "./client";
import type { QuickActionChip, QuickActionsConfig } from "./types";

// Settings is a GLOBAL surface in ocode: config endpoints are not scoped to a
// session or a remote project host, so neither method takes a `host` argument.
// If a host is ever threaded through here, the Settings panel would start
// reading a *remote* project's config — these tests pin the no-host shape.

const QUICK_ACTIONS_URL = "/api/config/ocode/quick-actions";

describe("quick actions config client", () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
    window.history.replaceState(null, "", "/");
  });
  afterEach(() => vi.unstubAllGlobals());

  // fetchJSON reads res.text() first (WebKit-safe parse) and only touches
  // res.json() on the !res.ok branch, so the stub must implement text().
  const ok = (body: unknown) =>
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      text: async () => JSON.stringify(body),
      headers: { get: () => "application/json" },
    } as unknown as Response);

  const urlOf = (call: unknown[]): string => String(call[0]);
  const initOf = (call: unknown[]): RequestInit => (call[1] ?? {}) as RequestInit;

  it("GETs the quick-actions config from the config endpoint and returns the decoded chips", async () => {
    ok({
      chips: [
        { id: "compact", label: "Compact", icon: "archive", message: "/compact", mode: "send", seed: "compact" },
        { id: "mine", label: "Explain", icon: "book-open", message: "explain this", mode: "fill" },
      ],
    });

    const got = await api.getQuickActionsConfig();

    expect(urlOf(fetchMock.mock.calls[0])).toBe(QUICK_ACTIONS_URL);
    // A GET carries no body and no explicit method (default GET), like every
    // other read in this client.
    expect(initOf(fetchMock.mock.calls[0]).method).toBeUndefined();
    expect(got.chips).toHaveLength(2);
    expect(got.chips[0]).toEqual({
      id: "compact",
      label: "Compact",
      icon: "archive",
      message: "/compact",
      mode: "send",
      seed: "compact",
    });
    expect(got.chips[1]).toEqual({
      id: "mine",
      label: "Explain",
      icon: "book-open",
      message: "explain this",
      mode: "fill",
    });
  });

  it("PUTs the quick-actions config with method PUT and a body that round-trips the chips", async () => {
    const chips: QuickActionChip[] = [
      { id: "compact", label: "Compact", icon: "archive", message: "/compact", mode: "send", seed: "compact" },
      { id: "recap", label: "Recap", icon: "file-text", message: "/recap", mode: "send", seed: "recap" },
      { id: "seedless", label: "Draft", icon: "zap", message: "draft it", mode: "fill" },
    ];
    const cfg: QuickActionsConfig = { chips };
    ok({ chips });

    const got = await api.setQuickActionsConfig(cfg);

    const call = fetchMock.mock.calls[0];
    expect(urlOf(call)).toBe(QUICK_ACTIONS_URL);
    expect(initOf(call).method).toBe("PUT");
    expect(JSON.parse(String(initOf(call).body))).toEqual({ chips });
    // The server echoes the stored config; the client hands it straight back.
    expect(got.chips).toEqual(chips);
  });

  it("decodes a chip with no seed field — seed is absent on the wire for custom chips", async () => {
    // Exactly what the Go handler emits for a custom chip: `seed,omitempty`
    // means the key is NOT present at all, so a client that typed seed as
    // required (or defaulted it to a real string) would misread it.
    ok({ chips: [{ id: "mine", label: "Explain", icon: "book-open", message: "explain this", mode: "fill" }] });

    const got = await api.getQuickActionsConfig();
    const chip = got.chips[0];

    expect(chip.seed).toBeUndefined();
    expect("seed" in chip).toBe(false); // absent on the wire, not "" or a default
    expect(chip.mode).toBe("fill");
    // A type-level proof too: this literal omits `seed` entirely and still
    // satisfies QuickActionChip / QuickActionsConfig.
    const typed: QuickActionsConfig = { chips: [chip] };
    expect(typed.chips[0].seed).toBeUndefined();
  });
});