import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import SpeechSummaryForm from "./SpeechSummaryForm";
import type { SpeechSummaryConfig } from "../../api/client";

// The Settings form must go through SpeechProvider — the runtime owner the
// speak path reads — when one is mounted, and read/write the SAME host's block.
// Otherwise a save here leaves the toolbar/sidebar stale until a reload (the
// drift the provider exists to prevent).
const hoisted = vi.hoisted(() => {
  const api = {
    getSpeechSummaryConfig: vi.fn(
      async (_host?: string): Promise<SpeechSummaryConfig> => ({
        model: "anthropic/claude-haiku-4-5",
        enabled: true,
      }),
    ),
    setSpeechSummaryConfig: vi.fn(
      async (patch: Partial<SpeechSummaryConfig>, _host?: string): Promise<SpeechSummaryConfig> => ({
        model: "",
        enabled: true,
        ...patch,
      }),
    ),
  };
  const speech = {
    host: "devbox" as string | undefined,
    updateSummaryConfig: vi.fn(
      async (patch: Partial<SpeechSummaryConfig>): Promise<SpeechSummaryConfig> => ({
        model: "",
        enabled: true,
        ...patch,
      }),
    ),
  };
  return { api, speech, present: true };
});

vi.mock("../../api/client", () => ({ api: hoisted.api }));
// The picker is form-owned here; a no-op keeps this test on the form's wiring.
vi.mock("../Layout/ModelDialog", () => ({ default: () => null }));
vi.mock("../Speech/SpeechProvider", () => ({
  useSpeechOptional: () => (hoisted.present ? hoisted.speech : null),
}));

beforeEach(() => {
  vi.clearAllMocks();
  hoisted.present = true;
});

describe("SpeechSummaryForm", () => {
  it("reads the block from the provider's host", async () => {
    render(<SpeechSummaryForm />);
    await waitFor(() => expect(hoisted.api.getSpeechSummaryConfig).toHaveBeenCalledWith("devbox"));
  });

  it("saves through the provider so the speak path sees it without a reload", async () => {
    render(<SpeechSummaryForm />);
    await screen.findByText("Shorten text before it is spoken");

    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() =>
      expect(hoisted.speech.updateSummaryConfig).toHaveBeenCalledWith({
        model: "anthropic/claude-haiku-4-5",
        enabled: true,
      }),
    );
    // The direct endpoint must NOT also be written: two writers is the drift.
    expect(hoisted.api.setSpeechSummaryConfig).not.toHaveBeenCalled();
  });

  it("falls back to the endpoint when no provider is mounted", async () => {
    hoisted.present = false;
    render(<SpeechSummaryForm />);
    await screen.findByText("Shorten text before it is spoken");

    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(hoisted.api.setSpeechSummaryConfig).toHaveBeenCalled());
    expect(hoisted.speech.updateSummaryConfig).not.toHaveBeenCalled();
  });
});
