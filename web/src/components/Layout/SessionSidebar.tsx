import { useState, useEffect, useRef } from "react";
import { api } from "../../api/client";
import { useProjectState } from "../../stores/projectStore";
import type { SessionInfo } from "../../api/types";
import { PanelLeftClose, PanelLeft, Plus, MessageSquare, Loader2, Copy, Check } from "lucide-react";

// Copy text to the clipboard with a fallback for non-secure contexts,
// matching the pattern used in AssetsPanel.
async function copyText(text: string): Promise<void> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return;
    }
  } catch {
    // fall through to the legacy path
  }
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.style.position = "absolute";
  ta.style.left = "-9999px";
  document.body.appendChild(ta);
  ta.select();
  try {
    document.execCommand("copy");
  } finally {
    document.body.removeChild(ta);
  }
}

interface Props {
  isOpen: boolean;
  onToggle: () => void;
  isMobile?: boolean;
}

const PAGE_SIZE = 30;

// SessionList renders the scrollable list of session entries. Extracted from
// SessionSidebar so the mobile and desktop branches can share one definition
// of "what a session row looks like" — the two branches previously duplicated
// the row markup, and any future change to the row had to be made twice.
// `onSelect` is the click handler; the mobile overlay chains onToggle() to
// close the sidebar, while the desktop branch passes its own handler.
function SessionList({
  sessions,
  onSelect,
  loadingId,
  hasMore,
  loadingMore,
  onLoadMore,
}: {
  sessions: SessionInfo[];
  onSelect: (id: string) => void;
  loadingId: string | null;
  hasMore: boolean;
  loadingMore: boolean;
  onLoadMore: () => void;
}) {
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const copyTimer = useRef<number | null>(null);

  const handleCopy = (id: string) => {
    void copyText(id);
    setCopiedId(id);
    if (copyTimer.current !== null) {
      window.clearTimeout(copyTimer.current);
    }
    copyTimer.current = window.setTimeout(() => setCopiedId(null), 1500);
  };

  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    if (!hasMore || loadingMore) return;
    const el = e.currentTarget;
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 100) {
      onLoadMore();
    }
  };

  if (sessions.length === 0) {
    return null;
  }
  return (
    <div className="flex-1 overflow-y-auto" onScroll={handleScroll}>
      {sessions.map((session) => {
        const loading = loadingId === session.id;
        const copied = copiedId === session.id;
        return (
          <div
            key={session.id}
            className={`group flex items-center border-b border-zinc-800 transition-colors hover:bg-zinc-800 ${loading ? "opacity-60" : ""}`}
          >
            <button
              onClick={() => onSelect(session.id)}
              disabled={loading}
              className="flex-1 text-left px-4 py-3 text-sm text-zinc-400 transition-colors"
            >
              <div className="flex items-center gap-2">
                {loading ? (
                  <Loader2 className="w-4 h-4 flex-shrink-0 text-zinc-500 animate-spin" />
                ) : (
                  <MessageSquare className="w-4 h-4 flex-shrink-0 text-zinc-600" />
                )}
                <div className="min-w-0">
                  <div className="truncate font-medium">
                    {session.title || session.id}
                  </div>
                  <div className="truncate text-xs text-zinc-600">
                    {new Date(session.updated_at).toLocaleDateString()}
                  </div>
                </div>
              </div>
            </button>
            <button
              onClick={() => handleCopy(session.id)}
              disabled={loading}
              title="Copy session ID"
              className="mr-2 p-1.5 rounded-md text-zinc-600 hover:text-zinc-200 transition-colors"
            >
              {copied ? (
                <Check className="w-3.5 h-3.5 text-emerald-500" />
              ) : (
                <Copy className="w-3.5 h-3.5" />
              )}
            </button>
          </div>
        );
      })}
      {loadingMore && (
        <div className="flex items-center justify-center gap-2 py-3 text-xs text-zinc-600">
          <Loader2 className="w-3.5 h-3.5 animate-spin" />
          Loading more…
        </div>
      )}
    </div>
  );
}

export default function SessionSidebar({ isOpen, onToggle, isMobile }: Props) {
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [total, setTotal] = useState(0);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadingId, setLoadingId] = useState<string | null>(null);
  const { activeTabId: sessionId, openSessionTab, openNewSessionTab } = useProjectState();

  const fetchSessions = () => {
    api
      .listSessions({ limit: PAGE_SIZE, offset: 0 })
      .then((r) => {
        setSessions(r.sessions);
        setTotal(r.total);
      })
      .catch(console.error);
  };

  const loadMore = () => {
    if (loadingMore || sessions.length >= total) return;
    setLoadingMore(true);
    api
      .listSessions({ limit: PAGE_SIZE, offset: sessions.length })
      .then((r) => {
        setSessions((prev) => [...prev, ...r.sessions]);
        setTotal(r.total);
      })
      .catch(console.error)
      .finally(() => setLoadingMore(false));
  };

  useEffect(() => {
    fetchSessions();
  }, []);

  useEffect(() => {
    if (isOpen) {
      fetchSessions();
    }
  }, [isOpen]);

  useEffect(() => {
    if (sessionId) {
      fetchSessions();
    }
  }, [sessionId]);

  const handleNewSession = () => {
    openNewSessionTab();
  };

  const handleSelectSession = async (id: string) => {
    setLoadingId(id);
    try {
      const session = await api.getSession(id);
      openSessionTab(id, session.title || id);
    } catch (err) {
      console.error("Failed to load session:", err);
    } finally {
      setLoadingId(null);
    }
  };

  if (isMobile) {
    return (
      <>
        {/* Backdrop */}
        {isOpen && (
          <div
            className="fixed inset-0 z-40 bg-black/50"
            onClick={onToggle}
          />
        )}
        {/* Overlay sidebar */}
        <div
          className={`fixed inset-y-0 left-0 z-50 w-64 bg-zinc-900 border-r border-zinc-700 flex flex-col transition-transform duration-200 ${
            isOpen ? "translate-x-0" : "-translate-x-full"
          }`}
        >
          <div className="flex items-center h-12 px-2 border-b border-zinc-700">
            <button
              onClick={onToggle}
              className="p-2 rounded-md text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors"
              title="Close sidebar"
            >
              <PanelLeftClose className="w-5 h-5" />
            </button>
            <button
              onClick={handleNewSession}
              className="ml-2 flex items-center gap-2 px-3 py-1.5 rounded-md text-sm text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors"
            >
              <Plus className="w-4 h-4" />
              New
            </button>
          </div>
          <SessionList
            sessions={sessions}
            onSelect={(id) => {
              handleSelectSession(id);
              onToggle();
            }}
            loadingId={loadingId}
            hasMore={sessions.length < total}
            loadingMore={loadingMore}
            onLoadMore={loadMore}
          />
        </div>
      </>
    );
  }

  return (
    <div
      className={`flex-shrink-0 border-r border-zinc-700 bg-zinc-900 flex flex-col transition-all duration-200 ${
        isOpen ? "w-64" : "w-12"
      }`}
    >
      {/* Toggle button */}
      <div className="flex items-center h-12 px-2 border-b border-zinc-700">
        <button
          onClick={onToggle}
          className="p-2 rounded-md text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors"
          title={isOpen ? "Collapse sidebar" : "Expand sidebar"}
        >
          {isOpen ? (
            <PanelLeftClose className="w-5 h-5" />
          ) : (
            <PanelLeft className="w-5 h-5" />
          )}
        </button>
        {isOpen && (
          <button
            onClick={handleNewSession}
            className="ml-2 flex items-center gap-2 px-3 py-1.5 rounded-md text-sm text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors"
          >
            <Plus className="w-4 h-4" />
            New
          </button>
        )}
      </div>

      {/* Session list */}
      {isOpen && (
        <SessionList
          sessions={sessions}
          onSelect={handleSelectSession}
          loadingId={loadingId}
          hasMore={sessions.length < total}
          loadingMore={loadingMore}
          onLoadMore={loadMore}
        />
      )}
    </div>
  );
}
