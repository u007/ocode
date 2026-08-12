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

function viewFor(config: TerminalConfig | null) {
  if (!config) {
    return { available: false, loading: true, scrollbackLines: 0 };
  }
  return {
    available: config.available !== false,
    loading: false,
    scrollbackLines: config.scrollback_lines ?? 0,
  };
}

/**
 * Terminal availability + scrollback. The terminal itself is always enabled —
 * there is no enable/disable switch — so this only reports whether the server
 * can safely expose it (authentication configured or loopback bind). Which
 * directory a shell opens in is chosen per connection via the WS
 * `project_path` parameter, so there is no project-scope gate here.
 */
export function useTerminalConfig() {
  const [config, setConfig] = useState<TerminalConfig | null>(cached);
  const [error, setError] = useState<string | null>(null);
  const view = viewFor(config);

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
    loading: view.loading,
    error,
    scrollbackLines: view.scrollbackLines,
    serverWorkDir: config?.work_dir ?? "",
  };
}
