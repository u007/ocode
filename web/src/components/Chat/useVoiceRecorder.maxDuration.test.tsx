import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useVoiceRecorder, VOICE_MAX_RECORDING_MS } from "./useVoiceRecorder";
import { FakeMediaRecorder, installVoiceEnv, removeVoiceEnv } from "./voiceTestUtils";

// The 5-minute cap is a safety net for a forgotten tab: the microphone must not
// stay hot (or the server reject an oversized clip) because the user walked
// away. Reaching the cap behaves exactly like pressing stop.
describe("useVoiceRecorder max duration", () => {
  beforeEach(() => {
    installVoiceEnv();
  });
  afterEach(() => {
    removeVoiceEnv();
    vi.useRealTimers();
  });

  it("stops and transcribes when a recording reaches VOICE_MAX_RECORDING_MS", async () => {
    vi.useFakeTimers();
    const transcribe = vi.fn(async () => ({ text: "long dictation", model: "m" }));
    const onTranscript = vi.fn();
    const { result } = renderHook(() => useVoiceRecorder({ transcribe, onTranscript }));

    await act(async () => {
      await result.current.start();
    });
    expect(result.current.state).toBe("recording");
    const recorder = FakeMediaRecorder.instances[0];
    expect(recorder).toBeDefined();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(VOICE_MAX_RECORDING_MS);
    });
    expect(recorder.stop).toHaveBeenCalledTimes(1);

    // onstop fires synchronously from the fake, then transcribeBlob resolves.
    await act(async () => {});
    expect(result.current.state).not.toBe("recording");
    expect(transcribe).toHaveBeenCalledTimes(1);
    expect(onTranscript).toHaveBeenCalledWith("long dictation");
  });

  it("does not stop a recording that was already stopped before the cap", async () => {
    vi.useFakeTimers();
    const transcribe = vi.fn(async () => ({ text: "short", model: "m" }));
    const { result } = renderHook(() => useVoiceRecorder({ transcribe, onTranscript: vi.fn() }));

    await act(async () => {
      await result.current.start();
    });
    const recorder = FakeMediaRecorder.instances[0];
    await act(async () => {
      result.current.stop();
    });
    expect(recorder.stop).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(VOICE_MAX_RECORDING_MS);
    });
    expect(recorder.stop).toHaveBeenCalledTimes(1);
  });
});
