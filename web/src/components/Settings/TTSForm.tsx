import { useCallback, useEffect, useState } from "react";
import { api } from "@/api/client";
import type { TTSEngine, TTSInstallState } from "@/api/types";
import { useSpeech } from "../Speech/SpeechProvider";

const POLL_MS = 1000;

function errorText(err: unknown) {
  return err instanceof Error ? err.message : String(err);
}

export default function TTSForm() {
  const { engines, config, status, error, setMode, retry, selectEngine, refresh } = useSpeech();
  const [installStates, setInstallStates] = useState<Record<string, TTSInstallState>>({});
  const [stateError, setStateError] = useState<string | null>(null);
  const [busyEngine, setBusyEngine] = useState<string | null>(null);
  const [engineErrors, setEngineErrors] = useState<Record<string, string>>({});
  const canRetry = status?.engine.availability !== "unavailable" && Boolean(status?.error || error);

  const refreshInstallStates = useCallback(async () => {
    try {
      setInstallStates(await api.getTTSState());
      setStateError(null);
    } catch (err) {
      setStateError(errorText(err));
    }
  }, []);

  useEffect(() => {
    void refreshInstallStates();
  }, [refreshInstallStates]);

  // Poll while any engine is downloading/installing so progress is visible.
  const installing = Object.values(installStates).some((s) => s.state === "downloading");
  useEffect(() => {
    if (!installing) return;
    const id = window.setInterval(() => void refreshInstallStates(), POLL_MS);
    return () => window.clearInterval(id);
  }, [installing, refreshInstallStates]);

  const run = async (engine: TTSEngine, action: () => Promise<void>) => {
    setBusyEngine(engine.id);
    setEngineErrors((previous) => ({ ...previous, [engine.id]: "" }));
    try {
      await action();
      await refreshInstallStates();
    } catch (err) {
      setEngineErrors((previous) => ({ ...previous, [engine.id]: errorText(err) }));
    } finally {
      setBusyEngine(null);
    }
  };

  const acceptLicense = (engine: TTSEngine) =>
    run(engine, async () => {
      await api.ttsAcceptLicense(engine.id, engine.license_name ?? engine.label + " license");
    });

  const install = (engine: TTSEngine, current: TTSInstallState | undefined) =>
    run(engine, async () => {
      if (!engine.manifest_version) throw new Error("engine has no pinned manifest");
      if (current?.state !== "pinned") await api.ttsPin(engine.id, engine.manifest_version);
      await api.ttsDownload(engine.id);
    });

  const enable = (engine: TTSEngine) =>
    run(engine, async () => {
      await api.ttsEnable(engine.id);
      await refresh();
    });

  const renderLocalEngine = (engine: TTSEngine) => {
    const inst = installStates[engine.id];
    const state = inst?.state ?? "not-accepted";
    const busy = busyEngine === engine.id;
    const isCurrent = config.engine === engine.id;
    if (engine.availability === "unavailable") {
      return <p className="mt-2 text-[11px] text-muted-foreground">{engine.reason}</p>;
    }
    return (
      <div className="mt-2 space-y-2 text-[11px] text-muted-foreground">
        <p>
          Voice: <span className="font-mono">{engine.voice_id}</span> · Manifest <span className="font-mono">{engine.manifest_version}</span>
        </p>
        <p>
          License: {engine.license_name}
          {engine.license_url && (
            <>
              {" "}
              <a className="underline" href={engine.license_url} target="_blank" rel="noreferrer">view</a>
            </>
          )}
        </p>
        <div className="flex flex-wrap items-center gap-2">
          {state === "not-accepted" && (
            <button type="button" disabled={busy} className="rounded border border-border bg-background px-2 py-0.5 text-[10px] hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50" onClick={() => void acceptLicense(engine)}>
              {busy ? "Accepting…" : "Accept License"}
            </button>
          )}
          {(state === "license-accepted" || state === "pinned" || state === "failed") && (
            <button type="button" disabled={busy} className="rounded border border-border bg-background px-2 py-0.5 text-[10px] hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50" onClick={() => void install(engine, inst)}>
              {busy ? "Starting…" : state === "failed" ? "Retry Install" : "Install"}
            </button>
          )}
          {state === "installed" && (
            <button type="button" disabled={busy} className="rounded border border-border bg-background px-2 py-0.5 text-[10px] hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50" onClick={() => void enable(engine)}>
              {busy ? "Enabling…" : "Enable"}
            </button>
          )}
          {state === "enabled" && !isCurrent && (
            <button type="button" disabled={busy} className="rounded border border-border bg-background px-2 py-0.5 text-[10px] hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50" onClick={() => void selectEngine(engine.id)}>
              Use
            </button>
          )}
          <span>Install state: {state}</span>
        </div>
        {state === "downloading" && (
          <div>
            <div className="h-1.5 w-full overflow-hidden rounded bg-muted">
              <div className="h-full bg-primary transition-[width]" style={{ width: `${inst?.progress ?? 0}%` }} />
            </div>
            <p className="mt-1">{inst?.progress ?? 0}% {inst?.step ? `· ${inst.step}` : ""}</p>
          </div>
        )}
        {inst?.error && <p className="text-destructive">{inst.error}</p>}
        {engineErrors[engine.id] && <p className="text-destructive">{engineErrors[engine.id]}</p>}
      </div>
    );
  };

  return (
    <section className="space-y-4 rounded-lg border border-border p-4">
      <div>
        <h2 className="text-sm font-semibold">Speech playback</h2>
        <p className="mt-1 text-xs text-muted-foreground">
          Browser Native is the default. Local engines download a pinned runtime and voice into the ocode data directory after you accept their license.
        </p>
      </div>

      <div className="space-y-3">
        {engines.map((engine) => {
          const isBrowser = engine.id === "browser-native";
          const isCurrent = config.engine === engine.id;
          return (
            <div data-testid={`tts-engine-${engine.id}`} key={engine.id} className="rounded-md border border-border bg-card p-3 text-xs shadow-sm">
              <div className="flex items-center justify-between gap-2">
                <div className="font-medium text-sm">{engine.label}</div>
                <div className="flex items-center gap-2">
                  {isCurrent && <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[10px] text-primary">current</span>}
                  <span className="text-[10px] uppercase tracking-wide text-muted-foreground">{engine.availability}</span>
                </div>
              </div>
              {isBrowser ? (
                <div className="mt-1 flex items-center justify-between gap-2 text-[10px] text-muted-foreground">
                  <span>No server artifact required. Runs in this browser tab.</span>
                  {!isCurrent && (
                    <button type="button" className="rounded border border-border bg-background px-2 py-0.5 hover:bg-muted" onClick={() => void selectEngine(engine.id)}>Use</button>
                  )}
                </div>
              ) : renderLocalEngine(engine)}
            </div>
          );
        })}
        {stateError && <p className="text-[10px] text-destructive">Install state unavailable: {stateError}</p>}
      </div>

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
