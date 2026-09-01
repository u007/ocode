import { useCallback, useEffect, useRef, useState } from "react";
import { api, apiPath, authHeaders } from "../api/client";
import {
  clearEditorDraft,
  hashContent,
  loadEditorDraft,
  loadEditorTabs,
  saveEditorDraft,
  saveEditorTabs,
} from "../components/Files/editorTabsPersistence";

export interface EditorTab {
  id: string;
  path: string;
  projectRoot?: string;
  content: string;
  originalContent: string;
  isDirty: boolean;
  diffVersion: number;
  /** The file changed (or vanished) on disk outside this app while this tab
   *  held unsaved edits — surfaced as a banner until reloaded or dismissed. */
  externalChange: boolean;
  /** When true (the default) the opened file is attached to the LLM loop /
   *  chat context on send. Unchecking excludes it without closing the tab. */
  includeInContext: boolean;
  /** Hash of the disk content at the last accepted point (load or successful
   *  save). Used as the expected_hash for the save guard; not rebased on
   *  external-change detection for dirty tabs so a normal save correctly 409s. */
  baseHash: string;
}
export interface ActiveEditorContext {
  path: string;
  projectRoot?: string;
  selection?: { startLine: number; endLine: number };
}

export interface UseEditorTabsResult {
  editorTabs: EditorTab[];
  activeEditorTabId: string | null;
  setActiveEditorTabId: (id: string | null) => void;
  handleOpenFile: (path: string, projectRoot?: string) => Promise<void>;
  reloadTabFromDisk: (id: string) => Promise<void>;
  dismissExternalChange: (id: string) => void;
  handleEditorChange: (id: string, content: string) => void;
  handleSelectionChange: (sel: { startLine: number; endLine: number } | null) => void;
  toggleIncludeInContext: (id: string) => void;
  closeTabsForPaths: (paths: string[], projectRoot?: string) => void;
  renameTabPath: (oldPath: string, newPath: string, projectRoot?: string) => void;
  activeEditorContext: ActiveEditorContext | null;
  requestCloseTab: (id: string) => void;
  saveEditorTab: (id: string, opts?: { force?: boolean }) => Promise<void>;
  forceSaveEditorTab: (id: string) => Promise<void>;
  pendingClose: { id: string; path: string } | null;
  confirmSaveAndClose: () => Promise<void>;
  confirmDiscardAndClose: () => void;
  cancelClose: () => void;
  saveError: string | null;
}

export function useEditorTabs(): UseEditorTabsResult {
  const [editorTabs, setEditorTabs] = useState<EditorTab[]>([]);
  const [activeEditorTabId, setActiveEditorTabId] = useState<string | null>(null);
  const [pendingClose, setPendingClose] = useState<{ id: string; path: string } | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [activeEditorContext, setActiveEditorContext] = useState<ActiveEditorContext | null>(null);
  const openFileIdsRef = useRef<Set<string>>(new Set());

  // Refs so callbacks that fire on the typing hot path don't need to close
  // over `editorTabs` / `activeEditorTabId` and thus don't change identity
  // on every keystroke (which would churn downstream Monaco subscriptions).
  const activeEditorTabIdRef = useRef<string | null>(null);
  useEffect(() => {
    activeEditorTabIdRef.current = activeEditorTabId;
  }, [activeEditorTabId]);

  const fetchFileContent = useCallback(async (path: string, projectRoot?: string): Promise<string> => {
    const query = new URLSearchParams({ path });
    if (projectRoot) query.set("project_root", projectRoot);
    const res = await fetch(apiPath(`/api/files/content?${query.toString()}`), {
      headers: authHeaders(),
    });
    if (!res.ok) throw new Error("Failed to load file");
    const data = await res.json();
    return data.content as string;
  }, []);

  const handleOpenFile = useCallback(async (path: string, projectRoot?: string) => {
    const id = projectRoot ? `editor-${projectRoot}::${path}` : `editor-${path}`;
    if (openFileIdsRef.current.has(id)) {
      setActiveEditorTabId(id);
      return;
    }
    // Claim the id SYNCHRONOUSLY before any await. The fetch below is async;
    // claiming only after it resolved let two rapid open calls (double-click
    // on a tree row, restore racing a user click) both pass the guard and
    // append two tabs with the same id — two stacked Monaco editors fighting
    // over focus, selection sync, and caret position.
    openFileIdsRef.current.add(id);
    const draft = loadEditorDraft(id);
    let tab: EditorTab;
    try {
      const disk = await fetchFileContent(path, projectRoot);
      if (draft && draft.content !== disk) {
        // Unsaved edits survive the reload. If the on-disk content no longer
        // matches what the draft was edited against, the file moved under the
        // draft — flag it so the tab shows a conflict banner.
        // Keep baseHash as the old draft base so the save guard still 409s
        // until the user reloads or force-saves; originalContent reflects
        // the current disk for diff display.
        tab = {
          id,
          path,
          projectRoot,
          content: draft.content,
          originalContent: disk,
          isDirty: true,
          diffVersion: 0,
          externalChange: draft.baseHash !== hashContent(disk),
          includeInContext: true,
          baseHash: draft.baseHash,
        };
      } else {
        if (draft) clearEditorDraft(id); // draft matches disk — it was saved elsewhere
        tab = {
          id,
          path,
          projectRoot,
          content: disk,
          originalContent: disk,
          isDirty: false,
          diffVersion: 0,
          externalChange: false,
          includeInContext: true,
          baseHash: hashContent(disk),
        };
      }
    } catch (err) {
      if (!draft) {
        // Release the claim — the tab was never created and must be openable
        // again once the failure clears.
        openFileIdsRef.current.delete(id);
        console.error("Failed to open file:", err);
        return;
      }
      // The file is gone (deleted/renamed outside the app) but we still hold
      // unsaved edits — open the tab from the draft so the user can inspect
      // and re-save rather than silently losing their work.
      console.error("Failed to open file, restoring unsaved draft instead:", err);
      tab = {
        id,
        path,
        projectRoot,
        content: draft.content,
        originalContent: "",
        isDirty: true,
        diffVersion: 0,
        externalChange: true,
        includeInContext: true,
        baseHash: draft.baseHash,
      };
    }
    // Defensive dedupe: never append a second tab for an id that slipped
    // through (e.g. a concurrent call that raced ahead of this one).
    setEditorTabs((prev) => (prev.some((t) => t.id === id) ? prev : [...prev, tab]));
    setActiveEditorTabId(id);
  }, [fetchFileContent]);

  // Restore the previously open editor tabs once on mount. Content is
  // re-fetched; a file that no longer exists on disk simply fails its fetch
  // inside handleOpenFile (logged there) and its tab is dropped from the
  // persisted list by the next save.
  const restored = useRef(false);
  const restoreDone = useRef(false);
  useEffect(() => {
    if (restored.current) return;
    restored.current = true;
    const saved = loadEditorTabs();
    if (!saved || saved.tabs.length === 0) {
      restoreDone.current = true;
      return;
    }
    (async () => {
      // Sequential so the restored tab order matches what was persisted.
      for (const t of saved.tabs) {
        await handleOpenFile(t.path, t.projectRoot);
      }
      setActiveEditorTabId((prev) => {
        if (saved.activeId && openFileIdsRef.current.has(saved.activeId)) return saved.activeId;
        return prev;
      });
      restoreDone.current = true;
    })();
  }, [handleOpenFile]);

  // Persist the open tab list whenever it changes — but never before the
  // restore above has finished, or the initial empty state (and each
  // half-restored intermediate) would clobber the saved list.
  useEffect(() => {
    if (!restoreDone.current) return;
    saveEditorTabs(
      editorTabs.map((t) => ({ path: t.path, projectRoot: t.projectRoot })),
      activeEditorTabId,
      editorTabs.map((t) => t.id),
    );
  }, [editorTabs, activeEditorTabId]);

  // Latest tabs, readable from timers/listeners without re-subscribing.
  const editorTabsRef = useRef<EditorTab[]>([]);
  useEffect(() => {
    editorTabsRef.current = editorTabs;
  }, [editorTabs]);

  // Debounced draft persistence per tab (localStorage write per keystroke
  // would be wasteful on large files).
  const draftTimers = useRef<Map<string, number>>(new Map());

  const flushDraft = useCallback((id: string) => {
    const tab = editorTabsRef.current.find((t) => t.id === id);
    if (!tab) return;
    if (tab.isDirty) {
      saveEditorDraft(id, { content: tab.content, baseHash: tab.baseHash });
    } else {
      clearEditorDraft(id);
    }
  }, []);

  const handleEditorChange = useCallback((id: string, content: string) => {
    setEditorTabs((prev) =>
      prev.map((t) => (t.id === id ? { ...t, content, isDirty: content !== t.originalContent } : t)),
    );
    const timers = draftTimers.current;
    const pending = timers.get(id);
    if (pending !== undefined) window.clearTimeout(pending);
    timers.set(
      id,
      window.setTimeout(() => {
        timers.delete(id);
        flushDraft(id);
      }, 500),
    );
  }, [flushDraft]);

  // A reload/quit inside the debounce window would drop the last keystrokes —
  // flush every pending draft synchronously on the way out.
  useEffect(() => {
    const flushAll = () => {
      for (const [id, timer] of draftTimers.current) {
        window.clearTimeout(timer);
        flushDraft(id);
      }
      draftTimers.current.clear();
    };
    window.addEventListener("beforeunload", flushAll);
    return () => window.removeEventListener("beforeunload", flushAll);
  }, [flushDraft]);

  const handleSelectionChange = useCallback(
    (sel: { startLine: number; endLine: number } | null) => {
      setActiveEditorContext((prev) => {
        if (prev) {
          return { ...prev, selection: sel ?? undefined };
        }

        const activeId = activeEditorTabIdRef.current;
        if (!activeId) return null;
        const tab = editorTabsRef.current.find((t) => t.id === activeId);
        if (!tab) return null;
        return {
          path: tab.path,
          projectRoot: tab.projectRoot,
          selection: sel ?? undefined,
        };
      });
    },
    [],
  );

  useEffect(() => {
    if (editorTabs.length === 0) {
      setActiveEditorContext(null);
      return;
    }

    if (!activeEditorTabId) {
      setActiveEditorContext(null);
      return;
    }

    const tab = editorTabs.find((t) => t.id === activeEditorTabId);
    if (!tab) {
      setActiveEditorContext(null);
      return;
    }

    setActiveEditorContext((prev) => {
      if (prev?.path === tab.path && prev?.projectRoot === tab.projectRoot) return prev;
      return { path: tab.path, projectRoot: tab.projectRoot };
    });
  }, [activeEditorTabId, editorTabs]);

  const closeTabNow = useCallback((id: string) => {
    openFileIdsRef.current.delete(id);
    setEditorTabs((prev) => prev.filter((t) => t.id !== id));
    setActiveEditorTabId((prev) => {
      if (prev !== id) return prev;
      const remaining = editorTabs.filter((t) => t.id !== id);
      return remaining[0]?.id ?? null;
    });
  }, [editorTabs]);

  const requestCloseTab = useCallback(
    (id: string) => {
      const tab = editorTabs.find((t) => t.id === id);
      if (!tab) return;
      if (tab.isDirty) {
        setPendingClose({ id, path: tab.path });
      } else {
        closeTabNow(id);
      }
    },
    [editorTabs, closeTabNow],
  );

  const saveEditorTab = useCallback(
    async (id: string, opts?: { force?: boolean }) => {
      const tab = editorTabs.find((t) => t.id === id);
      if (!tab) return;
      try {
        const expectedHash = tab.baseHash;
        await api.saveFileContent(tab.path, tab.content, tab.projectRoot, expectedHash, opts?.force);
        setSaveError(null);
        clearEditorDraft(id);
        setEditorTabs((prev) =>
          prev.map((t) =>
            t.id === id
              ? { ...t, originalContent: t.content, isDirty: false, diffVersion: t.diffVersion + 1, externalChange: false, baseHash: hashContent(t.content) }
              : t,
          ),
        );
      } catch (err) {
        const isConflict = err instanceof Error && (err as any).status === 409;
        if (isConflict) {
          setEditorTabs((prev) => prev.map((t) => (t.id === id ? { ...t, externalChange: true } : t)));
          setSaveError("File changed on disk since you opened it — reload from disk or force-save to overwrite.");
        } else {
          setSaveError(err instanceof Error ? err.message : "Failed to save file");
        }
        throw err;
      }
    },
    [editorTabs],
  );

  const forceSaveEditorTab = useCallback(
    async (id: string) => saveEditorTab(id, { force: true }),
    [saveEditorTab],
  );

  const confirmSaveAndClose = useCallback(async () => {
    if (!pendingClose) return;
    try {
      await saveEditorTab(pendingClose.id);
      closeTabNow(pendingClose.id);
      setPendingClose(null);
    } catch {
      // saveError is already set by saveEditorTab; keep the dialog open so
      // the user can retry or fall back to Discard/Cancel.
    }
  }, [pendingClose, saveEditorTab, closeTabNow]);

  const confirmDiscardAndClose = useCallback(() => {
    if (!pendingClose) return;
    closeTabNow(pendingClose.id);
    setPendingClose(null);
    setSaveError(null);
  }, [pendingClose, closeTabNow]);

  const cancelClose = useCallback(() => {
    setPendingClose(null);
    setSaveError(null);
  }, []);

  // Discards in-editor state (and any draft) in favour of the current on-disk
  // content — the "Reload from disk" action on the external-change banner.
  const reloadTabFromDisk = useCallback(
    async (id: string) => {
      const tab = editorTabsRef.current.find((t) => t.id === id);
      if (!tab) return;
      try {
        const disk = await fetchFileContent(tab.path, tab.projectRoot);
        clearEditorDraft(id);
        setEditorTabs((prev) =>
          prev.map((t) =>
            t.id === id
              ? { ...t, content: disk, originalContent: disk, isDirty: false, diffVersion: t.diffVersion + 1, externalChange: false, baseHash: hashContent(disk) }
              : t,
          ),
        );
      } catch (err) {
        console.error("Failed to reload file from disk:", err);
      }
    },
    [fetchFileContent],
  );

  const dismissExternalChange = useCallback((id: string) => {
    setEditorTabs((prev) => prev.map((t) => (t.id === id ? { ...t, externalChange: false } : t)));
  }, []);

  const closeTabsForPaths = useCallback((paths: string[], projectRoot?: string) => {
    const norm = (p: string) => p.replace(/\/+$/, "");
    const targets = paths.map(norm);
    setEditorTabs((prev) => {
      const keep = prev.filter((t) => {
        if ((t.projectRoot ?? "") !== (projectRoot ?? "")) return true;
        const tp = norm(t.path);
        for (const del of targets) {
          if (tp === del || tp.startsWith(del + "/")) return false;
        }
        return true;
      });
      // Drop orphaned open ids.
      const keepIds = new Set(keep.map((t) => t.id));
      for (const id of Array.from(openFileIdsRef.current)) {
        if (!keepIds.has(id)) openFileIdsRef.current.delete(id);
      }
      return keep;
    });
    setActiveEditorTabId((prev) => {
      if (prev && openFileIdsRef.current.has(prev)) return prev;
      // Active was removed — will be reconciled by render via remaining tabs.
      return prev;
    });
  }, []);

  const renameTabPath = useCallback((oldPath: string, newPath: string, projectRoot?: string) => {
    const normOld = oldPath.replace(/\/+$/, "");
    const normNew = newPath.replace(/\/+$/, "");
    setEditorTabs((prev) =>
      prev.map((t) => {
        if ((t.projectRoot ?? "") !== (projectRoot ?? "")) return t;
        if (t.path === normOld) return { ...t, path: normNew, id: `editor-${projectRoot ? `${projectRoot}::${normNew}` : normNew}` };
        if (t.path.startsWith(normOld + "/")) {
          const suffix = t.path.slice(normOld.length);
          const np = normNew + suffix;
          return { ...t, path: np, id: `editor-${projectRoot ? `${projectRoot}::${np}` : np}` };
        }
        return t;
      }),
    );
  }, []);

  // Watch all open tabs for modifications made outside the app: re-fetch
  // each file every 10s (and on window focus / visibility, the moment
  // users typically come back from an external editor). A clean tab
  // silently follows the disk; a dirty tab keeps the user's edits,
  // rebases originalContent onto the new disk state, and raises the
  // externalChange banner. This covers inactive tabs too, so switching
  // back never shows stale content.
  useEffect(() => {
    let cancelled = false;

    const checkOne = async (tab: EditorTab) => {
      try {
        const disk = await fetchFileContent(tab.path, tab.projectRoot);
        if (cancelled) return;
        // Use baseHash for change detection so dirty tabs that haven't rebased
        // don't compare against a stale originalContent that was already
        // updated to the new disk.
        const diskHash = hashContent(disk);
        if (diskHash === tab.baseHash) return;
        setEditorTabs((prev) =>
          prev.map((t) => {
            if (t.id !== tab.id || hashContent(disk) === t.baseHash) return t;
            if (!t.isDirty) {
              return { ...t, content: disk, originalContent: disk, baseHash: diskHash, diffVersion: t.diffVersion + 1 };
            }
            // Dirty: if external edit happens to match the buffer, resolve
            // to clean; otherwise keep the user's edits and flag conflict
            // without rebasing baseHash/originalContent so the save guard 409s.
            if (t.content === disk) {
              return { ...t, originalContent: disk, baseHash: diskHash, isDirty: false, externalChange: false, diffVersion: t.diffVersion + 1 };
            }
            return { ...t, externalChange: true };
          }),
        );
      } catch (err) {
        console.error("External-change check failed (file unreadable):", err);
        if (!cancelled) {
          setEditorTabs((prev) =>
            prev.map((t) => (t.id === tab.id && t.isDirty ? { ...t, externalChange: true } : t)),
          );
        }
      }
    };

    const checkAll = async () => {
      if (document.hidden) return;
      const tabs = editorTabsRef.current;
      if (tabs.length === 0) return;
      // Sequential to avoid thundering reads; parallel is fine too but
      // sequential keeps server contention low when many tabs are open.
      for (const tab of tabs) {
        if (cancelled) break;
        // eslint-disable-next-line no-await-in-loop
        await checkOne(tab);
      }
    };

    const interval = window.setInterval(checkAll, 10_000);
    window.addEventListener("focus", checkAll);
    document.addEventListener("visibilitychange", checkAll);
    return () => {
      cancelled = true;
      window.clearInterval(interval);
      window.removeEventListener("focus", checkAll);
      document.removeEventListener("visibilitychange", checkAll);
    };
  }, [fetchFileContent]);

  // Also re-check the newly activated tab immediately when switching,
  // so the user never briefly sees stale content before the next poll.
  useEffect(() => {
    if (!activeEditorTabId) return;
    let cancelled = false;
    (async () => {
      if (document.hidden) return;
      const tab = editorTabsRef.current.find((t) => t.id === activeEditorTabId);
      if (!tab) return;
      try {
        const disk = await fetchFileContent(tab.path, tab.projectRoot);
        if (cancelled) return;
        const diskHash = hashContent(disk);
        if (diskHash === tab.baseHash) return;
        setEditorTabs((prev) =>
          prev.map((t) => {
            if (t.id !== tab.id || hashContent(disk) === t.baseHash) return t;
            if (!t.isDirty) {
              return { ...t, content: disk, originalContent: disk, baseHash: diskHash, diffVersion: t.diffVersion + 1 };
            }
            if (t.content === disk) {
              return { ...t, originalContent: disk, baseHash: diskHash, isDirty: false, externalChange: false, diffVersion: t.diffVersion + 1 };
            }
            return { ...t, externalChange: true };
          }),
        );
      } catch {
        // Swallow; the periodic check will flag if needed.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [activeEditorTabId, fetchFileContent]);

  const toggleIncludeInContext = useCallback((id: string) => {
    setEditorTabs((prev) =>
      prev.map((t) => (t.id === id ? { ...t, includeInContext: !t.includeInContext } : t)),
    );
  }, []);

  return {
    editorTabs,
    activeEditorTabId,
    setActiveEditorTabId,
    handleOpenFile,
    reloadTabFromDisk,
    dismissExternalChange,
    handleEditorChange,
    handleSelectionChange,
    toggleIncludeInContext,
    closeTabsForPaths,
    renameTabPath,
    activeEditorContext,
    requestCloseTab,
    saveEditorTab,
    forceSaveEditorTab,
    pendingClose,
    confirmSaveAndClose,
    confirmDiscardAndClose,
    cancelClose,
    saveError,
  };
}
