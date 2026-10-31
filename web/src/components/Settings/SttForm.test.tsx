import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import SttForm from "./SttForm";
import { api } from "@/api/client";
import type { STTSettings } from "@/api/types";

const initial: STTSettings = {
  selected: "tiny",
  remove_fillers: true,
  models: [
    { id: "tiny", label: "Tiny", engine: "local", languages: "English", size_mb: 75, description: "Fastest local model", available: true },
    { id: "base", label: "Base", engine: "local", languages: "99 languages", size_mb: 142, description: "Better accuracy", available: true },
    { id: "whisper-1", label: "Whisper (OpenAI)", engine: "openai", languages: "99 languages", description: "Hosted transcription", available: false, reason: "Add an OpenAI API key to use this model." },
  ],
};

beforeEach(() => {
  vi.spyOn(api, "getSTT").mockResolvedValue(structuredClone(initial));
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("SttForm", () => {
  it("lists each model with engine, languages, size, description and the selected one", async () => {
    render(<SttForm />);
    const tiny = within(await screen.findByTestId("stt-model-tiny"));
    expect(tiny.getByText("Tiny")).toBeDefined();
    expect(tiny.getByText(/On this machine/)).toBeDefined();
    expect(tiny.getByText(/English/)).toBeDefined();
    expect(tiny.getByText(/75 MB/)).toBeDefined();
    expect(tiny.getByText("Fastest local model")).toBeDefined();
    expect((tiny.getByRole("radio") as HTMLInputElement).checked).toBe(true);

    const openai = within(screen.getByTestId("stt-model-whisper-1"));
    expect(openai.getByText(/^OpenAI ·/)).toBeDefined();
    expect(openai.getByText("Add an OpenAI API key to use this model.")).toBeDefined();

    expect(screen.getByText(/Selected model:/).textContent).toContain("Tiny");
  });

  it("disables a model that is not available and shows why", async () => {
    render(<SttForm />);
    const radio = (await screen.findByTestId("stt-model-whisper-1")).querySelector("input") as HTMLInputElement;
    expect(radio.disabled).toBe(true);
  });

  it("selecting a model PUTs {model} and renders the list from the response", async () => {
    const put = vi.spyOn(api, "setSTTModel").mockResolvedValue({
      ...structuredClone(initial),
      selected: "base",
    });
    render(<SttForm />);
    const base = (await screen.findByTestId("stt-model-base")).querySelector("input") as HTMLInputElement;

    fireEvent.click(base);

    await waitFor(() => expect(put).toHaveBeenCalledWith("base"));
    await waitFor(() => expect(base.checked).toBe(true));
    expect((screen.getByTestId("stt-model-tiny").querySelector("input") as HTMLInputElement).checked).toBe(false);
    expect(screen.getByText(/Selected model:/).textContent).toContain("Base");
  });

  it("a rejected selection is shown and the previous choice stays selected", async () => {
    vi.spyOn(api, "setSTTModel").mockRejectedValue(new Error("unknown model"));
    render(<SttForm />);
    const base = (await screen.findByTestId("stt-model-base")).querySelector("input") as HTMLInputElement;

    fireEvent.click(base);

    expect(await screen.findByText("unknown model")).toBeDefined();
    expect((screen.getByTestId("stt-model-tiny").querySelector("input") as HTMLInputElement).checked).toBe(true);
  });

  it("shows a load error instead of an empty list", async () => {
    vi.spyOn(api, "getSTT").mockRejectedValue(new Error("boom"));
    render(<SttForm />);
    expect(await screen.findByText(/Speech-to-text settings unavailable: boom/)).toBeDefined();
  });

  it("shows the filler-word cleanup setting and defaults it to on", async () => {
    render(<SttForm />);
    const box = (await screen.findByTestId("stt-remove-fillers")).querySelector("input") as HTMLInputElement;
    expect(box.type).toBe("checkbox");
    expect(box.checked).toBe(true);
  });

  it("toggling filler cleanup PUTs only the setting and renders the response", async () => {
    const put = vi.spyOn(api, "setSTTFillers").mockResolvedValue({
      ...structuredClone(initial),
      remove_fillers: false,
    });
    render(<SttForm />);
    const box = (await screen.findByTestId("stt-remove-fillers")).querySelector("input") as HTMLInputElement;

    fireEvent.click(box);

    await waitFor(() => expect(put).toHaveBeenCalledWith(false));
    await waitFor(() => expect(box.checked).toBe(false));
    // The model is untouched: the toggle sends only its own field.
    expect((screen.getByTestId("stt-model-tiny").querySelector("input") as HTMLInputElement).checked).toBe(true);
  });

  it("a rejected filler toggle keeps the previous value and shows the error", async () => {
    vi.spyOn(api, "setSTTFillers").mockRejectedValue(new Error("could not save"));
    render(<SttForm />);
    const box = (await screen.findByTestId("stt-remove-fillers")).querySelector("input") as HTMLInputElement;

    fireEvent.click(box);

    expect(await screen.findByText("could not save")).toBeDefined();
    expect(box.checked).toBe(true);
  });
});
