import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import {
  VOICE_NOTHING_HEARD_MESSAGE,
  VOICE_PARTIAL_INTERVAL_MS,
  VOICE_TIMESLICE_MS,
  VOICE_UNAVAILABLE_MESSAGE,
  VOICE_HOLD_THRESHOLD_MS,
  audioExtensionFor,
  describeMicError,
  pickRecordingMimeType,
  useVoiceRecorder,
} from "./useVoiceRecorder";
import { FakeMediaRecorder, installVoiceEnv, removeVoiceEnv, type VoiceEnv } from "./voiceTestUtils";

let env: VoiceEnv;

beforeEach(() => {
  env = installVoiceEnv();
});

afterEach(() => {
  removeVoiceEnv();
  vi.restoreAllMocks();
});

function setup(
  transcribe = vi.fn().mockResolvedValue({ text: "hello there", model: "m" }),
  canPreview: () => Promise<boolean> = async () => true,
) {
  const onTranscript = vi.fn();
  const hook = renderHook(() => useVoiceRecorder({ transcribe, onTranscript, canPreview }));
  return { ...hook, transcribe, onTranscript };
}

describe("useVoiceRecorder state transitions", () => {
  it("idle -> recording -> transcribing -> idle, sends the transcript, releases the mic", async () => {
    const { result, transcribe, onTranscript } = setup();
    expect(result.current.state).toBe("idle");
    expect(result.current.supported).toBe(true);

    await act(async () => {
      await result.current.start();
    });
    expect(env.getUserMedia).toHaveBeenCalledWith({ audio: true });
    expect(result.current.state).toBe("recording");
    expect(FakeMediaRecorder.instances).toHaveLength(1);
    expect(FakeMediaRecorder.instances[0].start).toHaveBeenCalled();

    act(() => result.current.stop());
    // The transcript arrives asynchronously; the mic must already be released.
    expect(env.trackStop).toHaveBeenCalled();

    await waitFor(() => expect(onTranscript).toHaveBeenCalledWith("hello there"));
    expect(transcribe).toHaveBeenCalledTimes(1);
    const [blob, filename] = transcribe.mock.calls[0];
    expect(blob).toBeInstanceOf(Blob);
    expect(filename).toBe("recording.webm");
    await waitFor(() => expect(result.current.state).toBe("idle"));
    expect(result.current.error).toBeNull();
  });

  it("press+release starts when idle, press+release stops when recording, ignoring input mid-transcription", async () => {
    let resolveTranscribe!: (v: { text: string; model: string }) => void;
    const transcribe = vi.fn(() => new Promise<{ text: string; model: string }>((r) => (resolveTranscribe = r)));
    const { result, onTranscript } = setup(transcribe);

    // A quick press+release starts recording.
    act(() => result.current.press());
    await act(async () => result.current.release());
    expect(result.current.state).toBe("recording");

    // A press (stop-intent) + release stops & transcribes.
    act(() => result.current.press());
    await act(async () => result.current.release());
    expect(result.current.state).toBe("transcribing");

    // A third press while transcribing must not start a second recorder.
    act(() => result.current.press());
    expect(FakeMediaRecorder.instances).toHaveLength(1);
    expect(env.getUserMedia).toHaveBeenCalledTimes(1);

    await act(async () => resolveTranscribe({ text: "done", model: "m" }));
    expect(onTranscript).toHaveBeenCalledWith("done");
    expect(result.current.state).toBe("idle");
  });

  it("holding past the threshold locks continuous mode; release does not stop", async () => {
    vi.useFakeTimers();
    try {
      let resolveTranscribe!: (v: { text: string; model: string }) => void;
      const transcribe = vi.fn(() => new Promise<{ text: string; model: string }>((r) => (resolveTranscribe = r)));
      const onTranscript = vi.fn();
      const { result } = renderHook(() => useVoiceRecorder({ transcribe, onTranscript }));

      // Press and hold past the threshold: locks continuous mode.
      act(() => result.current.press());
      await act(async () => {
        await vi.advanceTimersByTimeAsync(VOICE_HOLD_THRESHOLD_MS);
      });
      expect(result.current.state).toBe("recording");
      expect(result.current.holding).toBe(false);

      // Releasing the hold does NOT stop continuous recording.
      act(() => result.current.release());
      expect(result.current.state).toBe("recording");

      // A deliberate press+release now stops & transcribes.
      act(() => result.current.press());
      await act(async () => result.current.release());
      expect(result.current.state).toBe("transcribing");

      await act(async () => resolveTranscribe({ text: "hello there", model: "m" }));
      expect(onTranscript).toHaveBeenCalledWith("hello there");
      expect(result.current.state).toBe("idle");
    } finally {
      vi.useRealTimers();
    }
  });

  it("stop() is a no-op unless recording", () => {
    const { result } = setup();
    act(() => result.current.stop());
    expect(result.current.state).toBe("idle");
    expect(env.getUserMedia).not.toHaveBeenCalled();
  });

  it("a click during the permission prompt cancels the start and releases the stream", async () => {
    let grant!: (s: MediaStream) => void;
    env.getUserMedia.mockImplementationOnce(() => new Promise<MediaStream>((r) => (grant = r)));
    const { result, onTranscript } = setup();

    act(() => {
      void result.current.start();
    });
    expect(result.current.state).toBe("recording");
    act(() => result.current.stop());
    expect(result.current.state).toBe("idle");

    await act(async () => grant({ getTracks: () => [{ stop: env.trackStop }] } as unknown as MediaStream));
    expect(FakeMediaRecorder.instances).toHaveLength(0);
    expect(env.trackStop).toHaveBeenCalled();
    expect(onTranscript).not.toHaveBeenCalled();
  });
});

describe("useVoiceRecorder MIME and filename", () => {
  it("prefers webm/opus, then mp4, else the browser default", () => {
    FakeMediaRecorder.supported = new Set(["audio/mp4"]);
    expect(pickRecordingMimeType()).toBe("audio/mp4");
    FakeMediaRecorder.supported = new Set();
    expect(pickRecordingMimeType()).toBeUndefined();
  });

  it("passes the picked mimeType to MediaRecorder and names the upload by its real extension", async () => {
    FakeMediaRecorder.supported = new Set(["audio/mp4"]);
    const { result, transcribe } = setup();
    await act(async () => {
      await result.current.start();
    });
    expect(FakeMediaRecorder.instances[0].options).toEqual({ mimeType: "audio/mp4" });
    act(() => result.current.stop());
    await waitFor(() => expect(transcribe).toHaveBeenCalled());
    expect(transcribe.mock.calls[0][1]).toBe("recording.mp4");
  });

  it("maps container MIME types to extensions", () => {
    expect(audioExtensionFor("audio/webm;codecs=opus")).toBe(".webm");
    expect(audioExtensionFor("audio/mp4")).toBe(".mp4");
    expect(audioExtensionFor("audio/ogg;codecs=opus")).toBe(".ogg");
    expect(audioExtensionFor("")).toBe(".webm");
  });
});

describe("useVoiceRecorder empty and failed transcripts", () => {
  it("an empty transcript is 'nothing heard': no send, error shown", async () => {
    const { result, onTranscript } = setup(vi.fn().mockResolvedValue({ text: "   ", model: "m" }));
    await act(async () => result.current.start());
    act(() => result.current.stop());
    await waitFor(() => expect(result.current.state).toBe("idle"));
    expect(onTranscript).not.toHaveBeenCalled();
    expect(result.current.error).toBe(VOICE_NOTHING_HEARD_MESSAGE);
  });

  it("a rejected transcribe surfaces its message and returns to idle", async () => {
    const { result, onTranscript } = setup(vi.fn().mockRejectedValue(new Error("selected model not available")));
    await act(async () => result.current.start());
    act(() => result.current.stop());
    await waitFor(() => expect(result.current.error).toBe("selected model not available"));
    expect(result.current.state).toBe("idle");
    expect(onTranscript).not.toHaveBeenCalled();
  });

  it("a recording with no audio data reports it and does not transcribe", async () => {
    const transcribe = vi.fn();
    const { result } = setup(transcribe);
    await act(async () => result.current.start());
    FakeMediaRecorder.instances[0].emitData = false;
    act(() => result.current.stop());
    expect(transcribe).not.toHaveBeenCalled();
    expect(result.current.error).toBe("No audio was captured. Try again.");
    expect(result.current.state).toBe("idle");
  });

  it("a denied microphone permission is described and leaves the hook idle", async () => {
    env.getUserMedia.mockRejectedValueOnce(Object.assign(new Error("denied"), { name: "NotAllowedError" }));
    const { result } = setup();
    await act(async () => result.current.start());
    expect(result.current.error).toMatch(/Microphone permission was denied/);
    expect(result.current.state).toBe("idle");
  });
});

describe("useVoiceRecorder unavailable browser", () => {
  it("reports unsupported and refuses to start when getUserMedia is missing", async () => {
    removeVoiceEnv();
    const { result, transcribe } = setup();
    expect(result.current.supported).toBe(false);
    await act(async () => result.current.start());
    expect(result.current.error).toBe(VOICE_UNAVAILABLE_MESSAGE);
    expect(result.current.state).toBe("idle");
    expect(transcribe).not.toHaveBeenCalled();
  });

  it("is unsupported when MediaRecorder is missing even if a mic API exists", () => {
    vi.stubGlobal("MediaRecorder", undefined);
    const { result } = setup();
    expect(result.current.supported).toBe(false);
  });
});

describe("useVoiceRecorder unmount", () => {
  it("unmounting mid-recording stops the recorder, releases the mic and never transcribes", async () => {
    const transcribe = vi.fn();
    const { result, unmount } = setup(transcribe);
    await act(async () => result.current.start());
    unmount();
    expect(env.trackStop).toHaveBeenCalled();
    expect(FakeMediaRecorder.instances[0].stop).toHaveBeenCalled();
    expect(transcribe).not.toHaveBeenCalled();
  });

  it("describeMicError maps common DOMException names", () => {
    expect(describeMicError({ name: "NotFoundError" })).toBe("No microphone was found.");
    expect(describeMicError({ name: "NotReadableError" })).toMatch(/in use/);
  });
});

describe("useVoiceRecorder live partials", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  /** Lets timers and the promise chain settle together. */
  async function tick(ms = 0) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ms);
    });
  }

  function deferred<T>() {
    let resolve!: (v: T) => void;
    let reject!: (e: unknown) => void;
    const promise = new Promise<T>((res, rej) => {
      resolve = res;
      reject = rej;
    });
    return { promise, resolve, reject };
  }

  it("records with a timeslice and, every 3s, sends all chunks so far (no request when empty)", async () => {
    const transcribe = vi.fn().mockResolvedValue({ text: "partial", model: "m" });
    const { result } = setup(transcribe);
    await act(async () => result.current.start());
    const rec = FakeMediaRecorder.instances[0];
    expect(rec.start).toHaveBeenCalledWith(VOICE_TIMESLICE_MS);

    await tick(VOICE_PARTIAL_INTERVAL_MS);
    expect(transcribe).not.toHaveBeenCalled();

    act(() => rec.pushChunk(10));
    await tick(VOICE_PARTIAL_INTERVAL_MS);
    expect(transcribe).toHaveBeenCalledTimes(1);
    expect(transcribe.mock.calls[0][0]).toBeInstanceOf(Blob);
    expect(transcribe.mock.calls[0][0].size).toBe(10);
    expect(transcribe.mock.calls[0][1]).toBe("recording.webm");
    expect(result.current.partialText).toBe("partial");

    act(() => {
      rec.pushChunk(5);
      rec.pushChunk(7);
    });
    await tick(VOICE_PARTIAL_INTERVAL_MS);
    expect(transcribe).toHaveBeenCalledTimes(2);
    // Accumulated: the second request carries every chunk from the start.
    expect(transcribe.mock.calls[1][0].size).toBe(22);
  });

  it("keeps at most one partial request in flight", async () => {
    const first = deferred<{ text: string; model: string }>();
    const transcribe = vi
      .fn()
      .mockReturnValueOnce(first.promise)
      .mockResolvedValue({ text: "next", model: "m" });
    const { result } = setup(transcribe);
    await act(async () => result.current.start());
    act(() => FakeMediaRecorder.instances[0].pushChunk(10));

    await tick(VOICE_PARTIAL_INTERVAL_MS * 3);
    expect(transcribe).toHaveBeenCalledTimes(1);

    await act(async () => first.resolve({ text: "first", model: "m" }));
    expect(result.current.partialText).toBe("first");

    await tick(VOICE_PARTIAL_INTERVAL_MS);
    expect(transcribe).toHaveBeenCalledTimes(2);
    expect(result.current.partialText).toBe("next");
  });

  it("drops a partial that arrives after stop, and the final still runs", async () => {
    const partial = deferred<{ text: string; model: string }>();
    const transcribe = vi
      .fn()
      .mockReturnValueOnce(partial.promise)
      .mockResolvedValueOnce({ text: "final words", model: "m" });
    const { result, onTranscript } = setup(transcribe);
    await act(async () => result.current.start());
    act(() => FakeMediaRecorder.instances[0].pushChunk(10));
    await tick(VOICE_PARTIAL_INTERVAL_MS);
    expect(transcribe).toHaveBeenCalledTimes(1);

    act(() => result.current.stop());
    await tick();
    expect(transcribe).toHaveBeenCalledTimes(2);
    expect(onTranscript).toHaveBeenCalledWith("final words");

    await act(async () => partial.resolve({ text: "late partial", model: "m" }));
    expect(result.current.partialText).toBe("");
    expect(onTranscript).toHaveBeenCalledTimes(1);
    expect(onTranscript).toHaveBeenCalledWith("final words");
  });

  it("stop clears the preview, stops the timer, and the final carries every chunk", async () => {
    const transcribe = vi
      .fn()
      .mockResolvedValueOnce({ text: "preview", model: "m" })
      .mockResolvedValueOnce({ text: "final", model: "m" });
    const { result, onTranscript } = setup(transcribe);
    await act(async () => result.current.start());
    const rec = FakeMediaRecorder.instances[0];
    act(() => rec.pushChunk(10));
    await tick(VOICE_PARTIAL_INTERVAL_MS);
    expect(result.current.partialText).toBe("preview");

    act(() => result.current.stop());
    expect(result.current.partialText).toBe("");
    await tick();
    expect(onTranscript).toHaveBeenCalledWith("final");

    // Only the final upload follows stop: no more partial ticks.
    await tick(VOICE_PARTIAL_INTERVAL_MS * 3);
    expect(transcribe).toHaveBeenCalledTimes(2);
    // 10 bytes pushed mid-recording, plus the 4-byte clip the fake emits on stop.
    expect(transcribe.mock.calls[1][0].size).toBe(14);
  });

  it("partial errors are silent (no error state) and the next tick retries", async () => {
    const transcribe = vi
      .fn()
      .mockRejectedValueOnce(new Error("speech model busy"))
      .mockResolvedValue({ text: "recovered", model: "m" });
    const { result, onTranscript } = setup(transcribe);
    await act(async () => result.current.start());
    act(() => FakeMediaRecorder.instances[0].pushChunk(10));

    await tick(VOICE_PARTIAL_INTERVAL_MS);
    expect(result.current.error).toBeNull();
    expect(result.current.partialText).toBe("");

    await tick(VOICE_PARTIAL_INTERVAL_MS);
    expect(result.current.error).toBeNull();
    expect(result.current.partialText).toBe("recovered");
    expect(onTranscript).not.toHaveBeenCalled();
  });

  it("a partial never triggers onTranscript; only the final does", async () => {
    const transcribe = vi
      .fn()
      .mockResolvedValueOnce({ text: "partial only", model: "m" })
      .mockResolvedValueOnce({ text: "final", model: "m" });
    const { result, onTranscript } = setup(transcribe);
    await act(async () => result.current.start());
    act(() => FakeMediaRecorder.instances[0].pushChunk(10));
    await tick(VOICE_PARTIAL_INTERVAL_MS);
    expect(result.current.partialText).toBe("partial only");
    expect(onTranscript).not.toHaveBeenCalled();

    act(() => result.current.stop());
    await tick();
    expect(onTranscript.mock.calls).toEqual([["final"]]);
  });

  it("unmounting cancels the partial timer", async () => {
    const transcribe = vi.fn().mockResolvedValue({ text: "x", model: "m" });
    const { result, unmount } = setup(transcribe);
    await act(async () => result.current.start());
    act(() => FakeMediaRecorder.instances[0].pushChunk(10));
    unmount();
    await tick(VOICE_PARTIAL_INTERVAL_MS * 3);
    expect(transcribe).not.toHaveBeenCalled();
  });
});

describe("useVoiceRecorder preview gating", () => {
  it("sends no live partials when the selected engine is not local", async () => {
    vi.useFakeTimers();
    try {
      const transcribe = vi.fn().mockResolvedValue({ text: "x", model: "m" });
      const { result } = setup(transcribe, async () => false);
      await act(async () => {
        await result.current.start();
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(VOICE_PARTIAL_INTERVAL_MS * 3);
      });
      expect(transcribe).not.toHaveBeenCalled();
      await act(async () => {
        result.current.stop();
      });
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("useVoiceRecorder prewarm", () => {
  it("starts loading the model when the recording starts", async () => {
    const prewarm = vi.fn().mockResolvedValue(undefined);
    const onTranscript = vi.fn();
    const transcribe = vi.fn().mockResolvedValue({ text: "hello there", model: "m" });
    const { result } = renderHook(() =>
      useVoiceRecorder({ transcribe, onTranscript, canPreview: async () => true, prewarm }),
    );

    await act(async () => {
      await result.current.start();
    });

    expect(prewarm).toHaveBeenCalledTimes(1);
  });

  it("a rejected prewarm does not break the recording", async () => {
    const prewarm = vi.fn().mockRejectedValue(new Error("no model"));
    const onTranscript = vi.fn();
    const transcribe = vi.fn().mockResolvedValue({ text: "hello there", model: "m" });
    const { result } = renderHook(() =>
      useVoiceRecorder({ transcribe, onTranscript, canPreview: async () => true, prewarm }),
    );

    await act(async () => {
      await result.current.start();
    });

    expect(prewarm).toHaveBeenCalledTimes(1);
    expect(result.current.state).toBe("recording");
    expect(result.current.error).toBeNull();
  });

  it("works without a prewarm hook at all", async () => {
    const onTranscript = vi.fn();
    const transcribe = vi.fn().mockResolvedValue({ text: "hello there", model: "m" });
    const { result } = renderHook(() => useVoiceRecorder({ transcribe, onTranscript }));

    await act(async () => {
      await result.current.start();
    });

    expect(result.current.state).toBe("recording");
  });
});

describe("useVoiceRecorder hold-timer lifecycle", () => {
  it("does not start a second recording when a press is repeated", async () => {
    vi.useFakeTimers();
    try {
      const { result } = setup();
      // A browser repeats keydown while a key is held, and the keydown handler
      // calls press() on EVERY keydown. Each call re-armed the hold timer and
      // orphaned the previous one, so the orphan fired later — after the user
      // had already stopped the recording — and started a SECOND recording
      // with no user action (the mic indicator stayed lit).
      //
      // The sequence below is the one a browser produces: keydown arms timer
      // A, the click fallback arms timer B, the release clears B and starts
      // the recording, the user stops it, and the orphan A fires afterwards.
      act(() => {
        result.current.press();
      });
      act(() => {
        result.current.press();
      });
      act(() => {
        result.current.release();
      });
      await act(async () => {
        await Promise.resolve();
      });
      expect(result.current.state).toBe("recording");
      expect(env.getUserMedia).toHaveBeenCalledTimes(1);

      // The user stops the recording before the orphan would fire.
      act(() => {
        result.current.stop();
      });
      await act(async () => {
        await Promise.resolve();
        await Promise.resolve();
      });
      expect(result.current.state).toBe("idle");

      // Now the orphan would fire.
      await act(async () => {
        vi.advanceTimersByTime(VOICE_HOLD_THRESHOLD_MS + 200);
      });
      expect(env.getUserMedia).toHaveBeenCalledTimes(1);
      expect(FakeMediaRecorder.instances).toHaveLength(1);
      expect(result.current.state).toBe("idle");
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps the stop-intent when the hold timer fires", async () => {
    vi.useFakeTimers();
    try {
      const { result, transcribe } = setup();
      // Press and hold past the threshold: the recording starts in continuous
      // mode and the release must NOT stop it.
      act(() => result.current.press());
      await act(async () => {
        vi.advanceTimersByTime(VOICE_HOLD_THRESHOLD_MS + 50);
      });
      expect(result.current.state).toBe("recording");

      // A deliberate stop-press, then the release. The hold timer must not
      // have wiped the stop-intent, so the release stops the recording and
      // sends the transcript.
      act(() => result.current.press());
      act(() => result.current.release());
      // The transcription is async; drive the microtask queue under fake
      // timers (waitFor would not advance them).
      await act(async () => {
        await Promise.resolve();
        await Promise.resolve();
        await Promise.resolve();
      });
      expect(transcribe).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("clears the hold timer on unmount so no phantom recording starts", async () => {
    vi.useFakeTimers();
    try {
      const { result, unmount } = setup();
      // Arm the hold timer, then unmount before it fires.
      act(() => result.current.press());
      unmount();
      await act(async () => {
        vi.advanceTimersByTime(VOICE_HOLD_THRESHOLD_MS + 500);
      });
      // No microphone may be requested after the component is gone, and the
      // track must not be left open (the OS recording indicator would stay lit).
      expect(env.getUserMedia).not.toHaveBeenCalled();
      expect(FakeMediaRecorder.instances).toHaveLength(0);
      expect(env.trackStop).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });
});
