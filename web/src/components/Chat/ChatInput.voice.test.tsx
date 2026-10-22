import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import ChatInput from "./ChatInput";
import { VOICE_NOTHING_HEARD_MESSAGE, VOICE_UNAVAILABLE_MESSAGE, VOICE_HOLD_THRESHOLD_MS } from "./useVoiceRecorder";
import { FakeMediaRecorder, installVoiceEnv, removeVoiceEnv } from "./voiceTestUtils";
import { api } from "@/api/client";
import { clearDraft } from "../../lib/tabDrafts";

const sendMessage = vi.fn().mockResolvedValue(true);
vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage,
    executeShell: vi.fn().mockResolvedValue({ output: "", exitCode: 0, error: "" }),
    stop: vi.fn(),
    isStreaming: false,
    pendingPermission: null,
    suggestions: [],
  }),
}));
vi.mock("../../stores/projectStore", () => ({
  findTabForSession: () => undefined,
  useProjectState: () => ({
    state: { activeProject: { path: "/tmp/proj" } },
    dispatch: vi.fn(),
  }),
}));

const TAB = "voice-test-tab";

function micButton() {
  return screen.getByRole("button", { name: /voice input|stop recording/i }) as HTMLButtonElement;
}

function textarea() {
  return screen.getByPlaceholderText(/Type a message/i) as HTMLTextAreaElement;
}

/** One click to start, one to stop; the transcript resolves on its own.
 *  Real browsers fire mousedown→mouseup on a click, so we mirror that here
 *  (fireEvent.click alone would not reach the press/release handlers). */
async function recordOnce() {
  await act(async () => {
    fireEvent.mouseDown(micButton());
    fireEvent.mouseUp(micButton());
  });
  await act(async () => {
    fireEvent.mouseDown(micButton());
    fireEvent.mouseUp(micButton());
  });
  await flush();
}

/** Sends are batched by a 1.5s debounce (CHAT_INPUT_DEBOUNCE_MS), so drain timers
 *  and the promise chain together before asserting. */
async function flush() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000);
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  sendMessage.mockClear();
  sendMessage.mockResolvedValue(true);
  clearDraft(TAB);
  installVoiceEnv();
});

afterEach(() => {
  removeVoiceEnv();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe("ChatInput voice input", () => {
  it("sends the transcript through the normal send path (auto-send, not paste)", async () => {
    const transcribe = vi.spyOn(api, "transcribeSpeech").mockResolvedValue({ text: "run the tests", model: "m" });
    render(<ChatInput sessionTabId={TAB} />);

    await recordOnce();

    expect(sendMessage).toHaveBeenCalledWith("run the tests");
    expect(sendMessage).toHaveBeenCalledTimes(1);
    expect(transcribe).toHaveBeenCalledWith(expect.any(Blob), "recording.webm");
    expect(textarea().value).toBe("");
  });

  it("speaking leaves the user's typed draft in place", async () => {
    vi.spyOn(api, "transcribeSpeech").mockResolvedValue({ text: "spoken words", model: "m" });
    render(<ChatInput sessionTabId={TAB} />);
    fireEvent.change(textarea(), { target: { value: "half typed" } });

    await recordOnce();

    expect(sendMessage).toHaveBeenCalledWith("spoken words");
    expect(textarea().value).toBe("half typed");
  });

  it("shows the recording and transcribing states", async () => {
    let resolve!: (v: { text: string; model: string }) => void;
    vi.spyOn(api, "transcribeSpeech").mockImplementation(
      () => new Promise((r) => (resolve = r)),
    );
    render(<ChatInput sessionTabId={TAB} />);

    await act(async () => {
      fireEvent.mouseDown(micButton());
      fireEvent.mouseUp(micButton());
    });
    expect(screen.getByText("Recording… click to stop")).toBeDefined();
    expect(micButton().getAttribute("aria-pressed")).toBe("true");

    await act(async () => {
      fireEvent.mouseDown(micButton());
      fireEvent.mouseUp(micButton());
    });
    expect(screen.getByText("Transcribing…")).toBeDefined();

    await act(async () => resolve({ text: "ok", model: "m" }));
    await flush();
    expect(sendMessage).toHaveBeenCalledWith("ok");
    expect(screen.queryByText("Transcribing…")).toBeNull();
  });

  it("an empty transcript is not sent and says nothing was heard", async () => {
    vi.spyOn(api, "transcribeSpeech").mockResolvedValue({ text: "", model: "m" });
    render(<ChatInput sessionTabId={TAB} />);

    await recordOnce();

    expect(screen.getByRole("alert")).toBeDefined();
    expect(screen.getByRole("alert").textContent).toBe(VOICE_NOTHING_HEARD_MESSAGE);
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("a server error (for example 503) is shown inline and nothing is sent", async () => {
    vi.spyOn(api, "transcribeSpeech").mockRejectedValue(new Error("selected speech model is not available"));
    render(<ChatInput sessionTabId={TAB} />);

    await recordOnce();

    expect(screen.getByRole("alert").textContent).toBe("selected speech model is not available");
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("lets the user dismiss a failed-transcription notice without recording again", async () => {
    vi.spyOn(api, "transcribeSpeech").mockRejectedValue(new Error("STT worker crashed"));
    render(<ChatInput sessionTabId={TAB} />);

    await recordOnce();
    expect(screen.getByRole("alert").textContent).toContain("STT worker crashed");

    fireEvent.click(screen.getByRole("button", { name: "Dismiss voice error" }));

    expect(screen.queryByRole("alert")).toBeNull();
    // Dismissing must not disturb the send path: the failure still sent nothing.
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("disables the mic with an explanatory tooltip when the browser cannot record", () => {
    removeVoiceEnv();
    render(<ChatInput sessionTabId={TAB} />);
    const button = screen.getByRole("button", { name: "Start voice input" }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    expect(button.getAttribute("title")).toBe(VOICE_UNAVAILABLE_MESSAGE);
  });

  it("shows a live partial preview while recording, never sends it, and clears it on stop", async () => {
    vi.spyOn(api, "getSTT").mockResolvedValue({
      selected: "parakeet-tdt-0.6b-v3",
      models: [{ id: "parakeet-tdt-0.6b-v3", label: "Parakeet", engine: "local", languages: "EN", description: "", available: true }],
    });
    const transcribe = vi
      .spyOn(api, "transcribeSpeech")
      .mockResolvedValueOnce({ text: "almost there", model: "m" })
      .mockResolvedValueOnce({ text: "final words", model: "m" });
    render(<ChatInput sessionTabId={TAB} />);

    await act(async () => {
      fireEvent.mouseDown(micButton());
      fireEvent.mouseUp(micButton());
    });
    act(() => FakeMediaRecorder.instances[0].pushChunk(10));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });
    expect(screen.getByText("almost there")).toBeDefined();
    expect(sendMessage).not.toHaveBeenCalled();
    expect(textarea().value).toBe("");

    await act(async () => {
      fireEvent.mouseDown(micButton());
      fireEvent.mouseUp(micButton());
    });
    expect(screen.queryByText("almost there")).toBeNull();
    await flush();
    expect(transcribe).toHaveBeenCalledTimes(2);
    expect(sendMessage).toHaveBeenCalledTimes(1);
    expect(sendMessage).toHaveBeenCalledWith("final words");
  });

  it("cancel stops recording without transcribing or sending anything", async () => {
    const transcribe = vi.spyOn(api, "transcribeSpeech");
    render(<ChatInput sessionTabId={TAB} />);

    await act(async () => {
      fireEvent.mouseDown(micButton());
      fireEvent.mouseUp(micButton());
    });
    expect(screen.getByText("Recording… click to stop")).toBeDefined();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Cancel recording" }));
    });
    expect(screen.queryByText("Recording… click to stop")).toBeNull();
    expect(transcribe).not.toHaveBeenCalled();
    expect(sendMessage).not.toHaveBeenCalled();
    expect(textarea().value).toBe("");
  });

  it("holding past the threshold locks continuous recording; release does not stop, click stop sends", async () => {
    const transcribe = vi.spyOn(api, "transcribeSpeech").mockResolvedValue({ text: "keep talking", model: "m" });
    render(<ChatInput sessionTabId={TAB} />);

    // Press and hold past the threshold: locks continuous mode.
    await act(async () => {
      fireEvent.mouseDown(micButton());
      await vi.advanceTimersByTimeAsync(VOICE_HOLD_THRESHOLD_MS);
    });
    expect(screen.getByText("Recording… click to stop")).toBeDefined();

    // Releasing the hold does NOT stop continuous recording.
    fireEvent.mouseUp(micButton());
    expect(screen.getByText("Recording… click to stop")).toBeDefined();

    // A deliberate click stop transcribes & sends.
    await act(async () => {
      fireEvent.mouseDown(micButton());
      fireEvent.mouseUp(micButton());
    });
    await flush();
    expect(sendMessage).toHaveBeenCalledWith("keep talking");
    expect(transcribe).toHaveBeenCalledTimes(1);
  });

  it("keyboard Space activation starts one recording and the native click does not stop it", async () => {
    const getUserMedia = vi.spyOn(navigator.mediaDevices, "getUserMedia");
    render(<ChatInput sessionTabId={TAB} />);
    micButton().focus();

    await act(async () => {
      fireEvent.keyDown(micButton(), { key: " " });
      fireEvent.keyUp(micButton(), { key: " " });
      // A browser fires the native click after Space keyup; jsdom does not, so mirror it.
      fireEvent.click(micButton());
      // Let the permission promise resolve so the recorder is created.
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(getUserMedia).toHaveBeenCalledTimes(1);
    expect(FakeMediaRecorder.instances).toHaveLength(1);
    expect(FakeMediaRecorder.instances[0].stop).not.toHaveBeenCalled();
    expect(screen.getByText("Recording… click to stop")).toBeDefined();
  });

  it("Enter still sends typed text while the mic is available", async () => {
    render(<ChatInput sessionTabId={TAB} />);
    fireEvent.change(textarea(), { target: { value: "typed" } });
    await act(async () => {
      fireEvent.keyDown(textarea(), { key: "Enter" });
    });
    await flush();
    expect(sendMessage).toHaveBeenCalledWith("typed");
  });
});
