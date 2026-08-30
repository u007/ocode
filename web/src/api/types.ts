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
  | { kind: "status"; text: string };

export interface ChatRequest {
  content: string;
  sessionId?: string;
  model?: string;
}

export interface ChatResponse {
  content: string;
  sessionId: string;
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
}

export interface AgentInfo {
  name: string;
  description: string;
  mode: string;
}

export type CronScheduleKind = "at" | "every" | "cron";

export type CronPermissionMode = "normal" | "yolo" | "locked";

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

export interface GitStatus {
  branch: string;
  staged_files: string[];
  changed_files: string[];
  has_changes: boolean;
}

export interface GitDiffFile {
  path: string;
  status: string;
  patch: string;
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
  // carried an expected_output contract. satisfied=false means the result
  // did not meet the contract after the single retry (or verification
  // failed) — a shape check, not "verified correct".
  contract?: { checked: boolean; satisfied: boolean; deficiency?: string };
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
  small_model?: string;
  small_model_enabled?: boolean;
  advisor_model?: string;
  advisor_enabled?: boolean;
  recap_model?: string;
  recap_model_enabled?: boolean;
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
  modified_files?: FileStatus[];
  lsp_servers?: LSPStatus[];
  extra_allowed_paths?: string[];
  updated_at?: string;
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
  rules: PermissionRule[];
  bash_rules: PermissionRule[];
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
