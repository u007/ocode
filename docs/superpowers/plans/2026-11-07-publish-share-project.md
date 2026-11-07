# Publish / Share Project (Phase 1: Static Snapshots) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (default) or superpowers:subagent-driven-development (long plans of independent tasks) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Phase 1 of the publish/share project feature: immutable static snapshots served at `/p/{id}/{token}/...` with per-artifact capability tokens, access levels, allow-list invitations, notify-on-publish opt-in, local store + blob store, and web UI. (Hub-hosted Mode B protocol is designed in; hub-side endpoints are a separate deliverable.)

**Architecture:** New `internal/artifacts` store (SQLite + content-addressed blobs) under the project slug; new durable per-artifact token store + `artifactAuthMiddleware` in `internal/server`; owner CRUD/publish/serve/invite handlers; routes registered in `Server.registerRoutes()`; web API client + publish dialog + list. Hub upload reuses the same protocol (deferred to hub-side work).

**Tech Stack:** Go 1.26, SQLite (existing `internal/session` pattern), crypto/rand, Argon2id (golang.org/x/crypto), existing Tailscale/share-token/media-token precedent, React SPA.

**Spec:** `docs/superpowers/specs/2026-11-07-publish-share-project-design.md` (worktree `publish-share`, branch `publish-share`)

**Detail:** decisions, not code — the default; the implementer writes the implementation.

## Global Constraints

- Work in the `.worktrees/publish-share` worktree (branch `publish-share`), not `main`.
- No fallbacks, no upsert-on-conflict; stale revisions / missing rows are errors.
- New env vars go in `.env.example` (`OCODE_ARTIFACT_MAX_BYTES`, `OCODE_ARTIFACT_INVITE_TTL_DAYS`, `OCODE_ARTIFACT_LOG_RETENTION_DAYS`, `OCODE_ARTIFACT_ORIGIN`, `OCODE_ARTIFACT_HUB_URL`).
- Deferred items (Phase 2/3) are already in `TODO.md`; do not re-add.
- Listing endpoints must be paginated and sorted.
- Git subprocesses use `gitexec.Env()` (optional locks off).
- `Handler.mu` is a map lock, never a work lock: build/publish run outside it.
- Long-running goroutines go through `crashguard.Go`.
- Bundle docs (`docs/concepts/*`) are written via `okf:context-writer`, not direct edits. This plan's spec lives in `docs/superpowers/specs/` (outside the bundle).

## Review Focus

- Unchecked/omitted `notify_on_publish` must never send a notification email (narrow share default).
- Private artifacts must return 404 (no existence oracle) to unauthorized visitors.
- Artifact serving must read only from the blob store, never the live working tree or `allowedProjectRoots`.
- Path-containment: no `..`, no symlink escape, no directory listing.
- Tokens hashed at rest; revoked/expired tokens authenticate nothing.
- Anonymous visitor traffic must not poison the owner's API rate limiter.
- Remote projects publish via host; no local fallback on hub failure.

---

### Task 1: `internal/artifacts` store (types, SQLite, blobs, migrations)

**Risk:** high — persistence, migrations, concurrency.

**Files:**
- Create: `internal/artifacts/types.go`
- Create: `internal/artifacts/store.go`
- Create: `internal/artifacts/blobstore.go`
- Create: `internal/artifacts/store_test.go`
- Create: `internal/artifacts/blobstore_test.go`

**Interfaces:**
- Produces: `Store` with methods `NewStore(projectSlug string) (*Store, error)`, `CreateArtifact(a Artifact) (Artifact, error)`, `GetArtifact(id string) (Artifact, error)`, `ListArtifacts(limit, offset int) ([]Artifact, error)`, `PublishVersion(v Version, files []File) (Version, error)`, `GetVersion(id string) (Version, error)`, `ListVersions(artifactID string) ([]Version, error)`, `MintToken(t AccessToken) (AccessToken, error)`, `GetTokenByHash(hash string) (AccessToken, error)`, `RevokeToken(id string) error`, `AddAllowEntry(e AllowEntry) error`, `GetAllowEntry(artifactID, email string) (AllowEntry, error)`, `AcceptInvite(inviteTokenHash string) (AllowEntry, error)`, `LogAccess(e AccessLogEntry) error`, `GC() error`, `Migrate() error`.
- Produces: `BlobStore` with `Put(data []byte) (hash string, error)`, `Get(hash string) ([]byte, error)`, `Has(hash string) (bool, error)`, `GCUnreferenced(referenced map[string]bool) error`.
- Types: `Artifact{ID, ProjectHost, ProjectPath, Name, CreatedAt, CreatedBy, Status, HostingMode}`, `Version{ID, ArtifactID, Version, Entrypoint, BlobManifestHash, SizeBytes, CreatedAt, BuildCmd, BuildLogPath, SchemaVersion}`, `File{VersionID, Path, BlobHash, Size, Mode, Mime}`, `AccessToken{ID, ArtifactID, TokenHash, Kind, ExpiresAt, CreatedAt, RevokedAt, PasswordHash, Label}`, `AllowEntry{ArtifactID, Email, InvitedAt, AcceptedAt, InviteTokenHash}`, `AccessLogEntry{ArtifactID, TokenID, IP, UA, Path, Status, TS}`.

- [ ] **Step 1: Write failing test** — `store_test.go`: create store in temp dir, migrate, create artifact, fetch by id, list paginated+sorted; publish version with files; mint+revoke token; add allow entry + accept invite; GC removes unreferenced blob.

  ```go
  func TestStoreCreateAndFetchArtifact(t *testing.T) {
      s, err := NewStore(t.TempDir())
      require.NoError(t, err)
      require.NoError(t, s.Migrate())
      a, err := s.CreateArtifact(Artifact{Name: "demo", Status: "active"})
      require.NoError(t, err)
      got, err := s.GetArtifact(a.ID)
      require.NoError(t, err)
      require.Equal(t, "demo", got.Name)
  }
  ```

- [ ] **Step 2: Run test** — `go test ./internal/artifacts/...` → expect FAIL (package undefined).

- [ ] **Step 3: Implement** store (SQLite WAL, per-project flock via existing filelock, skip-on-contention), blob store (content-addressed under `blobs/<xx>/<hash>`, sha256), migrations (`CREATE TABLE IF NOT EXISTS`). Slug scoping matches session slug (worktrees get own slug). No upsert-on-conflict.

- [ ] **Step 4: Run tests** — `go test ./internal/artifacts/...` → expect PASS.

---

### Task 2: Artifact capability tokens + middleware

**Risk:** high — auth, revocation, rate-limit isolation.

**Files:**
- Create: `internal/server/artifact_tokens.go`
- Create: `internal/server/handler_artifact_serve.go` (static serve entry, can be stub returning 501 until Task 3)

**Interfaces:**
- Produces: `artifactTokenStore` (durable, hashed) with `issue(artifactID string, kind string, expiresAt time.Time, password string) (token string, id string, err error)`, `validate(token, artifactID string) (AccessToken, int /*accessLevel*/, error)`, `revoke(id string) error`, `pruneExpired()`.
- Produces: `artifactAuthMiddleware(next http.HandlerFunc) http.HandlerFunc` resolving `{artifactId, token}` from path, checking hash/expiry/revocation/allow-list/password, setting request context keys `artifactID`, `accessLevel`, `tokenID`. Returns 401/403/404 uniformly.
- Produces: separate rate limiter (not `authMiddleware`'s per-IP limiter).

- [ ] **Step 1: Write failing test** — `artifact_tokens_test.go`: issue token, validate returns accessLevel; revoked token fails; expired token fails; password-gated token requires password; wrong artifact id fails; constant-time compare.

  ```go
  func TestArtifactTokenRevokedFails(t *testing.T) {
      st := newArtifactTokenStore(t.TempDir())
      tok, id, err := st.issue("art1", "link", time.Now().Add(time.Hour), "")
      require.NoError(t, err)
      require.NoError(t, st.revoke(id))
      _, _, err = st.validate(tok, "art1")
      require.Error(t, err)
  }
  ```

- [ ] **Step 2: Run test** → FAIL.

- [ ] **Step 3: Implement** durable hashed token store (sha256 hash at rest, 32-byte base64url value, Argon2id for password), middleware with own limiter, uniform 401/403/404. No master credential reuse.

- [ ] **Step 4: Run tests** → PASS.

---

### Task 3: Owner + visitor handlers and routes

**Risk:** high — auth gating, pagination, secret scan, remote routing.

**Files:**
- Create: `internal/server/handler_artifacts.go`
- Create: `internal/server/handler_artifact_invite.go`
- Modify: `internal/server/server.go` (registerRoutes) — add owner routes behind `authMiddleware`, visitor routes behind `artifactAuthMiddleware`.
- Create: `internal/server/handler_artifacts_test.go`

**Interfaces:**
- Consumes: `internal/artifacts.Store`, `artifactTokenStore`, `allowedProjectRoots()`.
- Produces handlers: `HandleCreateArtifact`, `HandleListArtifacts`, `HandleGetArtifact`, `HandlePublish`, `HandleUnpublish`, `HandleDeleteArtifact`, `HandleMintToken`, `HandleRevokeToken`, `HandleAllowlistAdd`, `HandleAllowlistRemove`, `HandleAccessLog`, `handleServeArtifact`, `HandleInviteAccept`, `HandlePasswordGate`.

- [ ] **Step 1: Write failing tests** — create artifact from local project (validated via `allowedProjectRoots`); list paginated+sorted; publish with secret scan (block on hit unless overridden with recorded reason); unpublish revokes; delete schedules GC; mint/revoke token; allow-list add triggers invite; invite accept sets artifact-scoped cookie; serve returns blob with containment check; private artifact returns 404 to visitor; notify_on_publish unchecked → no email; checked → email sent (mock mailer).

  ```go
  func TestHandlePublish_BlocksOnSecretScanHit(t *testing.T) { ... }
  func TestHandleServe_PrivateArtifactReturns404ToVisitor(t *testing.T) { ... }
  func TestHandlePublish_NotifyUncheckedSendsNoEmail(t *testing.T) { ... }
  ```

- [ ] **Step 2: Run tests** → FAIL.

- [ ] **Step 3: Implement** handlers. Secret scan via existing `HandleSecretScan` logic (extract reusable helper). Build command via existing shell tool under permission+sandbox gates, capture output to build log. Snapshot working tree into blob store. Transactional version+files write. Remote routing: owner routes for remote projects proxy to host (`/api/remote/{host}/api/artifacts/...`); hub unreachable → 502, no fallback. Visitor routes never proxy bytes for remote (host serves).

- [ ] **Step 4: Run tests** → PASS.

---

### Task 4: Web API client + publish dialog + artifact list

**Risk:** standard — UI wiring.

**Files:**
- Create: `web/src/api/artifacts.ts`
- Create: `web/src/components/Artifacts/ArtifactPublishDialog.tsx`
- Create: `web/src/components/Artifacts/ArtifactList.tsx`
- Modify: web settings/sidebar entry point to surface artifacts (minimal: add route/tab).

**Interfaces:**
- Consumes: owner API endpoints (Task 3).
- `ArtifactPublishDialog` includes "Notify on publish" checkbox, default unchecked, not persisted across opens.

- [ ] **Step 1: Write tests** — dialog default checkbox unchecked; submitting with unchecked sends `notify_on_publish: false`; checked sends `true`; list renders paginated results; invite copy works.

  ```ts
  it("defaults notify_on_publish to unchecked", () => {
    render(<ArtifactPublishDialog ... />);
    expect(screen.getByLabelText(/notify on publish/i)).not.toBeChecked();
  });
  ```

- [ ] **Step 2: Run tests** → FAIL.

- [ ] **Step 3: Implement** API client + dialog + list. Checkbox state is component-local (no persistence). Default unchecked.

- [ ] **Step 4: Run tests** → PASS.

---

### Task 5: Mode A (self-hosted) tailscale exposure + `.env.example`

**Risk:** standard — wiring existing primitives.

**Files:**
- Modify: `internal/server/tailscale_share.go` (add `/p/*` mount dispatch by artifact id) — or a new helper.
- Modify: `.env.example` (add new env vars).
- Create: `internal/server/handler_artifacts_exposure_test.go`

**Interfaces:**
- Consumes: `tailscale.Expose`/`StartServeExpose`/funnel:8443, single global mount `/p/*`.

- [ ] **Step 1: Write failing test** — exposing artifacts mounts `/p/*` once; a second expose does not clobber; funnel only on explicit confirmation.

- [ ] **Step 2: Run test** → FAIL.

- [ ] **Step 3: Implement** single global `/p/*` mount dispatched by artifact id; funnel only on explicit user confirmation (never unattended).

- [ ] **Step 4: Run tests** + `go build ./...` in worktree → PASS.

---

## Self-Review Checklist (run before handoff)

1. Spec coverage: every § of the spec maps to a task above (data model→T1, auth→T2, routes→T3, web→T4, exposure/env→T5). Hub-side endpoints are explicitly out of scope (separate deliverable).
2. No step contains implementation bodies; test snippets included.
3. Type names consistent across tasks (Artifact, Version, File, AccessToken, AllowEntry, AccessLogEntry, Store, BlobStore, artifactTokenStore).
4. Review Focus items each have a test in T2/T3.
5. Proportion: plan is shorter than the spec's implementation surface.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-11-07-publish-share-project.md`. Please review the plan. Which execution approach would you prefer?

- **Subagent-driven** — fresh subagent per task + fresh reviewer; most thorough.
- **Native** — I implement all tasks in this session, one whole-branch review at the end.

For this plan I recommend **Native**, because the tasks are tightly coupled through shared interfaces (Store, token store, handlers) and there are only 5 tasks; a shipped mistake in the auth/serving path is high-cost but the interfaces are pinned in the plan. Does the plan capture what you want, and which approach should we use?
