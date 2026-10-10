import { beforeEach, describe, expect, it, vi } from "vitest";

/** Speech-to-text client: request shape for the model picker and the upload. */
function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("speech-to-text client", () => {
  let fetchSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    window.history.replaceState(null, "", "/");
    fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
  });

  it("setSTTModel PUTs {model} as JSON to /api/stt", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ selected: "base", models: [] }));
    const { api } = await import("./client");
    const res = await api.setSTTModel("base");
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/stt");
    const init = fetchSpy.mock.calls[0][1] as RequestInit;
    expect(init.method).toBe("PUT");
    expect(JSON.parse(init.body as string)).toEqual({ model: "base" });
    expect(res.selected).toBe("base");
  });

  it("transcribeSpeech uploads multipart field audio with the real extension and no forced Content-Type", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ text: "hi", model: "tiny" }));
    const { api } = await import("./client");
    const blob = new Blob(["x"], { type: "audio/mp4" });
    const res = await api.transcribeSpeech(blob, "recording.mp4");
    expect(fetchSpy.mock.calls[0][0]).toBe("/api/stt/transcribe");
    const init = fetchSpy.mock.calls[0][1] as RequestInit;
    expect(init.method).toBe("POST");
    expect(init.body).toBeInstanceOf(FormData);
    const audio = (init.body as FormData).get("audio") as File;
    expect(audio.name).toBe("recording.mp4");
    const headers = new Headers(init.headers as HeadersInit);
    expect(headers.has("Content-Type")).toBe(false);
    expect(res).toEqual({ text: "hi", model: "tiny" });
  });

  it("surfaces the JSON error message and status from a failed transcription", async () => {
    fetchSpy.mockResolvedValue(jsonResponse({ error: "selected speech model is not available" }, 503));
    const { api, ApiError } = await import("./client");
    const err = await api.transcribeSpeech(new Blob(["x"]), "recording.webm").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as InstanceType<typeof ApiError>).status).toBe(503);
    expect((err as Error).message).toBe("selected speech model is not available");
  });

  it("falls back to a plain-text error body", async () => {
    fetchSpy.mockResolvedValue(new Response("audio is larger than 25 MB", { status: 413 }));
    const { api } = await import("./client");
    await expect(api.transcribeSpeech(new Blob(["x"]), "recording.webm")).rejects.toThrow("audio is larger than 25 MB");
  });
});
