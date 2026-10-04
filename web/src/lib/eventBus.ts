import { apiPath, authHeaders, readSSEStream, reportAuthFailure } from "../api/client";
import { onWake } from "./wakeSignal";

/**
 * eventBus — the single frontend transport for the unified server event bus.
 *
 * ONE long-lived `fetch()`-based SSE stream carries every event type the
 * server publishes (chat mirror frames, turn lifecycle, status, logs, agent
 * runs, git status, spending) — for the local server AND for each remote host
 * with at least one open tab. Remote hosts are named in `?hosts=`; the local
 * server subscribes to each host's own stream and relays the frames down this
 * one, tagged with `host`. It is one stream on purpose: a browser on a
 * plain-HTTP origin (share URL, `ocode serve`) gets six connections per
 * origin, and a stream per host pinned one each
 * (docs/gotchas/share-url-http1-connection-cap.md). Consumers register
 * per-event-type handlers; handlers receive the full envelope so they can
 * route by `session_id` / `project`. Session ids are random on both sides, so
 * frames from different hosts never collide and all feed the same subscriber
 * path.
 *
 * `fetch` + `readSSEStream` (not `EventSource`) is used deliberately: the
 * stream must carry an `Authorization: Bearer` header for remote-mode auth,
 * and the browser's native `EventSource` cannot set custom headers.
 *
 * Reliability contract (design spec, Part 02/04):
 * - Reconnect with exponential backoff; on re-establishment every
 *   `onReconnect` handler fires so consumers can reconcile (state fetch +
 *   transcript refetch — never event replay). A remote host's upstream is
 *   reconnected by the server, which announces each (re)open with a
 *   `host_stream` envelope: the second one for a host fires the same
 *   reconcile handlers.
 * - `seq` is a monotonic counter per server process used for gap detection
 *   only, so the watermark is kept per origin (local, and each remote host):
 *   a gap logs a warning and fires the same reconcile handlers.
 * - The subscribed project list (declared via `setProjects`) drives the
 *   server's subscriber-aware git/spending emitters; changing it restarts
 *   the stream with the updated query, which also fires reconcile handlers.
 * - The set of remote hosts with an open tab (declared via `setHosts`) drives
 *   which hosts the server relays; changing it restarts the stream the same
 *   way.
 *
 * The bus is a singleton module-level instance. `on()`/`onReconnect()` calls
 * auto-start it on first use and it stays connected for the app lifetime
 * (one long-lived connection, alongside the terminal WebSocket).
 */

export interface BusEnvelope<T = unknown> {
  event: string;
  project?: string;
  session_id?: string;
  seq: number;
  data: T;
  /** The host this frame came from: "" for the local server, the remote
   *  host string otherwise. Set by the local server's relay on remote frames
   *  (the remote itself has no idea it is being relayed) and defaulted to ""
   *  here for local ones. Consumers that
   *  aggregate across hosts (e.g. the Pulse dashboard, whose list is built from
   *  the LOCAL server alone) need it to tell "unknown session" apart from
   *  "a session this client has no way of listing". */
  host?: string;
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

/** The control envelope the server's relay emits each time a remote host's
 *  upstream stream opens (handler_events_remote.go hostStreamEvent). */
const HOST_STREAM_EVENT = "host_stream";

/** State of the one stream: reconnect back-off, liveness, seq watermarks, and
 *  the in-flight abort handle. */
class StreamConnection {
  /** Identifies this connection's in-flight stream attempt. Aborting it (via
   *  closeConnection) and nulling this field is how a stale attempt's
   *  continuation (the fetch's `.then`/`.catch`, or a readSSEStream handler
   *  callback) knows to no-op instead of acting on/reconnecting a connection
   *  we deliberately tore down. */
  abortController: AbortController | null = null;
  /** Last seq seen per origin ("" = local, else the remote host) on the
   *  current stream. A remote host has an entry once its `host_stream` marker
   *  arrived; a second marker for it means its upstream re-established. */
  readonly lastSeq = new Map<string, number>();
  /** True once the stream has opened; subsequent opens are "reconnects". */
  hasOpenedOnce = false;
  reconnectDelay = RECONNECT_BASE_MS;
  reconnectTimer: number | undefined;
  livenessTimer: number | undefined;
}

class EventBus {
  private conn = new StreamConnection();
  private readonly handlers = new Map<string, Set<EnvelopeHandler>>();
  private readonly reconnectHandlers = new Set<ReconnectHandler>();
  private projects: string[] = [];
  /** Remote hosts with at least one open tab. The local "" stream always runs. */
  private hosts: string[] = [];
  private started = false;

  /** Unsubscribe for the shared wake trigger (online / visibilitychange). */
  private unsubscribeWake: (() => void) | null = null;

  /** Wake signal: reconnect the stream now instead of waiting out the
   *  backoff timer. `restartConnection` clears the pending timer and resets
   *  the per-connection delay, so a connection that had backed off to 30s
   *  recovers within a tick of the network returning. */
  private readonly onWakeSignal = () => {
    if (!this.started) return;
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
   *  subscriber-aware git/spending emitters. Restarts the open stream when
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
    this.reopenForNewQuery();
  }

  /** Declare the remote hosts that currently have at least one open tab. The
   *  server relays each one's events down the single stream. Restarts the
   *  open stream when the set changes, like setProjects. */
  setHosts(hosts: string[]): void {
    const next = [...new Set(hosts.filter(Boolean))].sort();
    if (
      next.length === this.hosts.length &&
      next.every((h, i) => h === this.hosts[i])
    ) {
      return;
    }
    this.hosts = next;
    this.reopenForNewQuery();
  }

  /** Controlled restart of an open stream whose query changed: the reconnect
   *  handlers reconcile, and the seq watermarks reset (no gap warning for the
   *  new stream). A stream waiting out its back-off picks the new query up
   *  when it reopens. */
  private reopenForNewQuery(): void {
    if (!this.conn.abortController) return;
    this.closeConnection(this.conn);
    this.openStream(this.conn);
  }

  /** Start the connection. No-op when already connected/connecting. */
  start(): void {
    if (this.started || typeof fetch === "undefined") return;
    this.started = true;
    this.unsubscribeWake = onWake(this.onWakeSignal);
    this.openStream(this.conn);
  }

  /** Tear the connection down and stop all timers (test/teardown only). Also
   *  resets every piece of state so a fresh start behaves like first boot. */
  stop(): void {
    this.started = false;
    this.unsubscribeWake?.();
    this.unsubscribeWake = null;
    this.closeConnection(this.conn);
    this.conn = new StreamConnection();
    this.handlers.clear();
    this.reconnectHandlers.clear();
    this.projects = [];
    this.hosts = [];
  }

  /** Restart the stream to pick up a new backend origin (apiPath).
   *  Preserves subscriptions, project list, and host set; fires reconnect
   *  handlers like setProjects does. No-op when not yet started — next start()
   *  will use the new apiPath automatically. */
  restart(): void {
    if (!this.started) return;
    this.restartConnection(this.conn);
  }

  private restartConnection(conn: StreamConnection): void {
    if (!this.started) return;
    this.closeConnection(conn);
    conn.reconnectDelay = RECONNECT_BASE_MS;
    this.openStream(conn);
  }

  private openStream(conn: StreamConnection): void {
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
  private async runStream(conn: StreamConnection, controller: AbortController): Promise<void> {
    const params = new URLSearchParams();
    if (this.projects.length > 0) params.set("projects", this.projects.join(","));
    if (this.hosts.length > 0) params.set("hosts", this.hosts.join(","));
    const url = apiPath(`/api/events?${params.toString()}`);

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
        conn.lastSeq.clear(); // fresh stream — no gap warnings for the first frames
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
            // Local frames carry no host; relayed ones were tagged by the
            // server. Mutated rather than copied — the bus is the hot path.
            const host = typeof env.host === "string" ? env.host : "";
            env.host = host;
            if (env.event === HOST_STREAM_EVENT) {
              // The host's upstream (re)opened: fresh seq watermark, and a
              // reconcile unless this is its first open on this stream (a
              // stream reopen has already reconciled everything).
              const reopened = conn.lastSeq.has(host);
              conn.lastSeq.set(host, 0);
              if (reopened) this.fireReconnect("reconnect");
              return;
            }
            this.trackSeq(conn, host, env.seq);
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

  private closeConnection(conn: StreamConnection): void {
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
    conn: StreamConnection,
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

  private clearLiveness(conn: StreamConnection): void {
    if (conn.livenessTimer !== undefined) {
      clearTimeout(conn.livenessTimer);
      conn.livenessTimer = undefined;
    }
  }

  /** Record the envelope's seq for its origin; a gap (missed events) warns and triggers the
   *  same reconcile the reconnect path uses. */
  private trackSeq(conn: StreamConnection, host: string, seq: number): void {
    const last = conn.lastSeq.get(host);
    if (last !== undefined && last > 0 && seq > last + 1) {
      console.warn(
        `eventBus: seq gap ${last} → ${seq} — events may have been missed; reconciling`,
      );
      this.fireReconnect("seq-gap");
    }
    conn.lastSeq.set(host, seq);
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
