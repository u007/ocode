import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import PromptsForm from "./PromptsForm";
import type { PromptOverrideEntry } from "../../api/types";

const mockGetPrompts = vi.fn();
const mockSetPrompt = vi.fn();
vi.mock("../../api/client", () => ({
  api: {
    getPromptOverrides: (...a: unknown[]) => mockGetPrompts(...a),
    setPromptOverride: (...a: unknown[]) => mockSetPrompt(...a),
  },
}));

const compaction: PromptOverrideEntry = {
  key: "compaction",
  label: "Compaction summariser",
  description: "Writes the summary that replaces older turns.",
  kind: "Helper task",
  enabled: false,
  text: "",
};
const title: PromptOverrideEntry = {
  key: "title",
  label: "Session titles",
  description: "Names a new session.",
  kind: "Helper task",
  enabled: true,
  text: "Keep titles under five words.",
};

const card = (key: string) => within(screen.getByTestId(`prompt-card-${key}`));
const saveButton = (key: string) => card(key).getByRole("button", { name: "Save" }) as HTMLButtonElement;
const resetButton = (key: string) => card(key).getByRole("button", { name: "Reset" }) as HTMLButtonElement;

beforeEach(() => {
  mockGetPrompts.mockReset();
  mockSetPrompt.mockReset();
  mockGetPrompts.mockResolvedValue({ prompts: [compaction, title] });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("PromptsForm", () => {
  it("renders one card per prompt with its label and kind, not the prompt text", async () => {
    render(<PromptsForm />);
    expect(await screen.findByText("Compaction summariser")).toBeTruthy();
    expect(screen.getByText("Session titles")).toBeTruthy();
    expect(card("compaction").getByText("Helper task")).toBeTruthy();
    expect(card("title").getByText("Helper task")).toBeTruthy();
    expect(screen.queryByText(/Show built-in prompt/)).toBeNull();
  });

  it("enables Save only after the checkbox or text changes, then saves the draft", async () => {
    mockSetPrompt.mockResolvedValue({ ...compaction, enabled: true, text: "Use bullets." });
    render(<PromptsForm />);
    await screen.findByText("Compaction summariser");

    expect(saveButton("compaction").disabled).toBe(true);

    fireEvent.click(card("compaction").getByLabelText("Enable custom instructions"));
    fireEvent.change(card("compaction").getByPlaceholderText("Appended to the built-in prompt when enabled."), {
      target: { value: "Use bullets." },
    });
    expect(saveButton("compaction").disabled).toBe(false);

    fireEvent.click(saveButton("compaction"));
    await waitFor(() =>
      expect(mockSetPrompt).toHaveBeenCalledWith("compaction", { enabled: true, text: "Use bullets." }),
    );
    await waitFor(() => expect(saveButton("compaction").disabled).toBe(true));
  });

  it("Reset saves enabled=false with empty text", async () => {
    mockSetPrompt.mockResolvedValue({ ...title, enabled: false, text: "" });
    render(<PromptsForm />);
    await screen.findByText("Session titles");

    expect(resetButton("title").disabled).toBe(false);
    fireEvent.click(resetButton("title"));
    await waitFor(() => expect(mockSetPrompt).toHaveBeenCalledWith("title", { enabled: false, text: "" }));
    await waitFor(() => expect(resetButton("title").disabled).toBe(true));
  });

  it("shows an error and logs it when loading fails", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    mockGetPrompts.mockRejectedValue(new Error("boom"));
    render(<PromptsForm />);
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByText("Loading failed: boom")).toBeTruthy();
    expect(consoleError).toHaveBeenCalled();
  });
});
