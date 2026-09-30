import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api, type SpeechSummaryConfig } from "../../api/client";
import type { TTSConfig, TTSEngine, TTSPlayback, TTSStatus } from "../../api/types";
import { useChatSelector } from "../../stores/chatStore";
import { chunkSpeechText, sanitizeSpeechText } from "./speechUtils";
import {
  loadSpeechSpeakMode,
  loadSpeechToolbarVisible,
  saveSpeechSpeakMode,
  saveSpeechToolbarVisible,
  type SpeechSpeakMode,
} from "./speechToolbarPersistence";
import { DEFAULT_SPEECH_SUMMARY_CONFIG } from "../../lib/speechSummaryConfig";

/** Result of a speak request. Never rejects, so fire-and-forget callers
 *  (terminal selections, "Speak visible") cannot leave an unhandled promise,
 *  while the per-message Speak button reads `ok`/`error` to drive its loading
 *  and fallback states. */
export interface SpeechOutcome {
  ok: boolean;
  error?: string;
}

interface SpeechContextValue {
  engines: TTSEngine[];
  status: TTSStatus | null;
  config: TTSConfig;
  isSpeaking: boolean;
  paused: boolean;
  error: string | null;
  currentText: string;
  speak: (text: string) => Promise<SpeechOutcome>;
  /** Re-speak text that was ALREADY prepared for speech (the toolbar's Replay).
   *  Bypasses the summary model: re-summarising a summary wastes an LLM call
   *  and reads a summary of a summary. */
  replay: (text: string) => Promise<SpeechOutcome>;
  stop: () => void;
  pause: () => void;
  resume: () => void;
  skip: (seconds: number) => void;
  selectEngine: (engine: TTSConfig["engine"]) => Promise<void>;
  setMode: (mode: TTSConfig["mode"]) => Promise<void>;
  retry: () => Promise<void>;
  refresh: () => Promise<void>;
  position: number;
  duration: number;
  seek: (position: number) => void;
  toolbarVisible: boolean;
  setToolbarVisible: (visible: boolean) => void;
  toggleToolbar: () => void;
  /** Whether assistant text is shortened by the summary model before it is
   *  spoken. Server-backed: the same flag the sidebar and settings toggle. */
  summaryEnabled: boolean;
  summaryModel: string;
  setSummaryEnabled: (enabled: boolean) => Promise<SpeechSummaryConfig>;
  /** Set the speech-summary model. Kept beside setSummaryEnabled so the
   *  Settings form, the sidebar row and the model picker all mutate the ONE
   *  owner of this config instead of writing the endpoint behind its back. */
  setSummaryModel: (model: string) => Promise<SpeechSummaryConfig>;
  /** Apply a partial update and keep this provider's copy in sync. The Settings
   *  form edits both keys at once, so it needs a single-call merge rather than
   *  two sequential setter calls. */
  updateSummaryConfig: (patch: Partial<SpeechSummaryConfig>) => Promise<SpeechSummaryConfig>;
  /** SSH/WSL host this provider is bound to (undefined = local). Exposed so the
   *  Settings form reads and writes the SAME host's block the speak path uses,
   *  rather than the local one. */
  host?: string;
  /** Per-user "speak full text instead" override. Client-side so reading one
   *  message verbatim does not disable the feature everywhere. */
  speakMode: SpeechSpeakMode;
  setSpeakMode: (mode: SpeechSpeakMode) => void;
  /** Items waiting behind the one currently playing. */
  queuedCount: number;
  /** The project + chat session the CURRENT item came from, captured when it
   *  was enqueued. A queue outlives a tab switch, so this must be the labels
   *  the item was queued under, not whatever is active now. */
  nowPlaying: SpeechItemLabels | null;
  /** Abandon the current item and start the next queued one. No-op when
   *  nothing is playing (the next item then simply starts on its own). */
  next: () => void;
  /** Drop every waiting item but leave the current one playing. */
  clearQueue: () => void;
}

/** What a queued item is labelled with. `projectTitle`/`sessionTitle` are both
 *  optional so a caller with no project context (an isolated test, a terminal
 *  outside any session) still gets a valid label. */
export interface SpeechItemLabels {
  projectTitle?: string;
  sessionTitle?: string;
}

/** One waiting speak request. The summariser inputs are snapshotted at enqueue
 *  time because the queue can outlive the tab it was queued from: an item
 *  queued in session A must not be summarised against session B just because
 *  the user switched tabs while it waited. */
interface SpeechQueueItem extends SpeechItemLabels {
  id: number;
  text: string;
  /** True when the text is already prepared for speech (Replay), so the worker
   *  must skip the summary pass rather than summarise a summary. */
  prepared?: boolean;
  sessionId?: string;
  host?: string;
  summaryEnabled: boolean;
  speakMode: SpeechSpeakMode;
  /** Settles the caller's promise when this item reaches playback (ok) or is
   *  abandoned, or reports a failure before playback. Idempotent: every
   *  teardown path (Stop/Next/clear/unmount) can call it safely. */
  finish: (error?: string) => void;
}

const defaultConfig: TTSConfig = { engine: "browser-native", voice: "", mode: "manual" };
const SpeechContext = createContext<SpeechContextValue | null>(null);

function browserSpeechAvailable() {
  return typeof window !== "undefined" && "speechSynthesis" in window && "SpeechSynthesisUtterance" in window;
}

export function SpeechProvider({
  children,
  sessionId,
  host,
  projectTitle,
  sessionTitle,
}: {
  children: ReactNode;
  /** Active chat session. The summary endpoint is session-scoped so it reuses
   *  that session's agent, credentials and profile; without it the funnel has
   *  nothing to summarise against and speaks the full text. */
  sessionId?: string;
  /** SSH/WSL host of the session's project: both the summary config and the
   *  summary request must go to the host that runs the session. */
  host?: string;
  /** Human-readable project name, shown while one of its messages plays. Read
   *  from the project store by the App-level bridge, because the provider
   *  itself is rendered bare (no ProjectProvider) in its own tests. */
  projectTitle?: string;
  /** Human-readable chat session title, same bridge and same reason. */
  sessionTitle?: string;
}) {
	const chatModel = useChatSelector((state) => state.model);
  const [engines, setEngines] = useState<TTSEngine[]>([]);
  const [status, setStatus] = useState<TTSStatus | null>(null);
  const [config, setConfig] = useState<TTSConfig>(defaultConfig);
  const [isSpeaking, setIsSpeaking] = useState(false);
  const [paused, setPaused] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [currentText, setCurrentText] = useState("");
  const [position, setPosition] = useState(0);
  const [duration, setDuration] = useState(0);
  const [toolbarVisible, setToolbarVisibleState] = useState<boolean>(() => loadSpeechToolbarVisible());
  const setToolbarVisible = useCallback((visible: boolean) => {
    setToolbarVisibleState(visible);
    saveSpeechToolbarVisible(visible);
  }, []);
  const toggleToolbar = useCallback(() => {
    const next = !toolbarVisible;
    setToolbarVisibleState(next);
    saveSpeechToolbarVisible(next);
  }, [toolbarVisible]);
  const [speakMode, setSpeakModeState] = useState<SpeechSpeakMode>(() => loadSpeechSpeakMode());
  const setSpeakMode = useCallback((mode: SpeechSpeakMode) => {
    setSpeakModeState(mode);
    saveSpeechSpeakMode(mode);
  }, []);
  const [summaryConfig, setSummaryConfig] = useState(DEFAULT_SPEECH_SUMMARY_CONFIG);
  const summaryEnabled = summaryConfig.enabled;
  const summaryModel = summaryConfig.model;
  const generation = useRef(0);
  const localRequestGeneration = useRef(0);
  // Invalidates a queue drain that is still awaiting an item's summary or
  // synthesis. Bumped by stop() and by next(), so a Stop during the (up to
  // 60s) summary wait cancels the pending playback rather than letting it
  // start after the user asked for silence.
  const queueRun = useRef(0);
  // In-flight speech-summary request for the item the worker is summarising.
  // haltCurrent aborts it: without that, Stop (or Next) left the worker parked
  // on a request that can take the full summary timeout, so anything enqueued
  // in the meantime waited behind an item the user had already abandoned.
  const summaryAbortRef = useRef<AbortController | null>(null);
  const selectionRequestGeneration = useRef(0);
  const localMutationTail = useRef(Promise.resolve());
  const chunks = useRef<string[]>([]);
  const chunkIndex = useRef(0);
  const lastText = useRef("");
  // ── The speech queue ──────────────────────────────────────────────────────
  // Items are plain objects in a ref (not state) because the queue is mutated
  // from event handlers and async continuations alike; React state is only
  // touched for the two things the toolbar renders, so a burst of enqueues
  // costs one render rather than one per item.
  const queue = useRef<SpeechQueueItem[]>([]);
  const queueSeq = useRef(0);
  // The item the worker is currently on. It has already been shifted out of
  // `queue`, so Stop/Next/unmount must settle it separately from the backlog.
  const currentItemRef = useRef<SpeechQueueItem | null>(null);
  // Single-flight latch for the drain loop. Without it, an item's natural end
  // and a user pressing Next in the same tick would start two workers, and
  // both would shift items — the same audio playing twice.
  const draining = useRef(false);
  const [queuedCount, setQueuedCount] = useState(0);
  const [nowPlaying, setNowPlaying] = useState<SpeechItemLabels | null>(null);
  // Settles the in-flight playItem promise exactly once. Held in a ref because
  // the ±10s skip and the timeline seek RESTART the current item under a new
  // playback generation, and the worker's single await has to be released by
  // whichever playback finishes last, not by the one that was replaced.
  const settleCurrent = useRef<(() => void) | null>(null);
  // Local-engine playback: one <audio> element fed by a blob fetched from
  // /api/tts/audio once the server reports the rendering ready.
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const audioUrlRef = useRef<string | null>(null);
  const isLocal = config.engine !== "browser-native";

  const releaseAudio = useCallback(() => {
    const audio = audioRef.current;
    if (audio) {
      audio.pause();
      audio.onended = null;
      audio.onerror = null;
      audio.ontimeupdate = null;
      audio.onloadedmetadata = null;
      audio.removeAttribute("src");
      audioRef.current = null;
    }
    if (audioUrlRef.current) {
      URL.revokeObjectURL(audioUrlRef.current);
      audioUrlRef.current = null;
    }
  }, []);

  const refresh = useCallback(async () => {
    try {
      const [catalog, selected, current] = await Promise.all([api.getTTSEngines(), api.getTTSConfig(), api.getTTSStatus()]);
      setEngines(catalog.engines);
      setConfig(selected);
      setStatus(current);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  // Server-side playback mutations must be observed in submission order. A
  // newer speak request otherwise can reach the server before an older stop
  // request and get stopped by it. Failed mutations do not poison the queue;
  // the next operation still gets a chance to run.
  const enqueueLocalMutation = useCallback(<T,>(operation: () => Promise<T>) => {
    const next = localMutationTail.current.catch(() => undefined).then(operation);
    localMutationTail.current = next.then(() => undefined, () => undefined);
    return next;
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => () => {
    generation.current++;
    queueRun.current++;
    // Release every pending Speak button instead of leaving it disabled
    // forever on a provider that no longer exists.
    currentItemRef.current?.finish();
    currentItemRef.current = null;
    for (const item of queue.current) item.finish();
    queue.current = [];
    // Release the worker before the teardown continues: it is parked on this
    // promise, and leaving it unsettled would keep a stale `finally` alive that
    // calls setState on an unmounted provider.
    settleCurrent.current?.();
    settleCurrent.current = null;
    window.speechSynthesis?.cancel();
    releaseAudio();
    chunks.current = [];
    chunkIndex.current = 0;
    setPosition(0);
    setDuration(0);
  }, [releaseAudio]);

  /**
   * Stops the CURRENT playback without touching the queue.
   *
   * playItem() needs this rather than stop(): stop() clears the queue and bumps
   * queueRun, which would make the drain loop abandon the very item it just
   * dequeued. Everything the queue must survive — the generation bumps that
   * kill an in-flight <audio>/utterance, the server-side ttsStop, and
   * releasing the blob URL — lives here.
   */
  const haltCurrent = useCallback(() => {
    // Abandon the summary request first, so the worker's await settles at once
    // instead of blocking the next item for the rest of the model timeout.
    summaryAbortRef.current?.abort();
    summaryAbortRef.current = null;
    generation.current++;
    localRequestGeneration.current++;
    chunks.current = [];
    chunkIndex.current = 0;
    if (browserSpeechAvailable()) window.speechSynthesis.cancel();
    releaseAudio();
    if (config.engine !== "browser-native") {
      const requestGeneration = localRequestGeneration.current;
      void enqueueLocalMutation(() => api.ttsStop()).then((playback) => {
        if (requestGeneration === localRequestGeneration.current) {
          setStatus((previous) => previous ? { ...previous, playback } : previous);
        }
      }).catch(() => undefined);
    }
  }, [config.engine, enqueueLocalMutation, releaseAudio]);

  const stop = useCallback(() => {
    // Cancel whatever the queue is doing: a drain parked on a summary call, a
    // synthesised clip about to start, and the pending items themselves.
    queueRun.current++;
    currentItemRef.current?.finish();
    currentItemRef.current = null;
    for (const item of queue.current) item.finish();
    queue.current = [];
    setQueuedCount(0);
    settleCurrent.current?.();
    settleCurrent.current = null;
    haltCurrent();
    setNowPlaying(null);
    setIsSpeaking(false);
    setPaused(false);
  }, [haltCurrent]);

  const beginBrowserPlayback = useCallback((parts: string[], startAt: number) => {
    const currentGeneration = ++generation.current;
    window.speechSynthesis.cancel();
    chunks.current = parts;
    chunkIndex.current = Math.max(0, Math.min(startAt, parts.length));
    setDuration(parts.length);
    setPosition(chunkIndex.current);
    const speakNext = () => {
      if (currentGeneration !== generation.current || chunkIndex.current >= chunks.current.length) {
        if (currentGeneration === generation.current) {
          setPosition(chunks.current.length);
          setIsSpeaking(false);
          // Natural end of the LAST chunk: hand control back to the queue
          // drain so the next item starts. The generation guard above means a
          // chunk chain that was replaced (skip, seek, stop, next) does NOT
          // release the worker — whoever replaced it owns that.
          settleCurrent.current?.();
        }
        return;
      }
      const index = chunkIndex.current++;
      setPosition(index);
      const utterance = new SpeechSynthesisUtterance(chunks.current[index]);
      utterance.onend = speakNext;
      utterance.onerror = (event) => {
        if (currentGeneration !== generation.current || event.error === "canceled") return;
        setError(`Browser Native speech failed: ${event.error || "unknown error"}`);
        setIsSpeaking(false);
        // A failed item must not strand the queue: release the worker so the
        // drain loop moves on to the next item instead of waiting forever.
        settleCurrent.current?.();
      };
      window.speechSynthesis.speak(utterance);
    };
    setError(null);
    setPaused(false);
    setIsSpeaking(true);
    speakNext();
  }, []);

  const speakBrowser = useCallback((text: string) => {
    if (!browserSpeechAvailable()) throw new Error("Browser Native speech is unavailable in this browser");
    beginBrowserPlayback(chunkSpeechText(text, 240), 0);
  }, [beginBrowserPlayback]);

  // Load the server-side speech-summary block. Host-threaded, and re-read
  // whenever the host changes so switching from a local project to a remote one
  // cannot leave the local block gating the remote session's speech.
  useEffect(() => {
    let cancelled = false;
    void api
      .getSpeechSummaryConfig(host)
      .then((next) => {
        if (!cancelled) setSummaryConfig(next);
      })
      .catch((err) => {
        // A failed read must not disable summarising: the server default is ON,
        // so keep the optimistic default and let the next speak try anyway.
        console.warn("speech summary config load failed", err);
      });
    return () => {
      cancelled = true;
    };
  }, [host]);

  const updateSummaryConfig = useCallback(
    async (patch: Partial<SpeechSummaryConfig>): Promise<SpeechSummaryConfig> => {
      // Optimistic so a toggle/pick feels instant, then reconciled from the
      // server's merged block so a rejected write cannot leave the UI lying.
      const previous = summaryConfig;
      setSummaryConfig({ ...summaryConfig, ...patch });
      try {
        const saved = await api.setSpeechSummaryConfig(patch, host);
        setSummaryConfig(saved);
        return saved;
      } catch (err) {
        setSummaryConfig(previous);
        console.warn("speech summary config update failed", err);
        throw err;
      }
    },
    [summaryConfig, host],
  );

  const setSummaryEnabled = useCallback(
    (enabled: boolean) => updateSummaryConfig({ enabled }),
    [updateSummaryConfig],
  );

  const setSummaryModel = useCallback(
    (model: string) => updateSummaryConfig({ model }),
    [updateSummaryConfig],
  );

  /**
   * Decide what to actually read: the summarised prose, or the original text.
   *
   * The gate inputs are passed in rather than read from the closure, because
   * the queue worker calls this for an item that was enqueued possibly minutes
   * ago: it must summarise with the session, host and speak mode that were
   * current WHEN THE USER CLICKED, not whatever the UI shows now.
   *
   * Every failure path returns the ORIGINAL text rather than an empty string,
   * because a side task failing must never silence speech -- reading a code
   * block aloud is strictly better than reading nothing.
   */
  const resolveSpeechText = useCallback(
    async (
      text: string,
      gate: { summaryEnabled: boolean; speakMode: SpeechSpeakMode; sessionId?: string; host?: string },
      signal?: AbortSignal,
    ): Promise<string> => {
      if (!gate.summaryEnabled || gate.speakMode === "full" || !gate.sessionId) return text;
      try {
        const { summary } = await api.summarizeSpeech(gate.sessionId, text, gate.host, signal);
        const trimmed = typeof summary === "string" ? summary.trim() : "";
        return trimmed || text;
      } catch (err) {
        // An abort is the user pressing Stop/Next, not a failure: return empty
        // so the worker's run check discards the item instead of speaking the
        // full message the user just cancelled.
        // intentionally not logged: an aborted request is the expected outcome
        // of haltCurrent, and a console warning per cancel is noise.
        if (signal?.aborted) return "";
        // Non-fatal by design: the summary is an optimisation, not a
        // prerequisite. Logged so the reason is visible in the console rather
        // than silently reading the full message.
        console.warn("speech summary failed; speaking full text", err);
        return text;
      }
    },
    [],
  );

  /**
   * Speak ONE item to completion and resolve when it is done.
   *
   * The returned promise is the queue worker's unit of work: it settles on the
   * natural end of the audio/utterance, on a failure, or when something
   * external (Stop, Next, engine switch) tears the playback down. It never
   * rejects — a failed item must not stall the items behind it.
   */
  const playItem = useCallback(
    (text: string, onStart?: () => void, onError?: (message: string) => void): Promise<void> => {
    const normalized = sanitizeSpeechText(text);
    if (!normalized) {
      setError("Nothing to speak");
      return Promise.resolve();
    }
    setError(null);
    setCurrentText(normalized);
    lastText.current = normalized;
    // A fresh promise per item. settleCurrent is a ref so the ±10s skip and the
    // timeline seek can replace the playback underneath it (they bump
    // `generation`, so the replaced chain's own end handler is inert) while
    // still resolving THIS item's promise when the replacement finishes.
    const done = new Promise<void>((resolve) => {
      let settled = false;
      const finish = () => {
        if (settled) return;
        settled = true;
        if (settleCurrent.current === finish) settleCurrent.current = null;
        resolve();
      };
      settleCurrent.current = finish;
    });
    if (config.engine === "browser-native") {
      try {
        speakBrowser(normalized);
        // Playback has started: the caller can leave its loading state now
        // rather than waiting for the whole read to finish.
        onStart?.();
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        setError(message);
        setIsSpeaking(false);
        setPaused(false);
        onError?.(message);
        settleCurrent.current?.();
      }
      return done;
    }
    haltCurrent();
    const requestGeneration = ++localRequestGeneration.current;
    const current = () => requestGeneration === localRequestGeneration.current;
    void (async () => {
      try {
        let playback = await enqueueLocalMutation(() => api.ttsSpeak(normalized, chatModel ?? undefined));
        if (!current()) return;
        setStatus((previous) => previous ? { ...previous, playback } : previous);
        setIsSpeaking(true);
        setPaused(false);
        setPosition(0);
        setDuration(0);
        // Synthesis runs server-side; poll until this generation is ready.
        while (current() && playback.status === "synthesizing") {
          await new Promise((resolve) => setTimeout(resolve, 400));
          if (!current()) return;
          const next = await api.getTTSStatus();
          if (next.playback.generation !== playback.generation) {
            throw new Error("speech request was replaced");
          }
          playback = next.playback;
          setStatus(next);
        }
        if (!current()) return;
        if (playback.status !== "ready" || !playback.audio_id) {
          throw new Error(playback.error || `speech synthesis ${playback.status}`);
        }
        const blob = await api.ttsAudioBlob(playback.audio_id);
        if (!current()) return;
        releaseAudio();
        const url = URL.createObjectURL(blob);
        audioUrlRef.current = url;
        const audio = new Audio(url);
        audioRef.current = audio;
        audio.onloadedmetadata = () => { if (current()) setDuration(Math.round(audio.duration)); };
        audio.ontimeupdate = () => { if (current()) setPosition(Math.round(audio.currentTime)); };
        // audio.play() resolves when playback STARTS, not when it ends, so the
        // queue advances from the ended/error events rather than from here.
        audio.onended = () => {
          if (current()) {
            setPaused(false);
            setPosition(Math.round(audio.duration));
          }
          settleCurrent.current?.();
        };
        audio.onerror = () => {
          if (current()) {
            setError("Audio playback failed");
            setIsSpeaking(false);
          }
          settleCurrent.current?.();
        };
        await audio.play();
        if (current()) onStart?.();
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        if (current()) {
          // Deliberately do not switch to Browser Native on local-engine failure.
          setError(message);
          setIsSpeaking(false);
          setPaused(false);
        }
        onError?.(message);
        // Either the error is ours to report, or the item was superseded. Both
        // ways the item is over, so the queue must move on.
        settleCurrent.current?.();
      }
    })();
    return done;
  }, [config.engine, enqueueLocalMutation, haltCurrent, releaseAudio, speakBrowser]);

  /**
   * The queue worker: play waiting items one at a time until the queue empties.
   *
   * Single-flight via the `draining` latch. Enqueueing while a drain is already
   * running is therefore safe and needs no restart: the loop re-reads the queue
   * on every iteration, so an item pushed mid-item is picked up on the next
   * pass. That also means a caller must not expect drain() to start anything
   * while one is in flight — the running loop owns the work from there.
   *
   * `queueRun` is re-read PER ITERATION, not captured once. A single captured
   * value would make next() (which bumps it) strand every remaining item: the
   * loop would see its own snapshot as stale and exit, while the latch it holds
   * on its way out blocks the restart next() just asked for. Read per item, a
   * bump simply means "this item is abandoned" and the loop carries on.
   */
  const drain = useCallback(async () => {
    if (draining.current) return;
    draining.current = true;
    try {
      while (queue.current.length > 0) {
        const run = queueRun.current;
        const item = queue.current.shift()!;
        currentItemRef.current = item;
        setQueuedCount(queue.current.length);
        // Labels come from the ITEM, not from the current props: a queue
        // outlives a tab switch, and the whole point of the label is to say
        // which project/session this particular audio belongs to.
        setNowPlaying({ projectTitle: item.projectTitle, sessionTitle: item.sessionTitle });
        try {
          // Summarise HERE, not at enqueue time. Resolving on arrival is what
          // keeps a five-item burst from firing five concurrent 60s model calls
          // that would all land out of order.
          // One controller per item: haltCurrent aborts whichever is current.
          const summaryAbort = new AbortController();
          summaryAbortRef.current = summaryAbort;
          let spoken: string;
          try {
            spoken = item.prepared
              ? item.text
              : await resolveSpeechText(item.text, item, summaryAbort.signal);
          } finally {
            if (summaryAbortRef.current === summaryAbort) summaryAbortRef.current = null;
          }
          // Stop (or Next) landed while this item was summarising: abandon it
          // rather than starting audio the user already cancelled. `continue`
          // rather than `return`, because Next is precisely the case where the
          // remaining items must still play.
          if (run !== queueRun.current) continue;
          if (!spoken) continue;
          await playItem(
            spoken,
            () => item.finish(),
            (message) => item.finish(message),
          );
        } finally {
          if (currentItemRef.current === item) currentItemRef.current = null;
          // Settles the caller's promise even when the item was abandoned
          // before playback; a no-op once onStart/onError already fired.
          item.finish();
        }
      }
    } finally {
      draining.current = false;
      setQueuedCount(queue.current.length);
      setNowPlaying(null);
      setIsSpeaking(false);
      // The queue can be refilled in the same tick the loop gives up (a click
      // landing between the last item finishing and the latch clearing), and
      // that enqueue's drain() call was swallowed by the latch. Re-check here
      // so that item cannot sit unspoken with nothing left to pick it up.
      if (queue.current.length > 0) void drain();
    }
    // The dependency list deliberately OMITS projectTitle/sessionTitle. This
    // loop must read them off each item, never off the props: the memoised
    // closure here can be arbitrarily older than the item being played (it is
    // only rebuilt when the engine or the playback helpers change), so a live
    // read here would label a message from project B with project A's name.
  }, [playItem, resolveSpeechText]);

  /**
   * Append one item and make sure a worker is running. This is the single
   * funnel every speak path goes through: the per-message speak button, the
   * toolbar replay, and at-bottom auto-speak. Summarising lives in the worker,
   * not in the callers, so a new caller cannot accidentally read a code block
   * aloud.
   *
   * `prepared` marks text that is already speech-ready (Replay), which skips
   * the summary pass instead of summarising a summary.
   */
  const enqueue = useCallback(
    (text: string, prepared?: boolean): Promise<SpeechOutcome> => {
      let settle!: (outcome: SpeechOutcome) => void;
      const result = new Promise<SpeechOutcome>((resolve) => {
        settle = resolve;
      });
      // Idempotent so every teardown path can settle the caller safely.
      let finished = false;
      const finish = (error?: string) => {
        if (finished) return;
        finished = true;
        settle(error ? { ok: false, error } : { ok: true });
      };
      queue.current.push({
        id: ++queueSeq.current,
        text,
        prepared,
        // Snapshot the summary gate now: an item queued in session A must be
        // summarised against session A even if the user has since switched.
        sessionId,
        host,
        summaryEnabled,
        speakMode,
        projectTitle,
        sessionTitle,
        finish,
      });
      setQueuedCount(queue.current.length);
      void drain();
      return result;
    },
    [drain, host, projectTitle, sessionId, sessionTitle, speakMode, summaryEnabled],
  );

  const speak = useCallback((text: string) => enqueue(text), [enqueue]);

  // Replay the already-prepared text verbatim, WITHOUT another summary pass.
  // The toolbar's Replay hands back `currentText`, which is the summarised
  // prose (or the full text when that mode is on); summarising it again would
  // cost an extra LLM call and read a summary of a summary.
  const replay = useCallback((text: string) => enqueue(text, true), [enqueue]);

  const clearQueue = useCallback(() => {
    for (const item of queue.current) item.finish();
    queue.current = [];
    setQueuedCount(0);
  }, []);

  // Abandon the CURRENT item and let the worker pick up the next one. The item
  // was already dequeued when it started, so dropping it here is what "next"
  // means; queueRun stops the in-flight item from resurrecting itself if it was
  // still summarising.
  //
  // No explicit drain() call: the worker is parked on playItem's promise, and
  // settling it is what makes the loop advance to the next item. Calling
  // drain() here would be swallowed by the single-flight latch anyway, since
  // the loop is still running — a call that looks like it starts the work but
  // never could. The empty-queue early return is what keeps "next" from
  // restarting the item that is already playing.
  const next = useCallback(() => {
    if (queue.current.length === 0) return;
    queueRun.current++;
    currentItemRef.current?.finish();
    settleCurrent.current?.();
    settleCurrent.current = null;
    haltCurrent();
    setIsSpeaking(false);
    setPaused(false);
  }, [haltCurrent]);


  useEffect(() => {
    const onRequest = (event: Event) => {
      const detail = (event as CustomEvent<SpeechRequestDetail>).detail;
      if (!detail?.text || detail.handled) return;
      // Claim the request synchronously: requestSpeech uses this to tell "no
      // provider mounted" from "accepted", and a second mounted provider must
      // not speak the same request twice.
      detail.handled = true;
      void speak(detail.text).then((outcome) => detail.onOutcome?.(outcome));
    };
    window.addEventListener("ocode:speak", onRequest as EventListener);
    return () => window.removeEventListener("ocode:speak", onRequest as EventListener);
  }, [speak]);

  useEffect(() => {
    const onAssistantComplete = (event: Event) => {
      const detail = (event as CustomEvent<{ text?: string; atBottom?: boolean }>).detail;
      if (config.mode === "at-bottom" && detail?.atBottom && detail.text) void speak(detail.text);
    };
    window.addEventListener("ocode:assistant-complete", onAssistantComplete as EventListener);
    return () => window.removeEventListener("ocode:assistant-complete", onAssistantComplete as EventListener);
  }, [config.mode, speak]);

  const selectEngine = useCallback(async (engine: TTSConfig["engine"]) => {
    const requestGeneration = ++selectionRequestGeneration.current;
    stop();
    localRequestGeneration.current++;
    try {
      const next = { ...config, engine };
      const nextStatus = await enqueueLocalMutation(() => api.setTTSConfig(next));
      if (requestGeneration !== selectionRequestGeneration.current) return;
      setConfig(next);
      setStatus(nextStatus);
      setError(nextStatus.error ?? null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [config, enqueueLocalMutation, stop]);

  const setMode = useCallback(async (mode: TTSConfig["mode"]) => {
    const requestGeneration = ++selectionRequestGeneration.current;
    try {
      const next = { ...config, mode };
      const nextStatus = await enqueueLocalMutation(() => api.setTTSConfig(next));
      if (requestGeneration !== selectionRequestGeneration.current) return;
      setConfig(next);
      setStatus(nextStatus);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [config, enqueueLocalMutation]);

  const skip = useCallback((seconds: number) => {
    const audio = audioRef.current;
    if (isLocal) {
      if (audio && isSpeaking) audio.currentTime = Math.max(0, Math.min(audio.duration || 0, audio.currentTime + seconds));
      return;
    }
    if (!isSpeaking || !lastText.current) return;
    const parts = chunkSpeechText(lastText.current, 240);
    const delta = Math.max(1, Math.round(Math.abs(seconds) / 10)) * (seconds < 0 ? -1 : 1);
    beginBrowserPlayback(parts, Math.max(0, Math.min(parts.length, chunkIndex.current + delta)));
  }, [beginBrowserPlayback, isLocal, isSpeaking]);

  const seek = useCallback((nextPosition: number) => {
    const audio = audioRef.current;
    if (isLocal) {
      if (audio) audio.currentTime = Math.max(0, Math.min(audio.duration || 0, nextPosition));
      return;
    }
    if (!lastText.current) return;
    const parts = chunkSpeechText(lastText.current, 240);
    const target = Math.max(0, Math.min(parts.length, Math.round(nextPosition)));
    beginBrowserPlayback(parts, target);
  }, [beginBrowserPlayback, isLocal]);

  const retry = useCallback(async () => {
    setError(null);
    if (config.engine === "browser-native" && lastText.current) {
      // Back through the queue rather than speaking around it: a bare
      // speakBrowser here would play on top of whatever the worker is parked on.
      void enqueue(lastText.current, true);
      return;
    }
    try {
      const nextStatus = await enqueueLocalMutation(() => api.setTTSConfig(config));
      setStatus(nextStatus);
      setError(nextStatus.error ?? null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [config, enqueue, enqueueLocalMutation]);

  const value = useMemo<SpeechContextValue>(() => ({
    engines, status, config, isSpeaking, paused, error, currentText, speak, replay, stop, host,
    pause: () => {
      if (isLocal) { audioRef.current?.pause(); setPaused(true); return; }
      if (browserSpeechAvailable()) { window.speechSynthesis.pause(); setPaused(true); }
    },
    resume: () => {
      if (isLocal) { void audioRef.current?.play(); setPaused(false); return; }
      if (browserSpeechAvailable()) { window.speechSynthesis.resume(); setPaused(false); }
    },
    skip, position, duration, seek,
    selectEngine, setMode, retry, refresh,
    toolbarVisible, setToolbarVisible, toggleToolbar,
    summaryEnabled, summaryModel, setSummaryEnabled, setSummaryModel, updateSummaryConfig, speakMode, setSpeakMode,
    queuedCount, nowPlaying, next, clearQueue,
  }), [config, currentText, duration, engines, error, isLocal, isSpeaking, paused, position, refresh, retry, seek, selectEngine, setMode, skip, speak, replay, status, stop, toolbarVisible, setToolbarVisible, toggleToolbar,
    summaryEnabled, summaryModel, setSummaryEnabled, setSummaryModel, updateSummaryConfig, speakMode, setSpeakMode, host,
    queuedCount, nowPlaying, next, clearQueue]);

  return <SpeechContext.Provider value={value}>{children}</SpeechContext.Provider>;
}

export function useSpeech() {
  const context = useContext(SpeechContext);
  if (!context) throw new Error("useSpeech must be used within SpeechProvider");
  return context;
}

/**
 * Like useSpeech, but returns null instead of throwing when there is no
 * provider. Components that persist speech-summary settings (CoworkSidebar,
 * ModelDialog) are also rendered without a provider in isolated tests and
 * unusual trees; they route the write through the provider — the single owner
 * of `summaryConfig` — when it exists, and fall back to the direct endpoint
 * otherwise. This keeps the provider's copy in sync so a toggle in one screen
 * takes effect in the speak path without a reload.
 */
export function useSpeechOptional() {
  return useContext(SpeechContext);
}

export function playbackLabel(playback: TTSPlayback | undefined) {
  // "ready" is the server's rendered-audio state; once the client finished
  // playing it the toolbar is idle again.
  if (!playback || playback.status === "stopped" || playback.status === "ready") return "Idle";
  if (playback.status === "synthesizing") return "Synthesizing";
  return playback.status === "playing" ? "Playing" : playback.status;
}

/** Payload for the `ocode:speak` window event that decouples requestSpeech
 *  from the React provider tree (AssistantText is rendered provider-free in
 *  isolated tests). */
export interface SpeechRequestDetail {
  text: string;
  /** Set synchronously by the provider that takes the request. */
  handled?: boolean;
  /** Called once with the request's outcome. */
  onOutcome?: (outcome: SpeechOutcome) => void;
}

/**
 * Ask the mounted SpeechProvider to speak `text`.
 *
 * Resolves with the request's outcome rather than rejecting: fire-and-forget
 * callers (terminal selections, "Speak visible") must not produce unhandled
 * rejections, while the per-message Speak button reads `ok`/`error` to drive
 * its loading and fallback UI. The outcome arrives when playback STARTS (or
 * the request is abandoned/fails), so a failure before audio leaves the
 * button usable.
 */
export function requestSpeech(text: string): Promise<SpeechOutcome> {
  return new Promise<SpeechOutcome>((resolve) => {
    const detail: SpeechRequestDetail = { text, onOutcome: resolve };
    // dispatchEvent is synchronous, so `handled` is final once it returns.
    window.dispatchEvent(new CustomEvent<SpeechRequestDetail>("ocode:speak", { detail }));
    if (!detail.handled) resolve({ ok: false, error: "Speech is unavailable" });
  });
}
