import { useCallback, useSyncExternalStore } from "react";
import { api } from "../api/client";
import type { GitWorktree } from "../api/types";
import { eventBus } from "./eventBus";

/**
 * sessionWorkspace — the checkout the CURRENT chat session is rooted in.
 *
 * This answers exactly one question — "is this chat on main, or in a
 * worktree?" — and is deliberately built only from state the server can
 * derive (`GET /api/git/worktrees`). It never guesses: a request that has not
 * answered yet, a project that is not a repository, or a remote project (the
 * worktree endpoints are local-only) all yield NO chip rather than a default
 * of "main".
 *
 * One module-level entry per `host\0project` is shared by every mount, so the
 * status bar's re-render churn (once per streamed token) costs one request:
 * an initial fetch, a 60s poll while mounted, and an instant refresh on a
 * `git_status` bus event for the same project. A failed fetch keeps the last
 * known list — losing the only signal about where work is happening because of
 * one transient error is worse than a slightly stale label.
 */

export const WORKSPACE_POLL_MS = 60_000;

export type WorkspaceTone = "muted" | "accent" | "ok" | "warn" | "danger";

export type WorkspaceChip = {
  /** Compact chip text, e.g. `main`, `wt: feat/x ↑2`, `feat/x ✓ contained`. */
  text: string;
  /** Full sentence for the tooltip. Every claim names its basis. */
  title: string;
  tone: WorkspaceTone;
};

export type WorkspaceSnapshot = {
  worktrees: GitWorktree[] | null;
  state: "idle" | "loading" | "ready" | "error" | "unavailable";
};

/** Stable reference so `useSyncExternalStore` does not re-render every pass. */
export const NO_WORKSPACE: WorkspaceSnapshot = { worktrees: null, state: "idle" };

function normPath(p: string): string {
  return p.replace(/\\/g, "/").replace(/\/+$/, "");
}

function plural(n: number, one: string, many: string): string {
  return n === 1 ? one : many;
}

/**
 * `describeSessionWorkspace` is the whole rendering policy, kept pure so it
 * can be tested without a browser or a server.
 *
 * Invariants:
 * - no list, an empty list, or a `cwd` that is not in the list => no chip
 *   (unknown must never be rendered as "main");
 * - `undefined`/`-1` fields are unknown, not zero/clean;
 * - `contained_in_base` is reported as containment, never as "merged".
 */
export function describeSessionWorkspace(
  cwd: string | undefined,
  worktrees: GitWorktree[] | null,
): WorkspaceChip | null {
  if (!cwd || !worktrees || worktrees.length === 0) return null;

  const target = normPath(cwd);
  // Match by PATH first: the session's project root is the authoritative
  // answer to "which checkout is this chat on". `is_current` only breaks a tie
  // (and covers a server that resolved a symlinked spelling differently).
  // A cwd that is not listed yields NO chip — it must never fall through to
  // whichever entry the server happened to mark current.
  const matches = worktrees.filter((w) => normPath(w.path) === target);
  const wt = matches.find((w) => w.is_current) ?? matches[0];
  if (!wt) return null;

  const isMain = Boolean(wt.is_main);
  const branch = wt.branch ?? "";
  const sha = (wt.head ?? "").slice(0, 7);
  const missing = wt.exists === false;
  const dirty = typeof wt.dirty === "number" ? wt.dirty : -1;
  const ahead = typeof wt.ahead === "number" ? wt.ahead : -1;
  const behind = typeof wt.behind === "number" ? wt.behind : -1;
  const contained = wt.contained_in_base === true;
  const base = wt.base_ref ?? "";
  const baseKnown = Boolean(wt.base_known) && base !== "";

  const label = isMain
    ? branch && branch !== "main"
      ? `main · ${branch}`
      : "main"
    : branch
      ? branch
      : `detached ${sha}`;

  const compact: string[] = [];
  if (missing) {
    compact.push("(missing)");
  } else {
    if (!isMain && ahead > 0) compact.push(`↑${ahead}`);
    if (!isMain && contained) compact.push("✓ contained");
    if (dirty > 0) compact.push(`${dirty} changed`);
  }

  const prefix = isMain ? "" : "wt: ";
  const text = compact.length
    ? `${prefix}${label} ${compact.join(" · ")}`
    : `${prefix}${label}`;

  const bits: string[] = [
    `${wt.path}${isMain ? " (main checkout)" : " (linked worktree)"}`,
  ];
  if (!isMain) {
    if (baseKnown) {
      if (ahead >= 0 && behind >= 0) {
        bits.push(
          ahead === 0 && behind === 0
            ? `same commit as ${base}`
            : `${ahead} ahead, ${behind} behind ${base}`,
        );
        if (contained) {
          bits.push(
            `contained in ${base} (ancestor check — squash/rebase merges are not detected)`,
          );
        } else if (ahead > 0) {
          bits.push(`not contained in ${base}`);
        }
      } else {
        bits.push(`divergence from ${base} unknown`);
      }
    } else {
      bits.push("base branch unknown");
    }
  }
  if (missing) {
    bits.push("checkout directory is missing");
  } else if (dirty > 0) {
    bits.push(`${dirty} changed ${plural(dirty, "path", "paths")}`);
  } else if (dirty === 0) {
    bits.push("working tree clean");
  }
  if (typeof wt.owner_session_count === "number") {
    bits.push(
      wt.owner_session_count === 0
        ? "no chat session uses this checkout"
        : `${wt.owner_session_count} chat ${plural(wt.owner_session_count, "session", "sessions")}`,
    );
  }
  if (wt.last_commit) {
    bits.push(`last commit ${wt.last_commit.slice(0, 10)}`);
  }

  const tone: WorkspaceTone = missing
    ? "danger"
    : dirty > 0
      ? "warn"
      : !isMain && contained
        ? "ok"
        : isMain
          ? "muted"
          : "accent";

  return { text, title: bits.join(" · "), tone };
}

type Entry = {
  readonly key: string;
  readonly project: string;
  readonly host?: string;
  snapshot: WorkspaceSnapshot;
  inFlight: boolean;
  timer: ReturnType<typeof setInterval> | null;
  readonly listeners: Set<() => void>;
};

const entries = new Map<string, Entry>();
let offGitStatus: (() => void) | null = null;

function keyOf(project: string, host?: string): string {
  return `${host ?? ""}\u0000${project}`;
}

function notify(entry: Entry): void {
  for (const listener of entry.listeners) listener();
}

function stopTimer(entry: Entry): void {
  if (entry.timer) {
    clearInterval(entry.timer);
    entry.timer = null;
  }
}

async function refresh(entry: Entry): Promise<void> {
  if (entry.inFlight || entry.host) return;
  entry.inFlight = true;
  try {
    const list = await api.gitWorktrees(entry.project);
    entry.snapshot = {
      worktrees: Array.isArray(list) ? list : [],
      state: "ready",
    };
  } catch (err) {
    // Keep the last known list: a transient failure must not erase the
    // session's only statement about where it is working.
    console.error("Failed to read worktrees for session workspace:", entry.project, err);
    entry.snapshot = { worktrees: entry.snapshot.worktrees, state: "error" };
  } finally {
    entry.inFlight = false;
    notify(entry);
  }
}

function ensureBus(): void {
  if (offGitStatus) return;
  offGitStatus = eventBus.on("git_status", (env) => {
    for (const entry of entries.values()) {
      // Remote entries never fetch (the endpoint is local-only), and the
      // envelope carries no host, so only an exact project match refreshes.
      if (entry.host) continue;
      if (!env.project || env.project !== entry.project) continue;
      void refresh(entry);
    }
  });
}

function subscribeEntry(
  project: string,
  host: string | undefined,
  listener: () => void,
): () => void {
  const key = keyOf(project, host);
  let entry = entries.get(key);
  if (!entry) {
    // No project (no session yet) is not a request: stay idle so the chip
    // never flashes a claim before the session's project root is known.
    const idle = project === "";
    entry = {
      key,
      project,
      host,
      snapshot: idle
        ? NO_WORKSPACE
        : host
          ? { worktrees: null, state: "unavailable" }
          : { worktrees: null, state: "loading" },
      inFlight: false,
      timer: null,
      listeners: new Set(),
    };
    entries.set(key, entry);
  }
  entry.listeners.add(listener);

  if (entry.listeners.size === 1 && !host && project !== "") {
    void refresh(entry);
    entry.timer = setInterval(() => void refresh(entry), WORKSPACE_POLL_MS);
    ensureBus();
  }

  return () => {
    entry.listeners.delete(listener);
    if (entry.listeners.size === 0) stopTimer(entry);
  };
}

/**
 * `useSessionWorkspace(project, host)` — the derived checkout state for the
 * session whose project root is `project`. `host` (a remote project) reports
 * `unavailable` and never fetches: worktrees are a local-only endpoint.
 */
export function useSessionWorkspace(project: string, host?: string): WorkspaceSnapshot {
  const subscribe = useCallback(
    (listener: () => void) => subscribeEntry(project, host, listener),
    [project, host],
  );
  const getSnapshot = useCallback(
    () => entries.get(keyOf(project, host))?.snapshot ?? NO_WORKSPACE,
    [project, host],
  );
  return useSyncExternalStore(subscribe, getSnapshot, () => NO_WORKSPACE);
}

/** Test-only: drop every cached entry and the bus subscription. */
export function __resetSessionWorkspaceForTests(): void {
  for (const entry of entries.values()) stopTimer(entry);
  entries.clear();
  if (offGitStatus) {
    offGitStatus();
    offGitStatus = null;
  }
}
