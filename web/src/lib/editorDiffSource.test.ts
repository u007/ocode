import { describe, expect, it } from "vitest";
import { resolveEditorDiffSource } from "./editorDiffSource";
import type { GitDiffFile, GitStatus } from "@/api/types";

const gitStatus = (over: Partial<GitStatus> = {}): GitStatus => ({
  branch: "main",
  staged_files: [],
  changed_files: [],
  conflicts: [],
  has_changes: false,
  is_repo: true,
  ahead: 0,
  behind: 0,
  has_upstream: false,
  ...over,
});

const GIT_PATCH = "diff --git a/src/app.ts b/src/app.ts\n@@ -1,1 +1,1 @@\n-old\n+new";
const SESSION_PATCH = "diff --git a/src/app.ts b/src/app.ts\n@@ -1,1 +1,1 @@\n-old\n+session";

const gitFiles = (patch: string): GitDiffFile[] => [
  { path: "src/app.ts", status: "modified", patch },
];

describe("resolveEditorDiffSource", () => {
  it("prefers the git working-tree (unstaged) diff in a repo", () => {
    const res = resolveEditorDiffSource({
      projectRoot: "/repo",
      gitStatus: gitStatus({ has_changes: true, changed_files: ["src/app.ts"] }),
      gitFiles: gitFiles(GIT_PATCH),
      path: "src/app.ts",
      sessionPatch: SESSION_PATCH,
    });
    expect(res).toEqual({ kind: "git", patch: GIT_PATCH });
  });

  it("highlights untracked files from the git diff (fresh repo, no branch)", () => {
    const res = resolveEditorDiffSource({
      projectRoot: "/repo",
      gitStatus: gitStatus({ branch: "", has_changes: false }),
      gitFiles: [
        { path: "src/new.ts", status: "untracked", patch: "diff --git a/src/new.ts b/src/new.ts\n@@ -0,0 +1 @@\n+hello" },
      ],
      path: "src/new.ts",
      sessionPatch: "",
    });
    expect(res.kind).toBe("git");
    expect(res.patch).toContain("+hello");
  });

  it("reports a clean repo as git with an empty patch — never falls back to session", () => {
    const res = resolveEditorDiffSource({
      projectRoot: "/repo",
      gitStatus: gitStatus(), // is_repo: true, no changes
      gitFiles: [],
      path: "src/app.ts",
      sessionPatch: SESSION_PATCH,
    });
    expect(res).toEqual({ kind: "git", patch: "" });
  });

  it("shows no decoration for a staged-only file (nothing unstaged)", () => {
    const res = resolveEditorDiffSource({
      projectRoot: "/repo",
      gitStatus: gitStatus({ staged_files: ["src/app.ts"], has_changes: true }),
      gitFiles: [], // working tree clean — the file is fully staged
      path: "src/app.ts",
      sessionPatch: SESSION_PATCH,
    });
    expect(res).toEqual({ kind: "git", patch: "" });
  });

  it("falls back to the session diff outside a git repo", () => {
    const res = resolveEditorDiffSource({
      projectRoot: "/plain-dir",
      gitStatus: gitStatus({ is_repo: false, branch: "" }),
      gitFiles: [],
      path: "src/app.ts",
      sessionPatch: SESSION_PATCH,
    });
    expect(res).toEqual({ kind: "session", patch: SESSION_PATCH });
  });

  it("falls back to the session diff when git status is unavailable (network error)", () => {
    const res = resolveEditorDiffSource({
      projectRoot: "/repo",
      gitStatus: undefined,
      gitFiles: [],
      path: "src/app.ts",
      sessionPatch: SESSION_PATCH,
    });
    expect(res).toEqual({ kind: "session", patch: SESSION_PATCH });
  });

  it("uses the session diff when no project root is known", () => {
    const res = resolveEditorDiffSource({
      gitFiles: [],
      path: "src/app.ts",
      sessionPatch: SESSION_PATCH,
    });
    expect(res).toEqual({ kind: "session", patch: SESSION_PATCH });
  });

  it("yields nothing when there is neither a repo diff nor a session diff", () => {
    const res = resolveEditorDiffSource({
      projectRoot: "/plain-dir",
      gitStatus: gitStatus({ is_repo: false, branch: "" }),
      gitFiles: [],
      path: "src/app.ts",
      sessionPatch: "",
    });
    expect(res).toEqual({ kind: "none", patch: "" });
  });

  it("returns the empty git patch when the repo diff does not cover the path", () => {
    const res = resolveEditorDiffSource({
      projectRoot: "/repo",
      gitStatus: gitStatus(),
      gitFiles: [{ path: "src/other.ts", status: "modified", patch: GIT_PATCH }],
      path: "src/app.ts",
      sessionPatch: SESSION_PATCH,
    });
    expect(res).toEqual({ kind: "git", patch: "" });
  });
});

// Phase 07 decision (deliberate, pinned here so it cannot drift by accident).
//
// A conflicted file gets NO git diff decorations in the editor, and the reason
// is NOT the status lists this module never reads. `git diff` emits a COMBINED
// diff for an unmerged path — a `diff --cc` header and a three-way `@@@` hunk —
// and the server's parseUnifiedDiff (internal/server/handler_git.go) only
// recognizes `diff --git`, so the file yields no GitDiffFile at all and arrives
// here as an empty gitFiles entry.
//
// This is accepted for now: the Git tab lists the conflict with its resolution
// actions, and the file tree badges it. Teaching the parser combined diffs
// would touch seven call sites (local + remote diff endpoints), so it is logged
// in TODO.md rather than smuggled into a badge-parity phase.
describe("resolveEditorDiffSource with a conflicted path", () => {
  const conflictedStatus = gitStatus({
    has_changes: true,
    conflicts: [{ path: "src/app.ts", code: "UU", ours: true, theirs: true }],
  });

  it("returns an empty git patch rather than falling back to the session diff", () => {
    const res = resolveEditorDiffSource({
      projectRoot: "/repo",
      gitStatus: conflictedStatus,
      gitFiles: [], // the combined diff is dropped server-side
      path: "src/app.ts",
      sessionPatch: SESSION_PATCH,
    });
    // "git" with an empty patch is the deliberate answer: a conflicted file is
    // neither a clean repo (which must not fall back) nor a session edit.
    expect(res).toEqual({ kind: "git", patch: "" });
  });
});
