import { useCallback, useState } from "react";
import { useChatDispatch, useChatState, getSessionSlice } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { isNewSessionTabEmpty } from "../../lib/tabDrafts";
import { clearQueue } from "../../lib/tabQueue";
import { api } from "../../api/client";
import type { SessionSlice } from "../../stores/chatStore";
import { X, List, Plus, Loader2 } from "lucide-react";

// While a tab's session has an in-flight turn, show what it's doing instead
// of its (possibly stale) title. Reverts to the title once idle.
function activeProcessLabel(slice: SessionSlice): string | null {
  if (!slice.turnActive) return null;
  for (let i = slice.live.length - 1; i >= 0; i--) {
    const part = slice.live[i];
    if (part.kind === "tool") return part.command || part.tool;
  }
  return "Running…";
}

export default function OpenSessionBar() {
  const { state: projectState, tabs, activeTabId, openSessionTab, closeSessionTab, toggleSessionPicker, openNewSessionTab, dispatch: projectDispatch } = useProjectState();
  const chatState = useChatState();
  const chatDispatch = useChatDispatch();
  const [editingTabId, setEditingTabId] = useState<string | null>(null);
  const [editValue, setEditValue] = useState("");

  const handleTabClick = useCallback((sessionId: string, title: string) => {
    // Already active — no-op
    if (activeTabId === sessionId) return;
    // Message loading for real sessions is handled centrally by
    // SessionTabSync (it watches activeTabId). Just activate the tab.
    openSessionTab(sessionId, title);
  }, [activeTabId, openSessionTab]);

  const startRename = useCallback((tabId: string, currentTitle: string) => {
    setEditingTabId(tabId);
    setEditValue(currentTitle);
  }, []);

  const commitRename = useCallback((tabId: string) => {
    const title = editValue.trim();
    setEditingTabId(null);
    if (!title) return;
    projectDispatch({ type: "UPDATE_TAB_TITLE", id: tabId, title });
    // Unsaved ("new-") tabs have no session on the server yet — persist the
    // title locally only; it's sent once the session is created.
    if (tabId.startsWith("new-")) return;
    api.setSessionTitle(tabId, title).catch((err) => {
      console.error("failed to save renamed tab title", err);
    });
  }, [editValue, projectDispatch]);

  const handleCloseTab = useCallback((e: React.MouseEvent, tabId: string) => {
    e.stopPropagation();
    closeSessionTab(tabId);
    chatDispatch({ type: "RESET", sessionId: tabId });
    clearQueue(tabId);
  }, [closeSessionTab, chatDispatch]);

  // A real session tab is "loading" while its slice hasn't finished its first
  // fetch yet (ChatPanel's own initial-load effect sets `initialized`).
  const isLoadingTab = (tabId: string) =>
    !tabId.startsWith("new-") && !getSessionSlice(chatState, tabId).initialized;

  // Always show when a project is active, even with zero tabs
  if (!projectState.activeProject) {
    return null;
  }

  return (
    <div className="flex items-center h-8 px-2 gap-0.5 bg-zinc-900 border-b border-zinc-700 overflow-x-auto scrollbar-hide">
      {tabs.map((tab) => {
        const isActive = activeTabId === tab.id;
        const slice = getSessionSlice(chatState, tab.id);
        const hasPending = !isActive && (slice.pendingPermission !== null || slice.pendingQuestion !== null);
        const processLabel = activeProcessLabel(slice);
        const displayTitle = processLabel ?? (tab.title || tab.id.slice(0, 12));
        const isEditing = editingTabId === tab.id;
        return (
          <div
            key={tab.id}
            className={`flex items-center gap-1 px-2 py-1 rounded-t text-xs cursor-pointer shrink-0 transition-colors ${
              isActive
                ? "bg-zinc-800 text-zinc-100 border-t border-t-blue-500"
                : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/60"
            }`}
            onClick={() => handleTabClick(tab.id, tab.title)}
            onMouseDown={(e) => {
              if (e.button === 1) {
                e.preventDefault(); // suppress middle-click autoscroll
                handleCloseTab(e, tab.id);
              }
            }}
          >
            {isLoadingTab(tab.id) && (
              <Loader2 className="w-3 h-3 animate-spin shrink-0" />
            )}
            {hasPending && (
              <span
                className="h-1.5 w-1.5 rounded-full bg-amber-400 shrink-0"
                title="Waiting for a response in this tab"
              />
            )}
            {isEditing ? (
              <input
                autoFocus
                value={editValue}
                onChange={(e) => setEditValue(e.target.value)}
                onClick={(e) => e.stopPropagation()}
                onBlur={() => commitRename(tab.id)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") commitRename(tab.id);
                  else if (e.key === "Escape") setEditingTabId(null);
                }}
                className="max-w-28 w-24 bg-zinc-950 text-zinc-100 rounded px-1 outline-none border border-blue-500"
              />
            ) : (
              <span
                className="max-w-28 truncate"
                title={processLabel ? `${displayTitle} (double-click to rename)` : "Double-click to rename"}
                onDoubleClick={(e) => {
                  e.stopPropagation();
                  startRename(tab.id, tab.title || "");
                }}
              >
                {displayTitle}
              </span>
            )}
            <span
              role="button"
              tabIndex={0}
              className="p-0.5 rounded hover:bg-zinc-700 text-zinc-500 hover:text-zinc-300 transition-colors shrink-0"
              onClick={(e) => handleCloseTab(e, tab.id)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  handleCloseTab(e as unknown as React.MouseEvent, tab.id);
                }
              }}
            >
              <X className="w-3 h-3" />
            </span>
          </div>
        );
      })}

      {/* New session button */}
      <button
        onClick={() => {
          // Add a fresh tab and keep the current (running) one — unless the
          // active tab is a completely empty new-session tab (no draft, no
          // session yet), in which case reuse it instead of duplicating.
          openNewSessionTab(isNewSessionTabEmpty(activeTabId));
        }}
        className="flex items-center gap-1 px-2 py-1 rounded text-xs text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors shrink-0"
        title="New session"
      >
        <Plus className="w-3.5 h-3.5" />
        <span className="hidden sm:inline">New</span>
      </button>

      {/* All sessions button */}
      <button
        onClick={toggleSessionPicker}
        className="flex items-center gap-1 px-2 py-1 rounded text-xs text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors shrink-0"
        title="Browse all sessions"
      >
        <List className="w-3.5 h-3.5" />
        <span className="hidden sm:inline">All sessions</span>
      </button>
    </div>
  );
}
