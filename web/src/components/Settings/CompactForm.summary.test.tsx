import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import CompactForm from "./CompactForm";
import { EMPTY_COMPACT_CONFIG } from "../../lib/compactConfig";
import type { CompactConfig } from "../../api/client";

const STORED: CompactConfig = {
  ...EMPTY_COMPACT_CONFIG,
  enabled: true,
  token_threshold: 0.85,
  keep_recent_turns: 3,
  min_messages: 8,
  summary_timeout_seconds: 300,
  summary_max_retries: 2,
  summary_model: "anthropic/claude-haiku-4-5",
};

const hoisted = vi.hoisted(() => {
  const models = [
    { name: "anthropic/claude-haiku-4-5", model: "claude-haiku-4-5", provider: "anthropic", active: false },
    { name: "openai/gpt-4o-mini", model: "gpt-4o-mini", provider: "openai", active: false },
  ];
  const dispatchSpy = vi.fn();
  const api = {
    getCompactConfig: vi.fn(async (): Promise<CompactConfig> => ({ ...EMPTY_COMPACT_CONFIG })),
    setCompactConfig: vi.fn(
      async (patch: Partial<CompactConfig>): Promise<CompactConfig> => ({ ...STORED, ...patch }),
    ),
    listModels: vi.fn(async () => models.map((m) => ({ ...m }))),
    getConfigModel: vi.fn(async () => ({ model: "" })),
    getSmallModel: vi.fn(async () => ({ model: "" })),
    getAdvisor: vi.fn(async () => ({ model: "" })),
    getLocalModelsConfig: vi.fn(async () => ({})),
  };
  return { api, dispatchSpy, models };
});

vi.mock("../../api/client", () => ({ api: hoisted.api }));
vi.mock("../../lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {}, start: () => {}, stop: () => {} },
}));
vi.mock("../../stores/chatStore", () => ({
  // Stable reference: it lands in ModelDialog's open-effect deps.
  useChatSelector: (sel: (s: { model: string; smallModel: string; advisorModel: string }) => unknown) =>
    sel({ model: "", smallModel: "", advisorModel: "" }),
  useChatDispatch: () => hoisted.dispatchSpy,
  getSessionSlice: () => ({ tuiStatus: { main_model: "" } }),
}));

beforeEach(() => {
  vi.clearAllMocks();
  hoisted.api.getCompactConfig.mockImplementation(async () => ({ ...STORED }));
  hoisted.api.setCompactConfig.mockImplementation(
    async (patch: Partial<CompactConfig>) => ({ ...STORED, ...patch }),
  );
});

describe("Settings → Compact summary model", () => {
  it("shows the stored summary model and the auto-fallback hint when unset", async () => {
    hoisted.api.getCompactConfig.mockImplementation(async () => ({ ...STORED, summary_model: "" }));
    render(<CompactForm />);

    expect(await screen.findByText("Not set — uses the small model, then the main model")).toBeTruthy();
  });

  it("saves the picked model as the canonical provider/model id and clears the provider override", async () => {
    render(<CompactForm />);
    await screen.findByText("anthropic/claude-haiku-4-5");

    fireEvent.click(screen.getByRole("button", { name: /change/i }));
    // The dialog belongs to the FORM here, so it must not write on its own.
    fireEvent.click(await screen.findByText("gpt-4o-mini"));
    expect(hoisted.api.setCompactConfig).not.toHaveBeenCalled();

    // The staged value is visible before Save.
    expect(screen.getByText("openai/gpt-4o-mini")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
    await waitFor(() =>
      // Settings is a global form: no host is threaded (1-arg call), unlike
      // the sidebar/dialog paths which pass the session's host.
      expect(hoisted.api.setCompactConfig).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ summary_model: "openai/gpt-4o-mini", summary_provider: "" }),
      ),
    );
  });

  it("clears a hand-edited provider override when a model is picked", async () => {
    // A hand-edited config can carry provider + a BARE model (e.g.
    // provider "openai" + "gpt-4o-mini"). Keeping the provider after a pick of
    // "anthropic/claude-haiku-4-5" would build "openai/anthropic/claude-…",
    // whose first "/" segment IS a provider — so the summary would run on
    // openai under a model it does not serve. The pick must clear it.
    hoisted.api.getCompactConfig.mockImplementation(async () => ({
      ...STORED,
      summary_provider: "openai",
      summary_model: "gpt-4o-mini",
    }));
    render(<CompactForm />);
    await screen.findByText("gpt-4o-mini");

    fireEvent.click(screen.getByRole("button", { name: /change/i }));
    fireEvent.click(await screen.findByText("claude-haiku-4-5"));
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() =>
      expect(hoisted.api.setCompactConfig).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ summary_model: "anthropic/claude-haiku-4-5", summary_provider: "" }),
      ),
    );
  });

  it("sends a complete block, because an ABSENT key is now a silent no-op", async () => {
    render(<CompactForm />);
    await screen.findByText("anthropic/claude-haiku-4-5");
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    // The endpoint merges the keys the body carries, so a complete body still
    // behaves as a replace — and it MUST be complete: a field the form forgot
    // to send would keep its stored value, so the user's edit to it would
    // silently not stick.
    const sent = vi.mocked(hoisted.api.setCompactConfig).mock.calls[0][0];
    expect(Object.keys(sent).sort()).toEqual(Object.keys(EMPTY_COMPACT_CONFIG).sort());
    expect(sent.keep_recent_turns).toBe(3);
  });

  it("adopts the server's merged block, so a concurrent writer's field shows up", async () => {
    hoisted.api.setCompactConfig.mockImplementation(async () => ({ ...STORED, summary_max_retries: 9 }));
    render(<CompactForm />);
    await screen.findByText("anthropic/claude-haiku-4-5");
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    // The form must render the response, not its own pre-save guess.
    expect(await screen.findByLabelText("Summary max retries")).toHaveValue(9);
  });

  it("keeps the Enabled checkbox driving compact.enabled", async () => {
    render(<CompactForm />);
    const box = (await screen.findByLabelText("Enabled")) as HTMLInputElement;
    expect(box.checked).toBe(true);

    fireEvent.click(box);
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() =>
      expect(hoisted.api.setCompactConfig).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ enabled: false }),
      ),
    );
  });

  it("surfaces a load failure instead of rendering a zeroed form", async () => {
    hoisted.api.getCompactConfig.mockRejectedValue(new Error("503 unavailable"));
    render(<CompactForm />);

    expect(await screen.findByText("503 unavailable")).toBeTruthy();
  });
});
