import { useCallback, useRef, useState } from "react";
import { api, apiPath, authHeaders } from "../api/client";

export interface EditorTab {
  id: string;
  path: string;
  content: string;
  originalContent: string;
  isDirty: boolean;
}

export interface UseEditorTabsResult {
  editorTabs: EditorTab[];
  activeTab: string;
  setActiveTab: (tab: string) => void;
  handleOpenFile: (path: string) => Promise<void>;
  handleEditorChange: (id: string, content: string) => void;
  requestCloseTab: (id: string) => void;
  saveEditorTab: (id: string) => Promise<void>;
  pendingClose: { id: string; path: string } | null;
  confirmSaveAndClose: () => Promise<void>;
  confirmDiscardAndClose: () => void;
  cancelClose: () => void;
  saveError: string | null;
}

export function useEditorTabs(initialTab = "chat"): UseEditorTabsResult {
  const [editorTabs, setEditorTabs] = useState<EditorTab[]>([]);
  const [activeTab, setActiveTab] = useState(initialTab);
  const [pendingClose, setPendingClose] = useState<{ id: string; path: string } | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const openFileIdsRef = useRef<Set<string>>(new Set());

  const handleOpenFile = useCallback(async (path: string) => {
    const id = `editor-${path}`;
    if (openFileIdsRef.current.has(id)) {
      setActiveTab(id);
      return;
    }
    try {
      const res = await fetch(apiPath(`/api/files/content?path=${encodeURIComponent(path)}`), {
        headers: authHeaders(),
      });
      if (!res.ok) throw new Error("Failed to load file");
      const data = await res.json();
      openFileIdsRef.current.add(id);
      setEditorTabs((prev) => [
        ...prev,
        { id, path, content: data.content, originalContent: data.content, isDirty: false },
      ]);
      setActiveTab(id);
    } catch (err) {
      console.error("Failed to open file:", err);
    }
  }, []);

  const handleEditorChange = useCallback((id: string, content: string) => {
    setEditorTabs((prev) =>
      prev.map((t) => (t.id === id ? { ...t, content, isDirty: content !== t.originalContent } : t)),
    );
  }, []);

  const closeTabNow = useCallback((id: string) => {
    openFileIdsRef.current.delete(id);
    setEditorTabs((prev) => prev.filter((t) => t.id !== id));
    setActiveTab((prev) => (prev === id ? "files" : prev));
  }, []);

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
    async (id: string) => {
      const tab = editorTabs.find((t) => t.id === id);
      if (!tab) return;
      try {
        await api.saveFileContent(tab.path, tab.content);
        setSaveError(null);
        setEditorTabs((prev) =>
          prev.map((t) => (t.id === id ? { ...t, originalContent: t.content, isDirty: false } : t)),
        );
      } catch (err) {
        setSaveError(err instanceof Error ? err.message : "Failed to save file");
        throw err;
      }
    },
    [editorTabs],
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

  return {
    editorTabs,
    activeTab,
    setActiveTab,
    handleOpenFile,
    handleEditorChange,
    requestCloseTab,
    saveEditorTab,
    pendingClose,
    confirmSaveAndClose,
    confirmDiscardAndClose,
    cancelClose,
    saveError,
  };
}
