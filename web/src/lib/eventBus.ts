import { apiPath, authHeaders, readSSEStream, reportAuthFailure } from "../api/client";

/**
 * eventBus — the single frontend transport for the unified server event bus.
 *
 * A single long-lived `fetch()`-based SSE stream on `GET /api/events` carries
 * every event type the server publishes (chat mirror frames, turn lifecycle,
 * status, logs, agent runs, git status, spending). Consumers register
 * per-event-type handlers; handlers receive the full envelope so they can
 * route by `session_id` / `project`.
 *
 * `fetch` + `readSSEStream` (not `EventSource`) is used deliberately: the
 * stream must carry an `Authorization: Bearer` header for remote-mode auth,
 * and the browser's native `EventSource` cannot set custom headers.
 *
 * Reliability contract (design spec, Part 02/04):
 * - Reconnect with exponential backoff; on re-establishment every
 *   `onReconnect` handler fires so consumers can reconcile (state fetch +
 *   transcript refetch — never event replay).
 * - `seq` is a global monotonic counter per server process used for gap
 *   detection only: a gap logs a warning and fires the same reconcile
 *   handlers.
 * - The subscribed project list (declared via `setProjects`) drives the
 *   server's subscriber-aware git/spending emitters; changing it restarts
 *   the stream with the updated query, which also fires reconcile handlers.
 *
 * The bus is a singleton module-level instance. `on()`/`onReconnect()` calls
 * auto-start it on first use and it stays connected for the app lifetime
 * (exactly one long-lived connection, per the design's 2-connection budget
 * alongside the terminal WebSocket).
 */

export interface BusEnvelope<T = unknown> {
  event: string;
  project?: string;
  session_id?: string;
  seq: number;
  data: T;
}

type EnvelopeHandler = (env: BusEnvelope) => void;
type ReconnectHandler = () => void;

export const RECONNECT_BASE_MS = 1_000;
export const RECONNECT_MAX_MS = 30_000;
/** The server writes a `: ping` comment every 20s on an idle stream
 *  (handler_events.go sseKeepaliveInterval). A body silent for longer than
 *  two missed pings is a dead connection the browser will never report —
 *  WKWebView suspending the fetch body, sleep/wake, an interface change —
 *  so the bus tears it down and reconnects itself. */
export const LIVENESS_TIMEOUT_MS = 45_000;

class EventBus {
  /** Identifies the in-flight stream attempt. Aborting it (via closeStream)
   *  and nulling this field is how a stale attempt's continuation (the
   *  fetch's `.then`/`.catch`, or a readSSEStream handler callback) knows to
   *  no-op instead of acting on/reconnecting a connection we deliberately
   *  tore down. */
  private abortController: AbortController | null = null;
  private readonly handlers = new Map<string, Set<EnvelopeHandler>>();
  private readonly reconnectHandlers = new Set<ReconnectHandler>();
  private projects: string[] = [];
  private lastSeq = 0;
  /** True once any connection has opened; subsequent opens are "reconnects". */
  private hasOpenedOnce = false;
  private reconnectDelay = RECONNECT_BASE_MS;
  private reconnectTimer: number | undefined;
  private livenessTimer: number | undefined;
  private started = false;
  private readonly onOnline = () => {
    // Interface change / network back: a stream opened on the old route
    // may be silently dead; don't wait out the liveness window.
    this.restart();
  };

  /** Subscribe to one event type. Returns an unsubscribe function. Auto-starts
   *  the connection on first subscriber. */
  on(event: string, handler: EnvelopeHandler): () => void {
    let set = this.handlers.get(event);
    if (!set) {
      set = new Set();
      this.handlers.set(event, set);
    }
    set.add(handler);
    this.start();
    return () => this.off(event, handler);
  }

  off(event: string, handler: EnvelopeHandler): void {
    this.handlers.get(event)?.delete(handler);
  }

  /** Subscribe to the reconcile signal: fired after the stream re-establishes
   *  (reconnect) and on seq-gap detection. Consumers reconcile open sessions
   *  here — recovery is state fetch + transcript refetch, never replay. */
  onReconnect(handler: ReconnectHandler): () => void {
    this.reconnectHandlers.add(handler);
    this.start();
    return () => {
      this.reconnectHandlers.delete(handler);
    };
  }

  offReconnect(handler: ReconnectHandler): void {
    this.reconnectHandlers.delete(handler);
  }

  /** Declare the project roots this client is viewing. Drives the server's
   *  subscriber-aware git/spending emitters. Restarts the stream when the set
   *  changes (the stream's query params are fixed for the life of the
   *  request). */
  setProjects(projects: string[]): void {
    const next = [...new Set(projects.filter(Boolean))].sort();
    if (
      next.length === this.projects.length &&
      next.every((p, i) => p === this.projects[i])
    ) {
      return;
    }
    this.projects = next;
    if (this.abortController) {
      // Controlled restart: the reconnect handlers reconcile, and the seq
      // watermark resets (no gap warning for the new stream).
      this.closeStream();
      this.openStream();
    }
  }

  /** Start the connection. No-op when already connected/connecting. */
  start(): void {
    if (this.started || typeof fetch === "undefined") return;
    this.started = true;
    window.addEventListener("online", this.onOnline);
    this.openStream();
  }

  /** Tear the connection down and stop all timers (test/teardown only). Also
   *  resets every piece of state so a fresh start behaves like first boot. */
  stop(): void {
    this.started = false;
    window.removeEventListener("online", this.onOnline);
    this.closeStream();
    if (this.reconnectTimer !== undefined) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = undefined;
    }
    this.handlers.clear();
    this.reconnectHandlers.clear();
    this.projects = [];
    this.lastSeq = 0;
    this.hasOpenedOnce = false;
    this.reconnectDelay = RECONNECT_BASE_MS;
  }

  /** Restart the SSE stream to pick up a new backend origin (apiPath).
   *  Preserves subscriptions and project list; fires reconnect handlers
   *  like setProjects does. No-op when not yet started — next start()
   *  will use the new apiPath automatically. */
  restart(): void {
    if (!this.started) return;
    this.closeStream();
    if (this.reconnectTimer !== undefined) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = undefined;
    }
    this.reconnectDelay = RECONNECT_BASE_MS;
    this.openStream();
  }

  private openStream(): void {
    if (this.abortController || !this.started) return;
    const controller = new AbortController();
    this.abortController = controller;
    void this.runStream(controller);
  }

  /** Runs one stream attempt end-to-end: fetch, dispatch the "open" reconcile
   *  logic on a good response, then read frames until the stream ends or
   *  fails. Whether it ends cleanly (server closed it) or fails (network
   *  error, non-2xx status), that's a lost connection and schedules a
   *  backoff reconnect — unless a deliberate closeStream()/restart() already
   *  moved `abortController` on, in which case this attempt is stale and
   *  no-ops. */
  private async runStream(controller: AbortController): Promise<void> {
    const params = new URLSearchParams();
    if (this.projects.length > 0) params.set("projects", this.projects.join(","));
    const url = apiPath(`/api/events?${params.toString()}`);

    let lostConnection = false;
    try {
      const res = await fetch(url, { headers: authHeaders(), signal: controller.signal });
      if (this.abortController !== controller) return; // superseded while awaiting fetch

      if (!res.ok) {
        console.error(`eventBus: stream request failed with status ${res.status}`);
        // The long-lived event stream is often the first thing to 401 after
        // a remote server restart (new random token) — report it so the app
        // can offer RemoteReconnect immediately instead of retrying forever.
        reportAuthFailure(res.status);
        lostConnection = true;
      } else {
        // The server sends every envelope as `event: envelope\ndata: <json>`.
        this.reconnectDelay = RECONNECT_BASE_MS;
        this.lastSeq = 0; // fresh stream — no gap warnings for the first frames
        if (this.hasOpenedOnce) {
          this.fireReconnect("reconnect");
        }
        this.hasOpenedOnce = true;

        await readSSEStream<BusEnvelope>(this.withLiveness(res, controller), {
          envelope: (env) => {
            if (this.abortController !== controller) return;
            if (typeof env.seq !== "number" || typeof env.event !== "string") {
              console.error("eventBus: malformed envelope", env);
              return;
            }
            this.trackSeq(env.seq);
            this.handlers.get(env.event)?.forEach((h) => {
              try {
                h(env);
              } catch (err) {
                console.error(`eventBus: handler for '${env.event}' threw`, err);
              }
            });
          },
        });
        if (this.abortController !== controller) return; // superseded while reading
        lostConnection = true; // stream ended cleanly — still a lost connection
      }
    } catch (err) {
      if (this.abortController !== controller) return; // deliberate abort
      console.error("eventBus: stream error", err);
      lostConnection = true;
    }

    if (!lostConnection) return;
    this.abortController = null;
    if (!this.started) return;
    const delay = this.reconnectDelay;
    this.reconnectDelay = Math.min(this.reconnectDelay * 2, RECONNECT_MAX_MS);
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = undefined;
      this.openStream();
    }, delay);
  }

  private closeStream(): void {
    this.clearLiveness();
    if (this.abortController) {
      this.abortController.abort();
      this.abortController = null;
    }
  }

  /** Returns a Response whose body re-arms the liveness timer on every chunk
   *  (keepalive comments included — the SSE parser discards those, so
   *  liveness is measured on raw bytes, not parsed frames). When the timer
   *  fires the attempt is torn down and a fresh one opened immediately: the
   *  connection is known dead, so backoff would only delay recovery. */
  private withLiveness(res: Response, controller: AbortController): Response {
    if (!res.body) return res;
    const arm = () => {
      this.clearLiveness();
      this.livenessTimer = window.setTimeout(() => {
        this.livenessTimer = undefined;
        if (this.abortController !== controller) return;
        console.warn(
          `eventBus: liveness timeout — no bytes for ${LIVENESS_TIMEOUT_MS / 1000}s; reconnecting`,
        );
        this.restart();
      }, LIVENESS_TIMEOUT_MS);
    };
    arm();
    const body = res.body.pipeThrough(
      new TransformStream<Uint8Array, Uint8Array>({
        transform: (chunk, ctrl) => {
          arm();
          ctrl.enqueue(chunk);
        },
      }),
    );
    return new Response(body, { status: res.status, headers: res.headers });
  }

  private clearLiveness(): void {
    if (this.livenessTimer !== undefined) {
      clearTimeout(this.livenessTimer);
      this.livenessTimer = undefined;
    }
  }

  /** Record the envelope's seq; a gap (missed events) warns and triggers the
   *  same reconcile the reconnect path uses. */
  private trackSeq(seq: number): void {
    if (this.lastSeq > 0 && seq > this.lastSeq + 1) {
      console.warn(
        `eventBus: seq gap ${this.lastSeq} → ${seq} — events may have been missed; reconciling`,
      );
      this.fireReconnect("seq-gap");
    }
    this.lastSeq = seq;
  }

  private fireReconnect(reason: "reconnect" | "seq-gap"): void {
    this.reconnectHandlers.forEach((h) => {
      try {
        h();
      } catch (err) {
        console.error(`eventBus: reconnect handler (${reason}) threw`, err);
      }
    });
  }
}

export const eventBus = new EventBus();
