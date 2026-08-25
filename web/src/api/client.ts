import type {
  ChatResponse,
  SessionInfo,
  SessionDetail,
  SessionListResponse,
  ModelInfo,
  AgentInfo,
  AgentRun,
  GitDiffFile,
  ThemeResponse,
  TUIStatus,
  LSPStatus,
  MCPStatus,
  ThemesListResponse,
  FileStatus,
  Project,
  ProjectGroup,
  BrowseResponse,
  PermissionsResponse,
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
} from "./types";

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

// Base path for API calls. When the SPA is served under a tailscale --set-path
// prefix (e.g. /<sessionID>), API calls must include the prefix or the tailscale
// proxy routes them to whichever session owns the root path. The /rc command
// embeds the same prefix in the opened URL, so derive BASE from the current
// location: everything before the trailing "/session/<id>" is the prefix.
// The same value is passed to <BrowserRouter basename=...> in main.tsx so client
// navigation stays in sync.
export const _basePath = (() => {
  const m = window.location.pathname.match(/^(.*?)\/session\/[^/]+$/);
  return m && m[1] ? m[1] : "";
})();
const BASE = _basePath;

// Auth token embedded in URL by /rc command (?token=...). Stored at load time
// so navigation or hash changes don't lose it.
const _token = new URLSearchParams(window.location.search).get("token") ?? "";

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
  return fetch(apiPath(path), { ...init, headers });
}

/** Prepends the current SPA base path to an API or SSE path. */
export function apiPath(path: string): string {
  return `${BASE}${path}`;
}

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  if (!headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  for (const [k, v] of Object.entries(authHeaders())) headers.set(k, v);
  const res = await fetch(apiPath(path), { ...init, headers });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.message || err.error || res.statusText);
  }
  return res.json();
}

async function fetchEmpty(path: string, init?: RequestInit): Promise<void> {
  const headers = new Headers(init?.headers);
  if (!headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  for (const [k, v] of Object.entries(authHeaders())) headers.set(k, v);
  const res = await fetch(apiPath(path), { ...init, headers });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || res.statusText);
  }
}

export const api = {
  listSessions: (opts?: { limit?: number; offset?: number }) => {
    const params = new URLSearchParams();
    if (opts?.limit) params.set("limit", String(opts.limit));
    if (opts?.offset) params.set("offset", String(opts.offset));
    const qs = params.toString();
    return fetchJSON<SessionListResponse>(
      `/api/sessions${qs ? `?${qs}` : ""}`,
    );
  },
  getSession: (id: string, opts?: { limit?: number; offset?: number }) => {
    const params = new URLSearchParams();
    if (opts?.limit) params.set("limit", String(opts.limit));
    if (opts?.offset) params.set("offset", String(opts.offset));
    const qs = params.toString();
    return fetchJSON<SessionDetail>(
      `/api/sessions/${id}${qs ? `?${qs}` : ""}`,
    );
  },
  listModels: () => fetchJSON<ModelInfo[]>("/api/models"),
  listAgents: () => fetchJSON<AgentInfo[]>("/api/config/agents"),
  listAgentRuns: (session?: string) =>
    fetchJSON<AgentRun[]>(
      `/api/agents/runs${session ? `?session=${encodeURIComponent(session)}` : ""}`,
    ),
  getConfigModel: () =>
    fetchJSON<{ model: string; context_max_tokens?: number }>("/api/config/model"),
  setConfigModel: (model: string) =>
    fetchJSON<{ model: string }>("/api/config/model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }),
  // Extended-thinking (reasoning effort) budget for the main model. budget 0 =
  // off; levels list the canonical off/low/med/high/xhigh/max options shared
  // with the TUI's /effort command.
  getThinkingBudget: () =>
    fetchJSON<{ budget: number; level: string; levels: { level: string; budget: number }[] }>(
      "/api/config/thinking-budget",
    ),
  setThinkingBudget: (level: string) =>
    fetchJSON<{ budget: number; level: string; levels: { level: string; budget: number }[] }>(
      "/api/config/thinking-budget",
      { method: "PUT", body: JSON.stringify({ level }) },
    ),
  getSmallModel: () =>
    fetchJSON<{ model: string; priority: string }>("/api/config/small-model"),
  setSmallModel: (model: string) =>
    fetchJSON<{ model: string; source: string }>("/api/config/small-model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }),
  // Flip the runtime small-model on/off gate (persisted, mirrors the TUI's
  // small-model sidebar toggle).
  setSmallModelEnabled: (enabled: boolean) =>
    fetchJSON<{ model: string; enabled: boolean; source: string }>("/api/config/small-model", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }),

  getPermissionModel: () =>
    fetchJSON<{ model: string; enabled: boolean }>("/api/config/permission-model"),
  setPermissionModel: (model: string) =>
    fetchJSON<{ model: string; enabled: boolean }>("/api/config/permission-model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }),
  setPermissionModelEnabled: (enabled: boolean) =>
    fetchJSON<{ model: string; enabled: boolean }>("/api/config/permission-model", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }),

  // --- New/extended OcodeConfig endpoints (Plan 1: configuration-api-backend) ---

  getRecapConfig: () =>
    fetchJSON<{ recap_model: string; recap_model_enabled: boolean; recap_timeout_seconds: number }>(
      "/api/config/ocode/recap",
    ),
  setRecapConfig: (recap_model: string, recap_model_enabled: boolean, recap_timeout_seconds: number) =>
    fetchJSON<{ recap_model: string; recap_model_enabled: boolean; recap_timeout_seconds: number }>(
      "/api/config/ocode/recap",
      { method: "PUT", body: JSON.stringify({ recap_model, recap_model_enabled, recap_timeout_seconds }) },
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

  getAdvisorFull: () =>
    fetchJSON<{ model: string; provider: string; claude_code: boolean; checkpoints: string[] }>(
      "/api/config/advisor",
    ),
  setAdvisorFull: (fields: Partial<{ model: string; provider: string; claude_code: boolean; checkpoints: string[] }>) =>
    fetchJSON<{ model: string; provider: string; claude_code: boolean; checkpoints: string[] }>(
      "/api/config/advisor",
      { method: "PUT", body: JSON.stringify(fields) },
    ),

  getAutoPermissionConfig: () => fetchJSON<AutoPermissionConfig>("/api/config/ocode/permissions-auto"),
  setAutoPermissionConfig: (cfg: AutoPermissionConfig) =>
    fetchJSON<AutoPermissionConfig>("/api/config/ocode/permissions-auto", {
      method: "PUT",
      body: JSON.stringify(cfg),
    }),

  getMaskAdvanced: () =>
    fetchJSON<{
      enabled: boolean; mode: string; model: string; base_url: string; fail_mode: string;
      allow_remote_tier2: boolean; custom_words: string[];
    }>("/api/config/mask"),
  setMaskAdvanced: (fields: { base_url: string; fail_mode: string; allow_remote_tier2: boolean; custom_words: string[] }) =>
    fetchJSON<typeof fields>("/api/config/mask/advanced", { method: "PUT", body: JSON.stringify(fields) }),

  getDiscoveryConfig: () => fetchJSON<DiscoveryConfig>("/api/config/ocode/discovery"),
  setDiscoveryConfig: (cfg: DiscoveryConfig) =>
    fetchJSON<DiscoveryConfig>("/api/config/ocode/discovery", { method: "PUT", body: JSON.stringify(cfg) }),

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
    fetchJSON<{ extra_allowed_paths: string[]; upload_dir: string }>("/api/config/ocode/paths"),
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

  getLocalModelsConfig: () =>
    fetchJSON<Record<string, { enabled: boolean; max_parallel: number }>>("/api/config/ocode/local-models"),
  setLocalModelsConfig: (models: Record<string, { enabled: boolean; max_parallel: number }>) =>
    fetchJSON<Record<string, { enabled: boolean; max_parallel: number }>>("/api/config/ocode/local-models", {
      method: "PUT",
      body: JSON.stringify(models),
    }),
  getGitDiff: (path?: string, project?: string) => {
    const params = new URLSearchParams();
    if (path) params.set("path", path);
    if (project) params.set("project", project);
    const query = params.toString();
    return fetchJSON<GitDiffFile[]>(`/api/git/diff${query ? `?${query}` : ""}`);
  },
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
  getMCP: () => fetchJSON<MCPStatus[]>("/api/mcp"),
  getAdvisor: () =>
    fetchJSON<{ model: string }>("/api/config/advisor"),
  setAdvisor: (model: string) =>
    fetchJSON<{ model: string }>("/api/config/advisor", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }),
  // Runtime advisor on/off toggle — session-lifetime only, never persisted to config.
  getAdvisorEnabled: () =>
    fetchJSON<{ enabled: boolean }>("/api/config/advisor-enabled"),
  setAdvisorEnabled: (enabled: boolean) =>
    fetchJSON<{ enabled: boolean }>("/api/config/advisor-enabled", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }),
  // Interactive pty terminal configuration for the server's single workdir.
  // The terminal itself is always enabled; these are availability/scrollback.
  getTerminalConfig: () =>
    fetchJSON<{
      available?: boolean;
      scrollback_lines: number;
      font_family: string;
      font_size: number;
      shell: string;
      default_shell: string;
      available_shells: string[];
      work_dir: string;
    }>("/api/config/terminal"),
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
  // TUI status (consolidated snapshot pushed by the TUI on every state
  // change). The web also subscribes to the "status" SSE event so the bar
  // updates live without polling.
  getTUIStatus: () => fetchJSON<TUIStatus>("/api/tui-status"),
  getSpending: () =>
    fetchJSON<{ spending_usd: number; records: number }>("/api/spending"),
  getLSPStatuses: () =>
    fetchJSON<{ lsp_servers: LSPStatus[] }>("/api/lsp/statuses"),
  getModifiedFiles: () =>
    fetchJSON<{ modified_files: FileStatus[] }>("/api/files/modified"),
  getSessionContext: (id: string) =>
    fetchJSON<{
      session_id: string;
      message_count: number;
      estimated_tokens: number;
      max_tokens?: number;
      model?: string;
    }>(`/api/sessions/${id}/context`),
  // Reconcile endpoint (Parts 03–05): authoritative turn state + the bus seq
  // watermark. Reconcile = state fetch + transcript refetch, never event
  // replay. The watchdog and the reconnect path use this to clear a stuck
  // streaming spinner.
  getSessionState: (id: string) =>
    fetchJSON<{ bootstrap_stage: string; turn_active: boolean; last_seq: number }>(
      `/api/sessions/${id}/state`,
    ),
  // Per-session status snapshot (Part 03): superset of /api/tui-status with
  // session_id populated and context_* included, so each tab renders its own
  // status without a TUI bridge.
  getSessionStatus: (id: string) =>
    fetchJSON<TUIStatus>(`/api/sessions/${id}/status`),
  getSmallModelWithEnabled: () =>
    fetchJSON<{ model: string; enabled: boolean; priority: string }>(
      "/api/config/small-model",
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
  sendMessage: (sessionId: string, content: string) => {
    let windowId = ""
    try {
      windowId = new URLSearchParams(window.location.search).get("windowId")?.trim() || ""
      if (!windowId) {
        windowId = sessionStorage.getItem("ocode.windowId") || ""
      }
      if (!windowId) {
        windowId = `win-${crypto.randomUUID().slice(0, 8)}`
        sessionStorage.setItem("ocode.windowId", windowId)
      }
    } catch {}
    return fetchJSON<ChatResponse>(`/api/sessions/${sessionId}/message`, {
      method: "POST",
      headers: windowId ? { "X-Window-Id": windowId } : undefined,
      body: JSON.stringify({ content, windowId, async: true }),
    })
  },
  chat: (content: string, sessionId?: string, model?: string, requestId?: string, projectPath?: string) => {
    let windowId = ""
    try {
      windowId = new URLSearchParams(window.location.search).get("windowId")?.trim() || ""
      if (!windowId) {
        windowId = sessionStorage.getItem("ocode.windowId") || ""
      }
      if (!windowId) {
        windowId = `win-${crypto.randomUUID().slice(0, 8)}`
        sessionStorage.setItem("ocode.windowId", windowId)
      }
    } catch {}
    return fetchJSON<ChatResponse>("/api/chat", {
      method: "POST",
      headers: windowId ? { "X-Window-Id": windowId } : undefined,
      body: JSON.stringify({
        content,
        sessionId,
        model,
        request_id: requestId,
        project_path: projectPath,
        windowId,
        async: true,
      }),
    })
  },
  openFile: (path: string, line?: number) =>
    fetchJSON<{ path: string; status: string }>("/api/files/open", {
      method: "POST",
      body: JSON.stringify({ path, line }),
    }),
  shellCommand: (command: string, workDir?: string) =>
    fetchJSON<{ output: string; exitCode: number; error: string }>("/api/shell", {
      method: "POST",
      body: JSON.stringify({ command, workDir }),
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
  removeProject: (path: string) =>
    fetchJSON<{ status: string }>("/api/projects/" + encodeURIComponent(path), {
      method: "DELETE",
    }),
  listProjectSessions: (path: string) =>
    fetchJSON<SessionInfo[]>("/api/projects/sessions?path=" + encodeURIComponent(path)),
  renameProject: (path: string, name: string) =>
    fetchJSON<{ status: string }>("/api/projects/rename", {
      method: "POST",
      body: JSON.stringify({ path, name }),
    }),
  reorderProjects: (paths: string[]) =>
    fetchJSON<{ status: string }>("/api/projects/reorder", {
      method: "POST",
      body: JSON.stringify({ paths }),
    }),
  setProjectGroup: (path: string, group: string) =>
    fetchJSON<{ status: string }>("/api/projects/group", {
      method: "POST",
      body: JSON.stringify({ path, group }),
    }),
  listGroups: () => fetchJSON<ProjectGroup[]>("/api/projects/groups"),
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
  compactSession: (id: string) =>
    fetchJSON<{ original_len: number; compacted_len: number }>(
      `/api/sessions/${encodeURIComponent(id)}/compact`, { method: "POST" },
    ),
  recapSession: (id: string) =>
    fetchJSON<{ recap: string }>(
      `/api/sessions/${encodeURIComponent(id)}/recap`,
    ),
  shareSession: (id: string) =>
    fetchJSON<{ markdown: string }>(
      `/api/sessions/${encodeURIComponent(id)}/share`,
    ),
  btwSession: (id: string, content: string) =>
    fetchJSON<{ status: string }>(
      `/api/sessions/${encodeURIComponent(id)}/btw`, {
        method: "POST",
        body: JSON.stringify({ content }),
      },
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
  undoFileChange: (session?: string) =>
    fetchJSON<{ path: string; action: string }>(
      `/api/files/undo${session ? `?session=${encodeURIComponent(session)}` : ""}`,
      { method: "POST" },
    ),
  redoFileChange: (session?: string) =>
    fetchJSON<{ path: string; action: string }>(
      `/api/files/redo${session ? `?session=${encodeURIComponent(session)}` : ""}`,
      { method: "POST" },
    ),

  // ── File content save (PUT) ──
  saveFileContent: (path: string, content: string, projectRoot?: string) =>
    fetchJSON<{ path: string; saved: boolean }>("/api/files/content", {
      method: "PUT",
      body: JSON.stringify({ path, content, project_root: projectRoot }),
    }),

  // ── Session title / export ──
  setSessionTitle: (id: string, title: string) =>
    fetchJSON<{ title: string }>(
      `/api/sessions/${encodeURIComponent(id)}/title`,
      { method: "PUT", body: JSON.stringify({ title }) },
    ),
  generateSessionTitle: (id: string) =>
    fetchJSON<{ title: string }>(
      `/api/sessions/${encodeURIComponent(id)}/title/generate`,
      { method: "POST" },
    ),
  // The server returns raw markdown (text/markdown), not JSON, so this uses a
  // raw fetch and reads the body as text.
  exportSessionMarkdown: async (id: string): Promise<string> => {
    const res = await fetch(
      apiPath(`/api/sessions/${encodeURIComponent(id)}/export`),
      { headers: authHeaders() },
    );
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || res.statusText);
    }
    return res.text();
  },
  exportClaudeSession: (id: string) =>
    fetchJSON<{ path: string }>(
      `/api/sessions/${encodeURIComponent(id)}/export-claude`,
    ),

  // ── Usage ──
  getUsage: (range?: string) =>
    fetchJSON<UsageSummary>(
      `/api/usage${range ? `?range=${encodeURIComponent(range)}` : ""}`,
    ),

  // ── Init ──
  initProject: () =>
    fetchJSON<{ path: string; status: string }>("/api/init", {
      method: "POST",
    }),

  // ── Permissions ──
  getPermissions: () => fetchJSON<PermissionsResponse>("/api/permissions"),
  getYolo: () => fetchJSON<{ yolo: boolean }>("/api/permissions/yolo"),
  setYolo: (enabled: boolean) =>
    fetchJSON<{ yolo: boolean }>("/api/permissions/yolo", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }),

  // ── Agent selection ──
  setAgent: (name: string, sessionId?: string) =>
    fetchJSON<{ name: string; description: string }>("/api/config/agent", {
      method: "PUT",
      body: JSON.stringify({ name, session_id: sessionId }),
    }),

  // ── MCP enable/disable ──
  setMCPEnabled: (name: string, enabled: boolean) =>
    fetchJSON<{ name: string; status: string }>(
      `/api/mcp/${encodeURIComponent(name)}/${enabled ? "enable" : "disable"}`,
      { method: "PUT" },
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
  getCommandContext: (name: string, args?: string) =>
    fetchJSON<{ prompt: string }>(
      `/api/command-context/${encodeURIComponent(name)}${args ? `?args=${encodeURIComponent(args)}` : ""}`,
    ),

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
  getAutoContinue: () =>
    fetchJSON<{ enabled: boolean; model: string }>("/api/config/ocode/autocontinue"),
  setAutoContinue: (fields: { enabled?: boolean; model?: string; clear?: boolean }) =>
    fetchJSON<{ enabled: boolean; model: string }>("/api/config/ocode/autocontinue", {
      method: "PUT",
      body: JSON.stringify(fields),
    }),
  connectProvider: (provider: string, api_key: string) =>
    fetchJSON<{ provider: string; key: string }>("/api/auth/connect", {
      method: "POST",
      body: JSON.stringify({ provider, api_key }),
    }),
  getDocsStatus: (project?: string) => {
    const query = project ? `?project=${encodeURIComponent(project)}` : "";
    return fetchJSON<{ enabled: boolean; text: string }>(`/api/docs/status${query}`);
  },
  docsInit: (project?: string) => {
    const query = project ? `?project=${encodeURIComponent(project)}` : "";
    return fetchJSON<{ result: string; annotate_prompt?: string }>(`/api/docs/init${query}`, {
      method: "POST",
    });
  },
  docsUpdate: (sessionId: string, focus: string, project?: string) => {
    const params = new URLSearchParams();
    if (project) params.set("project", project);
    const query = params.toString();
    return fetchJSON<{ result: string }>(`/api/docs/update${query ? `?${query}` : ""}`, {
      method: "POST",
      body: JSON.stringify({ session_id: sessionId, focus }),
    });
  },
  docsCleanup: (confirm: boolean, project?: string) => {
    const params = new URLSearchParams();
    if (project) params.set("project", project);
    const query = params.toString();
    return fetchJSON<{ result: string }>(`/api/docs/cleanup${query ? `?${query}` : ""}`, {
      method: "POST",
      body: JSON.stringify({ confirm }),
    });
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
  ) =>
    fetchJSON<ChatResponse>("/api/questions", {
      method: "POST",
      body: JSON.stringify({
        request_id: requestId,
        session_id: sessionId ?? undefined,
        answers,
      }),
    }),

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
  ) =>
    fetchJSON<ChatResponse>("/api/permissions/resolve", {
      method: "POST",
      body: JSON.stringify({
        request_id: requestId,
        session_id: sessionId ?? undefined,
        decision,
      }),
    }),
  // ── Changes tab (session file changes) ──
  listChanges: (session?: string) =>
    fetchJSON<FileChange[]>(
      `/api/changes${session ? `?session=${encodeURIComponent(session)}` : ""}`,
    ),
  getChangeDiff: (session: string | undefined, path: string) =>
    fetchJSON<ChangeDiff>(
      `/api/changes/diff?${session ? `session=${encodeURIComponent(session)}&` : ""}path=${encodeURIComponent(path)}`,
    ),
  undoChangeFile: (session: string | undefined, path: string) =>
    fetchJSON<Record<string, never>>(
      `/api/changes/undo-file${session ? `?session=${encodeURIComponent(session)}` : ""}`,
      { method: "POST", body: JSON.stringify({ path }) },
    ),
  undoChangeBlock: (session: string | undefined, path: string) =>
    fetchJSON<Record<string, never>>(
      `/api/changes/undo-block${session ? `?session=${encodeURIComponent(session)}` : ""}`,
      { method: "POST", body: JSON.stringify({ path }) },
    ),
};

export type SSEEventHandler = (
  event: string,
  data: unknown,
  sessionId?: string,
) => void;

// The legacy per-session SSE connectors (connectSessionMirror,
// connectAgentRunsSSE) were deleted in Part 04: every event type they carried
// now flows over the single /api/events stream consumed by `lib/eventBus`.
