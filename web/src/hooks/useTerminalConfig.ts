import { useEffect, useState } from "react";
import { api } from "@/api/client";

export interface TerminalConfig {
  available?: boolean;
  scrollback_lines?: number;
  work_dir?: string;
}

type Listener = (config: TerminalConfig) => void;

let cached: TerminalConfig | null = null;
let inFlight: Promise<TerminalConfig> | null = null;
const listeners = new Set<Listener>();

function publish(value: TerminalConfig) {
  cached = value;
  for (const fn of listeners) fn(value);
}

function sameProject(projectPath: string | undefined, serverWorkDir: string | undefined) {
  // Older/headless test responses did not include work_dir. The server now
  // always sends it, but treating an absent value as unknown preserves the
  // existing API compatibility for embedded clients.
  return !projectPath || !serverWorkDir || projectPath === serverWorkDir;
}

function viewFor(config: TerminalConfig | null, projectPath: string | undefined) {
  if (!config) {
    return { available: false, loading: true, scrollbackLines: 0, scopeMatches: false };
  }
  const scopeMatches = sameProject(projectPath, config.work_dir);
  const available = config.available !== false && scopeMatches;
  return {
    available,
    loading: false,
    scrollbackLines: config.scrollback_lines ?? 0,
    scopeMatches,
  };
}

/**
 * Terminal availability + scrollback for the server's single workdir. The
 * terminal itself is always enabled — there is no enable/disable switch —
 * so this only reports whether the server can safely expose it (authentication
 * configured or loopback bind) and whether the selected project matches the
 * server's workdir.
 */
export function useTerminalConfig(projectPath?: string) {
  const [config, setConfig] = useState<TerminalConfig | null>(cached);
  const [error, setError] = useState<string | null>(null);
  const view = viewFor(config, projectPath);

  useEffect(() => {
    const listener: Listener = (next) => setConfig(next);
    listeners.add(listener);
    if (cached !== null) setConfig(cached);
    return () => {
      listeners.delete(listener);
    };
  }, []);

  useEffect(() => {
    if (cached !== null) return;
    let cancelled = false;
    const req = inFlight ?? (inFlight = api.getTerminalConfig());
    req
      .then((next) => {
        inFlight = null;
        if (cancelled) return;
        publish(next);
      })
      .catch((err) => {
        inFlight = null;
        console.error("failed to load terminal configuration", err);
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return {
    available: view.available,
    scopeMatches: view.scopeMatches,
    loading: view.loading,
    error,
    scrollbackLines: view.scrollbackLines,
    serverWorkDir: config?.work_dir ?? "",
  };
}
