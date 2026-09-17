import { apiPath, authHeaders, readSSEStream, remoteApiBase, reportAuthFailure } from "../api/client";

/**
 * eventBus — the single frontend transport for the unified server event bus.
 *
 * One long-lived `fetch()`-based SSE stream per host carries every event type
 * the server publishes (chat mirror frames, turn lifecycle, status, logs,
 * agent runs, git status, spending). The local server (`host === ""`) always
 * has a stream; each remote host with at least one open tab gets its own,
 * proxied through `/api/remote/<host>/api/events`. Consumers register
 * per-event-type handlers; handlers receive the full envelope so they can
 * route by `session_id` / `project`. Session ids are random on both sides, so
 * frames from different hosts never collide and all connections feed the same
 * subscriber path.
 *
 * `fetch` + `readSSEStream` (not `EventSource`) is used deliberately: the
 * stream must carry an `Authorization: Bearer` header for remote-mode auth,
 * and the browser's native `EventSource` cannot set custom headers.
 *
 * Reliability contract (design spec, Part 02/04):
 * - Reconnect with exponential backoff; on re-establishment every
 *   `onReconnect` handler fires so consumers can reconcile (state fetch +
 *   transcript refetch — never event replay). Back-off, liveness, and the seq
 *   watermark are per-connection, so one host's outage cannot disturb another.
 * - `seq` is a global monotonic counter per server process used for gap
 *   detection only: a gap logs a warning and fires the same reconcile
 *   handlers.
 * - The subscribed project list (declared via `setProjects`) drives the
 *   server's subscriber-aware git/spending emitters; changing it restarts
 *   each stream with the updated query, which also fires reconcile handlers.
 * - The set of remote hosts with an open tab (declared via `setHosts`) drives
 *   which per-host streams exist; adding/removing a host opens/closes only
 *   that stream.
 *
 * The bus is a singleton module-level instance. `on()`/`onReconnect()` calls
 * auto-start it on first use and it stays connected for the app lifetime
 * (one long-lived connection per host with an open tab, alongside the
 * terminal WebSocket).
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

/** Per-host connection state. One instance per stream target: the local
 *  server (`host === ""`) plus each remote host with an open tab. Reconnect
 *  back-off, liveness, seq watermark, and the in-flight abort handle are all
 *  per-connection so one host's outage/reconnect cannot disturb another. */
class HostConnection {
  readonly host: string;
  /** Identifies this connection's in-flight stream attempt. Aborting it (via
   *  closeConnection) and nulling this field is how a stale attempt's
   *  continuation (the fetch's `.then`/`.catch`, or a readSSEStream handler
   *  callback) knows to no-op instead of acting on/reconnecting a connection
   *  we deliberately tore down. */
  abortController: AbortController | null = null;
  lastSeq = 0;
  /** True once this host's connection has opened; subsequent opens are
   *  "reconnects". */
  hasOpenedOnce = false;
  reconnectDelay = RECONNECT_BASE_MS;
  reconnectTimer: number | undefined;
  livenessTimer: number | undefined;

  constructor(host: string) {
    this.host = host;
  }
}

class EventBus {
  /** The live connection per host (empty string = local). Kept in sync with
   *  `hosts` by syncConnections. */
  private readonly connections = new Map<string, HostConnection>();
  private readonly handlers = new Map<string, Set<EnvelopeHandler>>();
  private readonly reconnectHandlers = new Set<ReconnectHandler>();
  private projects: string[] = [];
  /** Remote hosts with at least one open tab. The local "" stream always runs. */
  private hosts: string[] = [];
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
   *  subscriber-aware git/spending emitters. Restarts each open stream when
   *  the set changes (the stream's query params are fixed for the life of the
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
    for (const conn of this.connections.values()) {
      if (conn.abortController) {
        // Controlled restart: the reconnect handlers reconcile, and the seq
        // watermark resets (no gap warning for the new stream).
        this.closeConnection(conn);
        this.openStream(conn);
      }
    }
  }

  /** Declare the remote hosts that currently have at least one open tab. Each
   *  gets its own `/api/remote/<host>/api/events` stream alongside the local
   *  one. Adding a host opens only its stream; removing one aborts only its
   *  stream, leaving every other connection's back-off/seq state untouched. */
  setHosts(hosts: string[]): void {
    const next = [...new Set(hosts.filter(Boolean))].sort();
    if (
      next.length === this.hosts.length &&
      next.every((h, i) => h === this.hosts[i])
    ) {
      return;
    }
    this.hosts = next;
    this.syncConnections();
  }

  /** Start the connections. No-op when already connected/connecting. */
  start(): void {
    if (this.started || typeof fetch === "undefined") return;
    this.started = true;
    window.addEventListener("online", this.onOnline);
    this.syncConnections();
    for (const conn of this.connections.values()) this.openStream(conn);
  }

  /** Tear every connection down and stop all timers (test/teardown only). Also
   *  resets every piece of state so a fresh start behaves like first boot. */
  stop(): void {
    this.started = false;
    window.removeEventListener("online", this.onOnline);
    for (const conn of this.connections.values()) this.closeConnection(conn);
    this.connections.clear();
    this.handlers.clear();
    this.reconnectHandlers.clear();
    this.projects = [];
    this.hosts = [];
  }

  /** Restart every stream to pick up a new backend origin (apiPath).
   *  Preserves subscriptions, project list, and host set; fires reconnect
   *  handlers like setProjects does. No-op when not yet started — next start()
   *  will use the new apiPath automatically. */
  restart(): void {
    if (!this.started) return;
    for (const conn of this.connections.values()) this.restartConnection(conn);
  }

  /** Create connections for `[""] ∪ hosts` and drop those whose host is gone. */
  private syncConnections(): void {
    const desired = ["", ...this.hosts];
    const wanted = new Set(desired);
    for (const [host, conn] of [...this.connections]) {
      if (!wanted.has(host)) {
        this.closeConnection(conn);
        this.connections.delete(host);
      }
    }
    for (const host of desired) {
      if (!this.connections.has(host)) {
        const conn = new HostConnection(host);
        this.connections.set(host, conn);
        if (this.started) this.openStream(conn);
      }
    }
  }

  private restartConnection(conn: HostConnection): void {
    if (!this.started) return;
    this.closeConnection(conn);
    conn.reconnectDelay = RECONNECT_BASE_MS;
    this.openStream(conn);
  }

  private openStream(conn: HostConnection): void {
    if (conn.abortController || !this.started) return;
    const controller = new AbortController();
    conn.abortController = controller;
    void this.runStream(conn, controller);
  }

  /** Runs one stream attempt end-to-end: fetch, dispatch the "open" reconcile
   *  logic on a good response, then read frames until the stream ends or
   *  fails. Whether it ends cleanly (server closed it) or fails (network
   *  error, non-2xx status), that's a lost connection and schedules a
   *  backoff reconnect — unless a deliberate closeConnection()/restart() already
   *  moved `abortController` on, in which case this attempt is stale and
   *  no-ops. */
  private async runStream(conn: HostConnection, controller: AbortController): Promise<void> {
    const params = new URLSearchParams();
    if (this.projects.length > 0) params.set("projects", this.projects.join(","));
    const url = apiPath(`${remoteApiBase(conn.host)}/api/events?${params.toString()}`);

    let lostConnection = false;
    try {
      const res = await fetch(url, { headers: authHeaders(), signal: controller.signal });
      if (conn.abortController !== controller) return; // superseded while awaiting fetch

      if (!res.ok) {
        console.error(`eventBus: stream request failed with status ${res.status}`);
        // The long-lived event stream is often the first thing to 401 after
        // a remote server restart (new random token) — report it so the app
        // can offer RemoteReconnect immediately instead of retrying forever.
        reportAuthFailure(res.status);
        lostConnection = true;
      } else {
        // The server sends every envelope as `event: envelope\ndata: <json>`.
        conn.reconnectDelay = RECONNECT_BASE_MS;
        conn.lastSeq = 0; // fresh stream — no gap warnings for the first frames
        if (conn.hasOpenedOnce) {
          this.fireReconnect("reconnect");
        }
        conn.hasOpenedOnce = true;

        await readSSEStream<BusEnvelope>(this.withLiveness(res, conn, controller), {
          envelope: (env) => {
            if (conn.abortController !== controller) return;
            if (typeof env.seq !== "number" || typeof env.event !== "string") {
              console.error("eventBus: malformed envelope", env);
              return;
            }
            this.trackSeq(conn, env.seq);
            this.handlers.get(env.event)?.forEach((h) => {
              try {
                h(env);
              } catch (err) {
                console.error(`eventBus: handler for '${env.event}' threw`, err);
              }
            });
          },
        });
        if (conn.abortController !== controller) return; // superseded while reading
        lostConnection = true; // stream ended cleanly — still a lost connection
      }
    } catch (err) {
      if (conn.abortController !== controller) return; // deliberate abort
      console.error("eventBus: stream error", err);
      lostConnection = true;
    }

    if (!lostConnection) return;
    conn.abortController = null;
    this.clearLiveness(conn);
    if (!this.started) return;
    const delay = conn.reconnectDelay;
    conn.reconnectDelay = Math.min(conn.reconnectDelay * 2, RECONNECT_MAX_MS);
    conn.reconnectTimer = window.setTimeout(() => {
      conn.reconnectTimer = undefined;
      this.openStream(conn);
    }, delay);
  }

  private closeConnection(conn: HostConnection): void {
    this.clearLiveness(conn);
    if (conn.reconnectTimer !== undefined) {
      clearTimeout(conn.reconnectTimer);
      conn.reconnectTimer = undefined;
    }
    if (conn.abortController) {
      conn.abortController.abort();
      conn.abortController = null;
    }
  }

  /** Returns a Response whose body re-arms the liveness timer on every chunk
   *  (keepalive comments included — the SSE parser discards those, so
   *  liveness is measured on raw bytes, not parsed frames). When the timer
   *  fires the attempt is torn down and a fresh one opened immediately: the
   *  connection is known dead, so backoff would only delay recovery. */
  private withLiveness(
    res: Response,
    conn: HostConnection,
    controller: AbortController,
  ): Response {
    if (!res.body) return res;
    const arm = () => {
      this.clearLiveness(conn);
      conn.livenessTimer = window.setTimeout(() => {
        conn.livenessTimer = undefined;
        if (conn.abortController !== controller) return;
        console.warn(
          `eventBus: liveness timeout — no bytes for ${LIVENESS_TIMEOUT_MS / 1000}s; reconnecting`,
        );
        this.restartConnection(conn);
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

  private clearLiveness(conn: HostConnection): void {
    if (conn.livenessTimer !== undefined) {
      clearTimeout(conn.livenessTimer);
      conn.livenessTimer = undefined;
    }
  }

  /** Record the envelope's seq; a gap (missed events) warns and triggers the
   *  same reconcile the reconnect path uses. */
  private trackSeq(conn: HostConnection, seq: number): void {
    if (conn.lastSeq > 0 && seq > conn.lastSeq + 1) {
      console.warn(
        `eventBus: seq gap ${conn.lastSeq} → ${seq} — events may have been missed; reconciling`,
      );
      this.fireReconnect("seq-gap");
    }
    conn.lastSeq = seq;
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
