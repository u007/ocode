import { useEffect, useRef, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { api, connectSessionMirror } from "../api/client";
import { useChatState, useChatDispatch } from "../stores/chatStore";
import type { Message } from "../api/types";
import ChatPanel from "../components/Chat/ChatPanel";
import AgentPreview from "../components/Chat/AgentPreview";
import ChatInput from "../components/Chat/ChatInput";
import StatusBar from "../components/common/StatusBar";
import CoworkSidebar from "../components/Layout/CoworkSidebar";
import ModelDialog from "../components/Layout/ModelDialog";
import PermissionDialog from "../components/Chat/PermissionDialog";
import { useChat } from "../hooks/useChat";

type ModelDialogTab = "main" | "small" | "advisor";

export default function SessionPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const state = useChatState();
  const dispatch = useChatDispatch();
  const { resolvePermission, pendingPermission } = useChat();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [coworkOpen, setCoworkOpen] = useState(true);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [modelDialogTab, setModelDialogTab] = useState<ModelDialogTab>("main");

  // Keep a ref to the latest state for use in the SSE callback
  const stateRef = useRef(state);
  stateRef.current = state;

  useEffect(() => {
    if (!id) return;
    setLoading(true);
    setError(null);
    api
      .getSession(id)
      .then((_detail) => {
        dispatch({ type: "SET_SESSION", sessionId: id });
        // Don't set messages here — ChatPanel handles paginated loading
      })
      .catch((err) => {
        setError(err.message || "Failed to load session");
      })
      .finally(() => setLoading(false));
  }, [id, dispatch]);

  // Live 2-way mirror. Every event — whether the turn was started in the TUI or
  // in another browser — flows through this single stream: user messages, live
  // thinking/text tokens, tool activity, and the authoritative snapshot that
  // commits the turn. The browser renders purely from here.
  useEffect(() => {
    if (!id) return;
    return connectSessionMirror(id, (event, data) => {
      switch (event) {
        case "messages": {
          // SSE snapshot is the authoritative full message list from the TUI.
          // Replace all messages and update pagination state.
          const snapshot = data as Message[];
          const curState = stateRef.current;
          if (curState.hasMore && curState.messages.length > 0 && snapshot.length > curState.messages.length) {
            // We have a subset and snapshot has more — check if our messages are a prefix
            const cur = curState.messages;
            let isPrefix = true;
            for (let i = 0; i < cur.length; i++) {
              if (cur[i].content !== snapshot[i].content || cur[i].role !== snapshot[i].role) {
                isPrefix = false;
                break;
              }
            }
            if (isPrefix) {
              // New messages at the end — append only those
              const newMsgs = snapshot.slice(cur.length);
              newMsgs.forEach((msg: Message) =>
                dispatch({ type: "ADD_MESSAGE", message: msg })
              );
              // Update pagination total
              dispatch({ type: "SET_TOTAL", total: snapshot.length });
              return;
            }
          }
          // Default: replace all messages
          dispatch({ type: "SET_MESSAGES", messages: snapshot });
          dispatch({ type: "SET_TOTAL", total: snapshot.length });
          break;
        }
        case "user_message":
          dispatch({
            type: "ADD_MESSAGE",
            message: { role: "user", content: (data as { content: string }).content },
          });
          dispatch({ type: "SET_STREAMING", isStreaming: true });
          break;
        case "thinking":
          dispatch({ type: "LIVE_DELTA", kind: "thinking", delta: (data as { delta: string }).delta });
          dispatch({ type: "SET_STREAMING", isStreaming: true });
          break;
        case "text":
          dispatch({ type: "LIVE_DELTA", kind: "text", delta: (data as { delta: string }).delta });
          dispatch({ type: "SET_STREAMING", isStreaming: true });
          break;
        case "tool_start":
          dispatch({
            type: "LIVE_TOOL_START",
            tool: (data as { tool: string }).tool,
            command: (data as { command?: string }).command,
          });
          dispatch({ type: "SET_STREAMING", isStreaming: true });
          break;
        case "tool_result":
          dispatch({ type: "LIVE_TOOL_RESULT", output: (data as { output: string }).output });
          break;
        case "turn_done":
          dispatch({ type: "SET_STREAMING", isStreaming: false });
          break;
        case "error":
          dispatch({ type: "SET_ERROR", error: (data as { error: string }).error });
          dispatch({ type: "SET_STREAMING", isStreaming: false });
          break;
      }
    });
  }, [id, dispatch]);

  // Seed advisor state and current model from the server.
  useEffect(() => {
    api
      .getAdvisorEnabled()
      .then((res) =>
        dispatch({ type: "SET_ADVISOR_ENABLED", enabled: res.enabled }),
      )
      .catch(console.error);
    api
      .getConfigModel()
      .then((res) => dispatch({ type: "SET_MODEL", model: res.model }))
      .catch(console.error);
  }, [dispatch]);

  const openModelDialog = (tab: ModelDialogTab = "main") => {
    setModelDialogTab(tab);
    setModelDialogOpen(true);
  };

  const handleCommand = (cmd: string) => {
    // Extract the base command (first word)
    const baseCmd = cmd.split(" ")[0];
    
    if (baseCmd === "/clear") {
      dispatch({ type: "RESET" });
      return true;
    }
    if (baseCmd === "/new") {
      // Reset chat state and navigate to home page to start a new session
      dispatch({ type: "RESET" });
      navigate("/");
      return true;
    }
    if (baseCmd === "/model") {
      openModelDialog("main");
      return true;
    }
    // For other commands, let them pass through to the LLM
    return false;
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-screen bg-zinc-950 text-zinc-400">
        Loading session…
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-screen bg-zinc-950 text-red-400">
        {error}
      </div>
    );
  }

  return (
    <div className="flex flex-col h-screen bg-zinc-950">
      <div className="flex flex-1 overflow-hidden">
        <main className="flex flex-1 flex-col overflow-hidden">
          <ChatPanel />
          <AgentPreview />
          <ChatInput onSlashCommand={handleCommand} />
          <StatusBar
            onCoworkToggle={() => setCoworkOpen(!coworkOpen)}
            onModelClick={() => openModelDialog("main")}
          />
        </main>

        <CoworkSidebar
          isOpen={coworkOpen}
          onClose={() => setCoworkOpen(false)}
          activeAgent="build"
          onModelClick={openModelDialog}
        />
      </div>

      <ModelDialog
        open={modelDialogOpen}
        onClose={() => setModelDialogOpen(false)}
        initialTab={modelDialogTab}
      />

      {pendingPermission && (
        <PermissionDialog
          open={true}
          tool={pendingPermission.tool}
          command={pendingPermission.command}
          requestId={pendingPermission.request_id}
          onApprove={resolvePermission}
        />
      )}
    </div>
  );
}
