import { useEffect, useRef, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { api, connectSessionMirror } from "../api/client";
import { useChatState, useChatDispatch } from "../stores/chatStore";
import type { Message, TUIStatus } from "../api/types";
import ChatPanel from "../components/Chat/ChatPanel";
import AgentPreview from "../components/Chat/AgentPreview";
import ChatInput from "../components/Chat/ChatInput";
import StatusBar from "../components/common/StatusBar";
import StatusPanel from "../components/Status/StatusPanel";
import TopTabs from "../components/Layout/TopTabs";
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
  const [activeTab, setActiveTab] = useState("chat");

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
        case "status":
          // Consolidated TUI status snapshot — replaces the slice wholesale.
          // The TUI pushes this on advisor/small toggle, IDE mode change,
          // file edit, agent rebuild, title gen, and at every turn boundary.
          dispatch({ type: "SET_TUI_STATUS", status: data as TUIStatus });
          // Mirror the subset that the rest of the app already reads from
          // individual fields so the dialog + StatusBar stay in sync without
          // a separate fetch path.
          {
            const s = data as TUIStatus;
            if (s.advisor_enabled !== undefined) {
              dispatch({ type: "SET_ADVISOR_ENABLED", enabled: !!s.advisor_enabled });
            }
            if (s.advisor_model !== undefined) {
              dispatch({ type: "SET_ADVISOR_MODEL", model: s.advisor_model });
            }
            if (s.small_model !== undefined) {
              dispatch({ type: "SET_SMALL_MODEL", model: s.small_model });
            }
            if (s.main_model !== undefined && s.main_model !== "") {
              dispatch({ type: "SET_MODEL", model: s.main_model });
            }
          }
          break;
        case "advisor_enabled":
          // Lightweight per-field event from older TUI builds. Newer builds
          // fold this into the "status" payload above; this case is kept for
          // backward compatibility.
          {
            const payload = data as { enabled?: boolean };
            if (typeof payload.enabled === "boolean") {
              dispatch({ type: "SET_ADVISOR_ENABLED", enabled: payload.enabled });
            }
          }
          break;
        case "error":
          dispatch({ type: "SET_ERROR", error: (data as { error: string }).error });
          dispatch({ type: "SET_STREAMING", isStreaming: false });
          break;
      }
    });
  }, [id, dispatch]);

  // Seed the per-session state: advisor on/off, current model, small model,
  // advisor model, and the consolidated TUI status snapshot. The SSE "status"
  // event will overwrite these as the TUI pushes updates; this just gives the
  // UI a correct value before the first frame lands.
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
    api
      .getSmallModelWithEnabled()
      .then((res) => {
        dispatch({ type: "SET_SMALL_MODEL", model: res.model || "" });
      })
      .catch(console.error);
    api
      .getAdvisor()
      .then((res) =>
        dispatch({ type: "SET_ADVISOR_MODEL", model: res.model || "" }),
      )
      .catch(console.error);
    api
      .getTUIStatus()
      .then((res) => dispatch({ type: "SET_TUI_STATUS", status: res }))
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
      <TopTabs activeTab={activeTab} onTabChange={setActiveTab} />
      <div className="flex flex-1 overflow-hidden">
        <main className="flex flex-1 flex-col overflow-hidden">
          {activeTab === "chat" && (
            <>
              <ChatPanel />
              <AgentPreview />
              <ChatInput onSlashCommand={handleCommand} />
              <StatusBar
                onCoworkToggle={() => setCoworkOpen(!coworkOpen)}
                onModelClick={() => openModelDialog("main")}
                onStatusClick={() => setActiveTab("status")}
              />
            </>
          )}
          {activeTab === "status" && (
            <StatusPanel key={id} onClose={() => setActiveTab("chat")} />
          )}
        </main>

        {activeTab === "chat" && (
          <CoworkSidebar
            isOpen={coworkOpen}
            onClose={() => setCoworkOpen(false)}
            activeAgent="build"
            onModelClick={openModelDialog}
          />
        )}
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
