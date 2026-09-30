import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi, type MockedFunction } from "vitest";
import { api } from "../../api/client";
import { SpeechProvider, requestSpeech, useSpeech, type SpeechOutcome } from "./SpeechProvider";

// The provider only reads the session's model for the TTS voice-map lookup, so
// a stub selector keeps this suite off the real chat store.
vi.mock("../../stores/chatStore", () => ({
  useChatSelector: (selector: (state: { model: string }) => unknown) =>
    selector({ model: "main/model" }),
}));

const LOCAL_ENGINE = { engine: "kokoro" as const, voice: "", mode: "manual" as const };

let summarizeSpeech: MockedFunction<typeof api.summarizeSpeech>;
let ttsSpeak: MockedFunction<typeof api.ttsSpeak>;

// ── Fake <audio> ────────────────────────────────────────────────────────────
// jsdom's HTMLMediaElement.play() is not implemented and there is no way to
// make a real clip end on demand, so playback completion has to be driven by
// hand. The queue advances from the `ended` event, which is exactly the seam
// these tests need to step item by item.
class FakeAudio {
  static instances: FakeAudio[] = [];
  currentTime = 0;
  duration = 10;
  onended: (() => void) | null = null;
  onerror: (() => void) | null = null;
  ontimeupdate: (() => void) | null = null;
  onloadedmetadata: (() => void) | null = null;
  pause = vi.fn();
  removeAttribute = vi.fn();
  play = vi.fn().mockResolvedValue(undefined);
  constructor(public src: string) {
    FakeAudio.instances.push(this);
  }
}

/** End the clip that is currently playing, which is what promotes the next
 *  queued item. */
function finishCurrentAudio() {
  // Index arithmetic, not Array.prototype.at: the tsconfig target is ES2020.
  const audio = FakeAudio.instances[FakeAudio.instances.length - 1];
  if (!audio) throw new Error("no audio is playing");
  audio.currentTime = audio.duration;
  audio.onended?.();
}

function textsSpoken() {
  return ttsSpeak.mock.calls.map((call) => call[0]);
}

function Consumer() {
  const speech = useSpeech();
  return (
    <div>
      <button type="button" onClick={() => void speech.speak("first message")}>speak first</button>
      <button type="button" onClick={() => void speech.speak("second message")}>speak second</button>
      <button type="button" onClick={() => void speech.speak("third message")}>speak third</button>
      <button type="button" onClick={() => speech.stop()}>stop</button>
      <button type="button" onClick={() => speech.next()}>next</button>
      <button type="button" onClick={() => speech.clearQueue()}>clear</button>
      <span data-testid="queued">{speech.queuedCount}</span>
      <span data-testid="speaking">{String(speech.isSpeaking)}</span>
      <span data-testid="now-playing">
        {speech.nowPlaying ? `${speech.nowPlaying.projectTitle ?? ""}/${speech.nowPlaying.sessionTitle ?? ""}` : "none"}
      </span>
    </div>
  );
}

type ProviderProps = {
  sessionId?: string;
  host?: string;
  projectTitle?: string;
  sessionTitle?: string;
};

async function renderQueue(props: ProviderProps = {}) {
  const view = render(
    <SpeechProvider {...props}>
      <Consumer />
    </SpeechProvider>,
  );
  // Wait for the summary config read so no assertion races the initial fetch.
  await waitFor(() => {
    expect(vi.mocked(api.getSpeechSummaryConfig).mock.calls.length).toBeGreaterThan(0);
  });
  return view;
}

async function click(name: string) {
  await act(async () => {
    screen.getByRole("button", { name }).click();
  });
}

beforeEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
  FakeAudio.instances = [];
  vi.stubGlobal("Audio", FakeAudio);
  // jsdom ships neither of these; the provider creates a blob URL per item.
  let created = 0;
  vi.stubGlobal("URL", {
    ...URL,
    createObjectURL: () => `blob:fake/${++created}`,
    revokeObjectURL: () => undefined,
  });
  summarizeSpeech = vi.fn().mockImplementation(async (_session, text) => ({ summary: `summary of ${text}` }));
  ttsSpeak = vi.fn().mockResolvedValue({ generation: 1, audio_id: "a1", status: "ready" });
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
  vi.spyOn(api, "ttsAudioBlob").mockResolvedValue(new Blob(["audio"]));
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the speech queue", () => {
  it("plays queued messages one after another, in click order", async () => {
    // The headline behaviour: three Speak clicks, three reads. The old funnel
    // discarded all but the newest, so this is the test that fails without the
    // queue.
    await renderQueue({ sessionId: "sess-1" });

    await click("speak first");
    await click("speak second");
    await click("speak third");

    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(1));
    expect(textsSpoken()).toEqual(["summary of first message"]);

    await act(async () => { finishCurrentAudio(); });
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(2));
    expect(textsSpoken()[1]).toBe("summary of second message");

    await act(async () => { finishCurrentAudio(); });
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(3));
    expect(textsSpoken()[2]).toBe("summary of third message");
  });

  it("counts only the items still WAITING, not the one playing", async () => {
    await renderQueue({ sessionId: "sess-1" });

    await click("speak first");
    await click("speak second");
    await click("speak third");
    await waitFor(() => expect(screen.getByTestId("queued")).toHaveTextContent("2"));

    await act(async () => { finishCurrentAudio(); });
    await waitFor(() => expect(screen.getByTestId("queued")).toHaveTextContent("1"));
  });

  it("returns to idle once the last item finishes", async () => {
    await renderQueue({ sessionId: "sess-1" });
    await click("speak first");
    await click("speak second");
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(1));

    await act(async () => { finishCurrentAudio(); });
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(2));
    await act(async () => { finishCurrentAudio(); });

    await waitFor(() => expect(screen.getByTestId("queued")).toHaveTextContent("0"));
    await waitFor(() => expect(screen.getByTestId("speaking")).toHaveTextContent("false"));
    expect(screen.getByTestId("now-playing")).toHaveTextContent("none");
  });

  it("summarises one item at a time, never the whole backlog at once", async () => {
    // Five pending messages must not become five concurrent 60s model calls
    // racing to land out of order.
    await renderQueue({ sessionId: "sess-1" });
    await click("speak first");
    await click("speak second");
    await click("speak third");

    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(1));
    expect(summarizeSpeech).toHaveBeenCalledTimes(1);

    await act(async () => { finishCurrentAudio(); });
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(2));
    expect(summarizeSpeech).toHaveBeenCalledTimes(2);
  });
});

describe("controlling the queue", () => {
  it("stop drops the waiting items as well as the current one", async () => {
    await renderQueue({ sessionId: "sess-1" });
    await click("speak first");
    await click("speak second");
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(1));

    await click("stop");
    expect(screen.getByTestId("queued")).toHaveTextContent("0");

    // Even if the torn-down clip fires a late `ended`, nothing may restart.
    await act(async () => { finishCurrentAudio(); });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(ttsSpeak).toHaveBeenCalledTimes(1);
  });

  it("next abandons the current message and starts the queued one", async () => {
    await renderQueue({ sessionId: "sess-1" });
    await click("speak first");
    await click("speak second");
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(1));

    await click("next");
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(2));
    expect(textsSpoken()[1]).toBe("summary of second message");
  });

  it("next does nothing when the queue is empty", async () => {
    await renderQueue({ sessionId: "sess-1" });
    await click("speak first");
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(1));

    await click("next");
    await new Promise((resolve) => setTimeout(resolve, 0));
    // The item was already dequeued, so "next" on an empty queue must not
    // restart the one that is playing.
    expect(ttsSpeak).toHaveBeenCalledTimes(1);
  });

  it("clear drops the backlog but lets the current message finish", async () => {
    await renderQueue({ sessionId: "sess-1" });
    await click("speak first");
    await click("speak second");
    await click("speak third");
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(1));

    await click("clear");
    expect(screen.getByTestId("queued")).toHaveTextContent("0");

    await act(async () => { finishCurrentAudio(); });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(ttsSpeak).toHaveBeenCalledTimes(1);
  });
});

describe("at-bottom auto-speak", () => {
  it("queues behind the message already playing instead of cutting it off", async () => {
    vi.spyOn(api, "getTTSConfig").mockResolvedValue({ ...LOCAL_ENGINE, mode: "at-bottom" });
    await renderQueue({ sessionId: "sess-1" });
    await click("speak first");
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(1));

    // The event ChatPanel dispatches when a turn completes at the bottom.
    await act(async () => {
      window.dispatchEvent(new CustomEvent("ocode:assistant-complete", { detail: { text: "auto message", atBottom: true } }));
    });
    expect(screen.getByTestId("queued")).toHaveTextContent("1");

    await act(async () => { finishCurrentAudio(); });
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(2));
    expect(textsSpoken()[1]).toBe("summary of auto message");
  });

  it("ignores the event when the user was not at the bottom", async () => {
    vi.spyOn(api, "getTTSConfig").mockResolvedValue({ ...LOCAL_ENGINE, mode: "at-bottom" });
    await renderQueue({ sessionId: "sess-1" });

    await act(async () => {
      window.dispatchEvent(new CustomEvent("ocode:assistant-complete", { detail: { text: "scrolled away", atBottom: false } }));
    });
    expect(screen.getByTestId("queued")).toHaveTextContent("0");
    expect(ttsSpeak).not.toHaveBeenCalled();
  });
});

describe("labelling the now-playing item", () => {
  it("shows the project and chat session the item was queued from", async () => {
    await renderQueue({ sessionId: "sess-1", projectTitle: "ocode", sessionTitle: "Fix the speech queue" });

    await click("speak first");
    // Wait on the same milestone as the tests below rather than on the label
    // alone: setNowPlaying happens early in drain(), so a bare 1s waitFor for
    // its text is the one assertion in this file that a loaded full-suite run
    // (300 files in parallel) can outrun. ttsSpeak is a strictly later step in
    // the same drain loop, so reaching it implies the label is already on screen.
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(1));
    expect(screen.getByTestId("now-playing")).toHaveTextContent("ocode/Fix the speech queue");
  });

  it("keeps the ORIGINAL labels after the user switches to another tab", async () => {
    // The whole reason the labels are snapshotted onto the item: a queue
    // outlives a tab switch, so reading the props at play time would relabel
    // in-flight audio with whatever the user is now looking at.
    const view = await renderQueue({ sessionId: "sess-1", projectTitle: "project A", sessionTitle: "session A" });
    await click("speak first");
    await click("speak second");
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(1));
    expect(screen.getByTestId("now-playing")).toHaveTextContent("project A/session A");

    view.rerender(
      <SpeechProvider sessionId="sess-2" projectTitle="project B" sessionTitle="session B">
        <Consumer />
      </SpeechProvider>,
    );
    expect(screen.getByTestId("now-playing")).toHaveTextContent("project A/session A");

    // And the item that was queued under A keeps A's labels when it starts.
    await act(async () => { finishCurrentAudio(); });
    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(2));
    expect(screen.getByTestId("now-playing")).toHaveTextContent("project A/session A");
  });

  it("labels an item with the tab that was active WHEN IT WAS QUEUED", async () => {
    // The complement of the test above, and the one that actually pins the
    // snapshot. `drain` is memoised on [playItem, resolveSpeechText] and does
    // NOT depend on the titles, so its closure still holds the labels from
    // whenever it was last rebuilt. Reading the live props inside it would
    // therefore stamp a message queued from project B with project A's name.
    const view = await renderQueue({ sessionId: "sess-1", projectTitle: "project A", sessionTitle: "session A" });

    view.rerender(
      <SpeechProvider sessionId="sess-2" projectTitle="project B" sessionTitle="session B">
        <Consumer />
      </SpeechProvider>,
    );
    await click("speak first");

    await waitFor(() => expect(screen.getByTestId("now-playing")).toHaveTextContent("project B/session B"));
  });

  it("summarises each item against the session it was QUEUED from", async () => {
    // Same reason as the labels: the summary endpoint is session-scoped, so a
    // queued item summarised against the newly-active session would read
    // against the wrong conversation.
    const view = await renderQueue({ sessionId: "sess-1" });
    await click("speak first");
    await click("speak second");
    await waitFor(() => expect(summarizeSpeech).toHaveBeenCalledTimes(1));
    expect(summarizeSpeech.mock.calls[0][0]).toBe("sess-1");

    view.rerender(
      <SpeechProvider sessionId="sess-2">
        <Consumer />
      </SpeechProvider>,
    );
    await act(async () => { finishCurrentAudio(); });
    await waitFor(() => expect(summarizeSpeech).toHaveBeenCalledTimes(2));
    expect(summarizeSpeech.mock.calls[1][0]).toBe("sess-1");
  });
});

describe("a failing item", () => {
  it("does not strand the items queued behind it", async () => {
    // A synthesis failure must skip to the next message rather than leaving the
    // worker parked on a clip that will never end.
    ttsSpeak.mockRejectedValueOnce(new Error("kokoro exploded"));
    await renderQueue({ sessionId: "sess-1" });
    await click("speak first");
    await click("speak second");

    await waitFor(() => expect(ttsSpeak).toHaveBeenCalledTimes(2));
    expect(textsSpoken()[1]).toBe("summary of second message");
  });
});

describe("speak outcomes", () => {
  function OutcomeConsumer({ onOutcome }: { onOutcome: (outcome: SpeechOutcome) => void }) {
    const speech = useSpeech();
    return (
      <div>
        <span data-testid="outcome-engine">{speech.config.engine}</span>
        <button type="button" onClick={() => void speech.speak("outcome message").then(onOutcome)}>speak outcome</button>
        <button type="button" onClick={() => speech.stop()}>stop all</button>
      </div>
    );
  }

  async function renderOutcome() {
    const outcomes: SpeechOutcome[] = [];
    render(
      <SpeechProvider sessionId="sess-1">
        <OutcomeConsumer onOutcome={(outcome) => outcomes.push(outcome)} />
      </SpeechProvider>,
    );
    await waitFor(() => {
      expect(vi.mocked(api.getSpeechSummaryConfig).mock.calls.length).toBeGreaterThan(0);
      // Wait for the engine selection too: an unloaded provider is still on
      // browser-native, which would fail for a different reason than the
      // behaviour under test.
      expect(screen.getByTestId("outcome-engine")).toHaveTextContent("kokoro");
    });
    return outcomes;
  }

  it("resolves ok once playback starts, not when the clip ends", async () => {
    const outcomes = await renderOutcome();
    await act(async () => {
      screen.getByRole("button", { name: "speak outcome" }).click();
    });
    await waitFor(() => expect(outcomes).toHaveLength(1));
    // No finishCurrentAudio() call below: the clip is still open, proving the
    // button is released at the START of the read rather than at its end.
    expect(outcomes[0]).toEqual({ ok: true });
    expect(ttsSpeak).toHaveBeenCalledTimes(1);
  });

  it("reports a synthesis failure as ok:false instead of hanging", async () => {
    ttsSpeak.mockRejectedValueOnce(new Error("synthesis exploded"));
    const outcomes = await renderOutcome();
    await act(async () => {
      screen.getByRole("button", { name: "speak outcome" }).click();
    });
    await waitFor(() => expect(outcomes).toHaveLength(1));
    expect(outcomes[0]).toEqual({ ok: false, error: "synthesis exploded" });
  });

  it("settles a still-waiting item when the user presses Stop", async () => {
    // Park the item in the summariser so it never reaches playback, then Stop:
    // the button must be released even for an item that never started.
    summarizeSpeech.mockImplementationOnce(() => new Promise(() => {}));
    const outcomes = await renderOutcome();
    await act(async () => {
      screen.getByRole("button", { name: "speak outcome" }).click();
    });
    await waitFor(() => expect(summarizeSpeech).toHaveBeenCalledTimes(1));
    await act(async () => {
      screen.getByRole("button", { name: "stop all" }).click();
    });
    await waitFor(() => expect(outcomes).toHaveLength(1));
    expect(outcomes[0]).toEqual({ ok: true });
  });

  it("resolves an error outcome through requestSpeech when no provider is mounted", async () => {
    await expect(requestSpeech("nobody is listening")).resolves.toEqual({
      ok: false,
      error: "Speech is unavailable",
    });
  });

  it("resolves ok through the ocode:speak bridge when a provider is mounted", async () => {
    const outcomes = await renderOutcome();
    await act(async () => {
      void requestSpeech("bridged").then((outcome) => outcomes.push(outcome));
    });
    await waitFor(() => expect(outcomes).toHaveLength(1));
    expect(outcomes[0]).toEqual({ ok: true });
  });
});
