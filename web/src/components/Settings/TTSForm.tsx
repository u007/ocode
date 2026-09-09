import { useSpeech } from "../Speech/SpeechProvider";

export default function TTSForm() {
  const { engines, config, status, error, selectEngine, setMode, retry } = useSpeech();
  const canRetry = status?.engine.availability !== "unavailable" && Boolean(status?.error || error);
  return (
    <section className="space-y-4 rounded-lg border border-border p-4">
      <div>
        <h2 className="text-sm font-semibold">Speech playback</h2>
        <p className="mt-1 text-xs text-muted-foreground">
          Browser Native is the default and stays in this browser tab. Local engines are shown only when a verified runtime and model manifest is available.
        </p>
      </div>
      <label className="block space-y-1 text-sm">
        <span className="font-medium">Voice engine</span>
        <select
          className="w-full rounded-md border border-input bg-background px-3 py-2"
          value={config.engine}
          onChange={(event) => void selectEngine(event.target.value as typeof config.engine)}
        >
          {engines.map((engine) => (
            <option key={engine.id} value={engine.id}>
              {engine.label}{engine.availability === "unavailable" ? " — unavailable" : ""}
            </option>
          ))}
        </select>
      </label>
      <div className="space-y-2 text-xs">
        {engines.filter((engine) => engine.availability === "unavailable").map((engine) => (
          <div key={engine.id} className="rounded-md border border-border p-2 text-muted-foreground">
            <span className="font-medium text-foreground">{engine.label}:</span> {engine.reason}
          </div>
        ))}
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
