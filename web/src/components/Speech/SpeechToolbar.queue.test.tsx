import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SpeechToolbar, { speechItemLabel } from "./SpeechToolbar";
import type { SpeechItemLabels } from "./SpeechProvider";

vi.mock("../../stores/chatStore", () => ({
  useChatSelector: (sel: (s: { model: string }) => unknown) => sel({ model: "main/model" }),
}));

// A minimal surface over the context. This suite is about what the toolbar SHOWS
// and which controls it exposes; the queue mechanics themselves are covered by
// SpeechProvider.queue.test.tsx.
const speech = {
  config: { engine: "kokoro", voice: "", mode: "manual" },
  status: null,
  isSpeaking: true,
  paused: false,
  error: null,
  currentText: "Some assistant prose.",
  position: 1,
  duration: 4,
  replay: vi.fn(),
  stop: vi.fn(),
  pause: vi.fn(),
  resume: vi.fn(),
  skip: vi.fn(),
  seek: vi.fn(),
  selectEngine: vi.fn(),
  setMode: vi.fn(),
  retry: vi.fn(),
  refresh: vi.fn(),
  toolbarVisible: true,
  setToolbarVisible: vi.fn(),
  toggleToolbar: vi.fn(),
  summaryEnabled: true,
  summaryModel: "",
  setSummaryEnabled: vi.fn(),
  setSummaryModel: vi.fn(),
  updateSummaryConfig: vi.fn(),
  speakMode: "summarised" as const,
  setSpeakMode: vi.fn(),
  queuedCount: 0,
  nowPlaying: null as SpeechItemLabels | null,
  next: vi.fn(),
  clearQueue: vi.fn(),
  speak: vi.fn(),
  host: undefined as string | undefined,
};

vi.mock("./SpeechProvider", async () => {
  const actual = await vi.importActual<typeof import("./SpeechProvider")>("./SpeechProvider");
  return {
    ...actual,
    useSpeech: () => speech,
    playbackLabel: () => "Idle",
  };
});

beforeEach(() => {
  window.localStorage.clear();
  speech.queuedCount = 0;
  speech.nowPlaying = null;
  speech.isSpeaking = true;
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("speechItemLabel", () => {
  it("joins the project and the chat session", () => {
    expect(speechItemLabel({ projectTitle: "ocode", sessionTitle: "Fix the speech queue" }))
      .toBe("ocode · Fix the speech queue");
  });

  it("uses whichever half it has, rather than printing a dangling separator", () => {
    // A remote terminal or a bare test render can know one of the two. A
    // leading or trailing " · " would read as a rendering bug.
    expect(speechItemLabel({ projectTitle: "ocode" })).toBe("ocode");
    expect(speechItemLabel({ sessionTitle: "Session only" })).toBe("Session only");
  });

  it("is null when there is nothing to name", () => {
    expect(speechItemLabel(null)).toBeNull();
    expect(speechItemLabel(undefined)).toBeNull();
    expect(speechItemLabel({})).toBeNull();
  });
});

describe("the now-playing label", () => {
  it("names the project and chat session the audio came from", () => {
    speech.nowPlaying = { projectTitle: "ocode", sessionTitle: "Fix the speech queue" };
    render(<SpeechToolbar />);
    const label = screen.getByTestId("speech-now-playing");
    expect(label).toHaveTextContent("ocode · Fix the speech queue");
  });

  it("puts the full label in the tooltip even when the text is truncated", () => {
    // The bar wraps on narrow viewports and the label truncates, so the
    // tooltip is the only place the untruncated name is reachable.
    speech.nowPlaying = { projectTitle: "a-very-long-project-name", sessionTitle: "a-very-long-session-title" };
    render(<SpeechToolbar />);
    expect(screen.getByTestId("speech-now-playing").getAttribute("title"))
      .toBe("Reading from a-very-long-project-name · a-very-long-session-title");
  });

  it("renders nothing when there is no item to label", () => {
    render(<SpeechToolbar />);
    expect(screen.queryByTestId("speech-now-playing")).toBeNull();
  });
});

describe("the queued badge", () => {
  it("is absent when the queue is empty", () => {
    render(<SpeechToolbar />);
    expect(screen.queryByTestId("speech-queued-count")).toBeNull();
    // The controls go with it: Next/Clear over an empty queue are dead ends.
    expect(screen.queryByLabelText("Next queued message")).toBeNull();
    expect(screen.queryByLabelText("Clear speech queue")).toBeNull();
  });

  it("shows how many messages are still waiting", () => {
    speech.queuedCount = 3;
    render(<SpeechToolbar />);
    const badge = screen.getByTestId("speech-queued-count");
    expect(badge).toHaveTextContent("+3 queued");
    // A screen reader needs the count without the "+" decoration.
    expect(badge.getAttribute("aria-label")).toBe("3 queued");
  });

  it("skips past the current message to the next queued one", () => {
    speech.queuedCount = 2;
    render(<SpeechToolbar />);
    screen.getByLabelText("Next queued message").click();
    expect(speech.next).toHaveBeenCalledOnce();
  });

  it("drops the backlog without cutting off the message playing", () => {
    // Stop and Clear are different promises: Stop silences now, Clear only
    // stops the future. Collapsing them would make Clear unreachable in the
    // one case it exists for.
    speech.queuedCount = 2;
    render(<SpeechToolbar />);
    screen.getByLabelText("Clear speech queue").click();
    expect(speech.clearQueue).toHaveBeenCalledOnce();
    expect(speech.stop).not.toHaveBeenCalled();
  });
});
