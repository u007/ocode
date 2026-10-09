import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import TTSForm from "./TTSForm";
import { SpeechProvider } from "../Speech/SpeechProvider";
import { ChatProvider } from "../../stores/chatStore";
import { api } from "../../api/client";

const engines = [
  { id: "browser-native", label: "Browser Native", availability: "ready", browser_only: true },
  { id: "piper", label: "Piper", availability: "installable", reason: "Accept the license and install to enable.", browser_only: false, voice_id: "en_US-joe-medium", manifest_version: "piper-1.8.0-joe-1", license_name: "piper-tts GPL-3.0-or-later", license_text: "piper-tts GPL-3.0-or-later", license_hash: "piper-license-hash" },
  { id: "paradee", label: "Paradee", availability: "installable", reason: "Accept the license and install to enable.", browser_only: false, voice_id: "af_heart", voices: ["af_heart"], manifest_version: "paradee-1.0-f662642", license_name: "Paradee Apache-2.0", license_text: "Paradee model and inference code: Apache-2.0", license_hash: "paradee-license-hash" },
  { id: "melo", label: "MeloTTS", availability: "installable", reason: "Accept the license and install to enable.", browser_only: false, voice_id: "EN-US", voices: ["EN-US", "EN-BR", "EN_INDIA", "EN-AU", "EN-Default"], manifest_version: "melo-0.1.2-en-v2-2091453", license_name: "MeloTTS MIT", license_text: "MeloTTS: MIT", license_hash: "melo-license-hash" },
  { id: "kokoro", label: "Kokoro", availability: "installable", reason: "Accept the license and install to enable.", browser_only: false, voice_id: "af_sarah", voices: ["af_sarah", "af_bella"], manifest_version: "kokoro-v1.0-voices-v1.0", license_name: "kokoro-onnx MIT; kokoro model Apache-2.0", license_text: "kokoro-onnx: MIT\nkokoro model: Apache-2.0", license_hash: "kokoro-license-hash" },
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

function renderTTSForm() {
  return render(
    <ChatProvider>
      <SpeechProvider>
        <TTSForm />
      </SpeechProvider>
    </ChatProvider>,
  );
}

describe("TTSForm licensing and selection", () => {
  it("renders per-engine cards with license prompt", async () => {
    renderTTSForm();
    const piper = within(await screen.findByTestId("tts-engine-piper"));
    expect(piper.getByText(/License: piper-tts GPL-3.0-or-later/i)).toBeDefined();
    expect(piper.getByRole("button", { name: "Accept License" })).toBeDefined();
    const kokoro = within(await screen.findByTestId("tts-engine-kokoro"));
    expect(kokoro.getByText(/kokoro-onnx: MIT/i)).toBeDefined();
    expect(kokoro.getByRole("button", { name: "Accept License" })).toBeDefined();
  });

  it("renders Paradee and MeloTTS between Piper and Kokoro", async () => {
    // The engine order comes straight from the server catalog (internal/tts
    // Catalog()) because TTSForm maps the response without re-sorting, so the
    // rendered order is the presentation contract.
    renderTTSForm();
    await screen.findByTestId("tts-engine-melo");
    const order = ["tts-engine-browser-native", "tts-engine-piper", "tts-engine-paradee", "tts-engine-melo", "tts-engine-kokoro"];
    const positions = await Promise.all(
      order.map(async (testid) => {
        const card = await screen.findByTestId(testid);
        expect(card).toBeDefined();
        return Array.from(document.querySelectorAll("[data-testid]")).indexOf(card);
      }),
    );
    const sorted = [...positions].sort((a, b) => a - b);
    expect(positions).toEqual(sorted);
    expect(positions.every((p) => p >= 0)).toBe(true);
  });

  it("offers the Paradee license before install", async () => {
    renderTTSForm();
    const paradee = within(await screen.findByTestId("tts-engine-paradee"));
    expect(paradee.getByText(/License: Paradee model and inference code: Apache-2.0/i)).toBeDefined();
    expect(paradee.getByRole("button", { name: "Accept License" })).toBeDefined();
  });

  it("offers the MeloTTS license, manifest and accent voices", async () => {
    renderTTSForm();
    const melo = within(await screen.findByTestId("tts-engine-melo"));
    expect(melo.getByText(/License: MeloTTS: MIT/i)).toBeDefined();
    expect(melo.getByText(/melo-0\.1\.2-en-v2-2091453/)).toBeDefined();
    expect(melo.getByRole("button", { name: "Accept License" })).toBeDefined();
    const voiceSelect = melo.getByDisplayValue("EN-US") as HTMLSelectElement;
    for (const accent of ["EN-US", "EN-BR", "EN_INDIA", "EN-AU", "EN-Default"]) {
      expect(Array.from(voiceSelect.options).map((o) => o.value)).toContain(accent);
    }
  });

  it("installs MeloTTS only after the license is accepted", async () => {
    let state = {} as Record<string, unknown>;
    vi.mocked(api.getTTSState).mockImplementation(async () => state as never);
    const accept = vi.spyOn(api, "ttsAcceptLicense").mockImplementation(async () => {
      state = { melo: { engine_id: "melo", state: "license-accepted", progress: 0, pinned: false } };
      return { state: "license-accepted" };
    });
    const pin = vi.spyOn(api, "ttsPin").mockResolvedValue({ state: "pinned" });
    const download = vi.spyOn(api, "ttsDownload").mockResolvedValue({ state: "downloading" });

    renderTTSForm();
    const melo = within(await screen.findByTestId("tts-engine-melo"));
    fireEvent.click(melo.getByRole("button", { name: "Accept License" }));

    await waitFor(() => {
      expect(accept).toHaveBeenCalledWith("melo", "melo-license-hash", "MeloTTS MIT");
    });
    // Accepting consent is not installing: no pin or download until asked.
    expect(pin).not.toHaveBeenCalled();
    expect(download).not.toHaveBeenCalled();

    const installButton = await waitFor(() => melo.getByRole("button", { name: "Install" }));
    fireEvent.click(installButton);
    await waitFor(() => {
      expect(pin).toHaveBeenCalledWith("melo", "melo-0.1.2-en-v2-2091453");
    });
    expect(download).toHaveBeenCalledWith("melo");
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

    renderTTSForm();

    const piper = within(await screen.findByTestId("tts-engine-piper"));
    fireEvent.click(piper.getByRole("button", { name: "Accept License" }));

    await waitFor(() => expect(piper.getByText(/Install state: license-accepted/i)).toBeDefined());
    expect(accept).toHaveBeenCalledWith("piper", "piper-license-hash", "piper-tts GPL-3.0-or-later");
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

    renderTTSForm();

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

    renderTTSForm();

    const piper = within(await screen.findByTestId("tts-engine-piper"));
    fireEvent.click(await piper.findByRole("button", { name: "Enable" }));

    await waitFor(() => expect(piper.getByText(/Install state: enabled/i)).toBeDefined());
    expect(enable).toHaveBeenCalledWith("piper", "default");
  });

  it("saves per-model voice override", async () => {
    let state = piperState("enabled", { progress: 100 }) as Record<string, unknown>;
    vi.mocked(api.getTTSState).mockImplementation(async () => state as never);
    const setVoice = vi.spyOn(api, "ttsModelVoice").mockResolvedValue({ engine: "piper", model: "claude-sonnet", voice: "en_US-joe-medium" });

    renderTTSForm();

    const piper = within(await screen.findByTestId("tts-engine-piper"));
    const modelInput = piper.getByPlaceholderText("model id");
    fireEvent.change(modelInput, { target: { value: "claude-sonnet" } });
    fireEvent.click(piper.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(setVoice).toHaveBeenCalledWith("piper", "claude-sonnet", "en_US-joe-medium");
    });
  });

  it("keys model voice selection by engine and model", async () => {
    const save = vi.spyOn(api, "ttsModelVoice").mockResolvedValue({ engine: "kokoro", model: "model-b", voice: "af_sarah" });
    renderTTSForm();

    const kokoro = within(await screen.findByTestId("tts-engine-kokoro"));
    const model = kokoro.getByRole("textbox");
    const voice = kokoro.getByRole("combobox");
    fireEvent.change(voice, { target: { value: "af_bella" } });
    fireEvent.change(model, { target: { value: "model-b" } });

    expect(voice).toHaveValue("af_sarah");
    fireEvent.click(kokoro.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(save).toHaveBeenCalledWith("kokoro", "model-b", "af_sarah"));
  });

  it("shows model voice save failures", async () => {
    vi.spyOn(api, "ttsModelVoice").mockRejectedValue(new Error("save failed"));
    renderTTSForm();

    const kokoro = within(await screen.findByTestId("tts-engine-kokoro"));
    fireEvent.click(kokoro.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(kokoro.getByText("save failed")).toBeDefined());
  });
});
