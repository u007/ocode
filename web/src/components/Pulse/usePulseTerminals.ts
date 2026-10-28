import { useCallback, useEffect, useState } from "react";
import { ApiError, api } from "../../api/client";
import type { PulseTerminal } from "../../api/types";
import { loadGhostty } from "../Terminal/ghosttyRuntime";
import { restoreTerminalHistory } from "../Terminal/terminalHistory";
import { ghosttyCellsToText } from "./pulseScreenText";

/** How often the terminal list refreshes while the dashboard is open. */
export const PULSE_TERMINALS_POLL_MS = 3000;

/** Rows per page. "Load more" grows the window; each poll refetches all of it. */
const PULSE_TERMINALS_PAGE = 50;

/**
 * Statuses that mean the list can never load from this server: 403 when the bind
 * is non-loopback without auth, 501 on Windows (no pty). Nothing clears them
 * without a restart, so polling stops instead of repeating the same failure.
 */
const PERMANENT_STATUSES = new Set([403, 501]);

/**
 * usePulseTerminals: the live terminals for the Pulse dashboard, running
 * programs first. It polls the first `limit` rows and skips a tick while the
 * document is hidden, so a background tab costs nothing. On a permanent status
 * it stops polling for good and reports `unavailable`.
 */
export function usePulseTerminals() {
  const [limit, setLimit] = useState(PULSE_TERMINALS_PAGE);
  const [terminals, setTerminals] = useState<PulseTerminal[]>([]);
  const [total, setTotal] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [unavailable, setUnavailable] = useState(false);

  useEffect(() => {
    if (unavailable) return;
    let stale = false;
    const load = () => {
      api
        .listPulseTerminals(0, limit)
        .then((page) => {
          if (stale) return;
          setTerminals(page.terminals);
          setTotal(page.total);
          setError(null);
        })
        .catch((err: unknown) => {
          console.error("usePulseTerminals: listing terminals failed:", err);
          if (stale) return;
          if (err instanceof ApiError && PERMANENT_STATUSES.has(err.status)) {
            setUnavailable(true);
            return;
          }
          setError(`Listing terminals failed: ${err instanceof Error ? err.message : String(err)}`);
        });
    };
    load();
    const timer = window.setInterval(() => {
      if (!document.hidden) load();
    }, PULSE_TERMINALS_POLL_MS);
    return () => {
      stale = true;
      window.clearInterval(timer);
    };
  }, [limit, unavailable]);

  const loadMore = useCallback(() => setLimit((n) => n + PULSE_TERMINALS_PAGE), []);
  return { terminals, total, error, unavailable, hasMore: terminals.length < total, loadMore };
}

/**
 * The trailing bytes of a terminal's log that the output view replays. A
 * full-screen program (Claude Code, any TUI) repaints in place with cursor moves
 * and no newlines, so the view must run the bytes through a terminal emulator.
 * Measured on a live Claude Code log: 64 KiB left parts of the screen blank,
 * 256 KiB matched the real screen.
 */
export const PULSE_TERMINAL_SCREEN_BYTES = 256 * 1024;

/** Scrollback lines the emulator keeps above the visible screen (only the screen is read back). */
const PULSE_TERMINAL_SCROLLBACK = 1000;

type PulseScreenRead = { kind: "missing" } | { kind: "restored"; lines: string[] };

/**
 * Replays the last PULSE_TERMINAL_SCREEN_BYTES of one terminal's log into an
 * libghostty emulator of the pty's size and reads back its screen rows. The restore is pinned to
 * one snapshot, so the replay never mixes two states of a live log. A missing
 * log is reported as `missing`, not thrown.
 */
async function readPulseScreen(
  terminal: Pick<PulseTerminal, "id" | "project" | "cols" | "rows">,
  signal: AbortSignal,
): Promise<PulseScreenRead> {
  // The shared wasm instance (see loadGhostty); only the terminal below is per read.
  const ghostty = await loadGhostty();
  // The row may have collapsed while the wasm loaded; do not start a replay nobody reads.
  signal.throwIfAborted();
  const emulator = ghostty.createTerminal(terminal.cols, terminal.rows, { scrollbackLimit: PULSE_TERMINAL_SCROLLBACK });
  try {
    const restored = await restoreTerminalHistory({
      id: terminal.id,
      projectPath: terminal.project,
      maxBytes: PULSE_TERMINAL_SCREEN_BYTES,
      signal,
      // libghostty parses synchronously, so the buffer read below sees every chunk.
      onText: (text) => {
        emulator.write(text);
      },
    });
    if (restored.kind === "missing") return { kind: "missing" };
    const lines: string[] = [];
    for (let row = 0; row < emulator.rows; row++) {
      const cells = emulator.getLine(row);
      if (!cells) throw new Error(`terminal screen row ${row} is outside its ${emulator.rows} rows`);
      // Same rules as the server's terminal_screen: every row is right-trimmed.
      lines.push(ghosttyCellsToText(cells, (col) => emulator.getGraphemeString(row, col)));
    }
    while (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();
    return { kind: "restored", lines };
  } finally {
    emulator.free();
  }
}

/**
 * usePulseTerminalScreen: what one terminal's screen shows now, as text lines,
 * for an expanded dashboard row. The caller mounts it only while the row is
 * expanded, so it reads only then. It refreshes on the list cadence, skips a
 * tick while the document is hidden, and never starts a read while one is still
 * in flight. Nothing is latched: a missing log (`noOutput`) or a failed read
 * (`error`) is re-checked on the next tick. It does nothing until the terminal
 * has a reported pty size, because the screen cannot be laid out without one.
 */
export function usePulseTerminalScreen(terminal: Pick<PulseTerminal, "id" | "project" | "cols" | "rows">) {
  const { id, project, cols, rows } = terminal;
  const [screen, setScreen] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [noOutput, setNoOutput] = useState(false);

  useEffect(() => {
    if (cols === 0 || rows === 0) return;
    const controller = new AbortController();
    let reading = false;
    const load = () => {
      if (reading) return;
      reading = true;
      readPulseScreen({ id, project, cols, rows }, controller.signal)
        .then((next) => {
          if (controller.signal.aborted) return;
          setError(null);
          if (next.kind === "missing") {
            setNoOutput(true);
            setScreen(null);
            return;
          }
          setNoOutput(false);
          setScreen(next.lines);
        })
        .catch((err: unknown) => {
          if (controller.signal.aborted) return;
          console.error("usePulseTerminalScreen: reading terminal output failed:", err);
          setError(`Reading output failed: ${err instanceof Error ? err.message : String(err)}`);
        })
        .finally(() => {
          reading = false;
        });
    };
    load();
    const timer = window.setInterval(() => {
      if (!document.hidden) load();
    }, PULSE_TERMINALS_POLL_MS);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [id, project, cols, rows]);

  return { screen, error, noOutput };
}
