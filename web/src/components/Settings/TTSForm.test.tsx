import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import TTSForm from "./TTSForm";
import { SpeechProvider } from "../Speech/SpeechProvider";
import { api } from "../../api/client";

const engines = [
  { id: "browser-native", label: "Browser Native", availability: "ready", browser_only: true },
  { id: "piper", label: "Piper", availability: "installable", reason: "Accept the license and install to enable.", browser_only: false, voice_id: "en_US-joe-medium", manifest_version: "piper-1.8.0-joe-1", license_name: "piper-tts GPL-3.0-or-later" },
  { id: "kokoro", label: "Kokoro", availability: "unavailable", reason: "No verified manifest.", browser_only: false },
];

const piperState = (state: string, extra: Record<string, unknown> = {}) => ({
  piper: { engine_id: "piper", state, progress: 0, pinned: false, ...extra },
}) as never;

beforeEach(() => {
  vi.spyOn(api, "getTTSEngines").mockResolvedValue({ engines } as never);
  vi.spyOn(api, "getTTSConfig").mockResolvedValue({ engine: "browser-native", voice: "", mode: "manual" });
  vi.spyOn(api, "getTTSState").mockResolvedValue({});
  vi.spyOn(api, "getTTSStatus").mockResolvedValue({
    config: { engine: "browser-native", voice: "", mode: "manual" },
    engine: engines[0],
    host: "browser",
    state: "ready",
    hardware: "browser",
    playback: { status: "stopped", text: "", position: 0, duration: 0, selection_generation: 0 },
    selection_generation: 0,
  } as never);
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("TTSForm licensing and selection", () => {
  it("renders per-engine cards with license prompt", async () => {
    render(
      <SpeechProvider>
        <TTSForm />
      </SpeechProvider>,
    );
    const piper = within(await screen.findByTestId("tts-engine-piper"));
    expect(piper.getByText(/License: piper-tts GPL-3.0-or-later/i)).toBeDefined();
    expect(piper.getByRole("button", { name: "Accept License" })).toBeDefined();
    const kokoro = within(await screen.findByTestId("tts-engine-kokoro"));
    expect(kokoro.getByText(/No verified manifest\./i)).toBeDefined();
    expect(kokoro.queryByRole("button")).toBeNull();
  });

  it("accepts a license and shows the install button without installing", async () => {
    let state = {} as Record<string, unknown>;
    vi.mocked(api.getTTSState).mockImplementation(async () => state as never);
    const accept = vi.spyOn(api, "ttsAcceptLicense").mockImplementation(async () => {
      state = piperState("license-accepted");
      return { state: "license-accepted" };
    });
    const pin = vi.spyOn(api, "ttsPin").mockResolvedValue({ state: "pinned" });
    const download = vi.spyOn(api, "ttsDownload").mockResolvedValue({ state: "downloading" });

    render(
      <SpeechProvider>
        <TTSForm />
      </SpeechProvider>,
    );

    const piper = within(await screen.findByTestId("tts-engine-piper"));
    fireEvent.click(piper.getByRole("button", { name: "Accept License" }));

    await waitFor(() => expect(piper.getByText(/Install state: license-accepted/i)).toBeDefined());
    expect(accept).toHaveBeenCalledWith("piper", "piper-tts GPL-3.0-or-later");
    expect(piper.getByRole("button", { name: "Install" })).toBeDefined();
    expect(pin).not.toHaveBeenCalled();
    expect(download).not.toHaveBeenCalled();
  });

  it("install pins the manifest version, starts the download, and shows progress", async () => {
    let state = piperState("license-accepted") as Record<string, unknown>;
    vi.mocked(api.getTTSState).mockImplementation(async () => state as never);
    const pin = vi.spyOn(api, "ttsPin").mockResolvedValue({ state: "pinned" });
    const download = vi.spyOn(api, "ttsDownload").mockImplementation(async () => {
      state = piperState("downloading", { progress: 42, step: "installing piper-tts" });
      return { state: "downloading" };
    });

    render(
      <SpeechProvider>
        <TTSForm />
      </SpeechProvider>,
    );

    const piper = within(await screen.findByTestId("tts-engine-piper"));
    fireEvent.click(await piper.findByRole("button", { name: "Install" }));

    await waitFor(() => expect(piper.getByText(/42%/)).toBeDefined());
    expect(pin).toHaveBeenCalledWith("piper", "piper-1.8.0-joe-1");
    expect(download).toHaveBeenCalledWith("piper");
    expect(piper.getByText(/installing piper-tts/)).toBeDefined();
  });

  it("enable calls the enable endpoint once installed", async () => {
    let state = piperState("installed", { progress: 100 }) as Record<string, unknown>;
    vi.mocked(api.getTTSState).mockImplementation(async () => state as never);
    const enable = vi.spyOn(api, "ttsEnable").mockImplementation(async () => {
      state = piperState("enabled", { progress: 100 });
      return { config: { engine: "piper", voice: "en_US-joe-medium", mode: "manual" } } as never;
    });

    render(
      <SpeechProvider>
        <TTSForm />
      </SpeechProvider>,
    );

    const piper = within(await screen.findByTestId("tts-engine-piper"));
    fireEvent.click(await piper.findByRole("button", { name: "Enable" }));

    await waitFor(() => expect(piper.getByText(/Install state: enabled/i)).toBeDefined());
    expect(enable).toHaveBeenCalledWith("piper");
  });
});
