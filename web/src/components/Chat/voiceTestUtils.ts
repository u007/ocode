import { vi } from "vitest";

/**
 * Test double for MediaRecorder + getUserMedia. The fake mirrors what browsers
 * do on stop(): one dataavailable with the clip, then onstop.
 */
export class FakeMediaRecorder {
  static supported = new Set<string>(["audio/webm;codecs=opus"]);
  static instances: FakeMediaRecorder[] = [];
  static isTypeSupported = vi.fn((type: string) => FakeMediaRecorder.supported.has(type));

  state: "inactive" | "recording" = "inactive";
  mimeType: string;
  /** Set to make stop() deliver no data, as an empty recording does. */
  emitData = true;
  ondataavailable: ((event: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(
    readonly stream: MediaStream,
    readonly options?: { mimeType?: string },
  ) {
    this.mimeType = options?.mimeType ?? "audio/webm";
    FakeMediaRecorder.instances.push(this);
  }

  /** Timeslice passed to start(), in ms. Undefined means one chunk on stop(). */
  timeslice: number | undefined;

  start = vi.fn((timeslice?: number) => {
    this.state = "recording";
    this.timeslice = timeslice;
  });

  /** Simulates a timeslice dataavailable event carrying `size` bytes of audio. */
  pushChunk(size: number) {
    if (this.state !== "recording") return;
    this.ondataavailable?.({ data: new Blob([new Uint8Array(size)], { type: this.mimeType }) });
  }

  stop = vi.fn(() => {
    if (this.state === "inactive") return;
    this.state = "inactive";
    if (this.emitData) this.ondataavailable?.({ data: new Blob(["clip"], { type: this.mimeType }) });
    this.onstop?.();
  });

  static reset() {
    FakeMediaRecorder.supported = new Set<string>(["audio/webm;codecs=opus"]);
    FakeMediaRecorder.instances = [];
    FakeMediaRecorder.isTypeSupported.mockClear();
  }
}

export interface VoiceEnv {
  getUserMedia: ReturnType<typeof vi.fn>;
  trackStop: ReturnType<typeof vi.fn>;
}

/** Installs a microphone (getUserMedia) and MediaRecorder in the jsdom global. */
export function installVoiceEnv(): VoiceEnv {
  FakeMediaRecorder.reset();
  const trackStop = vi.fn();
  const getUserMedia = vi.fn(
    async () => ({ getTracks: () => [{ stop: trackStop }] }) as unknown as MediaStream,
  );
  vi.stubGlobal("MediaRecorder", FakeMediaRecorder);
  Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: { getUserMedia } });
  return { getUserMedia, trackStop };
}

/** Makes the browser look like it has no microphone recording support. */
export function removeVoiceEnv() {
  Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: undefined });
  vi.unstubAllGlobals();
  FakeMediaRecorder.reset();
}
