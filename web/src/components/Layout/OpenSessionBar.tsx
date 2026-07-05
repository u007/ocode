import { useCallback } from "react";
import { useChatDispatch, useChatState } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { api } from "../../api/client";
import { X, List, Plus } from "lucide-react";

export default function OpenSessionBar() {
  const { state: projectState, openSessionTab, closeSessionTab, toggleSessionPicker } = useProjectState();
  const chatState = useChatState();
  const chatDispatch = useChatDispatch();
  const { tabs, activeTabId } = projectState;

  const handleTabClick = useCallback(async (sessionId: string, title: string) => {
    // Already active — no-op
    if (activeTabId === sessionId) return;

    // New session tabs (temp IDs starting with "new-") — just activate with empty chat
    if (sessionId.startsWith("new-")) {
      openSessionTab(sessionId, title);
      if (!chatState.sessionId) {
        // Only reset if no real session was created yet (first message not sent)
        chatDispatch({ type: "RESET" });
      }
      return;
    }

    openSessionTab(sessionId, title);
    try {
      const session = await api.getSession(sessionId);
      chatDispatch({ type: "SET_SESSION", sessionId });
      chatDispatch({ type: "SET_MESSAGES", messages: session.messages || [] });
    } catch (err) {
      console.error("Failed to load session:", err);
    }
  }, [activeTabId, openSessionTab, chatDispatch]);

  const handleCloseTab = useCallback((e: React.MouseEvent, tabId: string) => {
    e.stopPropagation();
    closeSessionTab(tabId);
    if (chatState.sessionId === tabId) {
      chatDispatch({ type: "RESET" });
    }
  }, [closeSessionTab, chatState.sessionId, chatDispatch]);

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
            <span className="max-w-28 truncate">{tab.title || tab.id.slice(0, 12)}</span>
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
          const tempId = `new-${Date.now()}`;
          openSessionTab(tempId, "New session");
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
