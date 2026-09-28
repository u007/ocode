import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi, type MockedFunction } from "vitest";
import { api } from "../../api/client";
import { SpeechProvider, useSpeech } from "./SpeechProvider";
import { sanitizeSpeechText } from "./speechUtils";
import {
  SPEECH_SPEAK_MODE_STORAGE_KEY,
  loadSpeechSpeakMode,
} from "./speechToolbarPersistence";

// The provider only reads the session's model for the TTS voice-map lookup, so
// a stub selector keeps this suite off the real chat store.
vi.mock("../../stores/chatStore", () => ({
  useChatSelector: (selector: (state: { model: string }) => unknown) =>
    selector({ model: "main/model" }),
}));

const LOCAL_ENGINE = {
  engine: "kokoro" as const,
  voice: "",
  mode: "manual" as const,
};

/** The text every case feeds in: prose plus a fenced code block, which is the
 *  exact shape summarising exists to fix. */
const SOURCE =
  "Here is the retry loop.\n\n```go\nretries = 3\nfor retries > 0 {\n  call()\n}\n```\n\nThat is all.";

/** What the fallback path actually reaches the engine: `speakNow` sanitises
 *  markdown before speaking, so the fences are already gone even when no
 *  summariser runs. Asserting raw SOURCE would be asserting a stage that never
 *  reaches the synthesiser. */
const SANITISED_SOURCE = sanitizeSpeechText(SOURCE);

let summarizeSpeech: MockedFunction<typeof api.summarizeSpeech>;
let ttsSpeak: MockedFunction<typeof api.ttsSpeak>;

function Consumer() {
  const speech = useSpeech();
  return (
    <div>
      <button type="button" onClick={() => void speech.speak(SOURCE)}>
        speak
      </button>
      <span data-testid="mode">{speech.speakMode}</span>
      <span data-testid="enabled">{String(speech.summaryEnabled)}</span>
      <span data-testid="current">{speech.currentText}</span>
    </div>
  );
}

async function renderProvider(props: { sessionId?: string; host?: string } = {}) {
  render(
    <SpeechProvider {...props}>
      <Consumer />
    </SpeechProvider>,
  );
  // Wait for the summary config read to settle either way, so no assertion can
  // race the initial fetch.
  await waitFor(() => {
    expect(vi.mocked(api.getSpeechSummaryConfig).mock.calls.length).toBeGreaterThan(0);
  });
}

beforeEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
  summarizeSpeech = vi.fn().mockResolvedValue({ summary: "Here is the retry loop, retried three times." });
  ttsSpeak = vi.fn().mockResolvedValue({ status: "ready" });
  vi.spyOn(api, "getTTSEngines").mockResolvedValue({ engines: [] });
  vi.spyOn(api, "getTTSConfig").mockResolvedValue(LOCAL_ENGINE);
  vi.spyOn(api, "getTTSStatus").mockResolvedValue({
    engine: { id: "kokoro", label: "Kokoro", availability: "available" },
    playback: { status: "idle", generation: 0 },
  } as never);
  vi.spyOn(api, "getSpeechSummaryConfig").mockResolvedValue({ model: "", enabled: true });
  vi.spyOn(api, "summarizeSpeech").mockImplementation(summarizeSpeech);
  vi.spyOn(api, "ttsSpeak").mockImplementation(ttsSpeak);
  vi.spyOn(api, "ttsStop").mockResolvedValue({ status: "idle", generation: 0 } as never);
});

describe("speech summarising", () => {
  it("speaks the SUMMARY, not the raw message with its code block", async () => {
    await renderProvider({ sessionId: "sess-1" });

    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });

    await waitFor(() => expect(ttsSpeak).toHaveBeenCalled());
    // The whole point: the code never reaches the synthesiser.
    expect(ttsSpeak.mock.calls[0][0]).toBe("Here is the retry loop, retried three times.");
    expect(ttsSpeak.mock.calls[0][0]).not.toContain("retries = 3");
  });

  it("sends the source text and the session to the summary endpoint", async () => {
    const spy = vi.mocked(api.summarizeSpeech);
    await renderProvider({ sessionId: "sess-1" });

    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });

    await waitFor(() => expect(spy).toHaveBeenCalled());
    expect(spy.mock.calls[0][0]).toBe("sess-1");
    // The endpoint gets the RAW message, fences and all: the summariser is the
    // thing that decides what to drop, not the client.
    expect(spy.mock.calls[0][1]).toBe(SOURCE);
  });

  it("falls back to the full text when the summariser fails", async () => {
    // Speech must never be blocked because a side task could not run.
    summarizeSpeech.mockRejectedValue(new Error("summariser exploded"));
    await renderProvider({ sessionId: "sess-1" });

    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });

    await waitFor(() => expect(ttsSpeak).toHaveBeenCalled());
    expect(ttsSpeak.mock.calls[0][0]).toBe(SANITISED_SOURCE);
  });

  it("falls back to the full text when the summary comes back empty", async () => {
    // An empty summary means the side task produced nothing usable; speaking it
    // would produce silence, which is worse than reading the code aloud.
    summarizeSpeech.mockResolvedValue({ summary: "" });
    await renderProvider({ sessionId: "sess-1" });

    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });

    await waitFor(() => expect(ttsSpeak).toHaveBeenCalled());
    expect(ttsSpeak.mock.calls[0][0]).toBe(SANITISED_SOURCE);
  });

  it("falls back to the full text when the summary is only whitespace", async () => {
    summarizeSpeech.mockResolvedValue({ summary: "   \n  " });
    await renderProvider({ sessionId: "sess-1" });

    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });

    await waitFor(() => expect(ttsSpeak).toHaveBeenCalled());
    expect(ttsSpeak.mock.calls[0][0]).toBe(SANITISED_SOURCE);
  });

  it("speaks the full text without calling the summariser when the gate is off", async () => {
    vi.mocked(api.getSpeechSummaryConfig).mockResolvedValue({ model: "", enabled: false });
    await renderProvider({ sessionId: "sess-1" });
    await waitFor(() => expect(screen.getByTestId("enabled")).toHaveTextContent("false"));

    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });

    await waitFor(() => expect(ttsSpeak).toHaveBeenCalled());
    expect(summarizeSpeech).not.toHaveBeenCalled();
    expect(ttsSpeak.mock.calls[0][0]).toBe(SANITISED_SOURCE);
  });

  it("speaks the full text without calling the summariser when no session is bound", async () => {
    // The endpoint is session-scoped (it reuses the session's agent), so with
    // no session there is nothing to summarise against.
    await renderProvider();

    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });

    await waitFor(() => expect(ttsSpeak).toHaveBeenCalled());
    expect(summarizeSpeech).not.toHaveBeenCalled();
    expect(ttsSpeak.mock.calls[0][0]).toBe(SANITISED_SOURCE);
  });

  it("reads the summary gate and the model block from the session's host", async () => {
    // A remote SSH project summarises on the HOST; reading the local server's
    // block would show and gate the wrong configuration.
    const getSpy = vi.mocked(api.getSpeechSummaryConfig);
    await renderProvider({ sessionId: "sess-1", host: "james@217.216.72.49" });

    await waitFor(() => expect(getSpy).toHaveBeenCalled());
    expect(getSpy).toHaveBeenCalledWith("james@217.216.72.49");
  });

  it("threads the host into the summary request itself", async () => {
    const spy = vi.mocked(api.summarizeSpeech);
    await renderProvider({ sessionId: "sess-1", host: "james@217.216.72.49" });

    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });

    await waitFor(() => expect(spy).toHaveBeenCalled());
    expect(spy.mock.calls[0][2]).toBe("james@217.216.72.49");
  });

  it("defaults to the summarised speak mode", async () => {
    await renderProvider({ sessionId: "sess-1" });
    expect(screen.getByTestId("mode")).toHaveTextContent("summarised");
  });
});

describe("when the summary config cannot be read", () => {
  it("keeps summarising ON, because that is the server default", async () => {
    // Optimistic-on matters here: if a failed read flipped the gate off, a
    // transient /api hiccup would silently start reading code blocks aloud
    // again -- exactly the failure this feature exists to remove, and one the
    // user never asked for and would struggle to attribute.
    vi.mocked(api.getSpeechSummaryConfig).mockRejectedValue(new Error("502 from the proxy"));
    await renderProvider({ sessionId: "sess-1" });

    expect(screen.getByTestId("enabled")).toHaveTextContent("true");
  });

  it("still summarises, rather than falling back to the full text", async () => {
    // The config read and the summary request are independent: one failing
    // must not stop the other from being attempted.
    vi.mocked(api.getSpeechSummaryConfig).mockRejectedValue(new Error("502 from the proxy"));
    await renderProvider({ sessionId: "sess-1" });

    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });

    await waitFor(() => expect(ttsSpeak).toHaveBeenCalled());
    expect(summarizeSpeech).toHaveBeenCalled();
    expect(ttsSpeak.mock.calls[0][0]).toBe("Here is the retry loop, retried three times.");
  });
});

describe("the speak-full-text opt-out", () => {
  // The user-facing promise: speech is summarised BY DEFAULT, but one dropdown
  // switch reads the message verbatim without disabling the feature.
  beforeEach(() => {
    window.localStorage.setItem(SPEECH_SPEAK_MODE_STORAGE_KEY, JSON.stringify("full"));
  });

  it("starts in full mode when that was stored", async () => {
    await renderProvider({ sessionId: "sess-1" });
    expect(screen.getByTestId("mode")).toHaveTextContent("full");
  });

  it("does not call the summariser, and speaks the full text", async () => {
    // Opting out must be a real bypass, not merely a display change: spending a
    // model call to produce text that is then discarded would be pure waste.
    await renderProvider({ sessionId: "sess-1" });

    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });

    await waitFor(() => expect(ttsSpeak).toHaveBeenCalled());
    expect(summarizeSpeech).not.toHaveBeenCalled();
    expect(ttsSpeak.mock.calls[0][0]).toBe(SANITISED_SOURCE);
  });

  it("leaves the server-side gate ON, so one message does not disable the feature", async () => {
    // speakMode is a client preference; summaryEnabled is the persisted flag.
    // Conflating them would let a per-message choice silently reconfigure every
    // session and every device.
    await renderProvider({ sessionId: "sess-1" });
    expect(screen.getByTestId("mode")).toHaveTextContent("full");
    expect(screen.getByTestId("enabled")).toHaveTextContent("true");
  });
});

describe("setSpeakMode", () => {
  function ModeConsumer() {
    const speech = useSpeech();
    return (
      <div>
        <button type="button" onClick={() => speech.setSpeakMode("full")}>
          use full
        </button>
        <button type="button" onClick={() => speech.setSpeakMode("summarised")}>
          use summarised
        </button>
        <button type="button" onClick={() => void speech.speak(SOURCE)}>
          speak
        </button>
        <span data-testid="mode">{speech.speakMode}</span>
      </div>
    );
  }

  it("switches the funnel and persists the choice", async () => {
    render(
      <SpeechProvider sessionId="sess-1">
        <ModeConsumer />
      </SpeechProvider>,
    );
    await waitFor(() => expect(screen.getByTestId("mode")).toHaveTextContent("summarised"));

    await act(async () => {
      screen.getByRole("button", { name: "use full" }).click();
    });

    expect(screen.getByTestId("mode")).toHaveTextContent("full");
    // Survives a reload, which is what makes the dropdown's setting sticky.
    expect(loadSpeechSpeakMode()).toBe("full");

    await act(async () => {
      screen.getByRole("button", { name: "use summarised" }).click();
    });
    expect(loadSpeechSpeakMode()).toBe("summarised");
  });

  it("actually bypasses the summariser once switched to full", async () => {
    // Pins the wiring, not just the label: a dropdown that changed the label but
    // not the behaviour would pass the test above.
    render(
      <SpeechProvider sessionId="sess-1">
        <ModeConsumer />
      </SpeechProvider>,
    );
    await waitFor(() => expect(screen.getByTestId("mode")).toHaveTextContent("summarised"));

    await act(async () => {
      screen.getByRole("button", { name: "use full" }).click();
    });
    ttsSpeak.mockClear();
    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });

    await waitFor(() => expect(ttsSpeak).toHaveBeenCalled());
    expect(summarizeSpeech).not.toHaveBeenCalled();
  });
});

// The speak funnel is asynchronous: it awaits the summary model (up to 60s)
// before playback starts. These pin the two things that await must not break —
// a Stop during the wait, and two requests resolving out of order — plus the
// toolbar's Replay, which must read text that is ALREADY prepared.
describe("speak cancellation and replay", () => {
  const REPLAY_TEXT = "Already summarised prose.";

  function FunnelConsumer() {
    const speech = useSpeech();
    return (
      <div>
        <button type="button" onClick={() => void speech.speak(SOURCE)}>
          speak
        </button>
        <button type="button" onClick={() => void speech.replay(REPLAY_TEXT)}>
          replay
        </button>
        <button type="button" onClick={() => speech.stop()}>
          stop
        </button>
        <span data-testid="current">{speech.currentText}</span>
      </div>
    );
  }

  function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((res) => {
      resolve = res;
    });
    return { promise, resolve };
  }

  async function renderFunnel() {
    render(
      <SpeechProvider sessionId="sess-1">
        <FunnelConsumer />
      </SpeechProvider>,
    );
    await waitFor(() =>
      expect(vi.mocked(api.getSpeechSummaryConfig).mock.calls.length).toBeGreaterThan(0),
    );
  }

  it("does not start playback when Stop is pressed while the summary is pending", async () => {
    const pending = deferred<{ summary: string }>();
    summarizeSpeech.mockReturnValueOnce(pending.promise);
    await renderFunnel();

    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });
    await act(async () => {
      screen.getByRole("button", { name: "stop" }).click();
    });
    // The summary lands AFTER the Stop; it must be discarded.
    await act(async () => {
      pending.resolve({ summary: "a summary nobody wants any more" });
    });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(ttsSpeak).not.toHaveBeenCalled();
  });

  it("plays only the newest message when two summaries resolve out of order", async () => {
    const older = deferred<{ summary: string }>();
    const newer = deferred<{ summary: string }>();
    summarizeSpeech.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise);
    await renderFunnel();

    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });
    await act(async () => {
      screen.getByRole("button", { name: "speak" }).click();
    });

    // The newer request resolves first and starts playing...
    await act(async () => {
      newer.resolve({ summary: "newer text" });
    });
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(1));
    // ...then the older one lands and must be dropped, not played over it.
    await act(async () => {
      older.resolve({ summary: "older text" });
    });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(ttsSpeak).toHaveBeenCalledTimes(1);
    expect(ttsSpeak.mock.calls[0][0]).toBe("newer text");
  });

  it("replays the prepared text without re-summarising it", async () => {
    await renderFunnel();

    await act(async () => {
      screen.getByRole("button", { name: "replay" }).click();
    });

    await waitFor(() => expect(ttsSpeak).toHaveBeenCalled());
    // Replaying the already-summarised text must not spend another LLM call.
    expect(summarizeSpeech).not.toHaveBeenCalled();
    expect(ttsSpeak.mock.calls[0][0]).toBe(REPLAY_TEXT);
  });
});
