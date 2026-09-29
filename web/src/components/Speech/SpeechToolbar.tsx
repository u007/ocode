import { useEffect, useState } from "react";
import {
  FastForward,
  ListX,
  Pause,
  Play,
  Rewind,
  RotateCcw,
  SkipForward,
  Square,
  Volume2,
  X,
} from "lucide-react";
import { nextSpeechMode } from "./speechUtils";
import { useSpeech, playbackLabel, type SpeechItemLabels } from "./SpeechProvider";

/** "project · session", skipping whichever half is missing, or null when
 *  neither is known (an isolated render, or a terminal outside any session).
 *  Extracted so the label and its tooltip can never disagree. */
export function speechItemLabel(labels: SpeechItemLabels | null | undefined) {
  if (!labels) return null;
  const parts = [labels.projectTitle, labels.sessionTitle].filter(Boolean);
  return parts.length > 0 ? parts.join(" · ") : null;
}

export default function SpeechToolbar() {
  const { config, status, isSpeaking, paused, error, currentText, position, duration, replay, stop, pause, resume, skip, seek, retry, toolbarVisible, setToolbarVisible, setMode, speakMode, setSpeakMode, queuedCount, nowPlaying, next, clearQueue } = useSpeech();
  // Local mirror of visibility so the X button can hide it; always kept in sync
  // with context so external toggleToolbar() calls (e.g. from StatusBar) also hide it.
  const [visible, setVisible] = useState(toolbarVisible);
  useEffect(() => { setVisible(toolbarVisible); }, [toolbarVisible]);
  const activeText = currentText || status?.playback?.text;
  const canRetry = status?.engine?.availability !== "unavailable" && Boolean(error);
  // Which project/session this audio belongs to. Necessary once a queue
  // exists: playback survives a tab switch, so "what am I listening to" is no
  // longer answerable from whatever the user is looking at now.
  const playingLabel = speechItemLabel(nowPlaying);
  if (!visible) return null;
  return (
    // Mobile: full-width bottom bar that WRAPS, so nothing is clipped off the
    // right edge (the old single-line pill overflowed a 390px viewport). ≥sm
    // keeps the original centered, non-wrapping pill.
    <div className="fixed bottom-2 inset-x-2 z-40 flex flex-wrap items-center justify-center gap-1.5 rounded-lg border border-border bg-card/95 px-2 py-2 text-xs shadow-lg backdrop-blur sm:inset-x-auto sm:left-1/2 sm:max-w-[calc(100vw-1rem)] sm:-translate-x-1/2 sm:flex-nowrap sm:justify-start sm:gap-2 sm:px-3">
      <Volume2 className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      <span className="max-w-32 truncate text-muted-foreground" title={status?.engine?.label ?? config?.engine ?? "browser-native"}>
        {status?.engine?.label ?? "Browser Native"}
      </span>
      <span className="text-muted-foreground">{isSpeaking ? "Playing" : playbackLabel(status?.playback)}</span>
      {/* Which project/session the queued audio came from. Bounded on BOTH
          axes and wrapped, so a long session title cannot push the transport
          controls off a 390px viewport — the same wrapping the bar itself does. */}
      {playingLabel && (
        <span
          className="max-w-[min(60vw,28rem)] truncate rounded bg-muted px-1.5 py-0.5 text-muted-foreground"
          data-testid="speech-now-playing"
          title={`Reading from ${playingLabel}`}
        >
          {playingLabel}
        </span>
      )}
      {queuedCount > 0 && (
        <>
          <span className="text-muted-foreground" data-testid="speech-queued-count" aria-label={`${queuedCount} queued`}>
            +{queuedCount} queued
          </span>
          <button type="button" className="rounded p-1 hover:bg-muted" title="Skip to the next queued message" aria-label="Next queued message" onClick={next}><SkipForward className="h-4 w-4" /></button>
          <button type="button" className="rounded p-1 hover:bg-muted" title="Drop every queued message and keep playing this one" aria-label="Clear speech queue" onClick={clearQueue}><ListX className="h-4 w-4" /></button>
        </>
      )}
      <button type="button" className="rounded px-2 py-1 text-xs hover:bg-muted border border-border" title={`Mode: ${config?.mode ?? "manual"}. Click to cycle.`} aria-label="Cycle speech mode" onClick={() => setMode(nextSpeechMode(config?.mode))}>{config?.mode === "at-bottom" ? "At Bottom" : "Manual"}</button>
      {/* Which TEXT gets read, as distinct from WHEN playback happens (the mode
          button above). The label is the current state, so the control doubles as
          the indicator. A two-state toggle rather than a dropdown menu: the
          toolbar already uses click-to-cycle for its other option and a menu
          with two items adds a click for nothing. */}
      <button
        type="button"
        className="rounded px-2 py-1 text-xs hover:bg-muted border border-border"
        title="Read text summarised by the summary model. Click to read the full message instead."
        aria-label="Speech text mode"
        onClick={() => setSpeakMode(speakMode === "summarised" ? "full" : "summarised")}
      >
        {speakMode === "summarised" ? "Summarised" : "Full Text"}
      </button>
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
        <button type="button" className="rounded p-1 hover:bg-muted" title="Replay speech" aria-label="Replay speech" onClick={() => void replay(activeText)}><RotateCcw className="h-4 w-4" /></button>
      )}
      {error && <span className="max-w-64 truncate text-destructive" title={error}>{error}</span>}
      {canRetry && <button type="button" className="rounded border border-border px-2 py-1 hover:bg-muted" onClick={() => void retry()}>Retry</button>}
      <button type="button" className="rounded p-1 hover:bg-muted ml-auto" title="Hide speech toolbar" aria-label="Hide speech toolbar" onClick={() => { setVisible(false); setToolbarVisible(false); }}><X className="h-4 w-4" /></button>
    </div>
  );
}
