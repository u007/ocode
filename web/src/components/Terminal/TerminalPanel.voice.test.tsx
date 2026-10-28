import { render, act, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TerminalProvider } from "../../stores/terminalStore";
import TerminalPanel from "./TerminalPanel";
import { FakeTerminal } from "./ghosttyWebMock";
import { FakeMediaRecorder, installVoiceEnv, removeVoiceEnv } from "../Chat/voiceTestUtils";
import { VOICE_NOTHING_HEARD_MESSAGE, VOICE_UNAVAILABLE_MESSAGE } from "../Chat/useVoiceRecorder";
import { VOICE_INSECURE_CONTEXT_MESSAGE } from "./useTerminalVoice";
import { api, STTInstallRequiredError } from "@/api/client";
import { reportActionErrorMessage } from "@/lib/actionErrors";

// Voice input on the terminal panel. The shell is a fake socket; the mic is the
// shared FakeMediaRecorder; the transcribe endpoint is api.transcribeSpeech. The
// assertions are on what reaches the pty: term.paste, exactly once, never a send.
const h = vi.hoisted(() => ({
  sockets: [] as MockSocket[],
}));

vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    on: () => () => {},
    off: () => {},
    onReconnect: () => () => {},
    offReconnect: () => {},
  },
}));

vi.mock("ghostty-web", async () => (await import("./ghosttyWebMock")).ghosttyWebModule());
vi.mock("./ghosttyInternals", async () => (await import("./ghosttyWebMock")).ghosttyInternalsMock());
vi.mock("./terminalMouse", async () => (await import("./ghosttyWebMock")).terminalMouseMock());

vi.mock("@/api/client", () => {
  class STTInstallRequiredError extends Error {
    name = "STTInstallRequiredError";
  }
  return {
    api: {
      getTerminalTabs: () => Promise.resolve({ projects: {} }),
      setTerminalTabs: () => Promise.resolve({ status: "ok" }),
      transcribeSpeech: vi.fn(),
      prewarmSpeech: vi.fn(() => Promise.resolve(true)),
      cancelPrewarmSpeech: vi.fn(() => Promise.resolve(true)),
    },
    STTInstallRequiredError,
    authedFetch: () => Promise.resolve({ ok: true, status: 204 }),
    apiPath: (p: string) => p,
    apiWsPath: (p: string) => `ws://localhost${p}`,
    remoteApiBase: (host?: string) => (host ? `/api/remote/${encodeURIComponent(host)}` : ""),
    authToken: () => "tok",
    authHeaders: () => ({}),
    isRemoteSession: () => false,
  };
});
vi.mock("@/lib/actionErrors", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/actionErrors")>()),
  reportActionErrorMessage: vi.fn(),
}));
vi.mock("./terminalAlertSound", () => ({ playAlertSound: vi.fn() }));
vi.mock("./terminalFocus", () => ({
  registerTerminalFocus: vi.fn(),
  unregisterTerminalFocus: vi.fn(),
}));
vi.mock("./terminalPersistence", () => ({
  loadProjectTerminals: () => ({ terminals: [{ id: "t1", title: "Terminal 1" }], activeId: "t1" }),
  saveProjectTerminals: vi.fn(),
  readAllMirrorProjects: () => ({}),
  projectTerminalsKey: (path: string, host?: string) => (host ? `${host}::${path}` : path),
  loadTerminalBuffer: () => null,
  saveTerminalBuffer: vi.fn(),
}));
vi.mock("@/lib/debug/terminalRegistry", () => ({
  registerTerminal: vi.fn(),
  unregisterTerminal: vi.fn(),
}));

class MockSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 3;
  readyState = MockSocket.OPEN;
  binaryType = "";
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: unknown }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: ((e: { wasClean: boolean; code: number; reason: string }) => void) | null = null;
  send = vi.fn();
  close = vi.fn();
  constructor(public url: string) {
    h.sockets.push(this);
  }
}

const transcribeSpeech = vi.mocked(api.transcribeSpeech);
const reportError = vi.mocked(reportActionErrorMessage);

beforeEach(() => {
  FakeTerminal.instances = [];
  h.sockets.length = 0;
  window.localStorage.clear();
  reportError.mockClear();
  transcribeSpeech.mockReset();
  vi.spyOn(console, "error").mockImplementation(() => {});
  vi.spyOn(console, "warn").mockImplementation(() => {});
  vi.stubGlobal("WebSocket", MockSocket as unknown as typeof WebSocket);
  vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response("", { status: 404 }))));
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
  vi.stubGlobal("requestAnimationFrame", (() => 0) as unknown as typeof requestAnimationFrame);
  vi.stubGlobal("cancelAnimationFrame", (() => {}) as unknown as typeof cancelAnimationFrame);
  installVoiceEnv();
});

afterEach(() => {
  removeVoiceEnv();
  Object.defineProperty(window, "isSecureContext", { value: true, configurable: true });
  vi.restoreAllMocks();
});

/** Mounts the panel and drives the fake socket through open + attach, the
 *  handshake after which term.paste is forwarded to the pty. */
async function mountAttached(): Promise<MockSocket> {
  const sock = await mountDetached();
  act(() => {
    sock.onopen?.();
    sock.onmessage?.({ data: JSON.stringify({ type: "attach", resumed: true }) });
  });
  return sock;
}

/** Mounts the panel; the socket exists but has not completed the attach handshake. */
async function mountDetached(): Promise<MockSocket> {
  render(
    <TerminalProvider>
      <TerminalPanel active id="t1" projectPath="/proj" scrollbackLines={1000} fontFamily="mono" fontSize={12} />
    </TerminalProvider>,
  );
  await waitFor(() => expect(h.sockets.length).toBe(1));
  return h.sockets[0];
}

function paste() {
  return FakeTerminal.instances[0].paste;
}

function micButton(name: string) {
  return screen.getByRole("button", { name }) as HTMLButtonElement;
}

/** One tap to start, one to stop; the recording's transcript resolves on its own. */
async function dictate() {
  await act(async () => {
    fireEvent.click(micButton("Start voice input"));
  });
  // The recorder is created once getUserMedia resolves; a stop tapped before
  // that is a cancelled start, so wait for it.
  await waitFor(() => expect(FakeMediaRecorder.instances).toHaveLength(1));
  await act(async () => {
    fireEvent.click(micButton("Stop recording"));
  });
}

describe("TerminalPanel voice input", () => {
  it("pastes the transcript once, with no trailing newline, and never sends it on its own", async () => {
    transcribeSpeech.mockResolvedValue({ text: "run the tests", model: "m" });
    const sock = await mountAttached();

    await dictate();

    await waitFor(() => expect(paste()).toHaveBeenCalledTimes(1));
    expect(paste()).toHaveBeenCalledWith("run the tests");
    expect(transcribeSpeech).toHaveBeenCalledWith(expect.any(Blob), "recording.webm");
    // Nothing reaches the socket: the paste goes through the terminal, and the user presses Return.
    const sent = sock.send.mock.calls.map((call) => String(call[0])).join("");
    expect(sent).not.toMatch(/[\r\n]/);
  });

  it("trims the transcript and collapses internal newlines so it stays one line", async () => {
    transcribeSpeech.mockResolvedValue({ text: "  ls\n  -la\n", model: "m" });
    await mountAttached();

    await dictate();

    await waitFor(() => expect(paste()).toHaveBeenCalledTimes(1));
    expect(paste()).toHaveBeenCalledWith("ls -la");
  });

  it("ignores an empty transcript: nothing is pasted and the user is told nothing was heard", async () => {
    transcribeSpeech.mockResolvedValue({ text: "   ", model: "m" });
    await mountAttached();

    await dictate();

    await waitFor(() => expect(reportError).toHaveBeenCalledWith(VOICE_NOTHING_HEARD_MESSAGE));
    expect(paste()).not.toHaveBeenCalled();
  });

  it("asks before installing voice dependencies, then retries with approval and pastes", async () => {
    transcribeSpeech
      .mockRejectedValueOnce(new STTInstallRequiredError("Voice engine needs onnx-asr"))
      .mockResolvedValueOnce({ text: "hello there", model: "m" });
    await mountAttached();

    await dictate();

    fireEvent.click(await screen.findByRole("button", { name: "Install" }));

    await waitFor(() => expect(paste()).toHaveBeenCalledWith("hello there"));
    expect(transcribeSpeech).toHaveBeenCalledTimes(2);
    expect(transcribeSpeech).toHaveBeenLastCalledWith(expect.any(Blob), "recording.webm", {
      installApproved: true,
    });
  });

  it("does not install when the install is declined, and reports why nothing was pasted", async () => {
    transcribeSpeech.mockRejectedValueOnce(new STTInstallRequiredError("Voice engine needs onnx-asr"));
    await mountAttached();

    await dictate();

    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(reportError).toHaveBeenCalledWith("Voice input needs the dependency install; it was declined."),
    );
    expect(transcribeSpeech).toHaveBeenCalledTimes(1);
    expect(paste()).not.toHaveBeenCalled();
  });

  it("is disabled until the shell is attached, and a tap on it does not take focus from the terminal", async () => {
    const sock = await mountDetached();
    const mic = micButton("Start voice input");
    expect(mic.disabled).toBe(true);
    expect(mic.title).toBe("Terminal is not connected");

    act(() => {
      sock.onopen?.();
      sock.onmessage?.({ data: JSON.stringify({ type: "attach", resumed: true }) });
    });

    expect(micButton("Start voice input").disabled).toBe(false);
    // mousedown is cancelled, so the browser keeps focus where it was (the terminal).
    expect(fireEvent.mouseDown(micButton("Start voice input"))).toBe(false);
  });

  it("keeps stop working when the connection drops mid-recording, and does not paste into a dead socket", async () => {
    transcribeSpeech.mockResolvedValue({ text: "lost words", model: "m" });
    const sock = await mountAttached();
    await act(async () => {
      fireEvent.click(micButton("Start voice input"));
    });
    await waitFor(() => expect(FakeMediaRecorder.instances).toHaveLength(1));

    // A real dropped socket reads CLOSED; the paste guard depends on that.
    sock.readyState = MockSocket.CLOSED;
    act(() => {
      sock.onclose?.({ wasClean: false, code: 1006, reason: "" });
    });

    const stop = micButton("Stop recording");
    expect(stop.disabled).toBe(false);
    await act(async () => {
      fireEvent.click(stop);
    });

    await waitFor(() =>
      expect(reportError).toHaveBeenCalledWith(expect.stringMatching(/^Terminal is not connected/)),
    );
    expect(reportError).toHaveBeenCalledWith(expect.stringContaining("lost words"));
    expect(paste()).not.toHaveBeenCalled();
  });

  it("explains an insecure context instead of failing silently", async () => {
    Object.defineProperty(window, "isSecureContext", { value: false, configurable: true });
    vi.stubGlobal("MediaRecorder", undefined);
    await mountAttached();

    const mic = micButton("Start voice input");
    expect(mic.getAttribute("aria-disabled")).toBe("true");
    expect(mic.title).toBe(VOICE_INSECURE_CONTEXT_MESSAGE);

    await act(async () => {
      fireEvent.click(mic);
    });

    expect(reportError).toHaveBeenCalledWith(VOICE_INSECURE_CONTEXT_MESSAGE);
    expect(FakeMediaRecorder.instances).toHaveLength(0);
  });

  it("explains a browser without recording support on a secure page", async () => {
    vi.stubGlobal("MediaRecorder", undefined);
    await mountAttached();

    await act(async () => {
      fireEvent.click(micButton("Start voice input"));
    });

    expect(reportError).toHaveBeenCalledWith(VOICE_UNAVAILABLE_MESSAGE);
  });
});
