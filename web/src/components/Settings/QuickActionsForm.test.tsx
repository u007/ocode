import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import QuickActionsForm from "./QuickActionsForm";
import {
  QUICK_ACTION_RESERVED_IDS,
  QUICK_ACTIONS_MAX,
  SEED_CHIPS,
  type QuickActionsState,
} from "@/lib/quickActions";
import type { QuickActionChip } from "@/api/types";

const { saveMock } = vi.hoisted(() => ({ saveMock: vi.fn() }));
const state = vi.hoisted(() => ({ current: null as QuickActionsState | null }));

vi.mock("@/lib/quickActions", async () => {
  const actual = await vi.importActual<typeof import("@/lib/quickActions")>("@/lib/quickActions");
  return {
    ...actual,
    useQuickActions: () => state.current ?? actual.__stateForTest([], null, false),
    saveQuickActions: (...args: unknown[]) => saveMock(...args),
  };
});

const chip = (id: string, label: string): QuickActionChip => ({
  id,
  label,
  icon: "zap",
  message: `run ${id}`,
  mode: "fill",
});

const stateOf = (chips: QuickActionChip[]): QuickActionsState => ({
  chips,
  loading: false,
  error: null,
  revision: chips.map((c) => c.id).join("|"),
});

/** A store already AT the cap, as after the user filled the list in another pane. */
const atCap = (): QuickActionsState =>
  stateOf(Array.from({ length: QUICK_ACTIONS_MAX }, (_, i) => chip(`c${i}`, `C${i}`)));

const labelValues = (): string[] =>
  screen.getAllByLabelText(/chip label/i).map((el) => (el as HTMLInputElement).value);

const savedChips = (): QuickActionChip[] => saveMock.mock.calls[0][0] as QuickActionChip[];

const clickAdd = () => fireEvent.click(screen.getByRole("button", { name: /add chip/i }));
const clickSave = () => fireEvent.click(screen.getByRole("button", { name: /save/i }));

beforeEach(() => {
  saveMock.mockReset().mockResolvedValue(undefined);
  state.current = stateOf([...SEED_CHIPS]);
});

describe("QuickActionsForm", () => {
  it("lists the configured chips in order", () => {
    render(<QuickActionsForm />);
    expect(labelValues()).toEqual(["Compact", "Continue", "Recap"]);
  });

  it("saves an edited label", async () => {
    render(<QuickActionsForm />);
    fireEvent.change(screen.getAllByLabelText(/chip label/i)[0], { target: { value: "Shrink" } });
    clickSave();
    await waitFor(() => expect(saveMock).toHaveBeenCalled());
    expect(savedChips()[0].label).toBe("Shrink");
    // The other rows are untouched: an edit is a patch, not a rewrite.
    expect(labelValues().slice(1)).toEqual(["Continue", "Recap"]);
  });

  it("adds a chip whose minted id is not a reserved starter slug", async () => {
    render(<QuickActionsForm />);
    clickAdd();
    clickSave();
    await waitFor(() => expect(saveMock).toHaveBeenCalled());
    const sent = savedChips();
    expect(sent).toHaveLength(4);
    expect(QUICK_ACTION_RESERVED_IDS).not.toContain(sent[3].id);
    expect(new Set(sent.map((c) => c.id)).size).toBe(4);
  });

  // Review Focus #2: a minted id must also skip an id already in the list, or
  // two rows share a React key and one vanishes with no error.
  it("mints an id that skips one already present in the list", async () => {
    state.current = stateOf([chip("chip-1", "Taken"), ...SEED_CHIPS]);
    render(<QuickActionsForm />);
    clickAdd();
    clickSave();
    await waitFor(() => expect(saveMock).toHaveBeenCalled());
    expect(savedChips()[4].id).toBe("chip-2");
  });

  it("deletes a chip", async () => {
    render(<QuickActionsForm />);
    fireEvent.click(screen.getAllByRole("button", { name: /delete chip/i })[0]);
    clickSave();
    await waitFor(() => expect(saveMock).toHaveBeenCalled());
    const sent = savedChips();
    expect(sent).toHaveLength(2);
    expect(sent.map((c) => c.id)).toEqual(["continue", "recap"]);
  });

  it("disables Add at the cap and states the limit", () => {
    state.current = atCap();
    render(<QuickActionsForm />);
    expect(screen.getByRole("button", { name: /add chip/i })).toBeDisabled();
    expect(screen.getByText(new RegExp(`${QUICK_ACTIONS_MAX}`))).toBeInTheDocument();
  });

  // Review Focus #4: the 21st chip must be EXPLAINED and the draft preserved.
  it("surfaces the server's cap message verbatim and keeps the draft intact", async () => {
    state.current = atCap();
    saveMock.mockRejectedValue(new Error("quick_actions: at most 20 chips are allowed, got 21"));
    render(<QuickActionsForm />);
    clickSave();
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "quick_actions: at most 20 chips are allowed, got 21",
      ),
    );
    // Not just the right COUNT: the exact rows the user was looking at are still
    // there, so nothing has to be retyped.
    expect(labelValues()).toEqual(
      Array.from({ length: QUICK_ACTIONS_MAX }, (_, i) => `C${i}`),
    );
  });

  it("surfaces a whitespace-only message error from the server", async () => {
    saveMock.mockRejectedValue(new Error('quick_actions: chip "compact" must have a message'));
    render(<QuickActionsForm />);
    clickSave();
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent('chip "compact" must have a message'),
    );
    expect(labelValues()).toEqual(["Compact", "Continue", "Recap"]);
  });

  it("offers an icon picker listing exactly the allowlist", () => {
    render(<QuickActionsForm />);
    const first = screen.getAllByLabelText(/chip icon/i)[0] as HTMLSelectElement;
    expect(first.tagName).toBe("SELECT");
    expect(first.options).toHaveLength(24);
  });

  it("offers fill and send as the only modes, in that order", () => {
    render(<QuickActionsForm />);
    const first = screen.getAllByLabelText(/chip mode/i)[0] as HTMLSelectElement;
    expect(first.tagName).toBe("SELECT");
    expect([...first.options].map((o) => o.value)).toEqual(["fill", "send"]);
  });
});
