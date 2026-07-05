import { useCallback, useMemo, useRef, useState } from "react";
import { useChatState, useChatDispatch } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { api } from "../../api/client";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "../ui/dialog";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { MessageSquare, Plus, X, Loader2, Check } from "lucide-react";

export default function SessionDialog() {
  const { state: projectState, openSessionTab, closeSessionTab, toggleSessionPicker } = useProjectState();
  const chatState = useChatState();
  const chatDispatch = useChatDispatch();
  const { projectSessions, sessionsLoading, tabs, activeTabId, sessionPickerOpen, activeProject } = projectState;

  const [searchQuery, setSearchQuery] = useState("");
  const [loadingId, setLoadingId] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Auto-focus search input when dialog opens
  const handleOpenChange = useCallback((open: boolean) => {
    if (!open) {
      toggleSessionPicker();
      setSearchQuery("");
    }
  }, [toggleSessionPicker]);

  // Filter sessions by search query
  const filteredSessions = useMemo(() => {
    if (!searchQuery.trim()) return projectSessions;
    const q = searchQuery.toLowerCase();
    return projectSessions.filter(
      (s) => s.title?.toLowerCase().includes(q) || s.id.toLowerCase().includes(q)
    );
  }, [projectSessions, searchQuery]);

  // Open a session tab and switch to it
  const handleSessionClick = useCallback(async (sessionId: string, title: string) => {
    setLoadingId(sessionId);
    try {
      openSessionTab(sessionId, title);
      const session = await api.getSession(sessionId);
      chatDispatch({ type: "SET_SESSION", sessionId });
      chatDispatch({ type: "SET_MESSAGES", messages: session.messages || [] });
      toggleSessionPicker();
      setSearchQuery("");
    } catch (err) {
      console.error("Failed to load session:", err);
    } finally {
      setLoadingId(null);
    }
  }, [openSessionTab, chatDispatch, toggleSessionPicker]);

  // Create a new session
  const handleNewSession = useCallback(() => {
    chatDispatch({ type: "RESET" });
    toggleSessionPicker();
    setSearchQuery("");
  }, [chatDispatch, toggleSessionPicker]);

  // Close a session tab
  const handleCloseTab = useCallback((e: React.MouseEvent, tabId: string) => {
    e.stopPropagation();
    closeSessionTab(tabId);
    if (chatState.sessionId === tabId) {
      chatDispatch({ type: "RESET" });
    }
  }, [closeSessionTab, chatState.sessionId, chatDispatch]);

  // Check if a session is currently open as a tab
  const isTabOpen = useCallback((sessionId: string) => {
    return tabs.some((t) => t.id === sessionId);
  }, [tabs]);

  const isCurrentSession = useCallback((sessionId: string) => {
    return activeTabId === sessionId;
  }, [activeTabId]);

  if (!activeProject) return null;

  return (
    <Dialog open={sessionPickerOpen} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-lg max-h-[80vh] flex flex-col p-0 gap-0">
        <DialogHeader className="px-4 pt-4 pb-2">
          <DialogTitle className="text-sm font-semibold flex items-center gap-2">
            <MessageSquare className="w-4 h-4" />
            Sessions — {activeProject.name}
          </DialogTitle>
        </DialogHeader>

        {/* Search */}
        <div className="px-4 pb-3">
          <Input
            ref={inputRef}
            placeholder="Search sessions..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="h-9 text-sm"
            autoFocus
          />
        </div>

        {/* Session list */}
        <div className="flex-1 overflow-y-auto px-4 pb-4 min-h-0 max-h-[50vh]">
          {sessionsLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="w-5 h-5 animate-spin text-muted-foreground" />
            </div>
          ) : filteredSessions.length === 0 ? (
            <div className="text-center py-8 text-sm text-muted-foreground">
              {searchQuery ? "No sessions match your search" : "No sessions yet"}
            </div>
          ) : (
            <div className="space-y-1">
              {filteredSessions.map((session) => {
                const open = isTabOpen(session.id);
                const current = isCurrentSession(session.id);
                const loading = loadingId === session.id;
                return (
                  <button
                    key={session.id}
                    onClick={() => handleSessionClick(session.id, session.title)}
                    disabled={loading}
                    className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-left text-sm transition-colors ${
                      current
                        ? "bg-accent text-accent-foreground"
                        : "hover:bg-muted text-foreground"
                    } ${loading ? "opacity-60" : ""}`}
                  >
                    {/* Status indicator */}
                    <span className="shrink-0 w-4 flex items-center justify-center">
                      {loading ? (
                        <Loader2 className="w-3.5 h-3.5 animate-spin" />
                      ) : current ? (
                        <Check className="w-3.5 h-3.5 text-primary" />
                      ) : open ? (
                        <MessageSquare className="w-3.5 h-3.5 text-muted-foreground" />
                      ) : (
                        <MessageSquare className="w-3.5 h-3.5 text-muted-foreground/50" />
                      )}
                    </span>

                    {/* Session info */}
                    <span className="flex-1 min-w-0">
                      <span className="block truncate font-medium">
                        {session.title || session.id.slice(0, 16)}
                      </span>
                      <span className="block text-xs text-muted-foreground truncate">
                        {session.updated_at
                          ? new Date(session.updated_at).toLocaleString(undefined, {
                              month: "short",
                              day: "numeric",
                              hour: "2-digit",
                              minute: "2-digit",
                            })
                          : ""}
                      </span>
                    </span>

                    {/* Close button (only for open tabs) */}
                    {open && (
                      <span
                        role="button"
                        tabIndex={0}
                        className="shrink-0 p-1 rounded-md hover:bg-muted-foreground/20 text-muted-foreground hover:text-foreground transition-colors"
                        onClick={(e) => handleCloseTab(e, session.id)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault();
                            handleCloseTab(e as unknown as React.MouseEvent, session.id);
                          }
                        }}
                      >
                        <X className="w-3.5 h-3.5" />
                      </span>
                    )}
                  </button>
                );
              })}
            </div>
          )}
        </div>

        {/* Footer: New Session */}
        <div className="px-4 py-3 border-t border-border flex items-center gap-2">
          <Button
            variant="default"
            size="sm"
            className="h-8 text-xs gap-1.5"
            onClick={handleNewSession}
          >
            <Plus className="w-3.5 h-3.5" />
            New Session
          </Button>
          {tabs.length > 0 && (
            <span className="text-xs text-muted-foreground ml-auto">
              {tabs.length} open tab{tabs.length > 1 ? "s" : ""}
            </span>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
