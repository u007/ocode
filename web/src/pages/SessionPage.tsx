import { useCallback, useEffect, useRef, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { api, apiPath, authHeaders, connectSessionMirror } from "../api/client";
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
import QuestionDialog from "../components/Chat/QuestionDialog";
import FileEditor from "../components/Files/FileEditor";
import FileTree from "../components/Files/FileTree";
import GitPanel from "../components/Git/GitPanel";
import ChangesPanel from "../components/Changes/ChangesPanel";
import LogPanel from "../components/Logs/LogPanel";
import AssetsPanel from "../components/Assets/AssetsPanel";
import CronPanel from "../components/Cron/CronPanel";
import { useChat } from "../hooks/useChat";
import { useIsMobile } from "../hooks/useIsMobile";
import { dispatchCommand } from "../components/Chat/commands";

/** Trigger a browser file download from an in-memory string. */
function triggerDownload(filename: string, content: string, mimeType: string) {
  const blob = new Blob([content], { type: mimeType });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

type ModelDialogTab = "main" | "small" | "advisor";

export default function SessionPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const state = useChatState();
  const dispatch = useChatDispatch();
  const { resolvePermission, pendingPermission, submitQuestionAnswers, pendingQuestion } = useChat();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [coworkOpen, setCoworkOpen] = useState(true);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [modelDialogTab, setModelDialogTab] = useState<ModelDialogTab>("main");
  const [activeTab, setActiveTab] = useState("chat");

  // Editor tabs state
  interface EditorTab {
    id: string;
    path: string;
    content: string;
  }
  const [editorTabs, setEditorTabs] = useState<EditorTab[]>([]);
  const openFileIdsRef = useRef<Set<string>>(new Set());

  const isMobile = useIsMobile();

  const handleOpenFile = useCallback(async (path: string) => {
    const id = `editor-${path}`;
    if (openFileIdsRef.current.has(id)) {
      setActiveTab(id);
      return;
    }
    try {
      const res = await fetch(apiPath(`/api/files/content?path=${encodeURIComponent(path)}`), {
        headers: authHeaders(),
      });
      if (!res.ok) throw new Error("Failed to load file");
      const data = await res.json();
      openFileIdsRef.current.add(id);
      setEditorTabs((prev) => [...prev, { id, path, content: data.content }]);
      setActiveTab(id);
    } catch (err) {
      console.error("Failed to open file:", err);
    }
  }, []);

  const handleCloseEditorTab = useCallback((id: string) => {
    openFileIdsRef.current.delete(id);
    setEditorTabs((prev) => prev.filter((t) => t.id !== id));
    setActiveTab((prev) => (prev === id ? "files" : prev));
  }, []);

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
            if (s.ocr_backend !== undefined) {
              dispatch({ type: "SET_OCR_BACKEND", backend: s.ocr_backend || "openai-compat" });
            }
            if (s.main_model !== undefined && s.main_model !== "") {
              dispatch({ type: "SET_MODEL", model: s.main_model });
            }
            if (s.ocr_enabled !== undefined) {
              dispatch({ type: "SET_OCR_ENABLED", enabled: !!s.ocr_enabled });
            }
            if (s.ocr_model !== undefined) {
              dispatch({ type: "SET_OCR_MODEL", model: s.ocr_model });
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
        case "question": {
          // Agent paused on a question prompt. Show the dialog; the raw sentinel
          // tool message that also arrives in the `messages` snapshot is filtered
          // from the transcript (ChatPanel), so we don't double-render it.
          const q = data as { request_id: string; questions: import("../api/types").QuestionPrompt[] };
          dispatch({ type: "QUESTION_REQUEST", question: { request_id: q.request_id, questions: q.questions } });
          break;
        }
        case "question_resolved":
          dispatch({ type: "QUESTION_RESOLVED" });
          break;
        case "permission": {
          // Agent paused on a PERMISSION_ASK. Show the approve/deny dialog.
          const p = data as { request_id: string; tool: string; command?: string };
          dispatch({
            type: "PERMISSION_REQUEST",
            permission: { request_id: p.request_id, tool: p.tool, command: p.command },
          });
          break;
        }
        case "permission_resolved":
          dispatch({ type: "PERMISSION_RESOLVED" });
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
      .then((res) => {
        dispatch({ type: "SET_TUI_STATUS", status: res });
        if (res.ocr_backend !== undefined) {
          dispatch({ type: "SET_OCR_BACKEND", backend: res.ocr_backend || "openai-compat" });
        }
        if (res.ocr_enabled !== undefined) {
          dispatch({ type: "SET_OCR_ENABLED", enabled: !!res.ocr_enabled });
        }
        if (res.ocr_model !== undefined) {
          dispatch({ type: "SET_OCR_MODEL", model: res.ocr_model || "" });
        }
      })
      .catch(console.error);
    api
      .getOcrEnabled()
      .then((res) => {
        dispatch({ type: "SET_OCR_ENABLED", enabled: res.enabled });
        dispatch({ type: "SET_OCR_MODEL", model: res.model || "" });
      })
      .catch(console.error);
  }, [dispatch]);

  const openModelDialog = (tab: ModelDialogTab = "main") => {
    setModelDialogTab(tab);
    setModelDialogOpen(true);
  };

  const handleCommand = async (cmd: string) => {
    const baseCmd = cmd.split(" ")[0];

    if (baseCmd === "/clear") {
      dispatch({ type: "RESET" });
      return true;
    }
    if (baseCmd === "/new") {
      dispatch({ type: "RESET" });
      navigate("/");
      return true;
    }
    if (baseCmd === "/model") {
      openModelDialog("main");
      return true;
    }

    // Delegate to the shared command dispatch
    const result = await dispatchCommand(cmd, {
      commandName: baseCmd,
      args: cmd.slice(baseCmd.length).trim(),
      api: {
        listSessions: () => api.listSessions(),
        getSession: (id) => api.getSession(id),
        getOcrConfig: () => api.getOcrConfig(),
        setOcrConfig: (cfg) => api.setOcrConfig(cfg),
        getOcrModels: () => api.getOcrModels(),
        getOcrEnabled: () => api.getOcrEnabled(),
        setOcrEnabled: (enabled) => api.setOcrEnabled(enabled),
        setOcrModel: (model) => api.setOcrModel(model),
        compactSession: (id) => api.compactSession(id),
        recapSession: (id) => api.recapSession(id),
        shareSession: (id) => api.shareSession(id),
        btwSession: (id, content) => api.btwSession(id, content),
        getMaskConfig: () => api.getMaskConfig(),
        setMaskEnabled: (enabled) => api.setMaskEnabled(enabled),
        setMaskMode: (mode) => api.setMaskMode(mode),
        setMaskModel: (model) => api.setMaskModel(model),
      },
      getMessages: () => state.messages,
      getSessionId: () => state.sessionId,
    });

    if (!result.handled) return false;

    if (result.messages) {
      for (const msg of result.messages) {
        dispatch({ type: "ADD_MESSAGE", message: msg });
      }
    }
    if (result.sessionId) {
      dispatch({ type: "SET_SESSION", sessionId: result.sessionId });
    }
    if (result.newSession) {
      dispatch({ type: "RESET" });
    }
    if (result.download) {
      triggerDownload(result.download.filename, result.download.content, result.download.mimeType);
    }
    return true;
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
      <TopTabs
        activeTab={activeTab}
        onTabChange={setActiveTab}
        editorTabs={editorTabs.map((t) => ({ id: t.id, path: t.path }))}
        onEditorTabClose={handleCloseEditorTab}
      />
      <div className="flex flex-1 overflow-hidden">
        <main className="flex flex-1 flex-col overflow-hidden">
          {/* Editor tab — opened when a file is clicked from files/git/chat */}
          {activeTab.startsWith("editor-") &&
            (() => {
              const editorTab = editorTabs.find((t) => t.id === activeTab);
              if (!editorTab) return null;
              return (
                <FileEditor
                  key={editorTab.id}
                  path={editorTab.path}
                  content={editorTab.content}
                  readOnly={false}
                />
              );
            })()}
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
          {activeTab === "files" && <FileTree onOpenFile={handleOpenFile} />}
          {activeTab === "changes" && <ChangesPanel session={state.sessionId ?? undefined} />}
          {activeTab === "git" && <GitPanel onOpenFile={handleOpenFile} />}
          {activeTab === "status" && (
            <StatusPanel key={id} onClose={() => setActiveTab("chat")} />
          )}
          {activeTab === "logs" && <LogPanel />}
          {activeTab === "cron" && <CronPanel />}
          {activeTab === "assets" && <AssetsPanel />}
        </main>

        {activeTab === "chat" && (
          <CoworkSidebar
            isOpen={coworkOpen}
            onClose={() => setCoworkOpen(false)}
            activeAgent="build"
            onModelClick={openModelDialog}
            isMobile={isMobile}
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

      {pendingQuestion && (
        <QuestionDialog
          open={true}
          requestId={pendingQuestion.request_id}
          questions={pendingQuestion.questions}
          onSubmit={submitQuestionAnswers}
        />
      )}
    </div>
  );
}
