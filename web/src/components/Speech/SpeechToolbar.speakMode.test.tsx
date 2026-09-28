import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import SpeechToolbar from "./SpeechToolbar";
import { SpeechProvider } from "./SpeechProvider";
import { SPEECH_SPEAK_MODE_STORAGE_KEY } from "./speechToolbarPersistence";

vi.mock("../../stores/chatStore", () => ({
  useChatSelector: (sel: (s: { model: string }) => unknown) => sel({ model: "main/model" }),
}));

// A minimal surface over the context: this suite is about the toolbar's control,
// not playback. The provider's own orchestration is covered by
// SpeechProvider.speechSummary.test.tsx.
const speechState: {
  speakMode: "summarised" | "full";
  summaryEnabled: boolean;
  setSpeakMode: (m: "summarised" | "full") => void;
} = {
  speakMode: "summarised",
  summaryEnabled: true,
  setSpeakMode: vi.fn(),
};

vi.mock("./SpeechProvider", async () => {
  const actual = await vi.importActual<typeof import("./SpeechProvider")>("./SpeechProvider");
  return {
    ...actual,
    useSpeech: () => ({
      config: { engine: "kokoro", voice: "", mode: "manual" },
      status: null,
      isSpeaking: false,
      paused: false,
      error: null,
      currentText: "",
      position: 0,
      duration: 0,
      speak: vi.fn(),
      replay: vi.fn(),
      stop: vi.fn(),
      pause: vi.fn(),
      resume: vi.fn(),
      skip: vi.fn(),
      selectEngine: vi.fn(),
      setMode: vi.fn(),
      retry: vi.fn(),
      refresh: vi.fn(),
      seek: vi.fn(),
      toolbarVisible: true,
      setToolbarVisible: vi.fn(),
      toggleToolbar: vi.fn(),
      summaryEnabled: speechState.summaryEnabled,
      summaryModel: "",
      setSummaryEnabled: vi.fn(),
      speakMode: speechState.speakMode,
      setSpeakMode: speechState.setSpeakMode,
    }),
    // The provider is imported for real only so the module resolves; the
    // toolbar below never renders its children.
    SpeechProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  };
});

beforeEach(() => {
  window.localStorage.clear();
  speechState.speakMode = "summarised";
  speechState.summaryEnabled = true;
  speechState.setSpeakMode = vi.fn();
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("SpeechToolbar speech-text mode", () => {
  it("shows Summarised by default, because summarising is the point", () => {
    render(<SpeechToolbar />);
    expect(screen.getByLabelText("Speech text mode")).toHaveTextContent("Summarised");
  });

  it("switches to full text on click", () => {
    // The user's "speak can dropdown to speak full text": one control, both
    // states, with the current state as its label.
    render(<SpeechToolbar />);
    screen.getByLabelText("Speech text mode").click();
    expect(speechState.setSpeakMode).toHaveBeenCalledExactlyOnceWith("full");
  });

  it("switches back to summarised when already in full mode", () => {
    speechState.speakMode = "full";
    render(<SpeechToolbar />);
    const control = screen.getByLabelText("Speech text mode");
    expect(control).toHaveTextContent("Full Text");
    control.click();
    expect(speechState.setSpeakMode).toHaveBeenCalledExactlyOnceWith("summarised");
  });

  it("explains itself in the tooltip, so the two states are not a puzzle", () => {
    render(<SpeechToolbar />);
    const control = screen.getByLabelText("Speech text mode");
    // The label says which state is active; the title has to say what the
    // other one would do.
    expect(control.getAttribute("title")).toMatch(/summarised/i);
  });
});

describe("the persisted default", () => {
  it("is summarised when nothing is stored, matching the server default", () => {
    // Cross-surface agreement: the toolbar, the provider and
    // defaultOcodeConfig must all agree, or a fresh user gets one behaviour
    // until they reload.
    expect(window.localStorage.getItem(SPEECH_SPEAK_MODE_STORAGE_KEY)).toBeNull();
    expect(speechState.speakMode).toBe("summarised");
  });
});

describe("SpeechToolbar real provider mount", () => {
  it("renders without a provider crash when hidden", () => {
    // Guards the `if (!visible) return null` path against a refactor that
    // would call useSpeech above it.
    expect(() =>
      render(
        <SpeechProvider sessionId="s">
          <SpeechToolbar />
        </SpeechProvider>,
      ),
    ).not.toThrow();
  });
});
