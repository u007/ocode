import { useCallback, useEffect, useState } from "react";
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
  const [installStates, setInstallStates] = useState<Record<string, string>>({});
  const [stateError, setStateError] = useState<string | null>(null);
  const [submittingEngine, setSubmittingEngine] = useState<string | null>(null);
  const [licenseErrors, setLicenseErrors] = useState<Record<string, string>>({});
  const [licenseSuccess, setLicenseSuccess] = useState<Record<string, boolean>>({});
  const canRetry = status?.engine.availability !== "unavailable" && Boolean(status?.error || error);

  const refreshInstallStates = useCallback(async () => {
    try {
      setInstallStates(await api.getTTSState());
      setStateError(null);
    } catch (err) {
      setStateError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void refreshInstallStates();
  }, [refreshInstallStates]);

  const acceptLicense = async (engine: (typeof engines)[number]) => {
    setSubmittingEngine(engine.id);
    setLicenseErrors((previous) => ({ ...previous, [engine.id]: "" }));
    setLicenseSuccess((previous) => ({ ...previous, [engine.id]: false }));
    try {
      await postTTS("/api/tts/license", {
        engine: engine.id,
        license_hash: "sha256-" + engine.id,
        license_name: engine.label + " License",
      });
      await refreshInstallStates();
      setLicenseSuccess((previous) => ({ ...previous, [engine.id]: true }));
    } catch (err) {
      setLicenseErrors((previous) => ({
        ...previous,
        [engine.id]: err instanceof Error ? err.message : String(err),
      }));
    } finally {
      setSubmittingEngine(null);
    }
  };

  const engineStatus = (id: string) => {
    const s = status?.engine;
    return s?.id === id ? s.availability : "unavailable";
  };

  return (
    <section className="space-y-4 rounded-lg border border-border p-4">
      <div>
        <h2 className="text-sm font-semibold">Speech playback</h2>
        <p className="mt-1 text-xs text-muted-foreground">
          Browser Native is the default. Local engines can record license acceptance, but pinning, downloading, installing, and enabling remain unavailable until verified artifacts land.
        </p>
      </div>

      {/* Per-engine install cards */}
      <div className="space-y-3">
        {engines.map((engine) => {
          const isBrowser = engine.id === "browser-native";
          const avail = engine.availability;
          const installState = installStates[engine.id] || "not-accepted";
          const licenseAccepted = installState !== "not-accepted" && installState !== "failed";
          return (
            <div data-testid={`tts-engine-${engine.id}`} key={engine.id} className="rounded-md border border-border bg-card p-3 text-xs shadow-sm">
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
                    <p className="mt-1 text-[10px]">Accept the license to record your choice. Installation is unavailable until verified artifacts land.</p>
                    <div className="mt-2 flex items-center gap-2">
                      <button
                        type="button"
                        disabled={licenseAccepted || submittingEngine === engine.id}
                        className="rounded border border-border bg-background px-2 py-0.5 text-[10px] hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
                        onClick={() => void acceptLicense(engine)}
                      >
                        {submittingEngine === engine.id ? "Accepting…" : licenseAccepted ? "License Accepted" : "Accept License"}
                      </button>
                      <span className="text-[10px] text-muted-foreground">
                        Install state: {installState}
                      </span>
                    </div>
                    {licenseSuccess[engine.id] && (
                      <p className="mt-2 text-[10px] text-green-600 dark:text-green-400">
                        License accepted. This engine remains unavailable until its verified runtime and model manifest are available.
                      </p>
                    )}
                    {licenseErrors[engine.id] && (
                      <p className="mt-2 text-[10px] text-destructive">{licenseErrors[engine.id]}</p>
                    )}
                    {stateError && (
                      <p className="mt-2 text-[10px] text-destructive">Install state unavailable: {stateError}</p>
                    )}
                  </div>

                  {/* Progress / status row for download/install — unavailable until verified artifacts land */}
                  <div className="mt-2 flex items-center gap-2 text-[10px] text-muted-foreground italic">
                    <span>Pin, download, install, and enable are unavailable until a verified runtime and model manifest land.</span>
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
