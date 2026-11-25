import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/api/client";
import type { GitWorktree } from "@/api/types";

/** Page size for discovery. The server derives per-checkout state, so a large
 *  page costs more than a small one; 500 keeps the round trips near one. */
const PAGE_LIMIT = 500;

const cache = new Map<string, GitWorktree[]>();
const inFlight = new Map<string, Promise<GitWorktree[]>>();

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

async function fetchAll(project: string): Promise<GitWorktree[]> {
  const all: GitWorktree[] = [];
  for (let offset = 0; ; offset += PAGE_LIMIT) {
    const page = await api.gitWorktrees(project, {
      limit: PAGE_LIMIT,
      offset,
    });
    all.push(...page);
    if (page.length < PAGE_LIMIT) return all;
  }
}

/** One request per project at a time: concurrent hook mounts share the page
 *  walk, so a selector rendered twice does not double the Git probes. */
function load(project: string): Promise<GitWorktree[]> {
  const pending = inFlight.get(project);
  if (pending) return pending;
  const request = fetchAll(project)
    .then((list) => {
      cache.set(project, list);
      return list;
    })
    .catch((err: unknown) => {
      // A failed page is never cached: the next mount retries instead of
      // showing a stale list as if discovery had succeeded.
      console.error("Failed to load worktrees:", project, err);
      throw err;
    })
    .finally(() => {
      inFlight.delete(project);
    });
  inFlight.set(project, request);
  return request;
}

interface WorktreeState {
  worktrees: GitWorktree[];
  loading: boolean;
  error: string | null;
}

function initialState(projectPath?: string): WorktreeState {
  const cached = projectPath ? cache.get(projectPath) : undefined;
  return {
    worktrees: cached ?? [],
    loading: Boolean(projectPath) && cached === undefined,
    error: null,
  };
}

/** Every checkout of `projectPath`'s repository, loaded page by page and
 *  cached per project. An unknown project never substitutes another one's
 *  list: changing the path resets the result and drops the old response. */
export function useProjectWorktrees(projectPath?: string) {
  const [state, setState] = useState<WorktreeState>(() =>
    initialState(projectPath),
  );
  const requestSeq = useRef(0);

  const loadProject = useCallback((project: string, useCache: boolean) => {
    const seq = ++requestSeq.current;
    const cached = useCache ? cache.get(project) : undefined;
    if (cached !== undefined) {
      setState({ worktrees: cached, loading: false, error: null });
      return;
    }
    setState({ worktrees: [], loading: true, error: null });
    load(project).then(
      (worktrees) => {
        if (seq !== requestSeq.current) return;
        setState({ worktrees, loading: false, error: null });
      },
      (err: unknown) => {
        if (seq !== requestSeq.current) return;
        setState({
          worktrees: [],
          loading: false,
          error: errorMessage(err),
        });
      },
    );
  }, []);

  useEffect(() => {
    if (!projectPath) {
      requestSeq.current++;
      setState({ worktrees: [], loading: false, error: null });
      return;
    }
    loadProject(projectPath, true);
    return () => {
      requestSeq.current++;
    };
  }, [projectPath, loadProject]);

  const reload = useCallback(() => {
    if (projectPath) loadProject(projectPath, false);
  }, [projectPath, loadProject]);

  return { ...state, reload };
}
