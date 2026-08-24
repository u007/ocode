// Persists which terminal tabs were open per session tab (legacy) and per
// project (current), and each terminal's rendered scrollback text, across
// page reloads. Terminal *processes* are never resumed (a fresh pty is spawned
// on restore) — only the tab layout and visible text survive, so the user can
// see what happened before reload.
// The project-scoped key is the current source of truth: terminals survive
// switching chat sessions within the same project. The session-scoped key is
// retained for backward compat and GC.

export interface PersistedTerminal {
  id: string;
  title: string;
}

interface PersistedSessionTerminals {
  terminals: PersistedTerminal[];
  activeId: string;
}

const TABS_KEY = "ocode.ui.terminals.v1";
const PROJECT_TABS_KEY = "ocode.ui.terminals.project.v1";
const BUFFER_KEY_PREFIX = "ocode.term.buf.";

interface PersistedTabsFile {
  version: 1;
  sessions: Record<string, PersistedSessionTerminals>;
}

interface PersistedProjectTabsFile {
  version: 1;
  projects: Record<string, PersistedSessionTerminals>;
}

function readTabsFile(): PersistedTabsFile {
  try {
    const raw = window.localStorage.getItem(TABS_KEY);
    if (!raw) return { version: 1, sessions: {} };
    const parsed = JSON.parse(raw) as PersistedTabsFile;
    if (!parsed || parsed.version !== 1 || typeof parsed.sessions !== "object") {
      return { version: 1, sessions: {} };
    }
    return parsed;
  } catch (err) {
    console.error("Failed to load persisted terminal tabs:", err);
    return { version: 1, sessions: {} };
  }
}

function readProjectTabsFile(): PersistedProjectTabsFile {
  try {
    const raw = window.localStorage.getItem(PROJECT_TABS_KEY);
    if (!raw) return { version: 1, projects: {} };
    const parsed = JSON.parse(raw) as PersistedProjectTabsFile;
    if (!parsed || parsed.version !== 1 || typeof parsed.projects !== "object") {
      return { version: 1, projects: {} };
    }
    return parsed;
  } catch (err) {
    console.error("Failed to load project terminal tabs:", err);
    return { version: 1, projects: {} };
  }
}

export function loadSessionTerminals(sessionTabId: string): PersistedSessionTerminals | null {
  const file = readTabsFile();
  const entry = file.sessions[sessionTabId];
  if (!entry || !Array.isArray(entry.terminals) || entry.terminals.length === 0) return null;
  return entry;
}

/** Saves this session's terminal list and garbage-collects scrollback buffers
 *  for any terminal id no longer open in *any* session tab. */
export function saveSessionTerminals(sessionTabId: string, terminals: PersistedTerminal[], activeId: string) {
  const file = readTabsFile();
  if (terminals.length === 0) {
    delete file.sessions[sessionTabId];
  } else {
    file.sessions[sessionTabId] = { terminals, activeId };
  }
  try {
    window.localStorage.setItem(TABS_KEY, JSON.stringify(file));
  } catch (err) {
    console.error("Failed to persist terminal tabs:", err);
    return;
  }
  gcBuffers(file);
}

/** Called when a session tab is explicitly closed (not on a mere remount —
 *  callers must not invoke this from a React unmount effect, since that also
 *  fires for reasons other than the user closing the tab). Drops the
 *  session's terminal list and GCs its now-orphaned scrollback buffers. */
export function dropSessionTerminals(sessionTabId: string) {
  const file = readTabsFile();
  if (!(sessionTabId in file.sessions)) return;
  delete file.sessions[sessionTabId];
  try {
    window.localStorage.setItem(TABS_KEY, JSON.stringify(file));
  } catch (err) {
    console.error("Failed to drop persisted terminal tabs:", err);
    return;
  }
  gcBuffers(file);
}

function gcBuffers(file: PersistedTabsFile) {
  const live = new Set<string>();
  for (const entry of Object.values(file.sessions)) {
    for (const t of entry.terminals) live.add(t.id);
  }
  for (let i = window.localStorage.length - 1; i >= 0; i--) {
    const key = window.localStorage.key(i);
    if (!key || !key.startsWith(BUFFER_KEY_PREFIX)) continue;
    const id = key.slice(BUFFER_KEY_PREFIX.length);
    if (!live.has(id)) window.localStorage.removeItem(key);
  }
}

// ── Project-scoped persistence (current) ─────────────────────────────────────

export function loadProjectTerminals(projectPath: string): PersistedSessionTerminals | null {
  const key = projectPath || "__global__";
  const file = readProjectTabsFile();
  const entry = file.projects[key];
  if (!entry || !Array.isArray(entry.terminals) || entry.terminals.length === 0) return null;
  return entry;
}

/** Saves this project's terminal list and GCs scrollback buffers for ids no
 *  longer open in *any* project. Terminals are intentionally project-scoped
 *  so switching chat sessions within the same project never hides or kills
 *  the pty. */
export function saveProjectTerminals(projectPath: string, terminals: PersistedTerminal[], activeId: string) {
  const key = projectPath || "__global__";
  const file = readProjectTabsFile();
  if (terminals.length === 0) {
    delete file.projects[key];
  } else {
    file.projects[key] = { terminals, activeId };
  }
  try {
    window.localStorage.setItem(PROJECT_TABS_KEY, JSON.stringify(file));
  } catch (err) {
    console.error("Failed to persist project terminal tabs:", err);
    return;
  }
  gcProjectBuffers(file);
}

export function dropProjectTerminals(projectPath: string) {
  const key = projectPath || "__global__";
  const file = readProjectTabsFile();
  if (!(key in file.projects)) return;
  delete file.projects[key];
  try {
    window.localStorage.setItem(PROJECT_TABS_KEY, JSON.stringify(file));
  } catch (err) {
    console.error("Failed to drop project terminal tabs:", err);
    return;
  }
  gcProjectBuffers(file);
}

function gcProjectBuffers(file: PersistedProjectTabsFile) {
  const live = new Set<string>();
  for (const entry of Object.values(file.projects)) {
    for (const t of entry.terminals) live.add(t.id);
  }
  // Also keep any ids still referenced by the legacy per-session file so we
  // don't prematurely GC buffers that haven't been migrated yet.
  try {
    const legacy = readTabsFile();
    for (const entry of Object.values(legacy.sessions)) {
      for (const t of entry.terminals) live.add(t.id);
    }
  } catch {
    // ignore legacy read failure
  }
  for (let i = window.localStorage.length - 1; i >= 0; i--) {
    const key = window.localStorage.key(i);
    if (!key || !key.startsWith(BUFFER_KEY_PREFIX)) continue;
    const id = key.slice(BUFFER_KEY_PREFIX.length);
    if (!live.has(id)) window.localStorage.removeItem(key);
  }
}

export interface PersistedTerminalBuffer {
  text: string;
  cols: number;
  rows: number;
}

/** Reads a saved buffer. Also accepts the pre-dimensions legacy format (a raw
 *  serialized string), returned with cols/rows unset so the caller falls back
 *  to xterm's defaults. */
export function loadTerminalBuffer(id: string): PersistedTerminalBuffer | null {
  try {
    const raw = window.localStorage.getItem(BUFFER_KEY_PREFIX + id);
    if (!raw) return null;
    if (raw.startsWith("{")) {
      const parsed = JSON.parse(raw) as PersistedTerminalBuffer;
      if (typeof parsed.text === "string") return parsed;
      return null;
    }
    return { text: raw, cols: 0, rows: 0 };
  } catch (err) {
    console.error("Failed to load terminal buffer:", err);
    return null;
  }
}

export function saveTerminalBuffer(id: string, text: string, cols: number, rows: number) {
  try {
    window.localStorage.setItem(
      BUFFER_KEY_PREFIX + id,
      JSON.stringify({ text, cols, rows } satisfies PersistedTerminalBuffer),
    );
  } catch (err) {
    console.error("Failed to persist terminal buffer:", err);
  }
}
