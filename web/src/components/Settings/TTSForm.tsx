import { apiPath, authHeaders, api } from "@/api/client";
import { useSpeech } from "../Speech/SpeechProvider";

async function postTTS(path: string, body: Record<string, string>): Promise<void> {
  const res = await fetch(apiPath(path), {
    method: "POST",
    headers: { ...authHeaders(), "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const text = (await res.text()).trim();
    throw new Error(`${path} failed (${res.status})${text ? ": " + text : ""}`);
  }
}

export default function TTSForm() {
  const { engines, config, status, error, setMode, retry } = useSpeech();
  const canRetry = status?.engine.availability !== "unavailable" && Boolean(status?.error || error);

  const engineStatus = (id: string) => {
    const s = status?.engine;
    return s?.id === id ? s.availability : "unavailable";
  };

  return (
    <section className="space-y-4 rounded-lg border border-border p-4">
      <div>
        <h2 className="text-sm font-semibold">Speech playback</h2>
        <p className="mt-1 text-xs text-muted-foreground">
          Browser Native is the default. Local engines require: license acceptance → pin manifest → download → install → enable. Only Browser Native is active in v1 until pinned artifacts land.
        </p>
      </div>

      {/* Per-engine install cards */}
      <div className="space-y-3">
        {engines.map((engine) => {
          const isBrowser = engine.id === "browser-native";
          const avail = engine.availability;
          return (
            <div key={engine.id} className="rounded-md border border-border bg-card p-3 text-xs shadow-sm">
              <div className="flex items-center justify-between">
                <div className="font-medium text-sm">{engine.label}</div>
                <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
                  {avail === "ready" ? "ready" : avail === "unavailable" ? "unavailable" : engineStatus(engine.id)}
                </span>
              </div>

              {!isBrowser && (
                <>
                  {/* License prompt */}
                  <div className="mt-2 rounded-md bg-muted/40 p-2 text-[11px] leading-relaxed text-muted-foreground">
                    License: {engine.reason ? engine.reason.split(".")[0] + "." : "Separate license required."}
                    <p className="mt-1 text-[10px]">Accept the license to pin the manifest and begin download.</p>
                    <button
                      type="button"
                      className="mt-2 rounded border border-border bg-background px-2 py-0.5 text-[10px] hover:bg-muted"
                      onClick={async () => {
                        try {
                          const states: Record<string, string> = await api.getTTSState();
                          const current = states[engine.id] || "";
                          if (!current || current === "not-accepted" || current === "failed") {
                            await postTTS("/api/tts/license", { engine: engine.id, license_hash: "sha256-" + engine.id, license_name: engine.label + " License" });
                          }
                          const afterLicense = (await api.getTTSState())[engine.id] || "";
                          if (afterLicense === "license-accepted" || afterLicense === "failed") {
                            await postTTS("/api/tts/pin", { engine: engine.id, manifest_version: "v1" });
                          }
                          const afterPin = (await api.getTTSState())[engine.id] || "";
                          if (afterPin === "pinned" || afterPin === "failed") {
                            await postTTS("/api/tts/download", { engine: engine.id });
                          }
                          const afterDownload = (await api.getTTSState())[engine.id] || "";
                          if (afterDownload === "downloading" || afterDownload === "failed") {
                            await postTTS("/api/tts/install", { engine: engine.id });
                          }
                          const afterInstall = (await api.getTTSState())[engine.id] || "";
                          if (afterInstall === "installed" || afterInstall === "enabled") {
                            await postTTS("/api/tts/enable", { engine: engine.id });
                          }
                          alert("Pipeline completed: " + engine.label);
                        } catch (e) {
                          alert("Install failed: " + (e instanceof Error ? e.message : String(e)));
                        }
                      }}
                    >
                      Accept License → Pin → Download → Install → Enable
                    </button>
                  </div>

                  {/* Progress / status row for download/install — placeholder until backend pipeline lands */}
                  <div className="mt-2 flex items-center gap-2 text-[10px] text-muted-foreground italic">
                    <span>Pipeline blocked: pinned manifest/checksum/download endpoint not implemented (Phase 0).</span>
                  </div>
                </>
              )}

              {isBrowser && (
                <p className="mt-1 text-[10px] text-muted-foreground">No server artifact required. Runs in this browser tab.</p>
              )}
            </div>
          );
        })}
      </div>

      {/* Playback mode */}
      <label className="block space-y-1 text-sm">
        <span className="font-medium">Playback mode</span>
        <select
          className="w-full rounded-md border border-input bg-background px-3 py-2"
          value={config.mode}
          onChange={(event) => void setMode(event.target.value as typeof config.mode)}
        >
          <option value="manual">Manual / Speak</option>
          <option value="at-bottom">Auto-play when at bottom</option>
        </select>
      </label>

      <div className="rounded-md bg-muted/50 p-3 text-xs">
        <div className="font-medium">Status: {status?.state ?? "loading"}</div>
        {status?.error && <div className="mt-1 text-destructive">{status.error}</div>}
        {error && <div className="mt-1 text-destructive">{error}</div>}
        {canRetry && <button type="button" className="mt-2 rounded border border-border px-2 py-1 hover:bg-background" onClick={() => void retry()}>Retry</button>}
      </div>
    </section>
  );
}
