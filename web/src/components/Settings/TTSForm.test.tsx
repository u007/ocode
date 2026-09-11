import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import TTSForm from "./TTSForm";
import { SpeechProvider } from "../Speech/SpeechProvider";
import { api } from "../../api/client";

const engines = [
  { id: "browser-native", label: "Browser Native", availability: "ready", browser_only: true },
  { id: "piper", label: "Piper", availability: "unavailable", reason: "Runtime unavailable", browser_only: false },
];

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
      </SpeechProvider>
    );
    expect(screen.getByText(/Speech playback/i)).toBeDefined();
    expect(await screen.findByText(/Piper/i)).toBeDefined();
    expect(screen.getAllByText(/license/i).length).toBeGreaterThan(0);
    expect(screen.getByText(/Accept License/i)).toBeDefined();
  });

  it("accepts a license, refreshes the state, and does not claim installation", async () => {
    let installState: Record<string, string> = {};
    vi.mocked(api.getTTSState).mockImplementation(async () => installState);
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const body = String(init?.body || "");
      if (body.includes('"license_hash"')) installState = { piper: "license-accepted" };
      return new Response("{}", { status: 200 });
    });
    vi.stubGlobal("fetch", fetchMock);

    render(
      <SpeechProvider>
        <TTSForm />
      </SpeechProvider>,
    );

    const piper = within(await screen.findByTestId("tts-engine-piper"));
    fireEvent.click(piper.getByRole("button", { name: "Accept License" }));

    await waitFor(() => expect(piper.getByText(/License accepted\./i)).toBeDefined());
    expect(piper.getByText(/Install state: license-accepted/i)).toBeDefined();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(String(fetchMock.mock.calls[0][0])).toContain("/api/tts/license");
    expect(String(fetchMock.mock.calls[0][1]?.body)).toContain('"engine":"piper"');
  });
});
