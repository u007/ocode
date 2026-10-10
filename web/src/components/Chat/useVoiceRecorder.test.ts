import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import {
  VOICE_NOTHING_HEARD_MESSAGE,
  VOICE_UNAVAILABLE_MESSAGE,
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

function setup(transcribe = vi.fn().mockResolvedValue({ text: "hello there", model: "m" })) {
  const onTranscript = vi.fn();
  const hook = renderHook(() => useVoiceRecorder({ transcribe, onTranscript }));
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

  it("toggle starts when idle and stops when recording, ignoring clicks mid-transcription", async () => {
    let resolveTranscribe!: (v: { text: string; model: string }) => void;
    const transcribe = vi.fn(() => new Promise<{ text: string; model: string }>((r) => (resolveTranscribe = r)));
    const { result, onTranscript } = setup(transcribe);

    await act(async () => result.current.toggle());
    expect(result.current.state).toBe("recording");
    act(() => result.current.toggle());
    expect(result.current.state).toBe("transcribing");

    // A third click while transcribing must not start a second recorder.
    await act(async () => result.current.toggle());
    expect(FakeMediaRecorder.instances).toHaveLength(1);
    expect(env.getUserMedia).toHaveBeenCalledTimes(1);

    await act(async () => resolveTranscribe({ text: "done", model: "m" }));
    expect(onTranscript).toHaveBeenCalledWith("done");
    expect(result.current.state).toBe("idle");
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
