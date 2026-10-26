// Shares the embedded browser's tab strip and each tab's navigation state
// across every client on a server, so a phone and the desktop app show the same
// browser tabs and URLs.
//
// The shared state lives on the server (GET/PUT /api/browser-tabs →
// internal/browsertabs), exactly as the terminal tab list does. This module is
// the client side of that contract: the mapping to and from the wire shape, the
// sanitizing applied before anything is sent or applied, and the write-back
// hook. Only what this module owns is shared: the strip (ids, titles, manual
// renames, order), the active tab, and per tab the URL, history, history index,
// user mode and page title.
//
// Per-viewer and never sent to the server: scroll offsets per URL, in
// localStorage keyed by tab id. Scrolling one window must not move another
// window's page.
//
// Chrome's profile (cookies, localStorage, logins) persists server-side:
// headless Chrome launches with a stable --user-data-dir under the global
// data dir (<data>/browse/chrome-profile), so logins survive a restart.
// What is NOT persisted: the server-side browse session (grants, Chrome
// targets) and rendered page state. On restore the panel simply
// re-navigates to the saved URL.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSelector } from "@tanstack/react-store";
import { ApiError, api } from "../../api/client";
import type { ServerBrowserSurface, ServerProjectBrowsers } from "../../api/types";
import { eventBus } from "../../lib/eventBus";
import { normalizeBrowseURL } from "../../lib/browseURL";
import {
  browserActions,
  browserStore,
  type BrowseMode,
  type BrowserTabState,
  type StateKey,
} from "../../lib/browserStore";
import { useBrowserTabs, type BrowserTab } from "../../stores/browserTabsStore";
import { projectTerminalsKey } from "../Terminal/terminalPersistence";

export interface PersistedBrowserTab {
  id: string;
  title: string;
  manualTitle: string | null;
}

export interface PersistedBrowserSurface {
  url: string;
  history: string[];
  historyIndex: number;
  userMode: BrowseMode | null;
  pageTitle: string | null;
}

/** One project's shared browser state, in the client's shape. */
export interface PersistedProjectBrowsers {
  tabs: PersistedBrowserTab[];
  activeId: string | null;
  surfaces: Record<string, PersistedBrowserSurface>;
}

/** One server entry in the client's shape, with the revision the server gave it. */
export interface ServerSnapshot {
  state: PersistedProjectBrowsers;
  revision: number;
}

// Bounds match internal/browsertabs. A payload the client builds always passes
// the server's validation, and a payload from elsewhere is cut down to the same
// shape before it is applied.
const MAX_TABS = 25;
const MAX_HISTORY = 20;
const MAX_SCROLL_ENTRIES = 50;
const MAX_SCROLL_TABS = 200;
const MAX_TITLE_LEN = 200;
const MAX_URL_LEN = 2048;

const SCROLL_KEY = "ocode.ui.browser.scroll.v1";
// The pre-server, localStorage-only entry. Read once per project to migrate it
// to the server, then removed.
const LEGACY_BROWSERS_KEY = "ocode.ui.browser.project.v1";
const SAVE_DEBOUNCE_MS = 750;
// A failed save is retried once after this delay, unless a newer local edit
// replaces it first.
const SAVE_RETRY_MS = 5000;

/** The shared state of a project with no tabs. */
const EMPTY_STATE: PersistedProjectBrowsers = { tabs: [], activeId: null, surfaces: {} };
const EMPTY_JSON = JSON.stringify(EMPTY_STATE);

function asRecord(raw: unknown): Record<string, unknown> | null {
  return typeof raw === "object" && raw !== null && !Array.isArray(raw) ? (raw as Record<string, unknown>) : null;
}

function cleanUrl(raw: unknown): string | null {
  if (typeof raw !== "string" || raw.length === 0) return null;
  if (!/^https?:\/\//i.test(raw)) return null;
  let url: string;
  try {
    url = normalizeBrowseURL(raw);
  } catch {
    // intentionally not logged: an unparseable URL is an expected input, and
    // the null return is the caller's signal that this surface is not shareable.
    return null;
  }
  // The server caps UTF-8 bytes. JavaScript string length counts UTF-16 units,
  // so a URL of non-ASCII characters can pass a length check and still be
  // rejected whole by the server. Measure the bytes that will be sent.
  if (new TextEncoder().encode(url).length > MAX_URL_LEN) return null;
  return url;
}

function cleanTitle(raw: unknown): string {
  if (typeof raw !== "string") return "New tab";
  const t = raw.trim().slice(0, MAX_TITLE_LEN);
  return t || "New tab";
}

function cleanOptionalTitle(raw: unknown): string | null {
  if (typeof raw !== "string") return null;
  const t = raw.trim();
  return t ? raw.slice(0, MAX_TITLE_LEN) : null;
}

function cleanScroll(raw: unknown): Record<string, number> {
  const out: Record<string, number> = {};
  const entries = asRecord(raw);
  if (!entries) return out;
  for (const [k, v] of Object.entries(entries)) {
    const u = cleanUrl(k);
    if (u && typeof v === "number" && Number.isFinite(v) && v > 0 && Object.keys(out).length < MAX_SCROLL_ENTRIES) {
      out[u] = v;
    }
  }
  return out;
}

interface SurfaceInput {
  url: unknown;
  history: unknown;
  historyIndex: unknown;
  userMode: unknown;
  pageTitle: unknown;
}

/** Cuts one surface down to the shape the server stores. Returns null when the
 *  URL is not an http(s) URL, which means the surface is not shareable. */
function normalizeSurface(raw: SurfaceInput): PersistedBrowserSurface | null {
  const url = cleanUrl(raw.url);
  if (!url) return null;
  const listed = Array.isArray(raw.history) ? raw.history.map(cleanUrl).filter((u): u is string => u !== null) : [];
  const history = (listed.includes(url) ? listed : [...listed, url]).slice(-MAX_HISTORY);
  const index =
    typeof raw.historyIndex === "number" && Number.isFinite(raw.historyIndex)
      ? Math.floor(raw.historyIndex)
      : history.length - 1;
  return {
    url,
    history,
    historyIndex: Math.max(0, Math.min(index, history.length - 1)),
    userMode: raw.userMode === "local" || raw.userMode === "chrome" ? raw.userMode : null,
    pageTitle: cleanOptionalTitle(raw.pageTitle),
  };
}

/** The shared state for this client's current strip. Tabs without a navigable
 *  URL carry no surface. Local values are cut down to the server's bounds, so a
 *  state the client holds can always be sent. */
export function sharedStateFromLocal(
  tabs: BrowserTab[],
  activeId: string | null,
  byKey: Record<string, BrowserTabState>,
): PersistedProjectBrowsers {
  const strip = tabs.slice(0, MAX_TABS);
  const surfaces: Record<string, PersistedBrowserSurface> = {};
  for (const t of strip) {
    const s = byKey[`tab:${t.id}`];
    if (!s) continue;
    const surface = normalizeSurface(s);
    if (surface) surfaces[t.id] = surface;
  }
  const ids = new Set(strip.map((t) => t.id));
  return {
    tabs: strip.map((t) => ({ id: t.id, title: cleanTitle(t.title), manualTitle: cleanOptionalTitle(t.manualTitle) })),
    activeId: activeId !== null && ids.has(activeId) ? activeId : null,
    surfaces,
  };
}

/** Wire shape for a PUT. `revision` is the one this client last saw for the
 *  project (0 when the server has never held it). Absent optional fields are
 *  omitted, never sent as null. */
export function toWireProject(p: PersistedProjectBrowsers, revision: number): ServerProjectBrowsers {
  const surfaces: Record<string, ServerBrowserSurface> = {};
  for (const [id, s] of Object.entries(p.surfaces)) {
    surfaces[id] = {
      url: s.url,
      history: s.history,
      history_index: s.historyIndex,
      ...(s.userMode ? { user_mode: s.userMode } : {}),
      ...(s.pageTitle ? { page_title: s.pageTitle } : {}),
    };
  }
  return {
    tabs: p.tabs.map((t) => ({ id: t.id, title: t.title, ...(t.manualTitle ? { manual_title: t.manualTitle } : {}) })),
    ...(p.activeId ? { active_id: p.activeId } : {}),
    surfaces,
    revision,
  };
}

/** Client shape for one server entry, cut down to the same bounds, with the
 *  entry's revision. Null when the server holds no entry for the project. */
export function fromWireProject(wire: ServerProjectBrowsers | undefined): ServerSnapshot | null {
  if (!wire) return null;
  const tabs: PersistedBrowserTab[] = wire.tabs.slice(0, MAX_TABS).map((t) => ({
    id: t.id,
    title: cleanTitle(t.title),
    manualTitle: cleanOptionalTitle(t.manual_title),
  }));
  const surfaces: Record<string, PersistedBrowserSurface> = {};
  for (const t of tabs) {
    const raw = wire.surfaces[t.id];
    if (!raw) continue;
    const surface = normalizeSurface({
      url: raw.url,
      history: raw.history,
      historyIndex: raw.history_index,
      userMode: raw.user_mode,
      pageTitle: raw.page_title,
    });
    if (surface) surfaces[t.id] = surface;
  }
  const ids = new Set(tabs.map((t) => t.id));
  const activeId = wire.active_id !== undefined && ids.has(wire.active_id) ? wire.active_id : null;
  return { state: { tabs, activeId, surfaces }, revision: wire.revision };
}

/** The revision this client last saw for a project. A project the client has
 *  never seen, or that the server no longer holds, has revision 0. */
function trackedRevision(revisions: Map<string, number>, key: string): number {
  const seen = revisions.get(key);
  if (seen === undefined) return 0;
  return seen;
}

/** Records the revision of a server read. An absent project has none. */
function setTrackedRevision(revisions: Map<string, number>, key: string, snapshot: ServerSnapshot | null): void {
  if (snapshot) revisions.set(key, snapshot.revision);
  else revisions.delete(key);
}

/** The ids of tabs the server is known to have held, per project key. A local
 *  tab missing from a server read was closed by another client only if its id is
 *  in this set. A tab never synced is local-only, so the save sends it. */
function syncedIdsFor(byKey: Map<string, Set<string>>, key: string): Set<string> {
  let ids = byKey.get(key);
  if (!ids) {
    ids = new Set<string>();
    byKey.set(key, ids);
  }
  return ids;
}

function replaceSynced(byKey: Map<string, Set<string>>, key: string, ids: string[]): void {
  const synced = syncedIdsFor(byKey, key);
  synced.clear();
  for (const id of ids) synced.add(id);
}

/** The `{ error, projects }` body of a 409, or null when it does not have that
 *  shape. */
function conflictBody(body: unknown): { message: string; projects: Record<string, ServerProjectBrowsers> } | null {
  const b = asRecord(body);
  if (!b || typeof b.error !== "string") return null;
  const projects = asRecord(b.projects);
  if (!projects) return null;
  return { message: b.error, projects: projects as Record<string, ServerProjectBrowsers> };
}

/** Every project's shared state from the server. A response without the
 *  `projects` map breaks the contract, so it throws for the caller's try block
 *  to log, rather than surfacing later as an unhandled rejection. */
async function readServerProjects(): Promise<Record<string, ServerProjectBrowsers>> {
  const res = await api.getBrowserTabs();
  if (!res.projects) throw new Error("GET /api/browser-tabs returned no projects map");
  return res.projects;
}

// ── Scroll offsets (per viewer, localStorage) ────────────────────────────────

interface ScrollFile {
  version: 1;
  tabs: Record<string, Record<string, number>>;
}

function readScrollFile(): ScrollFile {
  try {
    const raw = window.localStorage.getItem(SCROLL_KEY);
    if (!raw) return { version: 1, tabs: {} };
    const parsed = JSON.parse(raw) as ScrollFile;
    if (!parsed || parsed.version !== 1 || typeof parsed.tabs !== "object" || !parsed.tabs) {
      return { version: 1, tabs: {} };
    }
    return parsed;
  } catch (err) {
    console.error("Failed to load persisted browser scroll offsets:", err);
    return { version: 1, tabs: {} };
  }
}

/** Saved scroll offsets for one tab, keyed by normalized page URL. */
export function loadScrollByUrl(tabId: string): Record<string, number> {
  return cleanScroll(readScrollFile().tabs[tabId]);
}

/** Saves one tab's scroll offsets. The file holds at most MAX_SCROLL_TABS tabs;
 *  the least recently saved is dropped first, since tab ids are not
 *  reclaimable from here (other projects' tabs are not in this window). */
export function saveScrollByUrl(tabId: string, scrollByUrl: Record<string, number>): void {
  const file = readScrollFile();
  delete file.tabs[tabId];
  const cleaned = cleanScroll(scrollByUrl);
  if (Object.keys(cleaned).length > 0) file.tabs[tabId] = cleaned;
  const ids = Object.keys(file.tabs);
  for (const oldest of ids.slice(0, Math.max(0, ids.length - MAX_SCROLL_TABS))) {
    delete file.tabs[oldest];
  }
  try {
    window.localStorage.setItem(SCROLL_KEY, JSON.stringify(file));
  } catch (err) {
    console.error("Failed to persist browser scroll offsets:", err);
  }
}

// ── One-time migration off the localStorage-only copy ────────────────────────

export interface LegacyBrowserState {
  shared: PersistedProjectBrowsers;
  scrollByTab: Record<string, Record<string, number>>;
}

function readLegacyProjects(): Record<string, unknown> {
  let raw: string | null;
  try {
    raw = window.localStorage.getItem(LEGACY_BROWSERS_KEY);
  } catch (err) {
    console.error("Failed to read legacy browser tabs:", err);
    return {};
  }
  if (!raw) return {};
  try {
    const parsed = asRecord(JSON.parse(raw));
    if (!parsed || parsed.version !== 1) return {};
    const projects = asRecord(parsed.projects);
    if (!projects) return {};
    return projects;
  } catch (err) {
    console.error("Failed to parse legacy browser tabs:", err);
    return {};
  }
}

function parseLegacyEntry(entry: unknown): LegacyBrowserState | null {
  const e = asRecord(entry);
  if (!e || !Array.isArray(e.tabs)) return null;
  const tabs: PersistedBrowserTab[] = [];
  const seen = new Set<string>();
  for (const raw of e.tabs) {
    const t = asRecord(raw);
    if (!t || typeof t.id !== "string" || !t.id || seen.has(t.id)) continue;
    seen.add(t.id);
    tabs.push({ id: t.id, title: cleanTitle(t.title), manualTitle: cleanOptionalTitle(t.manualTitle) });
    if (tabs.length >= MAX_TABS) break;
  }
  if (tabs.length === 0) return null;
  const activeId = typeof e.activeId === "string" && seen.has(e.activeId) ? e.activeId : tabs[tabs.length - 1].id;
  const surfacesRaw = asRecord(e.surfaces);
  const surfaces: Record<string, PersistedBrowserSurface> = {};
  const scrollByTab: Record<string, Record<string, number>> = {};
  for (const t of tabs) {
    const s = surfacesRaw ? asRecord(surfacesRaw[t.id]) : null;
    if (!s) continue;
    const surface = normalizeSurface({
      url: s.url,
      history: s.history,
      historyIndex: s.historyIndex,
      userMode: s.userMode,
      pageTitle: s.pageTitle,
    });
    if (surface) surfaces[t.id] = surface;
    scrollByTab[t.id] = cleanScroll(s.scrollByUrl);
  }
  return { shared: { tabs, activeId, surfaces }, scrollByTab };
}

/** Removes this project's legacy entry and returns its state, or null when
 *  there was none. The whole key is removed once no project remains. */
export function takeLegacyProjectBrowsers(projectPath: string): LegacyBrowserState | null {
  const projects = readLegacyProjects();
  if (!(projectPath in projects)) return null;
  const entry = projects[projectPath];
  delete projects[projectPath];
  try {
    if (Object.keys(projects).length === 0) {
      window.localStorage.removeItem(LEGACY_BROWSERS_KEY);
    } else {
      window.localStorage.setItem(LEGACY_BROWSERS_KEY, JSON.stringify({ version: 1, projects }));
    }
  } catch (err) {
    console.error("Failed to remove migrated browser tabs from localStorage:", err);
  }
  return parseLegacyEntry(entry);
}

// ── Sync hook ────────────────────────────────────────────────────────────────

/** A save waiting on its timer: the debounce after an edit, or the retry after
 *  a failed send. `flush` sends it now. */
interface SaveEntry {
  timer: ReturnType<typeof setTimeout>;
  flush: () => void;
}

/**
 * Keeps this project's browser strip and navigation state in step with the
 * server. Mount once per project tab bar.
 *
 * Four rules govern it:
 *   - Nothing is written before the server state has been read and applied
 *     (`restoredKey`). A write before that would replace the server's list
 *     with an empty or stale one.
 *   - The state the server is known to hold (`known`) is compared by content.
 *     Applying server state sets `known` first, so the save that follows finds
 *     nothing to send and the server is never echoed back to itself.
 *   - A server read is not applied while local changes are unsent, and a read
 *     that raced a write is dropped. The write's own echo triggers a fresh read.
 *   - Every save carries the revision this client last saw. The server refuses
 *     a save made against an older revision with 409, and this client then
 *     adopts the server's state. A failed save is retried, never left stranded.
 */
export function useBrowserPersistence(projectPath: string, host?: string): void {
  const { tabs, activeId, restoreBrowserTabs } = useBrowserTabs(projectPath);
  // Whole-store subscription: a surface's URL, history and page title change
  // without any strip action, and those must sync too.
  const surfacesSnap = useSelector(browserStore, (s) => s.byKey);
  const projectKey = projectPath ? projectTerminalsKey(projectPath, host) : "";

  const shared = useMemo(() => sharedStateFromLocal(tabs, activeId, surfacesSnap), [tabs, activeId, surfacesSnap]);
  // Primitive dependency: effects below rerun on content change, not on the
  // telemetry churn that rebuilds surfacesSnap.
  const sharedJson = useMemo(() => JSON.stringify(shared), [shared]);

  const [restoredKey, setRestoredKey] = useState<string | null>(null);
  const known = useRef<{ key: string; json: string } | null>(null);
  const latest = useRef({ tabs, json: sharedJson });
  const readSeq = useRef(0);
  const writeSeq = useRef(0);
  // Last server revision seen per project key. Sent with every save.
  const revisions = useRef<Map<string, number>>(new Map());
  // The newest local state per project key, as queued by the last edit. Any
  // send for the key sends this, so a retry can never send an older state.
  const newest = useRef<Map<string, { json: string; payload: PersistedProjectBrowsers }>>(new Map());
  // Saves waiting on a timer, per project key: the debounce, or a retry.
  const saves = useRef<Map<string, SaveEntry>>(new Map());
  // Sends run one after another, and a read waits for them, so a read can never
  // land between a write and its own result.
  const writing = useRef<Promise<void>>(Promise.resolve());
  const live = useRef(false);
  const syncedTabIds = useRef<Map<string, Set<string>>>(new Map());

  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);

  useEffect(() => {
    latest.current = { tabs, json: sharedJson };
  });

  const applyIncoming = useCallback(
    (incoming: PersistedProjectBrowsers, key: string) => {
      const synced = syncedIdsFor(syncedTabIds.current, key);
      const incomingIds = new Set(incoming.tabs.map((t) => t.id));
      // A local tab the server lacks is kept if it was never synced: it was
      // opened here (or in the background for this project) and the save sends
      // it. It is closed only when another client closed it, and that client has
      // already revoked the grant, so only the local state is dropped.
      const kept: BrowserTab[] = [];
      for (const t of latest.current.tabs) {
        if (incomingIds.has(t.id)) continue;
        if (synced.has(t.id)) browserActions.closeLocal(`tab:${t.id}`);
        else kept.push(t);
      }
      restoreBrowserTabs(
        [...incoming.tabs.map((t): BrowserTab => ({ id: t.id, title: t.title, manualTitle: t.manualTitle })), ...kept],
        incoming.activeId,
      );
      for (const t of incoming.tabs) {
        const key: StateKey = `tab:${t.id}`;
        const surface = incoming.surfaces[t.id];
        if (!surface) {
          browserActions.open(key);
          continue;
        }
        if (browserStore.state.byKey[key]) {
          browserActions.applySharedNavigation(key, surface);
        } else {
          browserActions.restoreSurface(key, { ...surface, scrollByUrl: loadScrollByUrl(t.id) });
        }
      }
    },
    [restoreBrowserTabs],
  );

  // Makes the server's state for `key` this client's state. Callers must have
  // checked that `key` is the project on screen.
  const applyServerState = useCallback(
    (key: string, snapshot: ServerSnapshot | null) => {
      const synced = syncedIdsFor(syncedTabIds.current, key);
      // The server holds no entry and this client synced none of its tabs: there
      // is nothing to apply.
      if (!snapshot && synced.size === 0) return;
      // The server holds no entry but this client synced tabs of this project:
      // another client deleted it, by saving an empty strip. Those tabs close
      // here. Tabs never synced stay local-only and are saved.
      const incoming = snapshot ? snapshot.state : EMPTY_STATE;
      known.current = { key, json: JSON.stringify(incoming) };
      applyIncoming(incoming, key);
      replaceSynced(
        syncedTabIds.current,
        key,
        incoming.tabs.map((t) => t.id),
      );
    },
    [applyIncoming],
  );

  // Records the revision of a server read, then applies its state for the
  // project on screen unless local edits are unsent.
  const takeServerRead = useCallback(
    (key: string, projects: Record<string, ServerProjectBrowsers>) => {
      const snapshot = fromWireProject(projects[key]);
      setTrackedRevision(revisions.current, key, snapshot);
      const current = known.current;
      if (!current || current.key !== key) return;
      // Unsent local edits are not overwritten. They go out with the revision
      // just read.
      if (latest.current.json !== current.json) return;
      const json = snapshot ? JSON.stringify(snapshot.state) : EMPTY_JSON;
      if (json === current.json) return;
      applyServerState(key, snapshot);
    },
    [applyServerState],
  );

  // Sends the newest local state for `key`, unless the server already holds it.
  const sendNewest = useCallback(
    async (key: string): Promise<void> => {
      const want = newest.current.get(key);
      if (!want) return;
      const held = known.current;
      if (held && held.key === key && held.json === want.json) return;
      writeSeq.current += 1;
      try {
        await api.setBrowserTabs({ [key]: toWireProject(want.payload, trackedRevision(revisions.current, key)) });
      } catch (err) {
        const conflict = err instanceof ApiError && err.status === 409 ? conflictBody(err.body) : null;
        if (conflict) {
          // The server holds a newer revision than this client saw. Its state
          // wins: this client's edit is dropped, and the strip re-syncs visibly.
          console.warn(`Browser tabs for ${key} changed on the server during a save; adopting the server state.`, {
            message: conflict.message,
          });
          const snapshot = fromWireProject(conflict.projects[key]);
          setTrackedRevision(revisions.current, key, snapshot);
          if (known.current && known.current.key === key) applyServerState(key, snapshot);
          return;
        }
        console.error(`Failed to save browser tabs to server for ${key}:`, err);
        // A panel that has unmounted is not retried: the error above is the record.
        if (!live.current) return;
        // A newer edit already waits for this key and sends the newest state.
        if (!saves.current.has(key)) scheduleSave(key, SAVE_RETRY_MS);
        return;
      }
      replaceSynced(
        syncedTabIds.current,
        key,
        want.payload.tabs.map((t) => t.id),
      );
      if (known.current && known.current.key === key) known.current = { key, json: want.json };
      // A successful PUT returns no revision, so the server's new revision comes
      // from a read right after it. A concurrent change from another client is
      // applied the same way as any other read.
      let projects: Record<string, ServerProjectBrowsers>;
      try {
        projects = await readServerProjects();
      } catch (err) {
        console.error("Failed to read browser tabs back after saving:", err);
        return;
      }
      takeServerRead(key, projects);
    },
    // scheduleSave is declared below and only ever called from within a send.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [applyServerState, takeServerRead],
  );

  // Queues a send of the newest state for `key` after `delayMs`. A save already
  // waiting for the key is left to the caller, which replaces it first.
  const scheduleSave = useCallback(
    (key: string, delayMs: number): void => {
      const fire = (): void => {
        if (saves.current.get(key) !== entry) return;
        clearTimeout(entry.timer);
        saves.current.delete(key);
        writing.current = writing.current.then(() => sendNewest(key));
      };
      const entry: SaveEntry = { timer: setTimeout(fire, delayMs), flush: fire };
      saves.current.set(key, entry);
    },
    [sendNewest],
  );

  // Initial read for this project: the server's state wins, a legacy entry
  // seeds a project the server has never seen, and the write gate opens after.
  const restore = useCallback(
    async (): Promise<void> => {
      const read = ++readSeq.current;
      await writing.current;
      let projects: Record<string, ServerProjectBrowsers>;
      try {
        projects = await readServerProjects();
      } catch (err) {
        console.error("Failed to restore browser tabs from server:", err);
        return;
      }
      if (read !== readSeq.current) return;
      const snapshot = fromWireProject(projects[projectKey]);
      setTrackedRevision(revisions.current, projectKey, snapshot);
      const legacy = host ? null : takeLegacyProjectBrowsers(projectPath);
      let incoming = EMPTY_STATE;
      let knownJson = EMPTY_JSON;
      if (snapshot) {
        incoming = snapshot.state;
        knownJson = JSON.stringify(snapshot.state);
        for (const t of snapshot.state.tabs) syncedIdsFor(syncedTabIds.current, projectKey).add(t.id);
      } else if (legacy) {
        // Not yet known to the server, so it is left unknown here: the save
        // effect sends it once the gate opens.
        incoming = legacy.shared;
        for (const [tabId, scroll] of Object.entries(legacy.scrollByTab)) saveScrollByUrl(tabId, scroll);
      }
      known.current = { key: projectKey, json: knownJson };
      applyIncoming(incoming, projectKey);
      setRestoredKey(projectKey);
    },
    [projectPath, projectKey, host, applyIncoming],
  );

  // Event-driven read after the initial restore. Never overwrites local edits
  // that have not been sent yet.
  const refresh = useCallback(
    async (): Promise<void> => {
      const read = ++readSeq.current;
      const writesAtStart = writeSeq.current;
      let projects: Record<string, ServerProjectBrowsers>;
      try {
        projects = await readServerProjects();
      } catch (err) {
        console.error("Failed to refresh browser tabs from server:", err);
        return;
      }
      if (read !== readSeq.current || writesAtStart !== writeSeq.current) return;
      takeServerRead(projectKey, projects);
    },
    [projectKey, takeServerRead],
  );

  useEffect(() => {
    if (!projectPath) return;
    known.current = null;
    setRestoredKey(null);
    void restore();
    // Sends the previous project's unsent edits now, rather than when the
    // timer fires, so switching away or unmounting never drops them.
    return () => {
      for (const entry of [...saves.current.values()]) entry.flush();
    };
  }, [projectPath, projectKey, restore]);

  useEffect(() => {
    if (!projectPath) return;
    const onChanged = () => {
      if (known.current === null) void restore();
      else void refresh();
    };
    const onReconnect = () => {
      if (known.current === null) {
        void restore();
        return;
      }
      // A save still waiting (debounced or retrying) goes out now. Its read-back
      // then re-syncs this client, so no separate refresh is needed.
      if (saves.current.size > 0) {
        for (const entry of [...saves.current.values()]) entry.flush();
        return;
      }
      void refresh();
    };
    const offEvent = eventBus.on("browser_tabs_changed", onChanged);
    const offReconnect = eventBus.onReconnect(onReconnect);
    return () => {
      offEvent();
      offReconnect();
    };
  }, [projectPath, restore, refresh]);

  // Debounced write-back. A newer edit replaces this project's unsent save,
  // debounced or retrying, and a save for another project is left to fire with
  // the state it captured.
  useEffect(() => {
    if (!projectKey || restoredKey !== projectKey) return;
    const replaced = saves.current.get(projectKey);
    if (replaced) {
      clearTimeout(replaced.timer);
      saves.current.delete(projectKey);
    }
    const current = known.current;
    if (current && current.key === projectKey && current.json === sharedJson) return;
    // `shared` is read here but deliberately not a dependency: `sharedJson`
    // already changes exactly when its content does.
    newest.current.set(projectKey, { json: sharedJson, payload: shared });
    scheduleSave(projectKey, SAVE_DEBOUNCE_MS);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [restoredKey, projectKey, sharedJson]);

  // Scroll offsets are per viewer and never sent, so they are saved locally
  // once the restore has read them (saving earlier would overwrite them).
  const scrollJson = useMemo(
    () =>
      JSON.stringify(
        tabs.flatMap((t) => {
          const s = surfacesSnap[`tab:${t.id}`];
          return s ? [[t.id, s.scrollByUrl]] : [];
        }),
      ),
    [tabs, surfacesSnap],
  );
  useEffect(() => {
    if (!projectKey || restoredKey !== projectKey) return;
    const timer = setTimeout(() => {
      for (const t of tabs) {
        const s = surfacesSnap[`tab:${t.id}`];
        if (s) saveScrollByUrl(t.id, s.scrollByUrl);
      }
    }, SAVE_DEBOUNCE_MS);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [restoredKey, projectKey, scrollJson]);
}
