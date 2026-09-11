import { FastForward, Pause, Play, Rewind, RotateCcw, Square, Volume2, X } from "lucide-react";
import { useSpeech, playbackLabel } from "./SpeechProvider";

export default function SpeechToolbar() {
  const { config, status, isSpeaking, paused, error, currentText, position, duration, speak, stop, pause, resume, skip, seek, retry, toolbarVisible, setToolbarVisible, setMode } = useSpeech();
  const activeText = currentText || status?.playback?.text;
  const canRetry = status?.engine?.availability !== "unavailable" && Boolean(error);
  if (!toolbarVisible) return null;
  return (
    <div className="fixed bottom-2 left-1/2 z-40 flex max-w-[calc(100vw-1rem)] -translate-x-1/2 items-center gap-2 rounded-lg border border-border bg-card/95 px-3 py-2 text-xs shadow-lg backdrop-blur">
      <Volume2 className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      <span className="max-w-32 truncate text-muted-foreground" title={status?.engine?.label ?? config?.engine ?? "browser-native"}>
        {status?.engine?.label ?? "Browser Native"}
      </span>
      <span className="text-muted-foreground">{isSpeaking ? "Playing" : playbackLabel(status?.playback)}</span>
      <button type="button" className="rounded px-2 py-1 text-xs hover:bg-muted border border-border" title={`Mode: ${config?.mode ?? "manual"}. Click to cycle.`} aria-label="Cycle speech mode" onClick={() => setMode ? setMode(config?.mode === "auto" ? "manual" : config?.mode === "manual" ? "at-bottom" : "auto") : undefined}>{config?.mode === "auto" ? "Auto" : config?.mode === "at-bottom" ? "At Bottom" : "Manual"}</button>
      <button type="button" className="rounded p-1 hover:bg-muted disabled:opacity-40" title="Back approximately 10 seconds" aria-label="Back approximately 10 seconds" disabled={!isSpeaking} onClick={() => skip(-10)}><Rewind className="h-4 w-4" /></button>
      {isSpeaking && !paused ? (
        <button type="button" className="rounded p-1 hover:bg-muted" title="Pause speech" aria-label="Pause speech" onClick={pause}><Pause className="h-4 w-4" /></button>
      ) : isSpeaking ? (
        <button type="button" className="rounded p-1 hover:bg-muted" title="Resume speech" aria-label="Resume speech" onClick={resume}><Play className="h-4 w-4" /></button>
      ) : null}
      <button type="button" className="rounded p-1 hover:bg-muted" title="Stop speech" aria-label="Stop speech" onClick={stop}><Square className="h-4 w-4" /></button>
      <button type="button" className="rounded p-1 hover:bg-muted disabled:opacity-40" title="Forward approximately 10 seconds" aria-label="Forward approximately 10 seconds" disabled={!isSpeaking} onClick={() => skip(10)}><FastForward className="h-4 w-4" /></button>
      <input
        className="w-28 accent-primary disabled:opacity-40"
        type="range"
        min={0}
        max={Math.max(duration, 1)}
        step={1}
        value={Math.min(position, Math.max(duration, 1))}
        aria-label="Speech timeline"
        title={duration ? `Approximate position ${position + 1} of ${duration}` : "Speech timeline unavailable"}
        disabled={!duration}
        onChange={(event) => seek(Number(event.target.value))}
      />
      {activeText && !isSpeaking && (
        <button type="button" className="rounded p-1 hover:bg-muted" title="Replay speech" aria-label="Replay speech" onClick={() => void speak(activeText)}><RotateCcw className="h-4 w-4" /></button>
      )}
      {error && <span className="max-w-64 truncate text-destructive" title={error}>{error}</span>}
      {canRetry && <button type="button" className="rounded border border-border px-2 py-1 hover:bg-muted" onClick={() => void retry()}>Retry</button>}
      <button type="button" className="rounded p-1 hover:bg-muted ml-auto" title="Hide speech toolbar" aria-label="Hide speech toolbar" onClick={() => setToolbarVisible(false)}><X className="h-4 w-4" /></button>
    </div>
  );
}
