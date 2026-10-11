import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import PulseAssistantForm from "./PulseAssistantForm";

const mockGetPrompt = vi.fn();
const mockSetPrompt = vi.fn();
const mockGetModel = vi.fn();
const mockSetModel = vi.fn();
vi.mock("../../api/client", () => ({
  api: {
    getPulseSystemPrompt: (...a: unknown[]) => mockGetPrompt(...a),
    setPulseSystemPrompt: (...a: unknown[]) => mockSetPrompt(...a),
    getPulseModel: (...a: unknown[]) => mockGetModel(...a),
    setPulseModel: (...a: unknown[]) => mockSetModel(...a),
  },
}));
vi.mock("../Layout/ModelDialog", () => ({
  default: ({
    open,
    purpose,
    onPick,
  }: {
    open: boolean;
    purpose: string;
    onPick: (p: string, id: string) => void;
  }) =>
    open ? (
      <div data-testid="model-dialog" data-purpose={purpose}>
        <button onClick={() => onPick(purpose, "openrouter/vendor/model-x:free")}>pick x</button>
      </div>
    ) : null,
}));

const prompt = () => screen.getByLabelText("System prompt") as HTMLTextAreaElement;

beforeEach(() => {
  for (const m of [mockGetPrompt, mockSetPrompt, mockGetModel, mockSetModel]) m.mockReset();
  mockGetPrompt.mockResolvedValue({ prompt: "be terse", default: "BUILT-IN PROMPT" });
  mockGetModel.mockResolvedValue({ model: "" });
  mockSetPrompt.mockResolvedValue({});
  mockSetModel.mockResolvedValue({});
  vi.spyOn(console, "error").mockImplementation(() => {});
});
afterEach(() => vi.restoreAllMocks());

describe("PulseAssistantForm", () => {
  it("loads the override prompt, the default and the model slot", async () => {
    mockGetModel.mockResolvedValue({ model: "openrouter/vendor/model-x:free" });
    render(<PulseAssistantForm />);

    await waitFor(() => expect(prompt().value).toBe("be terse"));
    expect(screen.getByTestId("pulse-assistant-default-prompt")).toHaveTextContent("BUILT-IN PROMPT");
    const model = screen.getByTestId("pulse-assistant-form-model");
    expect(model).toHaveTextContent("model-x:free");
    expect(model.title).toBe("openrouter/vendor/model-x:free");
  });

  it("says the default model applies when no slot is set", async () => {
    render(<PulseAssistantForm />);
    await waitFor(() => expect(prompt().value).toBe("be terse"));

    expect(screen.getByTestId("pulse-assistant-form-model")).toHaveTextContent(/uses the server default/i);
    expect(screen.getByRole("button", { name: "Use default" })).toBeDisabled();
  });

  it("saves the edited prompt and then shows what the server holds, not the draft", async () => {
    render(<PulseAssistantForm />);
    await waitFor(() => expect(prompt().value).toBe("be terse"));
    const save = screen.getByRole("button", { name: "Save" });
    expect(save).toBeDisabled();

    fireEvent.change(prompt(), { target: { value: "be verbose" } });
    // The server normalises it; the form must show the server's value.
    mockGetPrompt.mockResolvedValue({ prompt: "be verbose\n", default: "BUILT-IN PROMPT" });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mockSetPrompt).toHaveBeenCalledWith("be verbose"));
    await waitFor(() => expect(prompt().value).toBe("be verbose\n"));
    expect(screen.getByRole("status")).toHaveTextContent("Saved");
  });

  it("resets to the default by clearing the override", async () => {
    render(<PulseAssistantForm />);
    await waitFor(() => expect(prompt().value).toBe("be terse"));

    mockGetPrompt.mockResolvedValue({ prompt: "", default: "BUILT-IN PROMPT" });
    fireEvent.click(screen.getByRole("button", { name: "Reset to default" }));

    await waitFor(() => expect(mockSetPrompt).toHaveBeenCalledWith(""));
    await waitFor(() => expect(prompt().value).toBe(""));
    expect(screen.getByRole("button", { name: "Reset to default" })).toBeDisabled();
  });

  it("shows a failed save and keeps the draft", async () => {
    mockSetPrompt.mockRejectedValue(new Error("500 boom"));
    render(<PulseAssistantForm />);
    await waitFor(() => expect(prompt().value).toBe("be terse"));

    fireEvent.change(prompt(), { target: { value: "edited" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByTestId("pulse-assistant-form-error")).toHaveTextContent("500 boom");
    expect(prompt().value).toBe("edited");
    expect(console.error).toHaveBeenCalled();
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("shows a load failure instead of an empty form", async () => {
    mockGetPrompt.mockRejectedValue(new Error("503"));
    render(<PulseAssistantForm />);

    expect(await screen.findByTestId("pulse-assistant-form-error")).toHaveTextContent("503");
  });

  it("picks a model through the pulse chooser, then shows the server's slot", async () => {
    render(<PulseAssistantForm />);
    await waitFor(() => expect(prompt().value).toBe("be terse"));

    fireEvent.click(screen.getByRole("button", { name: "Change…" }));
    expect(screen.getByTestId("model-dialog")).toHaveAttribute("data-purpose", "pulse");
    mockGetModel.mockResolvedValue({ model: "openrouter/vendor/model-x:free" });
    fireEvent.click(screen.getByText("pick x"));

    await waitFor(() => expect(mockSetModel).toHaveBeenCalledWith("openrouter/vendor/model-x:free"));
    await waitFor(() => expect(screen.getByTestId("pulse-assistant-form-model")).toHaveTextContent("model-x:free"));
  });

  it("clears the model slot with Use default", async () => {
    mockGetModel.mockResolvedValue({ model: "a/b" });
    render(<PulseAssistantForm />);
    await waitFor(() => expect(screen.getByTestId("pulse-assistant-form-model")).toHaveTextContent("b"));

    mockGetModel.mockResolvedValue({ model: "" });
    fireEvent.click(screen.getByRole("button", { name: "Use default" }));

    await waitFor(() => expect(mockSetModel).toHaveBeenCalledWith(""));
    await waitFor(() =>
      expect(screen.getByTestId("pulse-assistant-form-model")).toHaveTextContent(/uses the server default/i),
    );
  });
});
