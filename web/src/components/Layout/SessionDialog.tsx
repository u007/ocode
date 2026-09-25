import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useChatDispatch } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { isNewSessionTabEmpty } from "../../lib/tabDrafts";
import { clearQueue } from "../../lib/tabQueue";
import { cancelLiveDeltas, closeSessionBackend } from "../../lib/sessionEvents";
import { prefetchSession } from "../../lib/sessionPrefetch";
import { isChildSessionId } from "../../lib/sessionId";
import { useListNavigation } from "../../hooks/useListNavigation";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { MessageSquare, Plus, X, Loader2, Check } from "lucide-react";

/** How many sessions render per page. The list infinite-scrolls: the first
 *  page paints instantly even for projects with thousands of sessions, and
 *  further rows stream in as the user scrolls. */
export const SESSION_DIALOG_PAGE_SIZE = 50;

export default function SessionDialog() {
  const { state: projectState, tabs, activeTabId, openSessionTab, closeSessionTab, toggleSessionPicker, openNewSessionTab, prefetchProjectSessions } = useProjectState();
  const chatDispatch = useChatDispatch();
  const { projectSessions, sessionsLoading, sessionPickerOpen, activeProject } = projectState;

  const [searchQuery, setSearchQuery] = useState("");
  const [pendingClose, setPendingClose] = useState<{ tabId: string; title: string } | null>(null);
  const [visibleCount, setVisibleCount] = useState(SESSION_DIALOG_PAGE_SIZE);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);

  // Auto-focus search input when dialog opens
  const handleOpenChange = useCallback((open: boolean) => {
    if (!open) {
      toggleSessionPicker();
      setSearchQuery("");
    }
  }, [toggleSessionPicker]);

  // Main sessions only — child ("context") sessions are subagent execution
  // detail, not resumable conversations (see lib/sessionId.ts).
  const mainSessions = useMemo(
    () => projectSessions.filter((s) => !isChildSessionId(s.id)),
    [projectSessions],
  );

  // Filter sessions by search query
  const filteredSessions = useMemo(() => {
    if (!searchQuery.trim()) return mainSessions;
    const q = searchQuery.toLowerCase();
    return mainSessions.filter(
      (s) => s.title?.toLowerCase().includes(q) || s.id.toLowerCase().includes(q)
    );
  }, [mainSessions, searchQuery]);

  // Reset the window whenever the query changes (a new search starts at page
  // one) or the dialog is (re)opened. A background list revalidation does NOT
  // reset it — the window is clamped instead, so the user keeps their place.
  useEffect(() => {
    if (sessionPickerOpen) setVisibleCount(SESSION_DIALOG_PAGE_SIZE);
  }, [sessionPickerOpen, searchQuery]);

  const visibleCountClamped = Math.min(visibleCount, filteredSessions.length);
  const visibleSessions = useMemo(
    () => filteredSessions.slice(0, visibleCountClamped),
    [filteredSessions, visibleCountClamped],
  );
  const hasMore = visibleCountClamped < filteredSessions.length;

  const loadMore = useCallback(() => {
    setVisibleCount((c) => c + SESSION_DIALOG_PAGE_SIZE);
  }, []);

  // Infinite scroll: load the next page when the sentinel below the last row
  // enters view. Guarded so jsdom (no IntersectionObserver) falls back to the
  // explicit "Load more" button rendered alongside the sentinel.
  useEffect(() => {
    if (!hasMore) return;
    const sentinel = sentinelRef.current;
    if (!sentinel || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) loadMore();
      },
      { root: listRef.current, rootMargin: "240px 0px" },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [hasMore, loadMore]);

  // The per-project session list is cached and revalidated in the background
  // on switch, so it can be slightly stale. Opening the picker is the one
  // moment that staleness is user-visible — force a revalidation here. The
  // cached list still renders immediately; the fresh one replaces it in place.
  useEffect(() => {
    if (!sessionPickerOpen || !activeProject) return;
    prefetchProjectSessions(activeProject, { force: true });
  }, [sessionPickerOpen, activeProject, prefetchProjectSessions]);

  // Open a session tab and switch to it. Message loading is handled centrally
  // by SessionTabSync (it watches activeTabId).
  const handleSessionClick = useCallback((sessionId: string, title: string) => {
    openSessionTab(sessionId, title);
    toggleSessionPicker();
    setSearchQuery("");
  }, [openSessionTab, toggleSessionPicker]);

  const sessionNavIds = useMemo(
    () => visibleSessions.map((session) => `session:${session.id}`),
    [visibleSessions],
  );
  const sessionNavigation = useListNavigation({
    itemIds: sessionNavIds,
    onActivate: (index) => {
      const session = visibleSessions[index];
      if (session) handleSessionClick(session.id, session.title);
    },
    inputRef,
    resetKey: `${sessionPickerOpen}|${activeProject?.path ?? ""}|${activeProject?.host ?? ""}`,
    ...(hasMore ? { hasMore: true, onReachEnd: loadMore } : {}),
  });

  // Create a new session
  const handleNewSession = useCallback(() => {
    openNewSessionTab(isNewSessionTabEmpty(activeTabId));
    toggleSessionPicker();
    setSearchQuery("");
  }, [activeTabId, openNewSessionTab, toggleSessionPicker]);

  // Close a session tab immediately. Middle-click uses this path; the X button
  // goes through the confirmation flow below.
  const closeTabNow = useCallback((tabId: string) => {
    closeSessionBackend(tabId);
    closeSessionTab(tabId);
    cancelLiveDeltas(tabId);
    chatDispatch({ type: "RESET", sessionId: tabId });
    clearQueue(tabId);
  }, [closeSessionTab, chatDispatch]);

  const handleCloseTab = useCallback((e: React.MouseEvent, tabId: string) => {
    e.stopPropagation();
    closeTabNow(tabId);
  }, [closeTabNow]);

  const requestCloseTab = useCallback((e: React.MouseEvent, tabId: string, title: string) => {
    e.stopPropagation();
    setPendingClose({ tabId, title });
  }, []);

  const confirmPendingClose = useCallback(() => {
    if (!pendingClose) return;
    const { tabId } = pendingClose;
    setPendingClose(null);
    closeTabNow(tabId);
  }, [pendingClose, closeTabNow]);

  const cancelPendingClose = useCallback(() => setPendingClose(null), []);

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
      <DialogContent
        className="sm:max-w-lg max-h-[80vh] flex flex-col p-0 gap-0"
        onKeyDown={sessionNavigation.onKeyDown}
      >
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
        <div
          ref={listRef}
          className="flex-1 overflow-y-auto px-4 pb-4 min-h-0 max-h-[50vh]"
        >
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
              {visibleSessions.map((session, index) => {
                const navProps = sessionNavigation.getItemProps(index);
                const open = isTabOpen(session.id);
                const current = isCurrentSession(session.id);
                const loading = false;
                return (
                  <button
                    key={session.id}
                    {...navProps}
                    role="button"
                    onClick={() => handleSessionClick(session.id, session.title)}
                    disabled={loading}
                    // Warm the transcript before the click lands so the tab
                    // opens without a cold fetch + loading spinner.
                    onMouseEnter={() => prefetchSession(session.id, activeProject.host)}
                    onFocus={(event) => {
                      navProps.onFocus(event);
                      prefetchSession(session.id, activeProject.host);
                    }}
                    onMouseDown={(e) => {
                      if (e.button === 1 && open) {
                        e.preventDefault(); // suppress middle-click autoscroll
                        handleCloseTab(e, session.id);
                      }
                    }}
                    className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-md text-left text-sm transition-colors ${
                      current
                        ? "bg-accent text-accent-foreground"
                        : "hover:bg-muted text-foreground"
                    } ${sessionNavigation.isActive(index) ? "ring-2 ring-inset ring-primary/60" : ""} ${loading ? "opacity-60" : ""}`}
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
                        aria-label={`Close ${session.title || session.id}`}
                        className="shrink-0 p-1 rounded-md hover:bg-muted-foreground/20 text-muted-foreground hover:text-foreground transition-colors"
                        onClick={(e) => requestCloseTab(e, session.id, session.title || session.id)}
                        onKeyDown={(e) => {
                          e.stopPropagation();
                          if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault();
                            requestCloseTab(e as unknown as React.MouseEvent, session.id, session.title || session.id);
                          }
                        }}
                      >
                        <X className="w-3.5 h-3.5" />
                      </span>
                    )}
                  </button>
                );
              })}

              {/* Infinite-scroll sentinel + fallback for environments without
                  IntersectionObserver (e.g. jsdom in tests). */}
              {hasMore && (
                <div ref={sentinelRef} className="pt-2">
                  <button
                    type="button"
                    onClick={loadMore}
                    className="w-full rounded-md px-3 py-2 text-xs text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
                  >
                    Load more ({filteredSessions.length - visibleCountClamped} remaining)
                  </button>
                </div>
              )}
            </div>
          )}
        </div>

        {pendingClose && (
          <Dialog open onOpenChange={(open) => !open && cancelPendingClose()}>
            <DialogContent className="max-w-sm">
              <DialogHeader>
                <DialogTitle className="text-sm">Close session tab?</DialogTitle>
              </DialogHeader>
              <p className="text-sm text-muted-foreground">
                Close <span className="font-medium text-foreground">{pendingClose.title}</span>? This cannot be undone.
              </p>
              <DialogFooter className="gap-2">
                <Button variant="ghost" onClick={cancelPendingClose}>Cancel</Button>
                <Button variant="destructive" onClick={confirmPendingClose}>Close tab</Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        )}

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
