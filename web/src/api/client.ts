import type {
  ChatResponse,
  SessionInfo,
  SessionDetail,
  SessionListResponse,
  ModelInfo,
  AgentInfo,
  AgentRun,
  GitStatus,
  GitDiffFile,
  GitCommit,
  GitWorkspace,
  GitHunkRequest,
  GitStash,
  ThemeResponse,
  TUIStatus,
  LSPStatus,
  MCPStatus,
  ThemesListResponse,
  FileStatus,
  Project,
  ProjectGroup,
  ServerProjectTabs,
  BrowseResponse,
  PermissionsResponse,
  PermissionModeConfigResponse,
  MemoryStatusResponse,
  UsageSummary,
  PluginInfo,
  CommandEntry,
  SkillEntry,
  CronJob,
  CronJobsResponse,
  CronJobPatchRequest,
  CronJobWriteRequest,
  CronOutboxResponse,
  CronRunsResponse,
  CronRun,
  CronTargetsResponse,
  FileChange,
  ChangeDiff,
  SyncStatusResponse,
  SyncLoginStartResponse,
  SyncLoginPollResponse,
	PermissionDecision,
	TTSEngine,
	TTSConfig,
	TTSStatus,
	TTSPlayback,
	TTSInstallState,
	PortMapView,
	PortMapTarget,
	ContextBudgetReport,
} from "./types";

import { noteSessionRevision } from "../lib/sessionRevision";

export interface CompactConfig {
  enabled: boolean;
  summary_provider: string;
  summary_model: string;
  token_threshold: number;
  keep_recent_turns: number;
  keep_recent_tokens: number;
  min_messages: number;
  summary_timeout_seconds: number;
  summary_max_retries: number;
  max_summary_input_tokens: number;
}

export interface AutoPermissionConfig {
  enabled?: boolean;
  model?: string;
  allow_destructive?: boolean;
  prompt?: string;
  max_context_bytes?: number;
  max_context_sources?: number;
  max_context_lines_per_source?: number;
  min_confidence?: number;
  grants?: unknown[];
  /** Concern categories the user has switched OFF enforcing — see
   *  {@link RelaxableConcern}. Absent or empty means every category is
   *  enforced (the default), so the field is a negative set on purpose. */
  relaxed_concerns?: string[];
}

/** One judge concern category that can be switched off in Settings → Permissions.
 *  Served by GET /api/config/ocode/permissions-concerns so the checkbox list can
 *  never drift from the Go rubric; `note` carries the caveat for categories a
 *  deterministic Go guard already covers. */
export interface RelaxableConcern {
  key: string;
  label: string;
  note?: string;
}

export interface DiscoveryConfig {
  enabled: boolean;
  embedding_model: string;
  embedding_backend: string;
  local_model_status: string;
  local_server_url: string;
  pinned_skills: string[];
  ignore_paths: string[];
}

/** Discovery settings plus live runtime status (GET /api/sessions/:id/discovery).
 *  The config half always arrives; the runtime half is meaningful only when
 *  `live` is true (no agent reachable, or a turn is mid-flight and the unsafe
 *  read was skipped). Mirrors the TUI's /discover status. */
export interface DiscoveryStatus extends DiscoveryConfig {
  live: boolean;
  active: boolean;
  init_error?: string;
  judge?: string;
  judge_vetoed: number;
  mcp_total: number;
  skill_total: number;
  attached_skills: string[];
  attached_mcp: string[];
  attached_md: string[];
  all_skills: string[];
  all_mcp: string[];
  all_md: string[];
  md_pending: number;
}

export interface TUISettings {
  theme: string;
  mouse: boolean | null;
  scroll_speed: number;
  keybinds: Record<string, string>;
  leader_timeout: number;
  branchless: boolean;
}

export interface ImageGenConfig {
  enabled: boolean;
  provider: string;
  model: string;
  output_path?: string;
  timeout?: number;
}

// HtrStatus is the managed `htrcli serve` daemon snapshot (Settings > Browser).
export interface HtrStatus {
  enabled: boolean;
  running: boolean;
  managed: boolean;
  addr: string;
  port: number;
  socket: string;
  binary: string;
  error?: string;
}

// HtrTab is one browser tab connected to the managed HTR daemon.
export interface HtrTab {
  id: number;
  url: string;
  title: string;
  active: boolean;
  browser?: string;
}

// Base path for API calls. When the SPA is served under a tailscale --set-path
// prefix (e.g. /<sessionID>), API calls must include the prefix or the tailscale
// proxy routes them to whichever session owns the root path. The /rc command
// embeds the same prefix in the opened URL, so derive BASE from the current
// location: everything before the trailing "/session/<id>" is the prefix.
// Any other path is a mount root (the desktop share lands on /desktop/, the
// only other SPA route is "*" = home), so the whole path minus its trailing
// slash is the prefix; plain "/" yields "". web/index.html applies the same
// rule to inject <base href> before the relative asset tags are parsed, and
// the value is passed to <BrowserRouter basename=...> in main.tsx so client
// navigation stays in sync.
export const _basePath = (() => {
  const path = window.location.pathname;
  const m = path.match(/^(.*?)\/session\/[^/]+$/);
  return m ? m[1] : path.replace(/\/+$/, "");
})();
const BASE = _basePath;

// Configurable backend origin (same-origin by default). When set via
// /api/config/ocode/backend, all API/SSE calls are routed to that origin.
// Empty means same-origin (existing behavior). Allowed values are
// http://localhost[:port] or http://127.0.0.1[:port] — local dev origins
// only. Config/auth sync is a separate flow (/api/sync/*) and never uses
// this override.
let backendBase: string | null = null;

export function getApiBackendBase(): string | null {
  return backendBase;
}

export function setApiBackendBase(url: string | null): void {
  if (!url || url.trim() === "") {
    backendBase = null;
    return;
  }
  backendBase = url.trim().replace(/\/+$/, "");
}

export async function initBackendBase(): Promise<string | null> {
  try {
    // Fetch via same-origin (backendBase is still null here) to discover the
    // configured backend without creating a circular dependency on the switch.
    const headers = new Headers();
    headers.set("Content-Type", "application/json");
    for (const [k, v] of Object.entries(authHeaders())) headers.set(k, v);
    const res = await fetch(`${BASE}/api/config/ocode/backend`, { headers });
    if (!res.ok) return backendBase;
    const data = (await res.json()) as { backend_url?: string };
    if (data.backend_url) {
      setApiBackendBase(data.backend_url);
    } else {
      setApiBackendBase(null);
    }
    return backendBase;
  } catch {
    return backendBase;
  }
}

// Auth token resolution, in priority order:
//   1. URL fragment (#token=...) — set by `ocode remote --web`'s one-time
//      browser-open URL. Read once, cached to sessionStorage, then the
//      fragment is stripped via history.replaceState so it never survives
//      a copy-paste of the URL or shows up in browser history.
//   2. sessionStorage (ocode.remoteToken) — the fragment token, cached
//      across reloads within the same tab session.
//   3. A persistent "this tab is a remote session" marker with no token —
//      set alongside (1)/(2) and never cleared by reportAuthFailure's token
//      wipe, so a reload after the server invalidates the cached token still
//      resolves isRemoteSession()=true (and authToken()="", which the App
//      guard below already treats as "show RemoteReconnect") instead of
//      silently falling through to the legacy /rc path.
//   4. ?token=... query string — the existing /rc (remote control) path.
// Fragments are never sent in HTTP requests, so (1)/(2)/(3) never reach
// server or proxy logs; (4) is a weaker, pre-existing mechanism kept for /rc.
const REMOTE_TOKEN_STORAGE_KEY = "ocode.remoteToken";
const REMOTE_SESSION_MARKER_KEY = "ocode.remoteSessionMarker";

function markRemoteSession(): void {
  try {
    sessionStorage.setItem(REMOTE_SESSION_MARKER_KEY, "1");
  } catch {
    // sessionStorage unavailable — the marker only helps a reload survive a
    // cleared token, which needs sessionStorage anyway. Not fatal.
  }
}

function resolveInitialToken(): { token: string; isRemote: boolean } {
  const hashParams = new URLSearchParams(window.location.hash.replace(/^#/, ""));
  const fragmentToken = hashParams.get("token");
  if (fragmentToken) {
    try {
      sessionStorage.setItem(REMOTE_TOKEN_STORAGE_KEY, fragmentToken);
    } catch {
      // sessionStorage unavailable (privacy mode, etc.) — the token still
      // works for this page load via the returned value; it just won't
      // survive a reload. Not fatal.
    }
    markRemoteSession();
    const url = new URL(window.location.href);
    url.hash = "";
    window.history.replaceState(null, "", url.toString());
    return { token: fragmentToken, isRemote: true };
  }

  let cached: string | null = null;
  try {
    cached = sessionStorage.getItem(REMOTE_TOKEN_STORAGE_KEY);
  } catch {
    cached = null;
  }
  if (cached) {
    markRemoteSession();
    return { token: cached, isRemote: true };
  }

  let hasMarker = false;
  try {
    hasMarker = sessionStorage.getItem(REMOTE_SESSION_MARKER_KEY) === "1";
  } catch {
    hasMarker = false;
  }
  if (hasMarker) {
    return { token: "", isRemote: true };
  }

  const queryToken = new URLSearchParams(window.location.search).get("token") ?? "";
  return { token: queryToken, isRemote: false };
}

const { token: _token, isRemote: _isRemoteSession } = resolveInitialToken();

// Registered by App.tsx for a remote session only. Fired by reportAuthFailure
// so the app can render RemoteReconnect immediately on a 401, without
// waiting for the user to reload the tab.
let onAuthFailure: (() => void) | null = null;

/** Registers (or clears, with null) the handler reportAuthFailure calls. */
export function setAuthFailureHandler(fn: (() => void) | null): void {
  onAuthFailure = fn;
}

/**
 * Called with a response's HTTP status after any remote-mode API call. A 401
 * means the server likely restarted (a fresh random token) and this tab's
 * cached token is now stale/invalid — every further call would 401 forever
 * with no explanation. Clears the cached token (the persistent marker above
 * keeps isRemoteSession() true regardless) and notifies the registered
 * handler so the app can switch to RemoteReconnect right away.
 *
 * `_isRemoteSession` never changes after module load — a genuinely fresh
 * non-remote server also returning 401 (wrong password) must not trip this;
 * gating on it (not just the status code) keeps that case a plain
 * unauthorized error instead of a false "reconnect" prompt.
 *
 * Exported so eventBus's long-lived SSE stream (which doesn't go through
 * fetchJSON/fetchEmpty/authedFetch below) can report its own non-2xx
 * responses too — after a server restart, the SSE stream is often the
 * *first* thing to 401, since it's the one connection every page keeps
 * open continuously.
 */
export function reportAuthFailure(status: number): void {
  if (status !== 401 || !_isRemoteSession) return;
  try {
    sessionStorage.removeItem(REMOTE_TOKEN_STORAGE_KEY);
  } catch (err) {
    console.error("client: failed to clear stale remote token from sessionStorage", err);
  }
  onAuthFailure?.();
}

/** True when this tab's token came from a `--remote` server's URL fragment
 *  (or its sessionStorage cache) rather than the legacy /rc ?token= path.
 *  Used to pick stricter, header-only auth for endpoints that also accept
 *  query-string tokens today (see TerminalPanel's WS connection). */
export function isRemoteSession(): boolean {
  return _isRemoteSession;
}

/** Returns auth headers for fetch() calls. Exported for components that use raw
 *  fetch or EventSource (which cannot set headers). */
export function authHeaders(): Record<string, string> {
  return _token ? { Authorization: `Bearer ${_token}` } : {};
}

/** Returns the auth token string. Useful for EventSource URLs. */
export function authToken(): string {
  return _token;
}

/**
 * fetch() wrapper for API calls. Injects the auth bearer token (when the SPA
 * was opened with ?token=... or behind an authenticated server) and the SPA
 * base path (when served under a proxy prefix). Always use this instead of a
 * raw fetch() for any /api call — raw fetches hit the auth middleware with no
 * credentials, which returns 401 "unauthorized", trips the rate limiter
 * (429 "too many requests"), and makes the non-JSON error body fail .json().
 */
export async function authedFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers);
  if (!headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  for (const [k, v] of Object.entries(authHeaders())) headers.set(k, v);
  const res = await fetch(apiPath(path), { ...init, headers });
  if (!res.ok) reportAuthFailure(res.status);
  return res;
}

/** Prepends the current SPA base path to an API or SSE path.
 *  When a backend origin is configured, the absolute origin is prepended
 *  (e.g. http://localhost:4096/api/...), otherwise same-origin relative.
 *  SSE/EventSource URLs and upload URLs all flow through here. */
export function apiPath(path: string): string {
  const withBase = `${BASE}${path}`;
  if (backendBase) {
    return `${backendBase}${withBase}`;
  }
  return withBase;
}

/** Returns the URL prefix for proxying API calls to a remote host's
 *  `ocode serve --remote` instance. Empty for local (no host); otherwise
 *  `/api/remote/<encoded-host>` so the local server reverse-proxies the
 *  request to the correct remote. The host string is URI-component-encoded
 *  (e.g. `user@host` → `user%40host`, `wsl:Ubuntu` → `wsl%3AUbuntu`). */
export function remoteApiBase(host?: string): string {
  if (!host) return "";
  return `/api/remote/${encodeURIComponent(host)}`;
}

/** Returns a WebSocket URL for the given API path, respecting the configured
 *  backend origin. Handles both same-origin (uses window.location.host) and
 *  absolute backendBase (derives host/protocol from the backend URL). */
export function apiWsPath(path: string): string {
  const httpUrl = apiPath(path);
  if (httpUrl.startsWith("http://") || httpUrl.startsWith("https://")) {
    const u = new URL(httpUrl);
    u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
    return u.toString();
  }
  const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${window.location.host}${httpUrl}`;
}

/** Shared browse-URL normalization (see lib/browseURL.ts). Re-exported here so
 *  existing imports from "@/api/client" keep working; the store and the proxy
 *  path share the same function via lib/browseURL to avoid drift. */
import { normalizeBrowseURL } from "../lib/browseURL";
export { normalizeBrowseURL };

/** Per-window id shared with the ProfileSwitcher (see lib/windowId.ts). Chat
 *  requests must bind to the same window whose active profile the pill set, or
 *  a profile switch is invisible to the session. */
import { getWindowId } from "../lib/windowId";

// projQuery appends ?project=<root> for endpoints that select a registered
// project root via the query string (git + fs mutation endpoints).
function projQuery(project?: string, host?: string): string {
  const params = new URLSearchParams();
  if (project) params.set("project", project);
  if (host) params.set("host", host);
  const q = params.toString();
  return q ? `?${q}` : "";
}

/** Non-2xx response from fetchJSON. Carries the HTTP status so callers can
 *  tell a terminal answer (404/409: the thing no longer exists / already
 *  changed) from a retryable failure (network, 5xx). */
export class ApiError extends Error {
  readonly status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export async function fetchJSON<T>(path: string, init?: RequestInit, host?: string, projectPath?: string): Promise<T> {
  const headers = new Headers(init?.headers);
  if (!headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  for (const [k, v] of Object.entries(authHeaders())) headers.set(k, v);
  if (host && projectPath) headers.set("X-Ocode-Project", projectPath);
  const prefixed = host ? `${remoteApiBase(host)}${path}` : path;
  const res = await fetch(apiPath(prefixed), { ...init, headers });
  if (!res.ok) {
    reportAuthFailure(res.status);
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new ApiError(err.message || err.error || res.statusText, res.status);
  }
  // Guard the success path: a 2xx response with an empty or non-JSON body
  // (server bug, proxy misconfig, HTML SPA fallback for an unknown /api
  // route) makes bare res.json() throw WebKit's cryptic
  // "SyntaxError: The string did not match the expected pattern" in the
  // desktop WKWebView (Chrome says "Unexpected token ... in JSON").
  // Parse defensively so callers get a readable ApiError naming the route.
  // NOTE: an empty body resolves undefined (cast to T). Every current
  // empty-2xx endpoint routes through fetchEmpty/authedFetch instead, so no
  // caller depends on this — but a future fetchJSON caller doing
  // `const s = await api.x(); s.field` on an empty body would get a
  // runtime TypeError with no compiler warning. Prefer fetchEmpty for
  // empty-body endpoints; treat a fetchJSON undefined as a server bug.
  const text = await res.text();
  if (text.trim() === "") return undefined as T;
  try {
    return JSON.parse(text) as T;
  } catch {
    throw new ApiError(
      `Non-JSON response from ${path} (status ${res.status}, content-type ${res.headers.get("content-type") ?? "none"})`,
      res.status,
    );
  }
}

async function fetchEmpty(path: string, init?: RequestInit): Promise<void> {
  const headers = new Headers(init?.headers);
  if (!headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  for (const [k, v] of Object.entries(authHeaders())) headers.set(k, v);
  const res = await fetch(apiPath(path), { ...init, headers });
  if (!res.ok) {
    reportAuthFailure(res.status);
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || res.statusText);
  }
}

/**
 * Reads an SSE stream from an already-open fetch Response and dispatches each
 * frame's data to the handler registered for its event name (frames without an
 * `event:` line dispatch to `message`). Handles LF and CRLF frame separators,
 * multi-line `data:` fields, comment lines, and frames split across network
 * chunks; JSON `data:` payloads are parsed, anything else passes through as a
 * raw string. Resolves when the stream ends, rejects on network/parse errors
 * or when the request's AbortSignal aborts (abort the *fetch* itself — the
 * underlying body read rejects with AbortError and propagates here).
 *
 * This is the fetch-based counterpart to EventSource: it works for one-shot
 * request-scoped streams (file search, exports) that must carry Authorization
 * headers, must not auto-reconnect, and need abort-on-demand.
 */
export async function readSSEStream<T = unknown>(
  res: Response,
  handlers: Record<string, (data: T) => void>,
): Promise<void> {
  if (!res.body) throw new Error("response has no readable body");
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let eventName = "";
  let dataLines: string[] = [];

  const dispatch = () => {
    if (dataLines.length === 0 && !eventName) return; // comment-only frame
    const name = eventName || "message";
    const raw = dataLines.join("\n");
    dataLines = [];
    eventName = "";
    const handler = handlers[name];
    if (!handler) return;
    let payload: T;
    try {
      payload = JSON.parse(raw) as T;
    } catch {
      payload = raw as unknown as T;
    }
    handler(payload);
  };

  const dispatchFrame = (frame: string) => {
    for (const line of frame.split("\n")) {
      if (line.startsWith(":")) continue; // comment / keepalive
      if (line.startsWith("data:")) dataLines.push(line.slice(5).replace(/^ /, ""));
      else if (line.startsWith("event:")) eventName = line.slice(6).trim();
    }
    dispatch();
  };

  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    buffer = buffer.replace(/\r\n/g, "\n");
    let sep: number;
    while ((sep = buffer.indexOf("\n\n")) !== -1) {
      const frame = buffer.slice(0, sep);
      buffer = buffer.slice(sep + 2);
      dispatchFrame(frame);
    }
  }
  // Trailing frame without a final blank-line separator.
  if (buffer) dispatchFrame(buffer);
}

/** Calls one remote-host lifecycle endpoint (status/connect/restart). Those
 *  live at /api/remote/{host}/<action> on the LOCAL server, so the path is
 *  built here rather than via the host-prefixing fetchJSON. On a non-2xx the
 *  thrown Error carries the body's `error` and `stage` so the UI can say which
 *  stage failed (remote-connect / remote-kill / remote-register). */
async function remoteLifecycleRequest<T>(path: string, method: "GET" | "POST"): Promise<T> {
  const res = await fetch(apiPath(path), { method, headers: authHeaders() });
  if (!res.ok) {
    reportAuthFailure(res.status);
    const body = (await res.json().catch((err: unknown) => {
      console.error(`remote lifecycle: non-JSON error body from ${path} (status ${res.status}):`, err);
      return {};
    })) as { error?: string; stage?: string };
    const message = body.error || res.statusText || `HTTP ${res.status}`;
    throw new Error(body.stage ? `${message} (stage: ${body.stage})` : message);
  }
  const text = await res.text();
  if (!text.trim()) return undefined as T;
  try {
    return JSON.parse(text) as T;
  } catch {
    throw new Error(`Non-JSON response from ${path} (status ${res.status})`);
  }
}

export const api = {
  listSessions: (opts?: { limit?: number; offset?: number }, host?: string) => {
    const params = new URLSearchParams();
    if (opts?.limit) params.set("limit", String(opts.limit));
    if (opts?.offset) params.set("offset", String(opts.offset));
    const qs = params.toString();
    return fetchJSON<SessionListResponse>(
      `/api/sessions${qs ? `?${qs}` : ""}`,
      undefined,
      host,
    );
  },
  getSession: (id: string, opts?: { limit?: number; offset?: number }, host?: string) => {
    const params = new URLSearchParams();
    if (opts?.limit) params.set("limit", String(opts.limit));
    if (opts?.offset) params.set("offset", String(opts.offset));
    const qs = params.toString();
    return fetchJSON<SessionDetail>(
      `/api/sessions/${id}${qs ? `?${qs}` : ""}`,
      undefined, host,
    ).then((detail) => {
      // Record the stored-transcript revision this transcript was fetched at,
      // so the cross-process revalidation poll can detect an out-of-process
      // write (see lib/sessionRevision). Centralized here because every
      // transcript load path must leave the same baseline.
      noteSessionRevision(id, host, detail.revision);
      return detail;
    });
  },
  truncateSession: (id: string, keepUntil: number, host?: string) =>
    fetchJSON<SessionDetail>(`/api/sessions/${id}/truncate`, {
      method: "POST",
      body: JSON.stringify({ keepUntil }),
    }, host),
  // Full-transcript message search (/search, Ctrl/Cmd+F). Returns matching
  // message INDICES in the server's post-load array — the same positions
  // `getSession` paginates, so the find bar can jump to an off-window hit.
  // See internal/server/handler_session_search.go for why this is server-side.
  searchSession: (id: string, q: string, opts?: { limit?: number }, host?: string) => {
    const params = new URLSearchParams({ q });
    if (opts?.limit) params.set("limit", String(opts.limit));
    return fetchJSON<{
      total: number;
      indices: number[];
      truncated: boolean;
      scanned: number;
    }>(`/api/sessions/${id}/search?${params.toString()}`, undefined, host);
  },
  listModels: (opts?: { provider?: string; refresh?: boolean; configured?: boolean }, host?: string) => {
    const params = new URLSearchParams();
    if (opts?.provider) params.set("provider", opts.provider);
    if (opts?.refresh) params.set("refresh", "true");
    // configured=true trims the response to providers with credentials/config on
    // the server that answers (the host's own state for a remote session).
    if (opts?.configured) params.set("configured", "true");
    const qs = params.toString();
    return fetchJSON<ModelInfo[]>(`/api/models${qs ? `?${qs}` : ""}`, undefined, host);
  },
  listAgents: (host?: string) => fetchJSON<AgentInfo[]>("/api/config/agents", undefined, host),
  listAgentRuns: (session?: string, host?: string) =>
    fetchJSON<AgentRun[]>(
      `/api/agents/runs${session ? `?session=${encodeURIComponent(session)}` : ""}`,
      undefined, host,
    ),
  // `host` routes a session-scoped config read/write to a remote project's
  // server (/api/remote/<host>/…). The agent that consumes these process-global
  // gates runs on the session's own host, so omitting it writes the LOCAL
  // config and the remote session's sidebar refetch then shows no change at
  // all — the "toggle does nothing" symptom on remote SSH.
  getConfigModel: (host?: string) =>
    fetchJSON<{ model: string; context_max_tokens?: number }>("/api/config/model", undefined, host),
  setConfigModel: (model: string, host?: string) =>
    fetchJSON<{ model: string }>("/api/config/model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }, host),
  // Per-session model override (Part: per-chat-session model). Sets the model
  // for one session only — persisted in its transcript metadata and reflected
  // in that session's status snapshot — without touching the global config
  // model or any other session.
  setSessionModel: (sessionId: string, model: string, host?: string) =>
    fetchJSON<{ model: string; session_id: string }>(
      `/api/sessions/${sessionId}/model`,
      { method: "PUT", body: JSON.stringify({ model }) },
      host,
    ),
  // Clears a session's per-session model override so it falls back to the
  // global config model.
  clearSessionModel: (sessionId: string, host?: string) =>
    fetchJSON<{ model: string; session_id: string }>(
      `/api/sessions/${sessionId}/model`,
      { method: "DELETE" },
      host,
    ),
  // Add/remove a "provider/model" from the favorites list shared with the
  // TUI model picker (ctrl+f). Idempotent; responds with the full favorites
  // list so the dialog can resync star states without a model refetch.
  setModelFavorite: (model: string, favorited: boolean, host?: string) =>
    fetchJSON<{ model: string; favorite: boolean; favorites: string[] }>(
      "/api/models/favorite",
      {
        method: favorited ? "PUT" : "DELETE",
        body: JSON.stringify({ model }),
      },
      host,
    ),
  // Extended-thinking (reasoning effort) budget for the main model. budget 0 =
  // off; levels list the canonical off/low/med/high/xhigh/max options shared
  // with the TUI's /effort command.
  getThinkingBudget: (host?: string) =>
    fetchJSON<{ budget: number; level: string; levels: { level: string; budget: number }[] }>(
      "/api/config/thinking-budget",
      undefined,
      host,
    ),
  setThinkingBudget: (level: string, host?: string) =>
    fetchJSON<{ budget: number; level: string; levels: { level: string; budget: number }[] }>(
      "/api/config/thinking-budget",
      { method: "PUT", body: JSON.stringify({ level }) },
      host,
    ),
  getSmallModel: (host?: string) =>
    fetchJSON<{ model: string; priority: string }>("/api/config/small-model", undefined, host),
  setSmallModel: (model: string, host?: string) =>
    fetchJSON<{ model: string; source: string }>("/api/config/small-model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }, host),
  // Flip the runtime small-model on/off gate (persisted, mirrors the TUI's
  // small-model sidebar toggle).
  setSmallModelEnabled: (enabled: boolean, host?: string) =>
    fetchJSON<{ model: string; enabled: boolean; source: string }>("/api/config/small-model", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }, host),

  getPermissionModel: (host?: string) =>
    fetchJSON<{ model: string; enabled: boolean }>("/api/config/permission-model", undefined, host),
  setPermissionModel: (model: string, host?: string) =>
    fetchJSON<{ model: string; enabled: boolean }>("/api/config/permission-model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }, host),
  setPermissionModelEnabled: (enabled: boolean, host?: string) =>
    fetchJSON<{ model: string; enabled: boolean }>("/api/config/permission-model", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }, host),

  // Explorer agent (explore/scout) model. Off or unset falls back to the
  // small model, then the main model — see agent.injectPurposeModelIfEligible.
  getExplorerModel: (host?: string) =>
    fetchJSON<{ model: string; enabled: boolean }>("/api/config/explorer-model", undefined, host),
  setExplorerModel: (model: string, host?: string) =>
    fetchJSON<{ model: string; enabled: boolean }>("/api/config/explorer-model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }, host),
  setExplorerModelEnabled: (enabled: boolean, host?: string) =>
    fetchJSON<{ model: string; enabled: boolean }>("/api/config/explorer-model", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }, host),

  // Context agent (context/doc-sync) model. Off or unset falls back to the
  // small model, then the main model — see agent.injectPurposeModelIfEligible.
  getContextModel: (host?: string) =>
    fetchJSON<{ model: string; enabled: boolean }>("/api/config/context-model", undefined, host),
  setContextModel: (model: string, host?: string) =>
    fetchJSON<{ model: string; enabled: boolean }>("/api/config/context-model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }, host),
  setContextModelEnabled: (enabled: boolean, host?: string) =>
    fetchJSON<{ model: string; enabled: boolean }>("/api/config/context-model", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }, host),

  // --- New/extended OcodeConfig endpoints (Plan 1: configuration-api-backend) ---

  getRecapConfig: (host?: string) =>
    fetchJSON<{ recap_model: string; recap_model_enabled: boolean; recap_timeout_seconds: number }>(
      "/api/config/ocode/recap",
      undefined,
      host,
    ),
  setRecapConfig: (recap_model: string, recap_model_enabled: boolean, recap_timeout_seconds: number, host?: string) =>
    fetchJSON<{ recap_model: string; recap_model_enabled: boolean; recap_timeout_seconds: number }>(
      "/api/config/ocode/recap",
      { method: "PUT", body: JSON.stringify({ recap_model, recap_model_enabled, recap_timeout_seconds }) },
      host,
    ),

  getCommitMsgConfig: () =>
    fetchJSON<{ commit_msg_model: string; commit_msg_prompt: string }>("/api/config/ocode/commit-msg"),
  setCommitMsgConfig: (commit_msg_model: string, commit_msg_prompt: string) =>
    fetchJSON<{ commit_msg_model: string; commit_msg_prompt: string }>("/api/config/ocode/commit-msg", {
      method: "PUT",
      body: JSON.stringify({ commit_msg_model, commit_msg_prompt }),
    }),

  getCompactConfig: () => fetchJSON<CompactConfig>("/api/config/ocode/compact"),
  setCompactConfig: (cfg: CompactConfig) =>
    fetchJSON<CompactConfig>("/api/config/ocode/compact", { method: "PUT", body: JSON.stringify(cfg) }),

  getAdvisorFull: (host?: string) =>
    fetchJSON<{ model: string; provider: string; claude_code: boolean; checkpoints: string[] }>(
      "/api/config/advisor",
      undefined,
      host,
    ),
  setAdvisorFull: (fields: Partial<{ model: string; provider: string; claude_code: boolean; checkpoints: string[] }>, host?: string) =>
    fetchJSON<{ model: string; provider: string; claude_code: boolean; checkpoints: string[] }>(
      "/api/config/advisor",
      { method: "PUT", body: JSON.stringify(fields) },
      host,
    ),

  getAutoPermissionConfig: () => fetchJSON<AutoPermissionConfig>("/api/config/ocode/permissions-auto"),
  setAutoPermissionConfig: (cfg: AutoPermissionConfig) =>
    fetchJSON<AutoPermissionConfig>("/api/config/ocode/permissions-auto", {
      method: "PUT",
      body: JSON.stringify(cfg),
    }),
  getPermissionConcerns: () =>
    fetchJSON<{ concerns: RelaxableConcern[] }>("/api/config/ocode/permissions-concerns"),

  getMaskAdvanced: () =>
    fetchJSON<{
      enabled: boolean; mode: string; model: string; base_url: string; fail_mode: string;
      allow_remote_tier2: boolean; custom_words: string[];
    }>("/api/config/mask"),
  setMaskAdvanced: (fields: { base_url: string; fail_mode: string; allow_remote_tier2: boolean; custom_words: string[] }) =>
    fetchJSON<typeof fields>("/api/config/mask/advanced", { method: "PUT", body: JSON.stringify(fields) }),

	getDiscoveryConfig: (host?: string) =>
	  fetchJSON<DiscoveryConfig>("/api/config/ocode/discovery", undefined, host),
	setDiscoveryConfig: (cfg: DiscoveryConfig, host?: string) =>
	  fetchJSON<DiscoveryConfig>("/api/config/ocode/discovery", { method: "PUT", body: JSON.stringify(cfg) }, host),
	/** Config + live runtime status for one session (/discover status). */
	getDiscoveryStatus: (id: string, host?: string) =>
	  fetchJSON<DiscoveryStatus>(`/api/sessions/${id}/discovery`, undefined, host),

	getTTSEngines: () => fetchJSON<{ engines: TTSEngine[] }>("/api/tts/engines"),
	getTTSStatus: () => fetchJSON<TTSStatus>("/api/tts/status"),
	getTTSState: () => fetchJSON<Record<string, TTSInstallState>>("/api/tts/state"),
	ttsAcceptLicense: (engine: string, license_hash: string, license_name: string) =>
	  fetchJSON<{ state: string }>("/api/tts/license", { method: "POST", body: JSON.stringify({ engine, license_hash, license_name }) }),
	ttsPin: (engine: string, manifest_version: string) =>
	  fetchJSON<{ state: string }>("/api/tts/pin", { method: "POST", body: JSON.stringify({ engine, manifest_version }) }),
	ttsDownload: (engine: string) =>
	  fetchJSON<{ state: string }>("/api/tts/download", { method: "POST", body: JSON.stringify({ engine }) }),
	ttsEnable: (engine: string, model?: string) =>
	  fetchJSON<TTSStatus>("/api/tts/enable", { method: "POST", body: JSON.stringify({ engine, model }) }),
	ttsModelVoice: (engine: string, model: string, voice?: string) =>
	  fetchJSON<{ engine: string; model: string; voice: string }>("/api/tts/model-voice", { method: "POST", body: JSON.stringify({ engine, model, voice }) }),
	ttsAudioBlob: async (audioId: string): Promise<Blob> => {
	  const res = await fetch(apiPath(`/api/tts/audio/${encodeURIComponent(audioId)}`), { headers: authHeaders() });
	  if (!res.ok) throw new Error(`audio fetch failed (${res.status})`);
	  return res.blob();
	},
	getTTSConfig: () => fetchJSON<TTSConfig>("/api/config/ocode/tts"),
	setTTSConfig: (cfg: TTSConfig) =>
	  fetchJSON<TTSStatus>("/api/config/ocode/tts", { method: "PUT", body: JSON.stringify(cfg) }),
	ttsSpeak: (text: string, model?: string) =>
	  fetchJSON<TTSPlayback>("/api/tts/speak", { method: "POST", body: JSON.stringify({ text, model }) }),
	ttsStop: () => fetchJSON<TTSPlayback>("/api/tts/stop", { method: "POST" }),

	getTUISettings: () => fetchJSON<TUISettings>("/api/config/ocode/tui"),
  setTUISettings: (cfg: TUISettings) =>
    fetchJSON<TUISettings>("/api/config/ocode/tui", { method: "PUT", body: JSON.stringify(cfg) }),

  getEditorConfig: () =>
    fetchJSON<{ editor: string; editor_mode: string; ide_mode: string }>("/api/config/ocode/editor"),
  setEditorConfig: (editor: string, editor_mode: string, ide_mode: string) =>
    fetchJSON<{ editor: string; editor_mode: string; ide_mode: string }>("/api/config/ocode/editor", {
      method: "PUT",
      body: JSON.stringify({ editor, editor_mode, ide_mode }),
    }),

  getImageGenConfig: () => fetchJSON<ImageGenConfig>("/api/config/ocode/imagegen"),
  setImageGenConfig: (cfg: ImageGenConfig) =>
    fetchJSON<ImageGenConfig>("/api/config/ocode/imagegen", { method: "PUT", body: JSON.stringify(cfg) }),

  getPathsConfig: () =>
    fetchJSON<{ extra_allowed_paths: string[]; upload_dir: string; platform?: string }>(
      "/api/config/ocode/paths",
    ),
  setPathsConfig: (extra_allowed_paths: string[], upload_dir: string) =>
    fetchJSON<{ extra_allowed_paths: string[]; upload_dir: string }>("/api/config/ocode/paths", {
      method: "PUT",
      body: JSON.stringify({ extra_allowed_paths, upload_dir }),
    }),

  getLimitsConfig: () =>
    fetchJSON<{ max_steps: number; image_max_dim: number; max_concurrent_agents: number; undo_max_age_delta: number }>(
      "/api/config/ocode/limits",
    ),
  setLimitsConfig: (fields: { max_steps: number; image_max_dim: number; max_concurrent_agents: number; undo_max_age_delta: number }) =>
    fetchJSON<typeof fields>("/api/config/ocode/limits", { method: "PUT", body: JSON.stringify(fields) }),

  getBrowserConfig: () =>
    fetchJSON<{ chrome_path: string; idle_timeout_minutes: number; screencast_quality: number }>(
      "/api/config/ocode/browser",
    ),
  setBrowserConfig: (fields: { chrome_path: string; idle_timeout_minutes: number; screencast_quality: number }) =>
    fetchJSON<typeof fields>("/api/config/ocode/browser", { method: "PUT", body: JSON.stringify(fields) }),

  // Managed `htrcli serve` daemon lifecycle (Settings > Browser). start/stop
  // also persist htr_enabled; a failure is reported in status.error at HTTP 200.
  getHtrStatus: () => fetchJSON<HtrStatus>("/api/config/ocode/htr"),
  startHtr: () => fetchJSON<HtrStatus>("/api/config/ocode/htr/start", { method: "POST" }),
  stopHtr: () => fetchJSON<HtrStatus>("/api/config/ocode/htr/stop", { method: "POST" }),
  listHtrTabs: () => fetchJSON<{ tabs: HtrTab[]; error?: string }>("/api/config/ocode/htr/tabs"),

  getFeaturesConfig: () =>
    fetchJSON<{ memory_enabled: boolean; doc_prompt_enabled: boolean }>("/api/config/ocode/features"),
  setFeaturesConfig: (memory_enabled: boolean, doc_prompt_enabled: boolean) =>
    fetchJSON<{ memory_enabled: boolean; doc_prompt_enabled: boolean }>("/api/config/ocode/features", {
      method: "PUT",
      body: JSON.stringify({ memory_enabled, doc_prompt_enabled }),
    }),

  getProfileDebugConfig: () => fetchJSON<{ profile_debug: boolean }>("/api/config/ocode/profile-debug"),
  setProfileDebugConfig: (profile_debug: boolean) =>
    fetchJSON<{ profile_debug: boolean }>("/api/config/ocode/profile-debug", {
      method: "PUT",
      body: JSON.stringify({ profile_debug }),
    }),

  getPluginsEnabledConfig: () => fetchJSON<{ ast: boolean }>("/api/config/ocode/plugins-enabled"),
  setPluginsEnabledConfig: (ast: boolean) =>
    fetchJSON<{ ast: boolean }>("/api/config/ocode/plugins-enabled", { method: "PUT", body: JSON.stringify({ ast }) }),

  getLocalModelsConfig: (host?: string) =>
    fetchJSON<Record<string, { enabled: boolean; max_parallel: number }>>("/api/config/ocode/local-models", undefined, host),
  setLocalModelsConfig: (models: Record<string, { enabled: boolean; max_parallel: number }>, host?: string) =>
    fetchJSON<Record<string, { enabled: boolean; max_parallel: number }>>("/api/config/ocode/local-models", {
      method: "PUT",
      body: JSON.stringify(models),
    }, host),

  getBackendConfig: () => fetchJSON<{ backend_url: string }>("/api/config/ocode/backend"),
  setBackendConfig: (backend_url: string) =>
    fetchJSON<{ backend_url: string }>("/api/config/ocode/backend", {
      method: "PUT",
      body: JSON.stringify({ backend_url }),
    }),

  // Config/auth sync server (kakiit) override — separate from backend_url
  // above. Empty sync_url means "use the default" (resolved_url reports
  // what that resolves to: OCODE_SYNC_URL, then the production hub).
  getSyncURLConfig: () => fetchJSON<{ sync_url: string; resolved_url: string }>("/api/config/ocode/sync-url"),
  setSyncURLConfig: (sync_url: string) =>
    fetchJSON<{ sync_url: string; resolved_url: string }>("/api/config/ocode/sync-url", {
      method: "PUT",
      body: JSON.stringify({ sync_url }),
    }),
  getFakeAgentConfig: () =>
    fetchJSON<{ fake_agent: string; active: string; options: string[] }>("/api/config/ocode/fake-agent"),
  setFakeAgentConfig: (fake_agent: string) =>
    fetchJSON<{ fake_agent: string; active: string; options: string[] }>("/api/config/ocode/fake-agent", {
      method: "PUT",
      body: JSON.stringify({ fake_agent }),
    }),

  getGitDiff: (path?: string, project?: string, staged?: boolean, host?: string) => {
    const params = new URLSearchParams();
    if (path) params.set("path", path);
    if (project) params.set("project", project);
    if (host) params.set("host", host);
    if (staged) params.set("staged", "true");
    const query = params.toString();
    return fetchJSON<GitDiffFile[]>(`/api/git/diff${query ? `?${query}` : ""}`);
  },

  /** Working-tree status of the repo at `project` (or the server workdir).
   *  host selects a registered remote project (SSH/WSL) — the git pipeline
   *  then runs on that host (see the terminal endpoint's ?host= contract). */
  getGitStatus: (project?: string, host?: string) =>
    fetchJSON<GitStatus>(`/api/git/status${projQuery(project, host)}`),

  /** One-shot SourceTree-style snapshot: status + staged + unstaged diffs. */
  getGitWorkspace: (project?: string, host?: string) =>
    fetchJSON<GitWorkspace>(`/api/git/workspace${projQuery(project, host)}`),

  /** Recent commits, newest first. */
  gitLog: (project?: string, limit = 50, host?: string) => {
    const params = new URLSearchParams();
    if (project) params.set("project", project);
    if (host) params.set("host", host);
    params.set("limit", String(limit));
    return fetchJSON<GitCommit[]>(`/api/git/log?${params.toString()}`);
  },

  /** Diff of a single commit (for the commit detail pane). */
  gitShow: (commit: string, project?: string, host?: string) => {
    const params = new URLSearchParams({ commit });
    if (project) params.set("project", project);
    if (host) params.set("host", host);
    return fetchJSON<GitDiffFile[]>(`/api/git/show?${params.toString()}`);
  },

  /** Stage / unstage / discard a single hunk; returns the refreshed workspace. */
  gitHunk: (req: GitHunkRequest, project?: string, host?: string) =>
    fetchJSON<GitWorkspace>(`/api/git/hunk${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify(req),
    }),

  // ── Git file actions (driven by the web file-tree context menu) ──
  // The target project is passed as ?project= (the same convention the GET
  // git endpoints use); paths/message travel in the JSON body.
  gitStage: (paths: string[], project?: string, host?: string) =>
    fetchJSON<GitStatus>(`/api/git/stage${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ paths }),
    }),
  gitUnstage: (paths: string[], project?: string, host?: string) =>
    fetchJSON<GitStatus>(`/api/git/unstage${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ paths }),
    }),
  gitDiscard: (paths: string[], project?: string, host?: string) =>
    fetchJSON<GitStatus>(`/api/git/discard${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ paths }),
    }),
  gitStash: (message: string, paths: string[], project?: string, host?: string, includeUntracked = false) =>
    fetchJSON<GitStatus>(`/api/git/stash${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ paths, message, include_untracked: includeUntracked }),
    }),

  /** Stash entries of the repo, newest first. */
  gitStashList: (project?: string, host?: string) =>
    fetchJSON<GitStash[]>(`/api/git/stash/list${projQuery(project, host)}`),

  /** Per-file diff of one stash entry (index = position in the stash list). */
  gitStashShow: (index: number, project?: string, host?: string) => {
    const params = new URLSearchParams({ index: String(index) });
    if (project) params.set("project", project);
    if (host) params.set("host", host);
    return fetchJSON<GitDiffFile[]>(`/api/git/stash/show?${params.toString()}`);
  },

  /** Restore selected files from a stash entry into the working tree
   *  (unstaged). The stash entry is kept. Returns the refreshed workspace. */
  gitStashApply: (index: number, paths: string[], project?: string, host?: string) =>
    fetchJSON<GitWorkspace>(`/api/git/stash/apply${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ index, paths }),
    }),

  /** Delete one stash entry. Returns the refreshed stash list. */
  gitStashDrop: (index: number, project?: string, host?: string) =>
    fetchJSON<GitStash[]>(`/api/git/stash/drop${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ index }),
    }),
  gitCommit: (message: string, paths: string[], project?: string, host?: string) =>
    fetchJSON<GitStatus>(`/api/git/commit${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ paths, message }),
    }),

  // ── Git network actions ──
  gitFetch: (project?: string, host?: string) =>
    fetchJSON<GitStatus>(`/api/git/fetch${projQuery(project, host)}`, {
      method: "POST",
    }),
  gitPull: (project?: string, host?: string) =>
    fetchJSON<GitStatus>(`/api/git/pull${projQuery(project, host)}`, {
      method: "POST",
    }),
  gitPush: (project?: string, force = false, host?: string) =>
    fetchJSON<GitStatus>(`/api/git/push${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ force }),
    }),
  gitResetRemote: (project?: string, host?: string) =>
    fetchJSON<GitStatus>(`/api/git/reset-remote${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ force: true }),
    }),

  // ── File-system actions (web file-tree context menu) ──
  fsCopy: (paths: string[], destDir: string, project?: string, host?: string) =>
    fetchJSON<{ success: boolean }>(`/api/fs/copy${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ paths, dest_dir: destDir }),
    }),
  fsMove: (paths: string[], destDir: string, project?: string, host?: string) =>
    fetchJSON<{ success: boolean }>(`/api/fs/move${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ paths, dest_dir: destDir }),
    }),
  fsDelete: (paths: string[], project?: string, host?: string) =>
    fetchJSON<{ success: boolean }>(`/api/fs/delete${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ paths }),
    }),
  fsRename: (path: string, newName: string, project?: string, host?: string) =>
    fetchJSON<{ success: boolean; path: string }>(`/api/fs/rename${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ path, new_name: newName }),
    }),
  fsNewFile: (path: string, project?: string, host?: string) =>
    fetchJSON<{ success: boolean; path: string }>(`/api/fs/new-file${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ path }),
    }),
  fsNewFolder: (path: string, project?: string, host?: string) =>
    fetchJSON<{ success: boolean; path: string }>(`/api/fs/new-folder${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ path }),
    }),
  fsDuplicate: (path: string, project?: string, host?: string) =>
    fetchJSON<{ success: boolean; path: string }>(`/api/fs/duplicate${projQuery(project, host)}`, {
      method: "POST",
      body: JSON.stringify({ path }),
    }),
  listCronJobs: () => fetchJSON<CronJobsResponse>("/api/cron"),
  getCronJob: (id: string) => fetchJSON<CronJob>(`/api/cron/${id}`),
  addCronJob: (job: CronJobWriteRequest) =>
    fetchJSON<{ id: string }>("/api/cron", {
      method: "POST",
      body: JSON.stringify(job),
    }),
  updateCronJob: (id: string, patch: CronJobPatchRequest) =>
    fetchJSON<CronJob>(`/api/cron/${id}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),
  deleteCronJob: (id: string) =>
    fetchEmpty(`/api/cron/${id}`, {
      method: "DELETE",
    }),
  getCronOutbox: () => fetchJSON<CronOutboxResponse>("/api/cron/outbox"),
  drainCronOutbox: () =>
    fetchJSON<CronOutboxResponse>("/api/cron/outbox?drain=true"),
  getCronRuns: (jobId: string, limit = 50, offset = 0) =>
    fetchJSON<CronRunsResponse>(`/api/cron/${encodeURIComponent(jobId)}/runs?limit=${limit}&offset=${offset}`),
  getCronRun: (jobId: string, runId: string) =>
    fetchJSON<CronRun>(`/api/cron/${encodeURIComponent(jobId)}/runs/${encodeURIComponent(runId)}`),
  getCronTargets: () => fetchJSON<CronTargetsResponse>("/api/cron/targets"),
  setCronTarget: (workdir: string, chatId: number) =>
    fetchJSON<{ ok: boolean }>("/api/cron/targets", {
      method: "POST",
      body: JSON.stringify({ workdir, chat_id: chatId }),
    }),
  getTheme: (name?: string) =>
    fetchJSON<ThemeResponse>(
      name ? `/api/theme?name=${encodeURIComponent(name)}` : "/api/theme",
    ),
  getThemes: () => fetchJSON<ThemesListResponse>("/api/themes"),
  getSyncStatus: () => fetchJSON<SyncStatusResponse>("/api/sync/status"),
  syncLoginStart: () =>
    fetchJSON<SyncLoginStartResponse>("/api/sync/login/start", {
      method: "POST",
    }),
  syncLoginPoll: (deviceCode: string) =>
    fetchJSON<SyncLoginPollResponse>("/api/sync/login/poll", {
      method: "POST",
      body: JSON.stringify({ deviceCode }),
    }),
  syncLogout: () => fetchEmpty("/api/sync/logout", { method: "POST" }),
  getMCP: (host?: string) => fetchJSON<MCPStatus[]>("/api/mcp", undefined, host),
  getAdvisor: (host?: string) =>
    fetchJSON<{ model: string }>("/api/config/advisor", undefined, host),
  setAdvisor: (model: string, host?: string) =>
    fetchJSON<{ model: string }>("/api/config/advisor", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }, host),
  // Advisor on/off gate. With a sessionId it reads/writes that chat session's
  // own override (persisted to the session transcript metadata by the server,
  // never to global config); without one it is the process-wide default used
  // by the Settings form and the pre-session sidebar fallback. `host` routes a
  // session-scoped call to a remote project's server (/api/remote/<host>/…) —
  // without it the PUT hits the local server, which cannot resolve that
  // session and 404s, leaving a remote tab's toggle silently inert.
  getAdvisorEnabled: (sessionId?: string, host?: string) =>
    fetchJSON<{ enabled: boolean }>(
      sessionId
        ? `/api/config/advisor-enabled?session_id=${encodeURIComponent(sessionId)}`
        : "/api/config/advisor-enabled",
      undefined,
      host,
    ),
  setAdvisorEnabled: (enabled: boolean, sessionId?: string, host?: string) =>
    fetchJSON<{ enabled: boolean }>(
      "/api/config/advisor-enabled",
      {
        method: "PUT",
        body: JSON.stringify(sessionId ? { enabled, session_id: sessionId } : { enabled }),
      },
      host,
    ),
  // Interactive pty terminal configuration for the server's single workdir.
  // The terminal itself is always enabled; these are availability/scrollback.
  // Pass a remote project's host+path to describe THAT host's shells instead
  // of this machine's (the server probes the remote and reports `remote: true`).
  getTerminalConfig: (host?: string, project?: string) => {
    const params = new URLSearchParams();
    if (host) params.set("host", host);
    if (host && project) params.set("project", project);
    const query = params.toString();
    return fetchJSON<{
      available?: boolean;
      scrollback_lines: number;
      font_family: string;
      font_size: number;
      shell: string;
      default_shell: string;
      available_shells: string[];
      work_dir: string;
      /** Present and true when the response describes a remote host. */
      remote?: boolean;
    }>(`/api/config/terminal${query ? `?${query}` : ""}`);
  },
  setTerminalScrollbackLines: (scrollback_lines: number) =>
    fetchJSON<{ scrollback_lines: number }>("/api/config/terminal", {
      method: "PUT",
      body: JSON.stringify({ scrollback_lines }),
    }),
  setTerminalFontConfig: (font_family: string, font_size: number) =>
    fetchJSON<{ font_family: string; font_size: number }>("/api/config/terminal", {
      method: "PUT",
      body: JSON.stringify({ font_family, font_size }),
    }),
  setTerminalShell: (shell: string) =>
    fetchJSON<{ shell: string }>("/api/config/terminal", {
      method: "PUT",
      body: JSON.stringify({ shell }),
    }),
  getTerminalProcesses: () =>
    fetchJSON<
      { id: string; pid: number; cpu_percent: number; mem_bytes: number }[]
    >("/api/terminal/processes"),
  getBrowseProcesses: () =>
    fetchJSON<
      {
        state_key: string;
        tab_id: string;
        title: string;
        url: string;
        pid: number;
        cpu_percent: number;
        mem_bytes: number;
        js_heap_bytes: number;
        shared: boolean;
      }[]
    >("/api/browse/processes"),
  // TUI status (consolidated snapshot pushed by the TUI on every state
  // change). The web also subscribes to the "status" SSE event so the bar
  // updates live without polling.
  getTUIStatus: () => fetchJSON<TUIStatus>("/api/tui-status"),
  getSpending: (host?: string) =>
    fetchJSON<{ spending_usd: number; records: number }>("/api/spending", undefined, host),
  getLSPStatuses: (host?: string) =>
    fetchJSON<{ lsp_servers: LSPStatus[] }>("/api/lsp/statuses", undefined, host),
  getModifiedFiles: (host?: string) =>
    fetchJSON<{ modified_files: FileStatus[] }>("/api/files/modified", undefined, host),
  getSessionContext: (id: string, host?: string) =>
    fetchJSON<{
      session_id: string;
      message_count: number;
      /** Provider-reported context occupancy from the backend (0 = none
       *  recorded yet). Never a client-side estimate. */
      current_tokens: number;
      max_tokens?: number;
      model?: string;
      /** Full token-budget breakdown shared with the TUI's local /context.
       *  Present only when a live agent existed and was not mid-turn. */
      report?: ContextBudgetReport;
    }>(`/api/sessions/${id}/context`, undefined, host),
  // Reconcile endpoint (Parts 03–05): authoritative turn state + the bus seq
  // watermark. Reconcile = state fetch + transcript refetch, never event
  // replay. The watchdog and the reconnect path use this to clear a stuck
  // streaming spinner.
  getSessionState: (id: string, host?: string) =>
    fetchJSON<{
      bootstrap_stage: string;
      turn_active: boolean;
      last_seq: number;
      // Opaque stored-transcript token (see lib/sessionRevision). The
      // revalidation poll compares it against the revision the tab's
      // transcript was fetched at and refetches when it moved — the
      // cross-process sync signal for a write made by another ocode server
      // sharing this project. Absent for bridged/in-memory sessions.
      revision?: string;
      // Streaming text/thinking/tool_* frames still buffered from the
      // session's current turn (see appendLiveFrame server-side), replayed
      // by reconcileOpenSessions so a mid-turn reload doesn't lose the
      // in-progress reply. Absent/empty once the turn ends.
      live_frames?: { event: string; data: unknown; seq: number }[];
      // Unresolved asks the session's resident agent is paused on, read from
      // its live transcript. The authoritative recovery source when the
      // paused tool result never reached disk (so the fetched transcript
      // carries no sentinel to derive the pending dialog from). Absent when
      // the session is idle or has no live agent.
      pending_asks?: {
        permissions?: import("../api/types").SSEPermissionEvent[];
        questions?: {
          request_id: string;
          questions: import("../api/types").QuestionPrompt[];
        }[];
      };
      // The session has SETTLED on an unfinished turn (idle, unattended, and
      // the transcript tail is not a reply) — see the server's
      // Handler.sessionInterrupted. Drives the chat's interrupted-turn notice.
      // Absent when false, so a missing flag never invents an interruption.
      interrupted?: boolean;
    }>(`/api/sessions/${id}/state`, undefined, host),
  // Per-session status snapshot (Part 03): superset of /api/tui-status with
  // session_id populated and context_* included, so each tab renders its own
  // status without a TUI bridge.
  getSessionStatus: (id: string, host?: string) =>
    fetchJSON<TUIStatus>(`/api/sessions/${id}/status`, undefined, host),
  // ── Remote host lifecycle (local server, never proxied) ──
  // These hit /api/remote/{host}/<action> on the local server. status never
  // connects; connect brings the host up; restart kills and restarts its
  // `ocode serve --remote` (unguarded — running turns and terminals die).
  getRemoteHostStatus: (host: string) =>
    remoteLifecycleRequest<import("../api/types").RemoteHostStatus>(
      `/api/remote/${encodeURIComponent(host)}/status`,
      "GET",
    ),
  connectRemoteHost: (host: string) =>
    remoteLifecycleRequest<import("../api/types").RemoteHostStatus>(
      `/api/remote/${encodeURIComponent(host)}/connect`,
      "POST",
    ),
  restartRemoteHost: (host: string) =>
    remoteLifecycleRequest<import("../api/types").RemoteHostStatus>(
      `/api/remote/${encodeURIComponent(host)}/restart`,
      "POST",
    ),
  // Live terminals on a remote host, via the existing reverse proxy. The
  // project header lets the proxy register the project on first use.
  listRemoteTerminals: (host: string, projectPath: string) =>
    fetchJSON<{ terminals: import("../api/types").RemoteTerminalEntry[] }>(
      `/api/terminal?project_path=${encodeURIComponent(projectPath)}`,
      undefined,
      host,
      projectPath,
    ).then((r) => r.terminals),
  getSmallModelWithEnabled: (host?: string) =>
    fetchJSON<{ model: string; enabled: boolean; priority: string }>(
      "/api/config/small-model",
      undefined,
      host,
    ),
  // ── OCR (new structured API) ──
  getOcrConfig: () =>
    fetchJSON<import("../api/types").OcrConfig>("/api/config/ocr"),
  setOcrConfig: (cfg: import("../api/types").OcrConfig) =>
    fetchJSON<import("../api/types").OcrConfig>("/api/config/ocr", {
      method: "PUT",
      body: JSON.stringify(cfg),
    }),
  getOcrModels: () =>
    fetchJSON<import("../api/types").OcrModelsResponse>("/api/ocr/models"),

  // ── Computer use ──
  getComputerUseConfig: () =>
    fetchJSON<import("../api/types").ComputerUseConfig>("/api/config/computer-use"),
  setComputerUseConfig: (enabled: boolean) =>
    fetchJSON<import("../api/types").ComputerUseConfig>("/api/config/computer-use", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }),
  // Triggers the operating-system permission prompts the computer tool needs
  // (macOS: Accessibility + Screen Recording + Automation). Informational and
  // no-op on Windows/Linux, which require no explicit grant.
  requestComputerUsePermissions: () =>
    fetchJSON<import("../api/types").ComputerUsePermissionReport>(
      "/api/config/computer-use/permissions",
      { method: "POST" },
    ),

  // ── System Permissions (OS permission manager) ──
  getSystemPermissions: () =>
    fetchJSON<import("../api/types").SystemPermissionsResponse>(
      "/api/config/system-permissions",
    ),
  // Toggle one entry, or add a custom path (send {path, label?}).
  setSystemPermission: (payload: {
    id?: string;
    enabled?: boolean;
    path?: string;
    label?: string;
  }) =>
    fetchJSON<import("../api/types").SystemPermissionsResponse>(
      "/api/config/system-permissions",
      { method: "PUT", body: JSON.stringify(payload) },
    ),
  deleteSystemPermission: (id: string) =>
    fetchJSON<import("../api/types").SystemPermissionsResponse>(
      `/api/config/system-permissions?id=${encodeURIComponent(id)}`,
      { method: "DELETE" },
    ),
  // With no id, reconciles every enabled-but-not-granted entry.
  requestSystemPermissions: (id?: string) =>
    fetchJSON<import("../api/types").SystemPermissionsResponse>(
      "/api/config/system-permissions/request",
      { method: "POST", body: JSON.stringify(id ? { id } : {}) },
    ),

  // ── OCR (legacy API, deprecated) ──
  getOcrEnabled: () =>
    fetchJSON<{ enabled: boolean; model: string }>("/api/config/ocr-enabled"),
  setOcrEnabled: (enabled: boolean) =>
    fetchJSON<{ enabled: boolean }>("/api/config/ocr-enabled", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }),
  setOcrModel: (model: string) =>
    fetchJSON<{ model: string }>("/api/config/ocr-model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }),
  // Both send endpoints are called with async:true: the server acknowledges
  // with 202 as soon as the turn is dispatched instead of holding the request
  // open until the agent finishes. A browser allows only six concurrent
  // connections per origin over HTTP/1.1 (no TLS here, so no h2 multiplexing),
  // and a connection pinned for every running turn starved the other sessions'
  // requests — a second session would just sit there doing nothing. The turn's
  // output arrives over the session mirror (see SessionTabSync), which is where
  // the UI renders it from anyway.
  sendMessage: (sessionId: string, content: string, host?: string) => {
    // Must be the same window id the ProfileSwitcher wrote its active profile
    // to (see getWindowId); re-deriving it here is what let the two diverge.
    const windowId = getWindowId()
    return fetchJSON<ChatResponse>(`/api/sessions/${sessionId}/message`, {
      method: "POST",
      headers: { "X-Window-Id": windowId },
      body: JSON.stringify({ content, windowId, async: true }),
    }, host)
  },
  chat: (content: string, sessionId?: string, model?: string, requestId?: string, projectPath?: string, host?: string, permissionMode?: string) => {
    const windowId = getWindowId()
    return fetchJSON<ChatResponse>("/api/chat", {
      method: "POST",
      headers: { "X-Window-Id": windowId },
      body: JSON.stringify({
        content,
        sessionId,
        model,
        request_id: requestId,
        project_path: projectPath,
        windowId,
        // A draft ("new-*") tab has no server session id yet, so its permission
        // mode rides along with the first message; the server persists it as
        // the new session's override. Ignored for an existing session.
        permission_mode: permissionMode,
        async: true,
      }),
    }, host, projectPath)
  },
  // Run a shell command via POST /api/shell (the `!` prefix). `host` targets a
  // registered ocode Remote project: the server runs the command on that host
  // through the host's own login shell instead of locally. Omitted (or empty)
  // keeps the historical local execution.
  // `session` is the tab id: for a LOCAL project the server keeps a persistent
  // per-session shell keyed by it, so env/aliases/functions are present and
  // state (cwd, exports) persists between `!` commands. `cwd` comes back on
  // every server path.
  shellCommand: (command: string, workDir?: string, host?: string, session?: string) =>
    fetchJSON<{ output: string; exitCode: number; error: string; cwd: string }>("/api/shell", {
      method: "POST",
      body: JSON.stringify({ command, workDir, host, session }),
    }),
  listProjects: () => fetchJSON<Project[]>("/api/projects"),
  /** The saved project root matching the server's working directory (auto-added
   *  when the cwd is a real project root), or null. Used to auto-select the
   *  sidebar project on startup. */
  getCurrentProject: () =>
    fetchJSON<{ project: Project | null; cwd?: string }>("/api/projects/current"),
  addProject: (path: string) =>
    fetchJSON<{ status: string }>("/api/projects", {
      method: "POST",
      body: JSON.stringify({ path }),
    }),
  /** Add a remote (SSH/WSL) project. Host is `[user@]host` or `wsl:<distro>`. */
  addRemoteProject: (host: string, path: string, port?: number) =>
    fetchJSON<{ status: string }>("/api/projects", {
      method: "POST",
      body: JSON.stringify({ host, path, ...(port ? { port } : {}) }),
    }),
  updateRemoteProject: (input: {
    old_host: string;
    old_path: string;
    path: string;
    kind: "ssh" | "wsl";
    user?: string;
    host?: string;
    port?: number;
    distro?: string;
  }) =>
    fetchJSON<Project>("/api/projects/remote", {
      method: "PATCH",
      body: JSON.stringify(input),
    }),
  removeProject: (path: string) =>
    fetchJSON<{ status: string }>("/api/projects/" + encodeURIComponent(path), {
      method: "DELETE",
    }),
  /** Remove with explicit host scope (for remote projects). */
  removeRemoteProject: (path: string, host: string) =>
    fetchJSON<{ status: string }>(
      `/api/projects/${encodeURIComponent(path)}?host=${encodeURIComponent(host)}`,
      { method: "DELETE" },
    ),
  listProjectSessions: (path: string, host?: string) =>
    fetchJSON<SessionInfo[]>(
      `${remoteApiBase(host)}/api/projects/sessions?path=${encodeURIComponent(path)}`,
    ),
  renameProject: (path: string, name: string, host?: string) =>
    fetchJSON<{ status: string }>("/api/projects/rename", {
      method: "POST",
      body: JSON.stringify(host ? { path, host, name } : { path, name }),
    }),
  reorderProjects: (refs: Array<{ path: string; host?: string }>) =>
    fetchJSON<{ status: string }>("/api/projects/reorder", {
      method: "POST",
      // Scoped form: remote entries are keyed by (host, verbatim path) so
      // the same path on two hosts (or local vs remote) keeps its own
      // position. Local entries omit host, preserving legacy semantics.
      body: JSON.stringify({ projects: refs.map((r) => ({ path: r.path, host: r.host ?? "" })) }),
    }),
  setProjectGroup: (path: string, group: string, host?: string) =>
    fetchJSON<{ status: string }>("/api/projects/group", {
      method: "POST",
      body: JSON.stringify(host ? { path, host, group } : { path, group }),
    }),
  listGroups: () => fetchJSON<ProjectGroup[]>("/api/projects/groups"),
  /** Every project's open-session tabs, server-side so every window/origin
   *  sees the same tab bar. */
  getTabs: () => fetchJSON<{ projects: Record<string, ServerProjectTabs> }>("/api/tabs"),
  /** Full replacement of every project's open-session tabs. */
  setTabs: (projects: Record<string, ServerProjectTabs>) =>
    fetchJSON<{ status: string }>("/api/tabs", { method: "PUT", body: JSON.stringify({ projects }) }),
  createGroup: (name: string) =>
    fetchJSON<{ status: string }>("/api/projects/groups", {
      method: "POST",
      body: JSON.stringify({ name }),
    }),
  deleteGroup: (name: string) =>
    fetchJSON<{ status: string }>("/api/projects/groups/" + encodeURIComponent(name), {
      method: "DELETE",
    }),
  renameGroup: (oldName: string, newName: string) =>
    fetchJSON<Project[]>("/api/projects/groups/rename", {
      method: "POST",
      body: JSON.stringify({ old_name: oldName, new_name: newName }),
    }),
  reorderGroups: (names: string[]) =>
    fetchJSON<{ status: string }>("/api/projects/groups/reorder", {
      method: "POST",
      body: JSON.stringify({ names }),
    }),
  setGroupCollapsed: (name: string, collapsed: boolean) =>
    fetchJSON<{ status: string }>("/api/projects/groups/collapse", {
      method: "POST",
      body: JSON.stringify({ name, collapsed }),
    }),
  // Monaco editor settings and extensions
  getMonacoSettings: () => fetchJSON<{ theme: string; font_size: number; tab_size: number; word_wrap: boolean; minimap: boolean; line_numbers: boolean }>("/api/monaco/settings"),
  setMonacoSettings: (settings: Record<string, unknown>) =>
    fetchJSON<{ status: string }>("/api/monaco/settings", {
      method: "PUT",
      body: JSON.stringify(settings),
    }),
  listMonacoExtensions: () =>
    fetchJSON<Array<{ name: string; label: string; enabled: boolean; builtin: boolean }>>("/api/monaco/extensions"),
  toggleMonacoExtension: (name: string) =>
    fetchJSON<{ name: string; label: string; enabled: boolean; builtin: boolean }[]>("/api/monaco/extensions/" + encodeURIComponent(name) + "/toggle", {
      method: "PUT",
    }),
  // Directory browser for the project sidebar folder picker.
  browseDirectory: (path?: string) =>
    fetchJSON<BrowseResponse>(
      "/api/browse" + (path ? "?path=" + encodeURIComponent(path) : ""),
    ),
  // Session operations
  compactSession: (id: string, host?: string, focus?: string) =>
    fetchJSON<{ original_len: number; compacted_len: number }>(
      `/api/sessions/${encodeURIComponent(id)}/compact`,
      { method: "POST", body: JSON.stringify(focus ? { focus } : {}) },
      host,
    ),
  recapSession: (id: string, host?: string) =>
    fetchJSON<{ recap: string }>(
      `/api/sessions/${encodeURIComponent(id)}/recap`,
      undefined, host,
    ),
  shareSession: (id: string, host?: string) =>
    fetchJSON<{ markdown: string }>(
      `/api/sessions/${encodeURIComponent(id)}/share`,
      undefined, host,
    ),
  btwSession: (id: string, content: string, host?: string) =>
    fetchJSON<{ status: string }>(
      `/api/sessions/${encodeURIComponent(id)}/btw`, {
        method: "POST",
        body: JSON.stringify({ content }),
      },
      host,
    ),

  // Mask (secret redaction) config
  getMaskConfig: () =>
    fetchJSON<{ enabled: boolean; mode: string; model: string }>("/api/config/mask"),
  setMaskEnabled: (enabled: boolean) =>
    fetchJSON<{ enabled: boolean }>("/api/config/mask/enabled", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }),
  setMaskMode: (mode: string) =>
    fetchJSON<{ mode: string }>("/api/config/mask/mode", {
      method: "PUT",
      body: JSON.stringify({ mode }),
    }),
  setMaskModel: (model: string) =>
    fetchJSON<{ model: string }>("/api/config/mask/model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }),

  // ── File edit history ──
  undoFileChange: (session?: string, host?: string) =>
    fetchJSON<{ path: string; action: string }>(
      `/api/files/undo${session ? `?session=${encodeURIComponent(session)}` : ""}`,
      { method: "POST" },
      host,
    ),
  redoFileChange: (session?: string, host?: string) =>
    fetchJSON<{ path: string; action: string }>(
      `/api/files/redo${session ? `?session=${encodeURIComponent(session)}` : ""}`,
      { method: "POST" },
      host,
    ),

  // ── File content save (PUT) ──
  // host selects a registered remote project: it travels in the ?host=
  // query string (the server dispatches on hostParam(r) only — a host
  // field in the JSON body is ignored and the save would take the LOCAL
  // branch against a remote project_root).
  saveFileContent: (path: string, content: string, projectRoot?: string, expectedHash?: string, force?: boolean, host?: string) =>
    fetchJSON<{ path: string; saved: boolean }>(`/api/files/content${host ? `?host=${encodeURIComponent(host)}` : ""}`, {
      method: "PUT",
      body: JSON.stringify({ path, content, project_root: projectRoot, expected_hash: expectedHash, force }),
    }),

  // ── File content load (GET) for the sidebar PreviewHost text/markdown
  // viewer (same endpoint the editor tabs use). host selects a registered
  // remote project — the read runs on that host.
  getFileContent: async (path: string, projectRoot?: string, host?: string): Promise<{ content: string; is_binary: boolean }> => {
    const query = new URLSearchParams({ path });
    if (projectRoot) query.set("project_root", projectRoot);
    if (host) query.set("host", host);
    const res = await fetch(apiPath(`/api/files/content?${query.toString()}`), { headers: authHeaders() });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || res.statusText);
    }
    const data = await res.json();
    return { content: data.content as string, is_binary: !!data.is_binary };
  },

  // ── Sidebar preview: open with the OS default app (native-fidelity
  // playback for pptx/docx) — forces the system opener, never the editor.
  openFileWithOS: (path: string, projectRoot?: string) =>
    fetchJSON<{ path: string; status: string }>("/api/files/open", {
      method: "POST",
      body: JSON.stringify({ path, mode: "os", project_root: projectRoot }),
    }),

  // ── Files tab: reveal a file/folder in the OS-native file manager (Finder
  // on macOS, Explorer on Windows, the desktop file manager on Linux). The
  // server runs the reveal command on its own host — there is no remote
  // branch, so callers must not use this for a remote project (it would
  // reveal an unrelated path on the local machine). A directory opens
  // directly; a file is selected in its containing folder.
  revealInFileManager: (path: string, projectRoot?: string) =>
    fetchJSON<{ path: string; status: string }>("/api/files/open", {
      method: "POST",
      body: JSON.stringify({ path, mode: "reveal", project_root: projectRoot }),
    }),

  // ── Sidebar preview: fetch raw bytes for pdf/docx/pptx/image/audio/video/mmd via
  // GET /api/files/raw (auth headers required — plain <img>/<iframe> tags
  // can't attach them, so callers use fetch + blob URLs). host selects a
  // registered remote project — the read runs on that host.
  fetchFileRaw: async (path: string, projectRoot?: string, host?: string): Promise<ArrayBuffer> => {
    const q = `path=${encodeURIComponent(path)}${projectRoot ? `&project_root=${encodeURIComponent(projectRoot)}` : ""}${host ? `&host=${encodeURIComponent(host)}` : ""}`;
    const res = await fetch(apiPath(`/api/files/raw?${q}`), { headers: authHeaders() });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || res.statusText);
    }
    return res.arrayBuffer();
  },

  // ── Sidebar preview (audio/video): exchange the bearer for a short-lived,
  // single-file capability the browser's media element can carry in the URL.
  // <video>/<audio> cannot set an Authorization header, and the master
  // ?token= form is forbidden in remote mode; the capability is scoped to this
  // one path. POST is authed normally, so the capability never leaves the
  // browser except as the media URL.
  getMediaToken: (path: string, projectRoot?: string) =>
    fetchJSON<{ token: string }>("/api/files/media-token", {
      method: "POST",
      body: JSON.stringify({ path, project_root: projectRoot }),
    }),

  // ── Session title / export ──
  setSessionTitle: (id: string, title: string, host?: string) =>
    fetchJSON<{ title: string }>(
      `/api/sessions/${encodeURIComponent(id)}/title`,
      { method: "PUT", body: JSON.stringify({ title }) },
      host,
    ),
  generateSessionTitle: (id: string, host?: string) =>
    fetchJSON<{ title: string }>(
      `/api/sessions/${encodeURIComponent(id)}/title/generate`,
      { method: "POST" },
      host,
    ),
  // The server returns raw markdown (text/markdown), not JSON, so this uses a
  // raw fetch and reads the body as text.
  exportSessionMarkdown: async (id: string, host?: string): Promise<string> => {
    const prefix = remoteApiBase(host);
    const res = await fetch(
      apiPath(`${prefix}/api/sessions/${encodeURIComponent(id)}/export`),
      { headers: authHeaders() },
    );
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || res.statusText);
    }
    return res.text();
  },
  exportClaudeSession: (id: string, host?: string) =>
    fetchJSON<{ path: string }>(
      `${remoteApiBase(host)}/api/sessions/${encodeURIComponent(id)}/export-claude`,
    ),

  // ── Usage ──
  // `sessionId` scopes the summary to that chat's attributed ledger rows
  // (per-session spend history); `host` routes a remote session's read to its
  // own server.
  getUsage: (range?: string, sessionId?: string, host?: string) => {
    const params = new URLSearchParams();
    if (range) params.set("range", range);
    if (sessionId) params.set("session_id", sessionId);
    const q = params.toString();
    return fetchJSON<UsageSummary>(`/api/usage${q ? `?${q}` : ""}`, undefined, host);
  },

  // ── Init ──
  initProject: (project?: string, host?: string) =>
    fetchJSON<{ path: string; status: string }>("/api/init", {
      method: "POST",
      body: JSON.stringify({ project: project || undefined }),
    }, host),

  // ── Permissions ──
  // Permission modes are PER CHAT SESSION. Every read/write takes an optional
  // sessionId so a yolo/sandbox toggle on one tab never leaks into another
  // chat or project. Omitting it (settings form) reports/inspects the
  // persisted config default. `host` routes a remote session's call to its
  // own server so the toggle applies to the session that actually lives there.
  getPermissions: (sessionId?: string, host?: string) =>
    fetchJSON<PermissionsResponse>(
      `/api/permissions${sessionId ? `?session_id=${encodeURIComponent(sessionId)}` : ""}`,
      undefined,
      host,
    ),
  getYolo: (sessionId?: string, host?: string) =>
    fetchJSON<{ yolo: boolean }>(
      `/api/permissions/yolo${sessionId ? `?session_id=${encodeURIComponent(sessionId)}` : ""}`,
      undefined,
      host,
    ),
  setYolo: (enabled: boolean, sessionId?: string, host?: string) =>
    fetchJSON<{ yolo: boolean }>("/api/permissions/yolo", {
      method: "PUT",
      body: JSON.stringify({ enabled, session_id: sessionId }),
    }, host),
  /** Set one chat session's live permission mode: normal|yolo|locked|sandbox.
   *  The override is persisted in that session's metadata so it survives
   *  resume and restart; it never affects any other session. */
  setPermissionMode: (mode: string, sessionId?: string, host?: string) =>
    fetchJSON<{ mode: string; session_id?: string }>("/api/permissions/mode", {
      method: "PUT",
      body: JSON.stringify({ mode, session_id: sessionId }),
    }, host),
  /** The persisted default permission mode new TUI/web/RC sessions start in. */
  getPermissionModeConfig: () =>
    fetchJSON<PermissionModeConfigResponse>("/api/config/ocode/permissions-mode"),
  /** Persist the default permission mode: normal|yolo|locked|sandbox. */
  setPermissionModeConfig: (mode: string) =>
    fetchJSON<PermissionModeConfigResponse>("/api/config/ocode/permissions-mode", {
      method: "PUT",
      body: JSON.stringify({ mode }),
    }),

  // ── Agent selection ──
  setAgent: (name: string, sessionId?: string, host?: string) =>
    fetchJSON<{ name: string; description: string }>("/api/config/agent", {
      method: "PUT",
      body: JSON.stringify({ name, session_id: sessionId }),
    }, host),

  // ── MCP enable/disable ──
  setMCPEnabled: (name: string, enabled: boolean) =>
    fetchJSON<{ name: string; status: string }>(
      `/api/mcp/${encodeURIComponent(name)}/${enabled ? "enable" : "disable"}`,
      { method: "PUT" },
    ),

  // ── MCP OAuth (/mcp-auth) ──
  // The server refuses non-loopback callers (the browser callback only reaches
  // the machine running ocode), so a remote session surfaces a 403 explanation.
  startMCPAuth: (name: string, host?: string) =>
    fetchJSON<{ job_id: string; server: string; status: string; browser_note?: string }>(
      `/api/mcp/${encodeURIComponent(name)}/auth`,
      { method: "POST" },
      host,
    ),
  getMCPAuthStatus: (jobId: string, host?: string) =>
    fetchJSON<{ job_id: string; server: string; status: string; error?: string }>(
      `/api/mcp/auth/${encodeURIComponent(jobId)}`,
      undefined,
      host,
    ),

  // ── Plugins ──
  listPlugins: () => fetchJSON<PluginInfo[]>("/api/plugins"),
  setPluginEnabled: (name: string, enabled: boolean) =>
    fetchJSON<{ name: string; status: string }>(
      `/api/plugins/${encodeURIComponent(name)}/${enabled ? "enable" : "disable"}`,
      { method: "PUT" },
    ),
  installPlugin: (source: string) =>
    fetchJSON<{ name: string; dir: string; source: string }>("/api/plugins", {
      method: "POST",
      body: JSON.stringify({ source }),
    }),
  removePlugin: async (name: string): Promise<void> => {
    const res = await fetch(
      apiPath(`/api/plugins/${encodeURIComponent(name)}`),
      { method: "DELETE", headers: authHeaders() },
    );
    // 204 No Content on success; any 2xx is acceptable.
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || res.statusText);
    }
  },

  // ── Dynamic commands / skills ──
  listCommands: () => fetchJSON<CommandEntry[]>("/api/commands"),
  listSkills: () => fetchJSON<SkillEntry[]>("/api/skills"),

  // ── Command context (repo-analysis prompts for /standup, /changes, /review) ──
  // `project` selects which registered repo root the prompt is assembled from
  // (the server resolves it against its registry); `host` routes the call to a
  // remote project's server. Without both, a remote/multi-project tab got the
  // server's default workdir instead of its own repo.
  getCommandContext: (name: string, args?: string, project?: string, host?: string) => {
    const params = new URLSearchParams();
    if (args) params.set("args", args);
    if (project) params.set("project", project);
    const qs = params.toString();
    return fetchJSON<{ prompt: string }>(
      `/api/command-context/${encodeURIComponent(name)}${qs ? `?${qs}` : ""}`,
      undefined,
      host,
    );
  },

  // ── Slash-command parity (/paths, /mem, /ban, /autocontinue, /connect, /docs) ──
  getPathsInfo: (project?: string) => {
    const query = project ? `?project=${encodeURIComponent(project)}` : "";
    return fetchJSON<{
      work_dir: string;
      extra_allowed_paths: string[];
      upload_dir: string;
      active_opencode_path: string;
      text: string;
    }>(`/api/paths${query}`);
  },
  getMemoryStatus: (project?: string) => {
    const query = project ? `?project=${encodeURIComponent(project)}` : "";
    return fetchJSON<MemoryStatusResponse>(`/api/memory/status${query}`);
  },
  setBashRule: (prefix: string, level: "allow" | "deny" | "ask") =>
    fetchJSON<{ prefix: string; level: string }>("/api/permissions/bash-rule", {
      method: "POST",
      body: JSON.stringify({ prefix, level }),
    }),
  getAutoContinue: (host?: string) =>
    fetchJSON<{ enabled: boolean; model: string }>("/api/config/ocode/autocontinue", undefined, host),
  setAutoContinue: (fields: { enabled?: boolean; model?: string; clear?: boolean }, host?: string) =>
    fetchJSON<{ enabled: boolean; model: string }>("/api/config/ocode/autocontinue", {
      method: "PUT",
      body: JSON.stringify(fields),
    }, host),
  connectProvider: (provider: string, api_key: string) =>
    fetchJSON<{ provider: string; key: string }>("/api/auth/connect", {
      method: "POST",
      body: JSON.stringify({ provider, api_key }),
    }),
  getDocsStatus: (project?: string, host?: string) => {
    const query = project ? `?project=${encodeURIComponent(project)}` : "";
    return fetchJSON<{ enabled: boolean; text: string }>(`/api/docs/status${query}`, undefined, host);
  },
  docsInit: (project?: string, host?: string) => {
    const query = project ? `?project=${encodeURIComponent(project)}` : "";
    return fetchJSON<{ result: string; annotate_prompt?: string }>(`/api/docs/init${query}`, {
      method: "POST",
    }, host);
  },
  docsUpdate: (sessionId: string, focus: string, project?: string, host?: string) => {
    const params = new URLSearchParams();
    if (project) params.set("project", project);
    const query = params.toString();
    return fetchJSON<{ result: string }>(`/api/docs/update${query ? `?${query}` : ""}`, {
      method: "POST",
      body: JSON.stringify({ session_id: sessionId, focus }),
    }, host);
  },
  docsCleanup: (confirm: boolean, project?: string, host?: string) => {
    const params = new URLSearchParams();
    if (project) params.set("project", project);
    const query = params.toString();
    return fetchJSON<{ result: string }>(`/api/docs/cleanup${query ? `?${query}` : ""}`, {
      method: "POST",
      body: JSON.stringify({ confirm }),
    }, host);
  },

  // ── GitHub (backing /github pr|issue) ──
  getGithubPR: (owner: string, repo: string, number: number) =>
    fetchJSON<{ pr: Record<string, unknown>; diff?: string }>(
      `/api/github/pr/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/${number}`,
    ),
  getGithubIssues: (owner: string, repo: string, state?: string) =>
    fetchJSON<Record<string, unknown>[]>(
      `/api/github/issues/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}${state ? `?state=${encodeURIComponent(state)}` : ""}`,
    ),

  // ── Agent question prompts ──
  // Answer a pending `question` prompt raised by the agent. Throws on 404/409
  // so callers can surface the failure and dismiss the dialog.
  answerQuestion: (
    requestId: string,
    sessionId: string | null,
    answers: import("./types").QuestionAnswerPayload[],
    host?: string,
  ) =>
    fetchJSON<ChatResponse>("/api/questions", {
      method: "POST",
      body: JSON.stringify({
        request_id: requestId,
        session_id: sessionId ?? undefined,
        answers,
      }),
    }, host),

  // Cancel (dismiss) a pending `question` prompt without answering it — the
  // web equivalent of the TUI's Esc on the question dialog. The server rewrites
  // the sentinel tool result in place with a dismissal notice, persists it, and
  // broadcasts `question_resolved`; no continuation turn runs, so the session
  // goes idle and the next user message starts an ordinary turn.
  cancelQuestion: (requestId: string, sessionId: string | null, host?: string) =>
    fetchJSON<ChatResponse>("/api/questions/cancel", {
      method: "POST",
      body: JSON.stringify({
        request_id: requestId,
        session_id: sessionId ?? undefined,
      }),
    }, host),

  // ── Agent permission prompts ──
  // Resolve a pending PERMISSION_ASK raised by the agent (headless serve mode).
  // Distinct from the config POST /api/permissions (which sets a tool rule).
  // `decision` is allow | deny | always_rule | always_tool; the legacy boolean
  // `approved` is still accepted by the server for old clients. Throws on
  // 404/409 so callers can surface the failure and dismiss the dialog.
  resolvePermission: (
    requestId: string,
    sessionId: string | null,
    decision: PermissionDecision,
    host?: string,
  ) =>
    fetchJSON<ChatResponse>("/api/permissions/resolve", {
      method: "POST",
      body: JSON.stringify({
        request_id: requestId,
        session_id: sessionId ?? undefined,
        decision,
      }),
    }, host),
  // ── Changes tab (session file changes) ──
  // `host` routes a remote (SSH/WSL) project's session-scoped request through
  // /api/remote/{host}; without it the local server answers (and returns an
  // empty list / 404) for a remote session.
  listChanges: (session?: string, host?: string) =>
    fetchJSON<FileChange[]>(
      `/api/changes${session ? `?session=${encodeURIComponent(session)}` : ""}`,
      undefined,
      host,
    ),
  getChangeDiff: (session: string | undefined, path: string, host?: string) =>
    fetchJSON<ChangeDiff>(
      `/api/changes/diff?${session ? `session=${encodeURIComponent(session)}&` : ""}path=${encodeURIComponent(path)}`,
      undefined,
      host,
    ),
  undoChangeFile: (session: string | undefined, path: string, host?: string) =>
    fetchJSON<Record<string, never>>(
      `/api/changes/undo-file${session ? `?session=${encodeURIComponent(session)}` : ""}`,
      { method: "POST", body: JSON.stringify({ path }) },
      host,
    ),
  undoChangeBlock: (session: string | undefined, path: string, host?: string) =>
    fetchJSON<Record<string, never>>(
      `/api/changes/undo-block${session ? `?session=${encodeURIComponent(session)}` : ""}`,
      { method: "POST", body: JSON.stringify({ path }) },
      host,
    ),

  // ── Session logs (Logs tab) ──
  // Session-scoped; `host` routes a remote project's logs to that host's
  // server instead of showing the local server's ring buffer.
  getLogs: (sessionId: string, host?: string) =>
    fetchJSON<{ kind: string; message: string; session_id?: string }[]>(
      `/api/logs?session_id=${encodeURIComponent(sessionId)}`,
      undefined,
      host,
    ),
  clearLogs: (sessionId: string, host?: string) =>
    fetchJSON<{ status: string }>(
      `/api/logs?session_id=${encodeURIComponent(sessionId)}`,
      { method: "DELETE" },
      host,
    ),

  // ── Secret (age-encrypted file/dir) management ──
  secretInit: (path: string, passphrase: string, confirmPassphrase: string) =>
    fetchJSON<{ key_path: string }>("/api/secret/init", {
      method: "POST",
      body: JSON.stringify({ path, passphrase, confirm_passphrase: confirmPassphrase }),
    }),
  secretScan: (path: string, mode: "encrypt" | "decrypt") =>
    fetchJSON<SecretScanResponse>(
      `/api/secret/scan?path=${encodeURIComponent(path)}&mode=${mode}`,
    ),
  // CLI-utility detection/install — the `/tools` command's data source. These
  // hit the local server; a remote project's SPA routes them through
  // /api/remote/<host>/… so the probe runs on the remote host's PATH.
  getCliTools: (host?: string) => fetchJSON<CliToolsResponse>("/api/cli-tools", undefined, host),
  startCliToolsInstall: (tool: string, host?: string) =>
    fetchJSON<CliToolInstallStartResponse>(
      "/api/cli-tools/install",
      { method: "POST", body: JSON.stringify({ tool }) },
      host,
    ),
  getCliToolsInstallStatus: (jobId: string, host?: string) =>
    fetchJSON<CliToolInstallStatusResponse>(
      `/api/cli-tools/install/${encodeURIComponent(jobId)}`,
      undefined,
      host,
    ),
  secretEncrypt: (path: string, passphrase: string, confirmPassphrase: string) =>
    fetchJSON<SecretTransformResponse>("/api/secret/encrypt", {
      method: "POST",
      body: JSON.stringify({ path, passphrase, confirm_passphrase: confirmPassphrase }),
    }),
  secretDecrypt: (path: string, passphrase: string) =>
    fetchJSON<SecretTransformResponse>("/api/secret/decrypt", {
      method: "POST",
      body: JSON.stringify({ path, passphrase }),
    }),
  secretRekey: (path: string, oldPassphrase: string, newPassphrase: string, confirmNewPassphrase: string) =>
    fetchJSON<{ key_path: string }>("/api/secret/rekey", {
      method: "POST",
      body: JSON.stringify({
        path,
        old_passphrase: oldPassphrase,
        new_passphrase: newPassphrase,
        confirm_new_passphrase: confirmNewPassphrase,
      }),
    }),
  secretCancel: (jobId: string) =>
    fetchJSON<{ cancelled: boolean }>("/api/secret/cancel", {
      method: "POST",
      body: JSON.stringify({ job_id: jobId }),
    }),
  cancelSession: (sessionId: string, host?: string) =>
    fetchJSON<{ cancelled: boolean }>(`/api/sessions/${encodeURIComponent(sessionId)}/cancel`, {
      method: "POST",
    }, host),
  /** Re-run the last turn in place (the composer's Retry after a Stop or an
   *  LLM-loop error). Unlike sendMessage, the server does NOT append a new user
   *  row — it re-steps the existing transcript tail — so the user's message is
   *  not duplicated. `host` routes remote (SSH/WSL) sessions to their server. */
  retrySession: (sessionId: string, host?: string) =>
    fetchJSON<ChatResponse>(`/api/sessions/${encodeURIComponent(sessionId)}/retry`, {
      method: "POST",
      headers: { "X-Window-Id": getWindowId() },
    }, host),
  /** Terminate the backend for a closed session (web/desktop tab close):
   *  cancels in-flight work AND releases the resident agent. Fire-and-forget
   *  safe on idle sessions (server no-ops). */
  closeSession: (sessionId: string, host?: string) =>
    fetchJSON<{ cancelled: boolean }>(`/api/sessions/${encodeURIComponent(sessionId)}/close`, {
      method: "POST",
    }, host),
  /** Re-key a chat with a fresh session id, keeping the transcript (/reset-id).
   *  The provider's X-Opencode-Session / x-session-id header derives from the
   *  session id, so this busts provider-side cache/rate-limit/sticky-routing
   *  grouping. `host` routes remote (SSH/WSL) sessions to their own server. */
  resetSessionId: (sessionId: string, host?: string) =>
    fetchJSON<{ old_id: string; new_id: string }>(`/api/sessions/${encodeURIComponent(sessionId)}/reset-id`, {
      method: "POST",
    }, host),
  // Port forwards. With a target, these hit the project-scoped family served by
  // internal/server (`/api/portmaps?host=&project=`) so the panel follows the
  // active remote SSH project. Without one they hit the desktop
  // remote-workspace's single-tunnel family (internal/desktop/portmaps.go) —
  // reached only in desktop remote-workspace mode, where /api/* is proxied to
  // the remote and only the local desktop mux can answer.
  listPortMaps: (target?: PortMapTarget) =>
    fetchJSON<PortMapView[]>(portMapsPath(target)),
  addPortMap: (remotePort: number, localPort: number, target?: PortMapTarget) =>
    fetchJSON<PortMapView[]>(portMapsPath(target), {
      method: "POST",
      body: JSON.stringify({ remote_port: remotePort, local_port: localPort }),
    }),
  removePortMap: (remotePort: number, target?: PortMapTarget) =>
    fetchJSON<PortMapView[]>(portMapsPath(target, `/${remotePort}`), { method: "DELETE" }),
  setPortMapEnabled: (remotePort: number, enabled: boolean, target?: PortMapTarget) =>
    fetchJSON<PortMapView[]>(portMapsPath(target, `/${remotePort}/${enabled ? "enable" : "disable"}`), {
      method: "POST",
    }),
};

/** Port-forwards route for a target: the project-scoped family when a remote
 *  project is given, else the desktop single-workspace family.
 *
 *  `suffix` is the path segment that follows the route (`/{port}`,
 *  `/{port}/enable`). It MUST be inserted before the target query string —
 *  appending it to the returned URL instead lands the port inside the query
 *  (`/api/portmaps?host=…&project=~/www/app/3510/disable`), which the server
 *  reads as project path `~/www/app/3510/disable` and rejects with
 *  "host/project_path is not a remote project registered with this server"
 *  (and the POST lands on the add route, not the enable route). The desktop
 *  family has no query, so its suffix appends directly. */
function portMapsPath(target?: PortMapTarget, suffix = ""): string {
  if (!target) return `/api/desktop/portmaps${suffix}`;
  const params = new URLSearchParams();
  params.set("host", target.host);
  params.set("project", target.path);
  return `/api/portmaps${suffix}?${params.toString()}`;
}

/** True when the Port forwards panel should be offered for target (or, with no
 *  target, for the desktop remote-workspace's single tunnel).
 *
 *  Unavailable means the route does not exist here — a plain server without the
 *  project-scoped family, or a local/desktop session with no remote workspace.
 *  Treat a non-array response as "unavailable" too: a 200 whose body is not a
 *  portmaps JSON array means the route is actually missing (SPA fallback or
 *  proxy) and every panel action would fail anyway. 400 is unavailable as well —
 *  the server answered but does not accept this project (unregistered host, or
 *  a WSL target, which never needs forwards). Any other error is assumed
 *  transient and keeps the panel offered, matching the historical behavior. */
export async function isPortMapsAvailable(target?: PortMapTarget): Promise<boolean> {
  try {
    const maps = await api.listPortMaps(target);
    return Array.isArray(maps);
  } catch (e) {
    if (e instanceof ApiError && (e.status === 404 || e.status === 400)) return false;
    return !(e instanceof ApiError && e.status === 404);
  }
}

export interface SecretScanResponse {
  path: string;
  is_dir: boolean;
  file_count?: number;
}

/** GET /api/cli-tools — the `/tools` command's status payload. */
export interface CliToolsResponse {
  platform: string;
  package_manager: string;
  /** Present only when no supported package manager is on PATH. */
  manager_hint?: string;
  tools: CliToolStatus[];
}

export interface CliToolStatus {
  name: string;
  aliases?: string[];
  description: string;
  project?: string;
  found: boolean;
  /** Resolved binary name (an alias of `name`); absent when not found. */
  command?: string;
}

/** POST /api/cli-tools/install — the started background install job. */
export interface CliToolInstallStartResponse {
  job_id: string;
  tool: string;
  status: "running";
}

/** GET /api/cli-tools/install/{id} — one poll of an install job. */
export interface CliToolInstallStatusResponse {
  job_id: string;
  tool: string;
  status: "running" | "done" | "error";
  manager?: string;
  /** Captured package-manager output (tail). */
  output?: string;
  error?: string;
  no_manager?: boolean;
  /** Remediation shown when `no_manager` is true. */
  hint?: string;
}

export interface SecretTransformResponse {
  status: "done" | "started";
  job_id?: string;
  total?: number;
}

export interface SecretProgressEvent {
  job_id: string;
  done: number;
  total: number;
  current?: string;
}

export type SSEEventHandler = (
  event: string,
  data: unknown,
  sessionId?: string,
) => void;

// The legacy per-session SSE connectors (connectSessionMirror,
// connectAgentRunsSSE) were deleted in Part 04: every event type they carried
// now flows over the single /api/events stream consumed by `lib/eventBus`.

// ---- Embedded browser (see internal/browse) --------------------------------

let _browseBase: string | null = null;
let _browseHTRNotice = "";
let _browseRemoteMode = false;

/** Test-only: clear the cached browse base URL. */
export function __resetBrowseBaseCache(): void {
	_browseBase = null;
	_browseHTRNotice = "";
	_browseRemoteMode = false;
}

/** Fetches (once, then cached) the browse-origin base URL from the main
 *  server. The browse origin is a SEPARATE loopback listener so proxied pages
 *  are cross-origin to this SPA. */
export async function getBrowseBase(): Promise<string> {
  if (_browseBase) return _browseBase;
  const res = await authedFetch("/api/browse/config", { method: "GET" });
  if (!res.ok) throw new Error(`browse config: ${res.status}`);
	const body = (await res.json()) as { base_url: string; htr_notice?: string; remote_mode?: boolean };
	_browseBase = body.base_url;
	_browseHTRNotice = body.htr_notice ?? "";
	_browseRemoteMode = body.remote_mode ?? false;
	return _browseBase;
}

export function getBrowseHTRNotice(): string {
	return _browseHTRNotice;
}

/** True when the browse origin backs a remote-workspace server (`ocode serve
 *  --remote`): every host, not just private ones, routes through the
 *  reverse-proxy pipeline, so the panel should never select chrome/CDP mode. */
export function getBrowseRemoteMode(): boolean {
	return _browseRemoteMode;
}

/** Mints a one-time grant for a stateKey; the first iframe navigation carries
 *  it and the browse origin exchanges it for an HttpOnly cookie. */
export async function mintBrowseGrant(stateKey: string): Promise<string> {
  const res = await authedFetch("/api/browse/grant", {
    method: "POST",
    body: JSON.stringify({ state_key: stateKey }),
  });
  if (!res.ok) throw new Error(`browse grant: ${res.status}`);
  const body = (await res.json()) as { grant: string };
  return body.grant;
}

/** Best-effort revoke of a browse session (called on panel close). */
/** Answers a Chrome-mode file chooser: uploads the picked files so headless
 *  Chrome can attach them to the page's <input type=file>. 409 means the
 *  page no longer has a chooser waiting. */
export async function uploadBrowseFiles(stateKey: string, files: File[]): Promise<void> {
  const form = new FormData();
  form.append("state_key", stateKey);
  for (const f of files) form.append("files", f, f.name);
  // No Content-Type: the browser sets the multipart boundary itself.
  const res = await fetch(apiPath("/api/browse/upload"), {
    method: "POST",
    headers: authHeaders(),
    body: form,
  });
  if (!res.ok && res.status !== 204) {
    throw new Error(`browse upload: ${res.status}`);
  }
}

export async function revokeBrowseSession(stateKey: string): Promise<void> {
  const res = await authedFetch("/api/browse/revoke", {
    method: "POST",
    body: JSON.stringify({ state_key: stateKey }),
  });
  if (!res.ok && res.status !== 204) {
    throw new Error(`browse revoke: ${res.status}`);
  }
}

/** Explicit TLS bypass for a self-signed local host after the user clicks “Continue anyway”. */
export async function bypassBrowseTLS(stateKey: string, host: string): Promise<void> {
  const res = await authedFetch("/api/browse/bypass", {
    method: "POST",
    body: JSON.stringify({ state_key: stateKey, host }),
  });
  if (!res.ok && res.status !== 204) {
    throw new Error(`browse bypass: ${res.status}`);
  }
}

/** Builds the iframe src pointing at the browse origin's stateless route:
  *  {base}/b/{stateKey}/{scheme}/{host}/{path}?{query}[&__grant=...].
  *  Only http/https targets are supported. */
export function browseSrc(base: string, grant: string | null, stateKey: string, url: string): string {
  const u = new URL(normalizeBrowseURL(url));
  const scheme = u.protocol.replace(":", "");
  const host = u.host; // host:port
  const path = u.pathname === "/" ? "/" : u.pathname;
  let out = `${base}/b/${stateKey}/${scheme}/${host}${path}`;
  const params = u.search ? u.search.slice(1) : "";
  const parts: string[] = [];
  if (params) parts.push(params);
  if (grant) parts.push(`__grant=${encodeURIComponent(grant)}`);
  if (parts.length) out += `?${parts.join("&")}`;
  return out;
}
