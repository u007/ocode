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
  | { kind: "tool"; tool: string; command?: string; output?: string };

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

export interface ModelInfo {
  name: string;
  model: string;
  provider: string;
  active: boolean;
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
  request_id: string;
}

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
  messages: AgentRunMessage[];
  children: AgentRun[];
}

// TUIStatus is the consolidated snapshot pushed by the TUI whenever any
// tracked field changes (model toggle, IDE mode, file edit, agent rebuild,
// title gen, turn boundary, etc.). The web subscribes to the "status" SSE
// event and reflects every field in the status bar / drill-down panel.
export interface TUIStatus {
  main_model?: string;
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
