// Persists open browser tabs per project across page reloads and desktop
// restarts: tab strip (ids, titles, manual renames, order, active tab) plus
// each tab's navigation state (URL, history, mode, page title, scroll
// offsets per URL). Mirrors terminalPersistence.ts (same versioned-file
// shape, same project-scoped key discipline).
//
// Chrome's profile (cookies, localStorage, logins) persists server-side:
// headless Chrome launches with a stable --user-data-dir under the global
// data dir (<data>/browse/chrome-profile), so logins survive a restart.
// What is NOT persisted: the server-side browse session (grants, Chrome
// targets) and rendered page state. On restore the panel simply
// re-navigates to the saved URL; scroll offsets restore best-effort after
// the page renders.

import { useEffect, useRef } from "react";
import { useSelector } from "@tanstack/react-store";
import { normalizeBrowseURL } from "../../lib/browseURL";
import { browserActions, browserStore, type BrowseMode } from "../../lib/browserStore";
import { useBrowserTabs, type BrowserTab } from "../../stores/browserTabsStore";

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
  scrollByUrl: Record<string, number>;
}

interface PersistedProjectBrowsers {
  tabs: PersistedBrowserTab[];
  activeId: string | null;
  surfaces: Record<string, PersistedBrowserSurface>;
}

interface PersistedBrowserFile {
  version: 1;
  projects: Record<string, PersistedProjectBrowsers>;
}

const BROWSERS_KEY = "ocode.ui.browser.project.v1";

// Bounds so one pathological session (long SPA history, huge page titles)
// cannot blow out localStorage.
const MAX_TABS = 25;
const MAX_HISTORY = 20;
const MAX_SCROLL_ENTRIES = 50;
const MAX_TITLE_LEN = 200;
const MAX_URL_LEN = 2048;

function cleanUrl(raw: unknown): string | null {
  if (typeof raw !== "string" || raw.length === 0 || raw.length > MAX_URL_LEN) return null;
  if (!/^https?:\/\//i.test(raw)) return null;
  try {
    return normalizeBrowseURL(raw);
  } catch {
    return null;
  }
}

function cleanTitle(raw: unknown): string {
  if (typeof raw !== "string") return "New tab";
  const t = raw.trim().slice(0, MAX_TITLE_LEN);
  return t || "New tab";
}

function readFile(): PersistedBrowserFile {
  try {
    const raw = window.localStorage.getItem(BROWSERS_KEY);
    if (!raw) return { version: 1, projects: {} };
    const parsed = JSON.parse(raw) as PersistedBrowserFile;
    if (!parsed || parsed.version !== 1 || typeof parsed.projects !== "object" || !parsed.projects) {
      return { version: 1, projects: {} };
    }
    return parsed;
  } catch (err) {
    console.error("Failed to load persisted browser tabs:", err);
    return { version: 1, projects: {} };
  }
}

function cleanSurface(raw: PersistedBrowserSurface | undefined): PersistedBrowserSurface | null {
  if (!raw || typeof raw !== "object") return null;
  const url = cleanUrl(raw.url);
  if (!url) return null;
  const history = Array.isArray(raw.history)
    ? raw.history.map(cleanUrl).filter((u): u is string => u !== null).slice(-MAX_HISTORY)
    : [];
  if (!history.includes(url)) history.push(url);
  let historyIndex =
    typeof raw.historyIndex === "number" && Number.isFinite(raw.historyIndex)
      ? Math.floor(raw.historyIndex)
      : history.length - 1;
  historyIndex = Math.max(0, Math.min(historyIndex, history.length - 1));
  const userMode = raw.userMode === "local" || raw.userMode === "chrome" ? raw.userMode : null;
  const pageTitle = typeof raw.pageTitle === "string" && raw.pageTitle.trim() ? raw.pageTitle.slice(0, MAX_TITLE_LEN) : null;
  const scrollByUrl: Record<string, number> = {};
  if (raw.scrollByUrl && typeof raw.scrollByUrl === "object") {
    for (const [k, v] of Object.entries(raw.scrollByUrl)) {
      const u = cleanUrl(k);
      if (u && typeof v === "number" && Number.isFinite(v) && v > 0 && Object.keys(scrollByUrl).length < MAX_SCROLL_ENTRIES) {
        scrollByUrl[u] = v;
      }
    }
  }
  return { url, history, historyIndex, userMode, pageTitle, scrollByUrl };
}

export function loadProjectBrowsers(projectPath: string): PersistedProjectBrowsers | null {
  const entry = readFile().projects[projectPath];
  if (!entry || !Array.isArray(entry.tabs) || entry.tabs.length === 0) return null;
  const tabs: PersistedBrowserTab[] = [];
  const seen = new Set<string>();
  for (const t of entry.tabs) {
    if (!t || typeof t.id !== "string" || !t.id || seen.has(t.id)) continue;
    seen.add(t.id);
    tabs.push({
      id: t.id,
      title: cleanTitle(t.title),
      manualTitle: typeof t.manualTitle === "string" && t.manualTitle.trim() ? t.manualTitle.slice(0, MAX_TITLE_LEN) : null,
    });
    if (tabs.length >= MAX_TABS) break;
  }
  if (tabs.length === 0) return null;
  const activeId = typeof entry.activeId === "string" && seen.has(entry.activeId) ? entry.activeId : tabs[tabs.length - 1].id;
  const surfaces: Record<string, PersistedBrowserSurface> = {};
  if (entry.surfaces && typeof entry.surfaces === "object") {
    for (const t of tabs) {
      const s = cleanSurface(entry.surfaces[t.id]);
      if (s) surfaces[t.id] = s;
    }
  }
  return { tabs, activeId, surfaces };
}

export function saveProjectBrowsers(
  projectPath: string,
  tabs: PersistedBrowserTab[],
  activeId: string | null,
  surfaces: Record<string, PersistedBrowserSurface>,
): void {
  const file = readFile();
  if (tabs.length === 0) {
    delete file.projects[projectPath];
  } else {
    file.projects[projectPath] = { tabs, activeId, surfaces };
  }
  try {
    window.localStorage.setItem(BROWSERS_KEY, JSON.stringify(file));
  } catch (err) {
    console.error("Failed to persist browser tabs:", err);
  }
}

/** Called when a project is removed: drops its browser state. */
export function dropProjectBrowsers(projectPath: string): void {
  const file = readFile();
  if (!(projectPath in file.projects)) return;
  delete file.projects[projectPath];
  try {
    window.localStorage.setItem(BROWSERS_KEY, JSON.stringify(file));
  } catch (err) {
    console.error("Failed to drop persisted browser tabs:", err);
  }
}

const SAVE_DEBOUNCE_MS = 750;

/**
 * Restores this project's browser tabs once (strip + surfaces; only the
 * active surface loads — inactive tabs load on first activation) and saves
 * on every strip/surface change (debounced). Mount once per project tab bar.
 */
export function useBrowserPersistence(projectPath: string): void {
  const { tabs, activeId, restoreBrowserTabs } = useBrowserTabs(projectPath);
  // Whole-store subscription: scroll offsets and titles change without any
  // strip action, and those must persist too. UnifiedTabBar re-renders
  // densely already; the save below is debounced and content-compared.
  const surfacesSnap = useSelector(browserStore, (s) => s.byKey);

  const restoredFor = useRef<string | null>(null);
  useEffect(() => {
    if (!projectPath) return;
    if (restoredFor.current === projectPath) return;
    restoredFor.current = projectPath;
    // The STRIP for this project is the restore gate — never the global
    // surface map. Surfaces are keyed `tab:<id>` with no project tag, so
    // another project's live surfaces are always present after a project
    // switch; gating on them would skip this project's restore and the
    // debounced save would then persist the empty strip, deleting the saved
    // entry. restoreSurface only writes this project's `tab:<id>` keys, so
    // restoring alongside foreign surfaces is safe. A non-empty strip means
    // live state (HMR / in-memory switch) wins.
    if (tabs.length > 0) return;
    const saved = loadProjectBrowsers(projectPath);
    if (!saved) return;
    restoreBrowserTabs(
      saved.tabs.map((t): BrowserTab => ({ id: t.id, title: t.title, manualTitle: t.manualTitle })),
      saved.activeId,
    );
    for (const t of saved.tabs) {
      const s = saved.surfaces[t.id];
      if (s) browserActions.restoreSurface(`tab:${t.id}`, s);
      else browserActions.open(`tab:${t.id}`);
    }
    // tabs intentionally omitted: one-shot restore per project mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectPath, restoreBrowserTabs]);

  const saveKey = useRef("");
  useEffect(() => {
    if (!projectPath) return;
    const persistedTabs: PersistedBrowserTab[] = tabs.map((t) => ({ id: t.id, title: t.title, manualTitle: t.manualTitle ?? null }));
    const surfaces: Record<string, PersistedBrowserSurface> = {};
    for (const t of tabs) {
      const s = surfacesSnap[`tab:${t.id}`];
      if (!s || !s.url) continue;
      const history = s.history.filter((u) => cleanUrl(u) !== null).slice(-MAX_HISTORY);
      const scrollByUrl: Record<string, number> = {};
      for (const [k, v] of Object.entries(s.scrollByUrl ?? {})) {
        if (cleanUrl(k) && Object.keys(scrollByUrl).length < MAX_SCROLL_ENTRIES) scrollByUrl[k] = v;
      }
      surfaces[t.id] = {
        url: s.url,
        history: history.length ? history : [s.url],
        historyIndex: Math.max(0, Math.min(s.historyIndex, Math.max(0, history.length - 1))),
        userMode: s.userMode,
        pageTitle: s.pageTitle,
        scrollByUrl,
      };
    }
    const key = JSON.stringify([persistedTabs, activeId, surfaces]);
    if (key === saveKey.current) return;
    const timer = setTimeout(() => {
      saveKey.current = key;
      saveProjectBrowsers(projectPath, persistedTabs, activeId, surfaces);
    }, SAVE_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [projectPath, tabs, activeId, surfacesSnap]);
}
