import { useEffect, useState } from "react";
import { api } from "@/api/client";
import {
  DEFAULT_TERMINAL_CELL_HEIGHT,
  DEFAULT_TERMINAL_FONT_FAMILY,
  DEFAULT_TERMINAL_FONT_SIZE,
} from "@/components/Terminal/terminalAppearance";
import type {
  CellAdjust,
  GhosttyReport,
  TerminalAppearance,
} from "@/components/Terminal/terminalAppearance";

export interface TerminalConfig {
  available?: boolean;
  scrollback_lines?: number;
  /** Resolved font family. Kept for clients that ignore `appearance`. */
  font_family?: string;
  /** Resolved font size. Kept for clients that ignore `appearance`. */
  font_size?: number;
  shell?: string;
  default_shell?: string;
  available_shells?: string[];
  work_dir?: string;
  /** Resolved font/colour source (see terminalAppearance). */
  appearance?: TerminalAppearance;
  /** What was read from this machine's Ghostty config. */
  ghostty?: GhosttyReport;
  /** "local" when `appearance` was resolved here, "remote" when omitted. */
  appearance_scope?: "local" | "remote";
}

type Listener = (config: TerminalConfig) => void;

let cached: TerminalConfig | null = null;
let inFlight: Promise<TerminalConfig> | null = null;
const listeners = new Set<Listener>();

function publish(value: TerminalConfig) {
  cached = value;
  for (const fn of listeners) fn(value);
}

// Defaults live in terminalAppearance (which must stay free of this hook's
// module graph); re-exported here because every existing consumer imports them
// from the hook.
export {
  DEFAULT_TERMINAL_CELL_HEIGHT,
  DEFAULT_TERMINAL_FONT_FAMILY,
  DEFAULT_TERMINAL_FONT_SIZE,
};

function viewFor(config: TerminalConfig | null) {
  if (!config) {
    return {
      available: false,
      loading: true,
      scrollbackLines: 0,
      fontFamily: DEFAULT_TERMINAL_FONT_FAMILY,
      fontSize: DEFAULT_TERMINAL_FONT_SIZE,
      cellWidth: {} as CellAdjust,
      cellHeight: DEFAULT_TERMINAL_CELL_HEIGHT,
      appearance: undefined as TerminalAppearance | undefined,
      ghostty: undefined as GhosttyReport | undefined,
      shell: "",
      defaultShell: "",
      availableShells: [] as string[],
    };
  }
  const appearance = config.appearance;
  return {
    available: config.available !== false,
    loading: false,
    scrollbackLines: config.scrollback_lines ?? 0,
    fontFamily: appearance?.font_family || config.font_family || DEFAULT_TERMINAL_FONT_FAMILY,
    fontSize: appearance?.font_size ?? config.font_size ?? DEFAULT_TERMINAL_FONT_SIZE,
    cellWidth: appearance?.cell_width_adjust ?? ({} as CellAdjust),
    cellHeight: appearance?.cell_height_adjust ?? DEFAULT_TERMINAL_CELL_HEIGHT,
    appearance,
    ghostty: config.ghostty,
    shell: config.shell ?? "",
    defaultShell: config.default_shell ?? "",
    availableShells: config.available_shells ?? [],
  };
}

/** Re-fetches terminal config and pushes it to every mounted useTerminalConfig() consumer. */
export async function refreshTerminalConfig() {
  const next = await api.getTerminalConfig();
  publish(next);
  return next;
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
    fontFamily: view.fontFamily,
    fontSize: view.fontSize,
    cellWidth: view.cellWidth,
    cellHeight: view.cellHeight,
    appearance: view.appearance,
    ghostty: view.ghostty,
    shell: view.shell,
    defaultShell: view.defaultShell,
    availableShells: view.availableShells,
    serverWorkDir: config?.work_dir ?? "",
  };
}
