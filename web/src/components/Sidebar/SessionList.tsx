import { useSessions } from "../../hooks/useSessions";
import { useChatState, useChatDispatch } from "../../stores/chatStore";
import type { SessionInfo } from "../../api/types";

export default function SessionList() {
  const { sessions, loading } = useSessions();
  const { sessionId: activeId } = useChatState();
  const dispatch = useChatDispatch();

  const handleSelect = (s: SessionInfo) => {
    dispatch({ type: "SET_SESSION", sessionId: s.id });
    dispatch({ type: "SET_MESSAGES", messages: [] });
    dispatch({ type: "SET_STREAMING", isStreaming: false });
    dispatch({ type: "SET_ERROR", error: null });
  };

  const handleNew = () => {
    dispatch({ type: "RESET" });
  };

  if (loading) {
    return <div className="p-4 text-zinc-500 text-sm">Loading sessions...</div>;
  }

  return (
    <div className="flex flex-col h-full">
      <div className="p-3 border-b border-zinc-700">
        <button
          onClick={handleNew}
          className="w-full rounded-lg bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-700"
        >
          + New Session
        </button>
      </div>
      <div className="flex-1 overflow-y-auto">
        {sessions.map((s) => (
          <button
            key={s.id}
            onClick={() => handleSelect(s)}
            className={`w-full text-left px-4 py-3 text-sm hover:bg-zinc-800 ${
              activeId === s.id ? "bg-zinc-800 text-white" : "text-zinc-400"
            }`}
          >
            <div className="truncate font-medium">{s.title || s.id}</div>
            <div className="truncate text-xs text-zinc-500">{s.updated_at}</div>
          </button>
        ))}
      </div>
    </div>
  );
}
