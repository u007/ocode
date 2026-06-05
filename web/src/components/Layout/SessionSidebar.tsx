import { useState, useEffect } from "react";
import { api } from "../../api/client";
import { useChatDispatch, useChatState } from "../../stores/chatStore";
import type { SessionInfo } from "../../api/types";
import { PanelLeftClose, PanelLeft, Plus, MessageSquare } from "lucide-react";

interface Props {
  isOpen: boolean;
  onToggle: () => void;
}

export default function SessionSidebar({ isOpen, onToggle }: Props) {
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const dispatch = useChatDispatch();
  const { sessionId } = useChatState();

  const fetchSessions = () => {
    api.listSessions().then(setSessions).catch(console.error);
  };

  // Initial fetch on mount
  useEffect(() => {
    fetchSessions();
  }, []);

  // Re-fetch when sidebar opens (catches sessions created while closed)
  useEffect(() => {
    if (isOpen) {
      fetchSessions();
    }
  }, [isOpen]);

  // Re-fetch when sessionId changes from null to a value (new session created)
  useEffect(() => {
    if (sessionId) {
      fetchSessions();
    }
  }, [sessionId]);

  const handleNewSession = () => {
    dispatch({ type: "RESET" });
  };

  const handleSelectSession = async (id: string) => {
    try {
      const session = await api.getSession(id);
      dispatch({ type: "SET_SESSION", sessionId: id });
      dispatch({ type: "SET_MESSAGES", messages: session.messages || [] });
    } catch (err) {
      console.error("Failed to load session:", err);
    }
  };

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
        <div className="flex-1 overflow-y-auto">
          {sessions.map((session) => (
            <button
              key={session.id}
              onClick={() => handleSelectSession(session.id)}
              className="w-full text-left px-4 py-3 text-sm hover:bg-zinc-800 text-zinc-400 border-b border-zinc-800 transition-colors"
            >
              <div className="flex items-center gap-2">
                <MessageSquare className="w-4 h-4 flex-shrink-0 text-zinc-600" />
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
          ))}
        </div>
      )}
    </div>
  );
}
