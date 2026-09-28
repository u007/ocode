import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import TTSForm from "./TTSForm";
import { DEFAULT_SPEECH_SUMMARY_CONFIG } from "../../lib/speechSummaryConfig";
import type { SpeechSummaryConfig } from "../../api/client";

const hoisted = vi.hoisted(() => {
  const api = {
    getTTSState: vi.fn(async () => ({})),
    ttsAcceptLicense: vi.fn(async () => ({})),
    getSpeechSummaryConfig: vi.fn(async (): Promise<SpeechSummaryConfig> => ({ ...DEFAULT_SPEECH_SUMMARY_CONFIG })),
    setSpeechSummaryConfig: vi.fn(
      async (patch: Partial<SpeechSummaryConfig>): Promise<SpeechSummaryConfig> => ({
        ...DEFAULT_SPEECH_SUMMARY_CONFIG,
        ...patch,
      }),
    ),
    listModels: vi.fn(async () => [
      { name: "anthropic/claude-haiku-4-5", model: "claude-haiku-4-5", provider: "anthropic", active: false },
      { name: "openai/gpt-4o-mini", model: "gpt-4o-mini", provider: "openai", active: false },
    ]),
    getConfigModel: vi.fn(async () => ({ model: "" })),
    getSmallModel: vi.fn(async () => ({ model: "" })),
    getAdvisor: vi.fn(async () => ({ model: "" })),
    getLocalModelsConfig: vi.fn(async () => ({})),
  };
  // The form drives the model through the provider's setter, so the harness
  // records what the form asked for rather than owning the state itself.
  const speech = {
    engines: [] as unknown[],
    config: { engine: "browser-native", voice: "", mode: "manual" as const },
    status: { state: "idle", engine: { availability: "ready", label: "Browser" } },
    error: null,
    currentText: "",
    position: 0,
    duration: 0,
    isSpeaking: false,
    paused: false,
    speak: vi.fn(async () => {}),
    stop: vi.fn(),
    pause: vi.fn(),
    resume: vi.fn(),
    skip: vi.fn(),
    seek: vi.fn(),
    selectEngine: vi.fn(async () => {}),
    setMode: vi.fn(async () => {}),
    retry: vi.fn(async () => {}),
    refresh: vi.fn(async () => {}),
    toolbarVisible: true,
    setToolbarVisible: vi.fn(),
    toggleToolbar: vi.fn(),
    summaryEnabled: true,
    summaryModel: "",
    setSummaryEnabled: vi.fn(async () => {}),
    setSummaryModel: vi.fn(async () => {}),
    speakMode: "summarised" as const,
    setSpeakMode: vi.fn(),
  };
  // STABLE reference: useChatDispatch lands in ModelDialog's open-effect deps,
  // so a fresh vi.fn() per render re-runs the effect forever.
  const dispatchSpy = vi.fn();
  return { api, speech, dispatchSpy };
});

vi.mock("@/api/client", () => ({ api: hoisted.api }));
vi.mock("@/api/types", () => ({}));
vi.mock("../../components/Speech/SpeechProvider", () => ({
  useSpeech: () => hoisted.speech,
  // ModelDialog (rendered for the picker) asks for the optional hook; there is
  // no provider here, so it must resolve to null rather than throw.
  useSpeechOptional: () => null,
}));
vi.mock("../../lib/externalLinks", () => ({
  isHTTPURL: () => false,
  openExternalURL: () => {},
}));
vi.mock("../../lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {}, start: () => {}, stop: () => {} },
}));
vi.mock("../../stores/chatStore", () => ({
  // Stable reference: it is in ModelDialog's open-effect dependency array.
  useChatSelector: (sel: (s: { model: string; smallModel: string; advisorModel: string }) => unknown) =>
    sel({ model: "", smallModel: "", advisorModel: "" }),
  useChatDispatch: () => hoisted.dispatchSpy,
  getSessionSlice: () => ({ tuiStatus: { main_model: "" } }),
}));

beforeEach(() => {
  vi.clearAllMocks();
});

describe("Settings → Speech summary model", () => {
  it("shows the configured model, or the auto fallback when unset", () => {
    render(<TTSForm />);
    // Unset model → the row must explain what the summary actually runs on,
    // not show an empty box.
    expect(screen.getByText("(auto: small model, then main)")).toBeTruthy();
  });

  it("renders a configured model", () => {
    hoisted.speech.summaryModel = "anthropic/claude-haiku-4-5";
    render(<TTSForm />);
    expect(screen.getByText("anthropic/claude-haiku-4-5")).toBeTruthy();
  });

  it("saves a picked model through the provider, not the form's own save", async () => {
    hoisted.speech.summaryModel = "anthropic/claude-haiku-4-5";
    render(<TTSForm />);

    fireEvent.click(screen.getByRole("button", { name: /change/i }));
    fireEvent.click(await screen.findByText("gpt-4o-mini"));

    // The form owns the pick, so it routes through the provider's setter —
    // SpeechProvider is the single owner of the config and must not be bypassed
    // (a direct api write here would leave the toolbar and sidebar stale).
    await waitFor(() => expect(hoisted.speech.setSummaryModel).toHaveBeenCalledWith("openai/gpt-4o-mini"));
  });

  it("toggles the gate through the provider", async () => {
    render(<TTSForm />);
    const box = screen.getByLabelText("Summarise before speaking") as HTMLInputElement;
    expect(box.checked).toBe(true);

    fireEvent.click(box);
    await waitFor(() => expect(hoisted.speech.setSummaryEnabled).toHaveBeenCalledWith(false));
  });

  it("picking a model must not touch the gate (the two controls are independent)", async () => {
    // This is the assertion that catches a real regression: a pick that also
    // flips `enabled` silently re-enables summarising for a user who
    // deliberately turned it off. Checking only the setSummaryModel arity is
    // NOT enough — the extra write comes from a separate call, which is exactly
    // how it survived a first mutation attempt.
    hoisted.speech.summaryModel = "anthropic/claude-haiku-4-5";
    render(<TTSForm />);
    fireEvent.click(screen.getByRole("button", { name: /change/i }));
    fireEvent.click(await screen.findByText("gpt-4o-mini"));

    await waitFor(() => expect(hoisted.speech.setSummaryModel).toHaveBeenCalledWith("openai/gpt-4o-mini"));
    // Model only, one argument…
    expect(hoisted.speech.setSummaryModel.mock.calls[0]).toHaveLength(1);
    // …and no gate write from the pick path at all.
    expect(hoisted.speech.setSummaryEnabled).not.toHaveBeenCalled();
    expect(hoisted.api.setSpeechSummaryConfig).not.toHaveBeenCalledWith(
      expect.objectContaining({ enabled: expect.anything() }),
    );
  });
});
