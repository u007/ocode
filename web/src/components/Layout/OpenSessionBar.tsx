import { useCallback } from "react";
import { useChatDispatch, useChatState } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { X, List, Plus, Loader2 } from "lucide-react";

export default function OpenSessionBar() {
  const { state: projectState, tabs, activeTabId, openSessionTab, closeSessionTab, toggleSessionPicker, openNewSessionTab } = useProjectState();
  const chatState = useChatState();
  const chatDispatch = useChatDispatch();

  const handleTabClick = useCallback((sessionId: string, title: string) => {
    // Already active — no-op
    if (activeTabId === sessionId) return;
    // Message loading for real sessions is handled centrally by
    // SessionTabSync (it watches activeTabId). Just activate the tab.
    openSessionTab(sessionId, title);
  }, [activeTabId, openSessionTab]);

  const handleCloseTab = useCallback((e: React.MouseEvent, tabId: string) => {
    e.stopPropagation();
    closeSessionTab(tabId);
    if (chatState.sessionId === tabId) {
      chatDispatch({ type: "RESET" });
    }
  }, [closeSessionTab, chatState.sessionId, chatDispatch]);

  // A real session tab is "loading" while it's active but its messages are not
  // yet in the chat store (SessionTabSync fetch in flight).
  const isLoadingTab = (tabId: string) =>
    activeTabId === tabId && !tabId.startsWith("new-") && chatState.sessionId !== tabId;

  // Always show when a project is active, even with zero tabs
  if (!projectState.activeProject) {
    return null;
  }

  return (
    <div className="flex items-center h-8 px-2 gap-0.5 bg-zinc-900 border-b border-zinc-700 overflow-x-auto scrollbar-none">
      {tabs.map((tab) => {
        const isActive = activeTabId === tab.id;
        return (
          <div
            key={tab.id}
            className={`flex items-center gap-1 px-2 py-1 rounded-t text-xs cursor-pointer shrink-0 transition-colors ${
              isActive
                ? "bg-zinc-800 text-zinc-100 border-t border-t-blue-500"
                : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/60"
            }`}
            onClick={() => handleTabClick(tab.id, tab.title)}
          >
            {isLoadingTab(tab.id) && (
              <Loader2 className="w-3 h-3 animate-spin shrink-0" />
            )}
            <span className="max-w-28 truncate" title={tab.title || tab.id}>{tab.title || tab.id.slice(0, 12)}</span>
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
          openNewSessionTab();
          chatDispatch({ type: "RESET" });
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
