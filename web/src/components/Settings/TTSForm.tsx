import { useCallback, useEffect, useState } from "react";
import { api } from "@/api/client";
import type { TTSEngine, TTSInstallState } from "@/api/types";
import { useSpeech } from "../Speech/SpeechProvider";
import { isHTTPURL, openExternalURL } from "../../lib/externalLinks";
import { speechSummaryDisplay } from "../../lib/speechSummaryConfig";
import ModelDialog from "../Layout/ModelDialog";

const POLL_MS = 1000;

function errorText(err: unknown) {
  return err instanceof Error ? err.message : String(err);
}

export default function TTSForm() {
  const {
    engines, config, status, error, setMode, retry, selectEngine, refresh,
    summaryEnabled, summaryModel, setSummaryEnabled, setSummaryModel,
  } = useSpeech();
  const [summaryDialogOpen, setSummaryDialogOpen] = useState(false);
  // A rejected write rethrows from the provider; surface it here rather than
  // leaving a control that looks accepted but was not.
  const [summaryError, setSummaryError] = useState<string | null>(null);
  const [installStates, setInstallStates] = useState<Record<string, TTSInstallState>>({});
  const [stateError, setStateError] = useState<string | null>(null);
  const [busyEngine, setBusyEngine] = useState<string | null>(null);
  const [engineErrors, setEngineErrors] = useState<Record<string, string>>({});
  const [modelId, setModelId] = useState<string>("default");
  const [modelVoiceState, setModelVoiceState] = useState<Record<string, string>>({});
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
      if (!engine.license_hash || !engine.license_name) throw new Error("engine has no authoritative license metadata");
      await api.ttsAcceptLicense(engine.id, engine.license_hash, engine.license_name);
    });

  const install = (engine: TTSEngine, current: TTSInstallState | undefined) =>
    run(engine, async () => {
      if (!engine.manifest_version) throw new Error("engine has no pinned manifest");
      if (current?.state !== "pinned") await api.ttsPin(engine.id, engine.manifest_version);
      await api.ttsDownload(engine.id);
    });

	const enable = (engine: TTSEngine) =>
    run(engine, async () => {
      await api.ttsEnable(engine.id, modelId);
		await refresh();
		});

  const renderLocalEngine = (engine: TTSEngine) => {
    const inst = installStates[engine.id];
    const state = inst?.state ?? "not-accepted";
    const busy = busyEngine === engine.id;
    const isCurrent = config.engine === engine.id;
    const modelKey = `${engine.id}/${modelId}`;
    const selectedVoice = modelVoiceState[modelKey] ?? config.model_voice?.[modelKey] ?? engine.voice_id;
    if (engine.availability === "unavailable") {
      return (
        <div className="mt-2 space-y-2 text-[11px] text-muted-foreground">
          <p>{engine.reason}</p>
          {engine.license_text && (
            <p>
              License: {engine.license_text}
              {engine.license_url && (
                <>
                  {" "}
                  <a
                    className="underline"
                    href={engine.license_url}
                    target="_blank"
                    rel="noreferrer"
                    onClick={(e) => {
                      if (!engine.license_url || !isHTTPURL(engine.license_url)) return;
                      e.preventDefault();
                      openExternalURL(engine.license_url);
                    }}
                  >
                    view
                  </a>
                </>
              )}
            </p>
          )}
          {state === "not-accepted" && engine.license_hash && engine.license_name && (
            <div className="flex flex-wrap items-center gap-2">
              <button type="button" disabled={busy} className="rounded border border-border bg-background px-2 py-0.5 text-[10px] hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50" onClick={() => void acceptLicense(engine)}>
                {busy ? "Accepting…" : "Accept License"}
              </button>
            </div>
          )}
          {(state === "license-accepted" || state === "failed") && (
            <p className="text-[10px]">License accepted; runtime and voice artifacts are not yet available for installation.</p>
          )}
          {state === "installed" && (
            <p className="text-[10px]">Installed but unavailable.</p>
          )}
          <span>Install state: {state}</span>
          {inst?.error && <p className="text-destructive">{inst.error}</p>}
          {engineErrors[engine.id] && <p className="text-destructive">{engineErrors[engine.id]}</p>}
        </div>
      );
    }
    return (
      <div className="mt-2 space-y-2 text-[11px] text-muted-foreground">
        <p>
          Voice: <span className="font-mono">{engine.voice_id}</span> · Manifest <span className="font-mono">{engine.manifest_version}</span>
        </p>
        <p>
          License: {engine.license_text ?? engine.license_name}
          {engine.license_url && (
            <>
              {" "}
              <a className="underline" href={engine.license_url} target="_blank" rel="noreferrer">view</a>
            </>
          )}
        </p>
        {engine.voice_id && (engine.availability === "installable" || engine.availability === "ready") && (
          <div className="flex flex-wrap items-center gap-2 pt-1">
            <label className="text-[10px] text-muted-foreground">Voice for model:</label>
            <input
              type="text"
              value={modelId}
              onChange={(e) => setModelId(e.target.value)}
              placeholder="model id"
              className="rounded border border-input bg-background px-2 py-0.5 text-[10px] w-28"
            />
            <select
              value={selectedVoice}
              onChange={(e) => setModelVoiceState((previous) => ({ ...previous, [modelKey]: e.target.value }))}
              className="rounded border border-input bg-background px-2 py-0.5 text-[10px]"
            >
              {(engine.voices && engine.voices.length > 0 ? engine.voices : [engine.voice_id]).map((v) => (
                <option key={v} value={v}>{v}</option>
              ))}
            </select>
            <button
              type="button"
              disabled={busy}
              className="rounded border border-border bg-background px-2 py-0.5 text-[10px] hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
              onClick={() => void run(engine, async () => {
                if (!selectedVoice) return;
                await api.ttsModelVoice(engine.id, modelId, selectedVoice);
                await refresh();
              })}
            >
              Save
            </button>
            {config.model_voice?.[modelKey] && (
              <span className="text-[10px] text-primary">Override: {config.model_voice[modelKey]}</span>
            )}
          </div>
        )}
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

      {/* Speech summary. The MODEL rewrites a message into spoken prose (code
          described, not read aloud) before synthesis; the GATE turns that off
          to read the message verbatim. They are separate controls and separate
          writes — picking a model must not re-enable summarising the user
          turned off, and vice versa. */}
      <div className="space-y-1.5">
        <span className="block text-sm font-medium">Speech summary model</span>
        <div className="flex items-center gap-2">
          <div
            className="flex h-9 min-w-0 flex-1 items-center truncate rounded-md border border-border bg-muted px-3 text-sm"
            title={summaryModel || undefined}
          >
            {speechSummaryDisplay({ model: summaryModel })}
          </div>
          <button
            type="button"
            className="h-9 shrink-0 rounded-md border border-border bg-background px-3 text-sm hover:bg-muted"
            onClick={() => setSummaryDialogOpen(true)}
          >
            Change…
          </button>
        </div>
        <p className="text-[11px] text-muted-foreground">
          Rewrites a message into spoken prose before it is read aloud, describing code instead of
          reading it. Unset means the small model, then the main model.
        </p>
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          <input
            type="checkbox"
            aria-label="Summarise before speaking"
            checked={summaryEnabled}
            onChange={(event) => {
              setSummaryError(null);
              void setSummaryEnabled(event.target.checked).catch((err) =>
                setSummaryError(errorText(err)),
              );
            }}
          />
          Summarise before speaking
        </label>
        {summaryError && <p className="text-[11px] text-destructive">{summaryError}</p>}
        <ModelDialog
          open={summaryDialogOpen}
          onClose={() => setSummaryDialogOpen(false)}
          purpose="speechsummary"
          // The form owns the pick and hands it to the provider, which is the
          // single owner of this config; a direct api write here would leave the
          // toolbar and sidebar showing a stale model.
          onPick={(_, model) => {
            setSummaryError(null);
            void setSummaryModel(model).catch((err) => setSummaryError(errorText(err)));
          }}
          currentValues={{ speechsummary: summaryModel }}
        />
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
