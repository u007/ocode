import { fetchJSON } from "./client";

export interface Artifact {
  id: string;
  project_host?: string;
  project_path?: string;
  name?: string;
  created_at?: string;
  created_by?: string;
  status?: string;
  hosting_mode?: string;
}

export interface ArtifactVersion {
  id?: string;
  artifact_id?: string;
  version?: string;
  entrypoint?: string;
  blob_manifest_hash?: string;
  size_bytes?: number;
  created_at?: string;
  build_cmd?: string;
  build_log_path?: string;
  schema_version?: number;
}

export interface ArtifactFile {
  version_id?: string;
  path: string;
  blob_hash?: string;
  size?: number;
  mode?: string;
  mime?: string;
}

export interface ArtifactToken {
  id: string;
  artifact_id?: string;
  token_hash?: string;
  kind?: string;
  expires_at?: string;
  created_at?: string;
  revoked_at?: string;
  password_hash?: string;
  label?: string;
}

export interface ArtifactAllowEntry {
  artifact_id?: string;
  email: string;
  invited_at?: string;
  accepted_at?: string;
  invite_token_hash?: string;
}

export interface ArtifactAccessLogEntry {
  artifact_id?: string;
  token_id?: string;
  ip?: string;
  ua?: string;
  path?: string;
  status?: number;
  ts?: string;
}

export interface ArtifactListResponse {
  artifacts?: Artifact[];
  items?: Artifact[];
  data?: Artifact[];
  results?: Artifact[];
  total?: number;
  limit?: number;
  offset?: number;
}

export interface PublishArtifactRequest {
  notify_on_publish?: boolean;
  notifyOnPublish?: boolean;
  notify?: boolean;
  build_cmd?: string;
  buildCmd?: string;
  build?: string;
  entrypoint?: string;
  secret_scan_hit?: boolean;
  secretScanHit?: boolean;
  override_reason?: string;
  overrideReason?: string;
  reason?: string;
  project?: string;
  project_path?: string;
  project_host?: string;
  host?: string;
  name?: string;
  title?: string;
  hosting_mode?: string;
  mode?: string;
  hosting?: string;
  /** Visitor access: public, password, or allowlist (hub artifacts only). */
  access_mode?: string;
  accessMode?: string;
  /** Required when access_mode is "password" (minimum 8 characters). */
  password?: string;
  /** Required when access_mode is "allowlist"; the hub emails each invite. */
  invite_emails?: string[];
  inviteEmails?: string[];
  emails?: string[];
}

export interface PublishArtifactResponse {
  version?: ArtifactVersion;
  artifact?: Artifact;
  notify_sent?: boolean;
  email_sent?: boolean;
  /** Present when the artifact was published to the hub (hosting_mode=hub). */
  hub_url?: string;
  access_mode?: string;
  hub_invites?: { email?: string; inviteUrl?: string; emailSent?: boolean }[];
  invite_errors?: string[];
}

export interface CreateArtifactRequest {
  project?: string;
  project_path?: string;
  path?: string;
  source?: string;
  source_path?: string;
  project_host?: string;
  host?: string;
  name?: string;
  title?: string;
  hosting_mode?: string;
  mode?: string;
  hosting?: string;
}

function normalizeList(
  res: ArtifactListResponse | Artifact[] | any,
): Artifact[] {
  if (Array.isArray(res)) return res;
  if (!res) return [];
  return (res.artifacts ||
    res.items ||
    res.data ||
    res.results ||
    []) as Artifact[];
}

/**
 * Ensures a project has an artifact to publish into. The list endpoint is
 * project-scoped by the caller (host + projectPath), so the newest match is the
 * project's existing artifact; only when none exists does this create one.
 * `mode` is the hosting mode: "hub" (default) or "local".
 */
export async function ensureArtifact(
  projectPath: string,
  host?: string,
  mode: "hub" | "local" = "hub",
): Promise<Artifact> {
  const path = (projectPath || "").trim();
  if (!path) throw new Error("publish: no project path");
  const { artifacts } = await listArtifacts({ limit: 100 }, host, path);
  // Hosting mode is fixed on the artifact row, so a project can have one hub
  // artifact and one local artifact. Matching only on the path would make
  // `/publish local` reuse the hub artifact and hit the same hub failure again.
  const sameMode = (a: Artifact) =>
    mode === "hub"
      ? (a.hosting_mode ?? "").toLowerCase() === "hub"
      : (a.hosting_mode ?? "").toLowerCase() !== "hub";
  const matches = artifacts.filter(
    (a) => (a.project_path ?? "") === path && sameMode(a),
  );
  const match =
    matches.find((a) => !host || (a.project_host ?? "") === host) ?? matches[0];
  if (match) return match;
  return createArtifact({ project: path, hosting_mode: mode }, host, path);
}

export async function listArtifacts(
  opts?: { limit?: number; offset?: number },
  host?: string,
  projectPath?: string,
): Promise<{
  artifacts: Artifact[];
  total: number;
  limit: number;
  offset: number;
}> {
  const params = new URLSearchParams();
  if (opts?.limit) params.set("limit", String(opts.limit));
  if (opts?.offset) params.set("offset", String(opts.offset));
  const qs = params.toString();
  const res = await fetchJSON<ArtifactListResponse>(
    `/api/artifacts${qs ? `?${qs}` : ""}`,
    undefined,
    host,
    projectPath,
  );
  const artifacts = normalizeList(res);
  return {
    artifacts,
    total: res && typeof res.total === "number" ? res.total : artifacts.length,
    limit:
      res && typeof res.limit === "number"
        ? res.limit
        : (opts?.limit ?? artifacts.length),
    offset:
      res && typeof res.offset === "number" ? res.offset : (opts?.offset ?? 0),
  };
}

export async function createArtifact(
  req: CreateArtifactRequest = {},
  host?: string,
  projectPath?: string,
): Promise<Artifact> {
  const body: Record<string, any> = {};
  if (req.project) body.project = req.project;
  if (req.project_path) body.project_path = req.project_path;
  if (req.path) body.path = req.path;
  if (req.source) body.source = req.source;
  if (req.source_path) body.source_path = req.source_path;
  if (req.project_host) body.project_host = req.project_host;
  if (req.host) body.project_host = req.host;
  if (req.name) body.name = req.name;
  if (req.title) body.name = req.title;
  if (req.hosting_mode) body.hosting_mode = req.hosting_mode;
  if (req.mode) body.hosting_mode = req.mode;
  if (req.hosting) body.hosting_mode = req.hosting;
  // pass through projectPath
  if (projectPath && !body.project) body.project = projectPath;
  return fetchJSON<Artifact>(
    "/api/artifacts",
    { method: "POST", body: JSON.stringify(body) },
    host,
    projectPath,
  );
}

export async function getArtifact(
  id: string,
  host?: string,
  projectPath?: string,
): Promise<Artifact> {
  return fetchJSON<Artifact>(
    `/api/artifacts/${encodeURIComponent(id)}`,
    undefined,
    host,
    projectPath,
  );
}

export async function publishArtifact(
  id: string,
  req: PublishArtifactRequest = {},
  host?: string,
  projectPath?: string,
): Promise<PublishArtifactResponse> {
  const body: Record<string, any> = {};
  // notify
  if (typeof req.notify_on_publish === "boolean")
    body.notify_on_publish = req.notify_on_publish;
  else if (typeof req.notifyOnPublish === "boolean")
    body.notify_on_publish = req.notifyOnPublish;
  else if (typeof req.notify === "boolean") body.notify_on_publish = req.notify;
  else body.notify_on_publish = false;
  if (req.build_cmd) body.build_cmd = req.build_cmd;
  if (req.buildCmd) body.build_cmd = req.buildCmd;
  if (req.build) body.build_cmd = req.build;
  if (req.entrypoint) body.entrypoint = req.entrypoint;
  if (typeof req.secret_scan_hit === "boolean")
    body.secret_scan_hit = req.secret_scan_hit;
  if (typeof req.secretScanHit === "boolean")
    body.secret_scan_hit = req.secretScanHit;
  if (req.override_reason) body.override_reason = req.override_reason;
  if (req.overrideReason) body.override_reason = req.overrideReason;
  if (req.reason) body.override_reason = req.reason;
  if (req.project) body.project = req.project;
  if (req.project_path) body.project = req.project_path;
  if (req.project_host) body.project_host = req.project_host;
  if (req.host) body.project_host = req.host;
  if (req.name) body.name = req.name;
  if (req.title) body.name = req.title;
  if (req.hosting_mode) body.hosting_mode = req.hosting_mode;
  if (req.mode) body.hosting_mode = req.mode;
  if (req.hosting) body.hosting_mode = req.hosting;
  // Access fields are deliberately separate from hosting_mode: `mode` above is
  // an alias for hosting_mode, so it must never carry the access mode.
  const accessMode = req.access_mode ?? req.accessMode;
  if (accessMode) body.access_mode = accessMode;
  if (req.password) body.password = req.password;
  const inviteEmails = req.invite_emails ?? req.inviteEmails ?? req.emails;
  if (inviteEmails && inviteEmails.length > 0)
    body.invite_emails = inviteEmails;
  return fetchJSON<PublishArtifactResponse>(
    `/api/artifacts/${encodeURIComponent(id)}/publish`,
    { method: "POST", body: JSON.stringify(body) },
    host,
    projectPath,
  );
}

export async function unpublishArtifact(
  id: string,
  host?: string,
  projectPath?: string,
): Promise<any> {
  return fetchJSON<any>(
    `/api/artifacts/${encodeURIComponent(id)}/unpublish`,
    { method: "POST" },
    host,
    projectPath,
  );
}

export async function deleteArtifact(
  id: string,
  host?: string,
  projectPath?: string,
): Promise<void> {
  await fetchJSON<any>(
    `/api/artifacts/${encodeURIComponent(id)}`,
    { method: "DELETE" },
    host,
    projectPath,
  );
}

export async function mintArtifactToken(
  id: string,
  body?: any,
  host?: string,
  projectPath?: string,
): Promise<ArtifactToken> {
  return fetchJSON<ArtifactToken>(
    `/api/artifacts/${encodeURIComponent(id)}/tokens`,
    { method: "POST", body: JSON.stringify(body || {}) },
    host,
    projectPath,
  );
}

export async function revokeArtifactToken(
  id: string,
  tid: string,
  host?: string,
  projectPath?: string,
): Promise<void> {
  await fetchJSON<any>(
    `/api/artifacts/${encodeURIComponent(id)}/tokens/${encodeURIComponent(tid)}`,
    { method: "DELETE" },
    host,
    projectPath,
  );
}

export async function addArtifactAllowlist(
  id: string,
  email: string,
  host?: string,
  projectPath?: string,
): Promise<any> {
  return fetchJSON<any>(
    `/api/artifacts/${encodeURIComponent(id)}/allowlist`,
    { method: "POST", body: JSON.stringify({ email }) },
    host,
    projectPath,
  );
}

export async function removeArtifactAllowlist(
  id: string,
  email: string,
  host?: string,
  projectPath?: string,
): Promise<void> {
  await fetchJSON<any>(
    `/api/artifacts/${encodeURIComponent(id)}/allowlist/${encodeURIComponent(email)}`,
    { method: "DELETE" },
    host,
    projectPath,
  );
}

export async function getArtifactAccessLog(
  id: string,
  host?: string,
  projectPath?: string,
): Promise<ArtifactAccessLogEntry[]> {
  const res = await fetchJSON<any>(
    `/api/artifacts/${encodeURIComponent(id)}/access-log`,
    undefined,
    host,
    projectPath,
  );
  return (res &&
    (res.entries ||
      res.items ||
      res.data ||
      res.results ||
      res)) as ArtifactAccessLogEntry[];
}

// Convenience default object + named aliases for various import styles.
const artifactsApi = {
  listArtifacts,
  createArtifact,
  getArtifact,
  publishArtifact,
  unpublishArtifact,
  deleteArtifact,
  mintArtifactToken,
  revokeArtifactToken,
  addArtifactAllowlist,
  removeArtifactAllowlist,
  getArtifactAccessLog,
  list: listArtifacts,
  create: createArtifact,
  get: getArtifact,
  publish: publishArtifact,
  unpublish: unpublishArtifact,
  delete: deleteArtifact,
};

export default artifactsApi;
export { artifactsApi };
export const api = artifactsApi;
