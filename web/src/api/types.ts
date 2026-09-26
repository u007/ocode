export interface ToolCall {
  id: string;
  type?: string;
  function: { name: string; arguments: string };
}

export interface Message {
  role: "user" | "assistant" | "system" | "tool";
  content: string;
  tool_calls?: ToolCall[];
  tool_call_id?: string;
  reasoning_content?: string;
  /** Durable per-session sequence stamped on user-role messages by the
   *  server, identical on the persisted copy and the `user_message` SSE echo.
   *  Dedup identity for the snapshot-before-SSE race; absent/0 on legacy
   *  messages, which are never deduplicated. */
  user_seq?: number;
}

export type PendingRewindStatus = "armed" | "committed" | "stale";

/** Client-safe durable rewind resource returned by prepare/status. */
export interface PendingRewind {
  token: string;
  session_id: string;
  status: PendingRewindStatus;
  expires_at: string;
  target_index: number;
  user_seq?: number;
  committed_user_seq: number;
}

export interface PreparePendingRewindRequest {
  targetIndex: number;
  targetContent: string;
  userSeq?: number;
}

// A part of the in-progress turn, streamed live before the authoritative
// snapshot lands at turn_done. Ordered as produced by the agent.
export type LivePart =
  | { kind: "thinking"; text: string }
  | { kind: "text"; text: string }
  | {
      kind: "tool";
      tool: string;
      /** The model's tool-call id, used to pair streamed output and the final
       *  result with this bubble. Absent on legacy events, which fall back to
       *  positional matching. */
      callId?: string;
      command?: string;
      /** Incremental output received while the tool is still running. Progress
       *  only — `output` carries the authoritative result. */
      stream?: string;
      output?: string;
    }
  | { kind: "status"; text: string }
  /** A transient, informational notice (e.g. "Discovered: …" / "Indexing: …"
   *  mirrored from the TUI's discovery notices). Not LLM output and not
   *  persisted — cleared with the rest of the live buffer at the turn
   *  boundary. */
  | { kind: "notice"; text: string };

export interface ChatRequest {
  content: string;
  sessionId?: string;
  model?: string;
  rewindToken?: string;
  async?: boolean;
}

export interface ChatResponse {
  content: string;
  sessionId: string;
  /** Backend-resolved model used for this accepted dispatch. */
  model: string;
}

export interface SessionInfo {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface SessionDetail extends SessionInfo {
  messages: Message[];
  total: number;
  /** Opaque stored-transcript token (see lib/sessionRevision). Moves whenever
   *  any process writes this session, so the revalidation poll knows to
   *  refetch. Absent for bridged/in-memory sessions. */
  revision?: string;
}

export interface SessionListResponse {
  sessions: SessionInfo[];
  total: number;
}

export interface ModelInfo {
  name: string;
  model: string;
  provider: string;
  active: boolean;
  /** Human-readable name from the models.dev registry (e.g. "Ox Alpha Free" for codename ids). Absent when unknown. */
  display_name?: string;
  /** Raw membership in the shared favorites list (TUI picker ctrl+f). A model
   *  can be both favorite and recent; section placement then favors "recent". */
  favorite?: boolean;
  /** Raw membership in the shared recently-used list. */
  recent?: boolean;
  /** True when a model-specific custom prompt ({model}.OCODE.md or the
   *  embedded fallback) is injectable for this model — the picker badges it,
   *  mirroring the TUI's "◆ Model prompt" row. */
  has_model_prompt?: boolean;
  /** True when at least one force-injected Kaizen tuning directive
   *  (digest-bearing conduct skill) is admitted for this model in the current
   *  project. */
  has_kaizen?: boolean;
}

export type TTSEngineId = "browser-native" | "piper" | "kokoro" | "fish-audio" | "breeze";
export type TTSPlaybackMode = "manual" | "at-bottom" | "auto";

export interface TTSEngine {
  id: TTSEngineId;
  label: string;
  availability: "ready" | "installable" | "unavailable";
  reason?: string;
  browser_only: boolean;
  voice_id?: string;
  voices?: string[];
  manifest_version?: string;
	license_name?: string;
	license_url?: string;
	license_text?: string;
	license_hash?: string;
}

export type TTSInstallStatus =
  | "not-accepted"
  | "license-accepted"
  | "pinned"
  | "downloading"
  | "installed"
  | "failed"
  | "enabled";

export interface TTSInstallState {
  engine_id: string;
  state: TTSInstallStatus;
  progress: number;
  step?: string;
  error?: string;
  manifest_version?: string;
  pinned: boolean;
}

export interface TTSConfig {
  engine: TTSEngineId;
  voice: string;
  mode: TTSPlaybackMode;
  model_voice?: Record<string, string>;
}

export type ChatVerbosityPreset = "full" | "balanced" | "quiet";
export type ChatDisplayOverride = "preset" | "expanded" | "collapsed";

export interface ChatVerbosityOverrides {
  older_thinking: ChatDisplayOverride;
  tool_calls: ChatDisplayOverride;
  tool_output: ChatDisplayOverride;
  activity_notices: ChatDisplayOverride;
}

export interface ChatVerbosityConfig {
  preset: ChatVerbosityPreset;
  overrides: ChatVerbosityOverrides;
}

export interface ChatDisplayPolicy {
  older_thinking: "expanded" | "collapsed";
  latest_thinking: "expanded";
  tool_calls: "expanded" | "collapsed";
  tool_output: "expanded" | "collapsed";
  notices: "expanded" | "collapsed";
  status: "expanded";
}

export type ChatVerbosityResponse = ChatVerbosityConfig;

export interface TTSPlayback {
  generation: number;
  engine: TTSEngineId;
  status: string;
  text?: string;
  error?: string;
  audio_id?: string;
}

export interface TTSStatus {
  config: TTSConfig;
  engine: TTSEngine;
  host: string;
  state: string;
  hardware?: string;
  error?: string;
  playback: TTSPlayback;
  selection_generation: number;
}

export interface AgentInfo {
  name: string;
  description: string;
  mode: string;
}

export type CronScheduleKind = "at" | "every" | "cron";

export type CronPermissionMode = "normal" | "yolo" | "locked" | "sandbox";

export interface CronSchedule {
  kind: CronScheduleKind;
  at_ms?: number;
  every_ms?: number;
  expr?: string;
  tz?: string;
}

export interface CronPayload {
  message: string;
  notes?: string;
  owner?: string;
  deliver_to?: string;
  perm_mode?: CronPermissionMode;
}

export interface CronJobState {
  next_run_at_ms?: number;
  last_run_at_ms?: number;
  last_status?: string;
  last_error?: string;
  runs?: number;
}

export interface CronJob {
  id: string;
  name: string;
  schedule: CronSchedule;
  payload: CronPayload;
  state: CronJobState;
  created_at_ms: number;
  enabled: boolean;
}

export interface CronDelivery {
  job_id: string;
  job_name: string;
  owner: string;
  delivered_to?: string;
  result: string;
  error?: string;
  at: string;
}

export interface CronRunLog {
  at: string;
  level?: string;
  message: string;
}

export interface CronRun {
  id: string;
  job_id: string;
  job_name: string;
  started_at: string;
  finished_at: string;
  duration_ms: number;
  status: string;
  input: string;
  output?: string;
  error?: string;
  logs: CronRunLog[];
}

export interface CronRunsResponse {
  runs: CronRun[];
  total: number;
}

export interface CronJobsResponse {
  jobs: CronJob[];
}

export interface CronOutboxResponse {
  entries: CronDelivery[];
}

export interface CronTargetsResponse {
  targets: Record<string, number>;
}

export interface CronJobWriteRequest {
  name?: string;
  message: string;
  notes?: string;
  owner?: string;
  deliver_to?: string;
  perm_mode?: CronPermissionMode;
  schedule: CronSchedule;
}

export interface CronJobPatchRequest {
  enabled?: boolean;
  name?: string;
  message?: string;
  notes?: string;
  owner?: string;
  deliver_to?: string;
  perm_mode?: CronPermissionMode;
  schedule?: CronSchedule;
}

export interface SSETextEvent {
  delta: string;
}

export interface SSEToolStartEvent {
  tool: string;
  command?: string;
  content?: string;
}

export interface SSEToolResultEvent {
  tool: string;
  output: string;
}

export interface OcrConfig {
  enabled: boolean;
  backend: "openai-compat" | "paddle" | "lmstudio";
  openai: { base_url: string; model: string };
  paddle: { endpoint: string; variant: string };
}

export interface ComputerUseConfig {
  enabled: boolean;
  status_lines: string[];
}

export interface ComputerUsePermissionReport {
  platform: string;
  granted: boolean;
  lines: string[];
}

// ── System Permissions (OS permission manager) ──
export type SystemPermissionStatus =
  | "granted"
  | "denied"
  | "not_determined"
  | "unknown"
  | "not_required";

export interface SystemPermissionEntry {
  id: string;
  label: string;
  detail?: string;
  kind: "category" | "path";
  platform: string;
  supported: boolean;
  status: SystemPermissionStatus;
  enabled: boolean;
  requested: boolean;
  path?: string;
  source: "builtin" | "discovered" | "custom";
}

export interface SystemPermissionResult {
  id: string;
  status: SystemPermissionStatus;
  message: string;
  opened_settings: boolean;
}

export interface SystemPermissionsResponse {
  platform: string;
  supported: boolean;
  entries: SystemPermissionEntry[];
  /** Present on PUT when the entry was enabled and requested. */
  result?: SystemPermissionResult;
  /** Present on the reconcile/request-all endpoint. */
  results?: SystemPermissionResult[];
}

export interface OcrModelsResponse {
  backends: { name: string; models: string[]; error?: string }[];
}

export interface SSEToolErrorEvent {
  tool: string;
  error: string;
}

export interface SSEPermissionEvent {
  tool: string;
  command?: string;
  /** Complete execution parameters, separate from the command summary. */
  args?: unknown;
  rule?: string;
  summary?: string;
  deny_reason?: string;
  model_unavailable?: string;
  request_id: string;
  /** "tool" | "bash_prefix" — drives always-allow button availability. */
  scope?: string;
  /** Bash prefix for bash_prefix-scope asks (e.g. "rm", "git push"). */
  prefix?: string;
  /** Out-of-workspace target path; "always" persists this root to extra_allowed_paths. */
  out_of_scope_path?: string;
}

/** Decisions accepted by POST /api/permissions/resolve (`decision` field). */
export type PermissionDecision =
  | "allow"
  | "deny"
  | "always_rule"
  | "always_tool";

export interface SSEDoneEvent {
  session_id: string;
  model: string;
}

// ── Agent question prompts (mirrors internal/tool QuestionPrompt) ──
export interface QuestionOption {
  label: string;
  description?: string;
}

export interface QuestionPrompt {
  header: string;
  question: string;
  options: QuestionOption[];
  multiple?: boolean;
}

// `question` SSE frame emitted when the agent pauses on a question prompt.
export interface SSEQuestionEvent {
  request_id: string;
  questions: QuestionPrompt[];
}

// One selected answer sent back to POST /api/questions. `custom` marks the
// free-text "Something else" option, whose typed value rides in `text`.
export interface QuestionAnswerValue {
  label: string;
  text?: string;
  custom?: boolean;
}

export interface QuestionAnswerPayload {
  header?: string;
  question: string;
  answers: QuestionAnswerValue[];
}

export interface SSESessionEvent {
  session_id: string;
}

export interface AgentRunMessage {
  role: string;
  content: string;
  toolCalls?: { name: string; arguments: string }[];
  toolCallId?: string;
  reasoningContent?: string;
}

export type FileChangeStatus = "added" | "modified" | "deleted";

export interface ChangeAuthor {
  agentId: string;
  agentName: string;
  changes: number;
}

export interface FileChange {
  originalPath: string;
  status: FileChangeStatus;
  firstBackupPath: string;
  undoable: boolean;
  undoAllTcId: string;
  changeCount: number;
  authors: ChangeAuthor[];
  createdAt: string;
  updatedAt: string;
  lastBashCommand: string;
  lastBashExitCode: number;
}

export interface ChangeDiff {
  path: string;
  patch: string;
}

/** One unmerged path: git could not merge it and the user must choose a side. */
export interface GitConflict {
  path: string;
  /** Git's two-character porcelain XY for an unmerged entry (UU, AA, UD, ...). */
  code: string;
  /** Whether the stage-2 (ours) index entry exists. False means that side is
   *  a deletion, which the resolver must handle with `git rm`, not checkout. */
  ours: boolean;
  /** Whether the stage-3 (theirs) index entry exists. See `ours`. */
  theirs: boolean;
}

/** A git operation that stopped before completing and is waiting for the user. */
export interface GitOperation {
  kind:
    | "merge"
    | "rebase"
    | "rebase-interactive"
    | "am"
    | "cherry-pick"
    | "revert"
    | "bisect";
  label: string;
  step: number;
  total: number;
}

/** Body of POST /api/git/conflict/resolve. */
export interface GitConflictResolveRequest {
  /** Repo-relative path of the conflicted file. The server re-validates it. */
  path: string;
  /**
   * "ours" / "theirs" keep that side wholesale; "mark" stages the file as the
   * user edited it and is REFUSED by the server while conflict markers remain.
   *
   * During a rebase these names are inverted relative to intuition: git's
   * "ours" is the upstream branch and "theirs" is the commit being replayed.
   * The UI relabels them accordingly — see GitConflictSideLabels.
   */
  resolution: "ours" | "theirs" | "mark";
}

/** Body of POST /api/git/operation. */
export interface GitOperationRequest {
  action: "continue" | "abort" | "skip" | "good" | "bad";
  /** The operation the panel believes is running. The server re-detects and
   *  answers 409 on a mismatch, so this is a guard, never an instruction. */
  kind: GitOperation["kind"];
}

/** Response of POST /api/git/operation. */
export interface GitOperationResult {
  workspace: GitWorkspace;
  /** Combined git output. Continue/skip run commit hooks, so their output is
   *  meaningful and is shown rather than dropped. */
  output?: string;
}

export interface GitStatus {
  branch: string;
  staged_files: string[];
  changed_files: string[];
  /** Unmerged paths. Deliberately NOT also listed in staged_files or
   *  changed_files — a conflicted path used to be counted three times across
   *  those two lists. Always present (never null). */
  conflicts: GitConflict[];
  /** Present only while an operation is in progress; absent when idle. */
  operation?: GitOperation;
  has_changes: boolean;
  /** True when the directory is inside a git repo — even a clean one. The web
   *  editor uses this to distinguish "no unstaged changes" (repo, show no
   *  decorations) from "not a repo" (fall back to session diffs). */
  is_repo: boolean;
  /** Ahead: local commits not yet pushed. Behind: remote commits not yet pulled. */
  ahead: number;
  behind: number;
  has_upstream: boolean;
}

export interface GitDiffFile {
  path: string;
  status: string;
  patch: string;
}

/** One commit in the Git tab's commit list. */
export interface GitCommit {
  hash: string;
  short: string;
  message: string;
  author: string;
  email: string;
  /** ISO-8601 timestamp from git (--date=iso-strict). */
  date: string;
}

/** Full SourceTree-style snapshot: status + staged diffs + unstaged diffs. */
export interface GitWorkspace {
  status: GitStatus;
  staged: GitDiffFile[];
  unstaged: GitDiffFile[];
}

export type GitHunkAction = "stage" | "unstage" | "discard";

export interface GitHunkRequest {
  path: string;
  hunk_index: number;
  action: GitHunkAction;
  staged: boolean;
}

/** One entry from `git stash list`. `index` is the position in the stash
 *  reflog (stash@{index}) and is how the API addresses an entry. */
export interface GitStash {
  index: number;
  ref: string;
  hash: string;
  short: string;
  /** Reflog subject: "WIP on main: <base subject>" or "On main: <message>". */
  message: string;
  author: string;
  /** ISO-8601 timestamp from git. */
  date: string;
}

export interface ThemeColors {
  user: string;
  assistant: string;
  header: string;
  border: string;
  hint: string;
  text: string;
  background: string;
  status_bg: string;
  status_fg: string;
  selected_fg: string;
  selected_bg: string;
  success: string;
  error: string;
  accent: string;
  dim: string;
  thinking: string;
}

export interface ThemeResponse {
  name: string;
  colors: ThemeColors;
}

export interface SyncLoginStartResponse {
  deviceCode: string;
  userCode: string;
  verifyUrl: string;
  expiresIn: number;
}

export interface SyncLoginPollResponse {
  status: "pending" | "approved" | "expired";
}

export interface SyncBlobStatus {
  version: number;
  syncedAt: string;
  synced: boolean;
}

export interface SyncStatusResponse {
  loggedIn: boolean;
  config: SyncBlobStatus;
  auth: SyncBlobStatus;
}

export interface AgentRun {
  id: string;
  name: string;
  status: "running" | "done" | "failed" | string;
  result?: string;
  err?: string;
  model?: string;
  startedAt: string;
  endedAt?: string;
  inputTokens: number;
  outputTokens: number;
  // Contract is the output-contract verdict, present only when the dispatch
  // carried an expected_output contract. satisfied=false means the result did
  // not meet the contract after the single retry. checkFailed distinguishes
  // the two reasons satisfied can be false: true means verification itself
  // failed (timeout / error / unparseable verdict) and the result was never
  // judged — NOT a contract failure. A shape check, not "verified correct".
  contract?: {
    checked: boolean;
    satisfied: boolean;
    checkFailed?: boolean;
    // timedOut is a subset of checkFailed: the check was abandoned because a
    // caller-configured deadline elapsed. Report it as a timeout, not a generic
    // verification failure.
    timedOut?: boolean;
    deficiency?: string;
  };
  messages: AgentRunMessage[];
  children: AgentRun[];
}

// TUIStatus is the consolidated snapshot pushed by the TUI whenever any
// tracked field changes (model toggle, IDE mode, file edit, agent rebuild,
// title gen, turn boundary, etc.). The web subscribes to the "status" SSE
// event and reflects every field in the status bar / drill-down panel.
export interface TUIStatus {
  main_model?: string;
  // Extended-thinking token budget for the main model (0 = off). Mirrors the
  // TUI's ctrl+d / /effort reasoning level so the sidebar can display and
  // change it.
  thinking_budget?: number;
  mode?: string;
  temperature?: number;
  permission_mode?: string;
  permission_auto_allow?: boolean;
  permission_model?: string;
  /** True when this OS has a real sandbox backend. Mirrors server/TUI. */
  permission_sandbox_supported?: boolean;
  /** Effective behavior: "confined" / "degraded_normal" / mode name. */
  permission_effective_behavior?: string;
  small_model?: string;
  small_model_enabled?: boolean;
  advisor_model?: string;
  advisor_enabled?: boolean;
  recap_model?: string;
  recap_model_enabled?: boolean;
  /** Explorer agent (explore/scout) model. Off or unset falls back to the
   *  small model, then the main model. */
  explorer_model?: string;
  explorer_model_enabled?: boolean;
  /** Context agent (context/doc-sync) model. Off or unset falls back to the
   *  small model, then the main model. */
  context_agent_model?: string;
  context_agent_model_enabled?: boolean;
  /** Auto-continue: runtime on/off gate + optional judge model (mirrors the
   *  TUI's autocont sidebar row). No judge model = StepLimitHit-only resumes. */
  auto_continue_model?: string;
  auto_continue_enabled?: boolean;
  ide_mode?: string;
  ide_status?: string;
  ocr_backend?: string;
  ocr_model?: string;
  ocr_enabled?: boolean;
  image_gen_enabled?: boolean;
  image_gen_provider?: string;
  image_gen_model?: string;
  subagent_model?: string;
  // Agent activity — mirrors the TUI's activity row so the status bar can show
  // what the agent is doing right now. All empty when idle.
  llm_running?: boolean;
  active_tools?: ToolActivityStatus[];
  active_agents?: string[];
  session_id?: string;
  session_title?: string;
  cwd?: string;
  context_current_tokens?: number;
  context_max_tokens?: number;
  context_model?: string;
  spending_usd?: number;
  input_tokens?: number;
  output_tokens?: number;
  cached_tokens?: number;
  total_tokens?: number;
  modified_files?: FileStatus[];
  lsp_servers?: LSPStatus[];
  extra_allowed_paths?: string[];
  // Model-prompt indicator (mirror of the TUI "◆ Model prompt" row): which
  // model-specific custom prompt is active plus the force-injected Kaizen
  // directives. Absent when the active model has neither.
  model_prompt?: ModelPromptInfo;
  // Turn timing — lets the web show elapsed for the current input and took for
  // the last turn.
  turn_started_at?: string;
  turn_elapsed_ms?: number;
  turn_ended_at?: string;
  turn_took_ms?: number;
  session_created_at?: string;
  updated_at?: string;
}

// Model-prompt indicator payload (TUIStatus.model_prompt). Kind is "file"
// (on-disk {model}.OCODE.md) or "embedded" (bundled fallback). Path is the
// absolute file path for "file", the embedded filename for "embedded". Tokens
// is the estimated prompt length (len/4, same as the TUI's estimateTok).
// Kaizen lists the force-injected tuning directives ("conduct-tuning-X → Y").
export interface ModelPromptInfo {
  kind?: string;
  path?: string;
  tokens?: number;
  kaizen?: KaizenDirective[];
}

export interface KaizenDirective {
  name: string;
  tuned_for: string;
  stack?: string;
}

export interface FileStatus {
  path: string;
  status?: string;
}

// One in-flight tool call, pushed in TUIStatus.active_tools. started_at is
// RFC3339 on the server clock; the status bar renders it as a local time.
export interface ToolActivityStatus {
  name: string;
  started_at?: string;
}

// Payload of the session-scoped `agent_activity` event: the agent-loop
// activity fields of TUIStatus and nothing else. Pushed by the headless
// server (web chat / desktop chat) on every ActivityTracker change so the
// status bar's `⟳ llm · ⚙ tool · @ agent` row tracks the agent loop live, the
// way it does when a TUI is attached. Merged into the session's tuiStatus —
// NOT a replacement, because a `status` event replaces the whole snapshot
// while this one only carries these three fields. Field names match
// TUIStatus on purpose so StatusBar reads one shape from either source.
export interface AgentActivityEvent {
  session_id: string;
  llm_running?: boolean;
  active_tools?: ToolActivityStatus[];
  active_agents?: string[];
}

export interface LSPStatus {
  cmd: string;
  lang_id?: string;
  root?: string;
  state: "running" | "starting" | "failed" | string;
  detail?: string;
  diagnostics_errors?: number;
  diagnostics_warnings?: number;
}

export interface MCPStatus {
  name: string;
  type: string;
  enabled: boolean;
}

export interface ThemesListResponse {
  current: string;
  themes: { name: string; label: string }[];
}

export interface Project {
  path: string;
  name: string;
  added_at: string;
  last_used_at: string;
  order: number;
  group: string;
  /** Optional remote host for ocode Remote SSH/WSL projects. Empty for local projects. */
  host?: string;
  remote_kind?: "ssh" | "wsl";
  remote_user?: string;
  remote_host?: string;
  remote_port?: number;
  remote_distro?: string;
}

/** One project's persisted open-session tabs (GET/PUT /api/tabs). */
export interface ServerProjectTabs {
  tabs: { id: string; title: string; sub_tab?: string }[];
  active: string;
}

export interface ProjectGroup {
  name: string;
  order: number;
  collapsed: boolean;
}

// ── Permissions ──
export interface PermissionRule {
  tool: string;
  level: string;
}

export interface PermissionsResponse {
  mode: string;
  auto_allow: boolean;
  /** Whether this OS has a real sandbox backend (darwin/linux). */
  sandbox_supported?: boolean;
  /** Effective sandbox behavior: "confined", "degraded_normal" (unsupported
   * OS), or the plain mode name. Lets the web surface the Windows degrade. */
  effective_behavior?: string;
  rules: PermissionRule[];
  bash_rules: PermissionRule[];
}

/** The persisted default permission mode new TUI/web/RC sessions start in —
 * distinct from the live mode in PermissionsResponse. */
export interface PermissionModeConfigResponse {
  mode: string;
}

// ── Memory status (GET /api/memory/status, backing /mem) ──
export interface MemoryScopeInfo {
  path: string;
  present: boolean;
  preview: string;
}

export interface MemoryStatusResponse {
  enabled: boolean;
  scopes: {
    user: MemoryScopeInfo;
    project: MemoryScopeInfo;
    global: MemoryScopeInfo;
  };
}

// ── Usage summary (GET /api/usage) ──
export interface UsageModelSummary {
  model: string;
  request_count: number;
  prompt_tokens: number;
  completion_tokens: number;
  cache_read_tokens: number;
  total_tokens: number;
  spend: number;
}

export interface UsageSummary {
  total_requests: number;
  total_prompt_tokens: number;
  total_completion_tokens: number;
  total_cache_read_tokens: number;
  total_tokens: number;
  total_spend: number;
  by_model: UsageModelSummary[];
  start_time: string;
  end_time: string;
  days: number;
}

// ── Plugins ──
export interface PluginInfo {
  name: string;
  source: string;
  dir: string;
  enabled: boolean;
  description?: string;
}

// ── Dynamic commands / skills (GET /api/commands, /api/skills) ──
export interface CommandEntry {
  name: string;
  description?: string;
}

export interface SkillEntry {
  name: string;
  description?: string;
  status?: string;
  source?: string;
}

export interface DirectoryEntry {
  name: string;
  path: string;
}

export interface BrowseResponse {
  current_path: string;
  parent_path: string;
  directories: DirectoryEntry[];
}

/** One user-added SSH port forward, from GET/POST /api/portmaps* (a remote SSH
 *  project — see internal/server/handler_portmaps.go) or
 *  /api/desktop/portmaps* (the desktop remote-workspace's single tunnel — see
 *  internal/desktop/portmaps.go). live is the current session's forward
 *  process state; enabled is the persisted intent. */
export interface PortMapView {
  remote_port: number;
  local_port: number;
  enabled: boolean;
  live: boolean;
}

/** The remote SSH project a port-forwards call is scoped to. Omit it to talk to
 *  the desktop remote-workspace's single-tunnel /api/desktop/portmaps family. */
export interface PortMapTarget {
  /** Canonical "[user@]host" (optionally ":port") the server stores on the
   *  project entry — the same string every other ?host= endpoint takes. */
  host: string;
  /** The remote project path, matched verbatim (remote separators). */
  path: string;
}

/** One row of the /context token-budget report (internal/contextbudget). Value
 *  is the pre-formatted right-hand side; lines are indented detail — a verbatim
 *  multi-line dump when raw is set. */
export interface ContextBudgetRow {
  label: string;
  value?: string;
  lines?: string[];
  raw?: boolean;
  subhead?: boolean;
}

export interface ContextBudgetSection {
  title: string;
  note?: string;
  rows: ContextBudgetRow[];
}

/** The full context-window breakdown, identical to the TUI's local `/context`.
 *  Returned by GET /api/sessions/:id/context as `report` when a live agent was
 *  available and not mid-turn. */
export interface ContextBudgetReport {
  model: string;
  sections: ContextBudgetSection[];
  notes?: string[];
}

/** Remote host server state, from the local server's lifecycle endpoints
 *  (GET/POST /api/remote/{host}/status|connect|restart). `outdated` marks a
 *  reused remote server whose version differs from the local build. */
export interface RemoteHostStatus {
  host: string;
  connected: boolean;
  version: string;
  local_version: string;
  outdated: boolean;
  pid: number;
}

/** One live terminal session on a remote host, from GET /api/terminal. */
export interface RemoteTerminalEntry {
  id: string;
  title: string;
  pid: number;
  started_at: string;
  attached: boolean;
}

// ── Password vault (Phase 1: server-side store + Settings UI) ──────────────

/** Non-secret projection of a vault item (list + URL-match responses). */
export interface VaultItemMeta {
  id: string;
  site: string;
  url: string;
  title: string;
  username: string;
}

/** A full vault item, including its secret fields (reveal/create/update). */
export interface VaultItem extends VaultItemMeta {
  password: string;
  notes: string;
  created: string;
  updated: string;
}

/** GET /api/vault/status. `unlocked` is true only when the requesting surface
 *  holds an unlock grant, not merely when the process has a data key. */
export interface VaultStatus {
  exists: boolean;
  unlocked: boolean;
}

/** POST /api/vault/generate options. Omitted fields default to false/20. */
export interface VaultGenOptions {
  length?: number;
  upper?: boolean;
  digits?: boolean;
  symbols?: boolean;
}
