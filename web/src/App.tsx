import { useCallback, useEffect, useRef, useState } from "react";
import { Routes, Route } from "react-router-dom";
import { ChatProvider, useChatDispatch, useChatState } from "./stores/chatStore";
import { ProjectProvider } from "./stores/projectStore";
import { api, apiPath, authHeaders } from "./api/client";
import ErrorBoundary from "./components/common/ErrorBoundary";
import ChatPanel from "./components/Chat/ChatPanel";
import AgentPreview from "./components/Chat/AgentPreview";
import ChatInput from "./components/Chat/ChatInput";
import StatusBar from "./components/common/StatusBar";
import StatusPanel from "./components/Status/StatusPanel";
import CommandPalette from "./components/common/CommandPalette";
import GitPanel from "./components/Git/GitPanel";
import FileTree from "./components/Files/FileTree";
import FileEditor from "./components/Files/FileEditor";
import LogPanel from "./components/Logs/LogPanel";
import AssetsPanel from "./components/Assets/AssetsPanel";
import TopTabs from "./components/Layout/TopTabs";
import ProjectSidebar from "./components/Layout/ProjectSidebar";
import SessionTabs from "./components/Layout/SessionTabs";
import CoworkSidebar from "./components/Layout/CoworkSidebar";
import ModelDialog from "./components/Layout/ModelDialog";
import PermissionDialog from "./components/Chat/PermissionDialog";
import { useKeyboard } from "./hooks/useKeyboard";
import { useTheme } from "./hooks/useTheme";
import { useChat } from "./hooks/useChat";
import { dispatchCommand } from "./components/Chat/commands";
import SessionPage from "./pages/SessionPage";

type ModelDialogTab = "main" | "small" | "advisor";

function StatusMetricsHydrator() {
  const { tuiStatus } = useChatState();
  const dispatch = useChatDispatch();
  const lastSessionId = useRef<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const sessionId = tuiStatus?.session_id ?? null;

    if (lastSessionId.current !== sessionId) {
      dispatch({ type: "SET_SESSION_CONTEXT", context: null });
      lastSessionId.current = sessionId;
    }

    const updateSpending = async () => {
      try {
        const res = await api.getSpending();
        if (!cancelled) {
          dispatch({ type: "SET_SPENDING", spendingUSD: res.spending_usd });
        }
      } catch (err) {
        console.error(err);
      }
    };

    updateSpending();
    const interval = setInterval(updateSpending, 60000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [tuiStatus?.session_id, dispatch]);

  return null;
}

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

function HomeApp() {
  const dispatch = useChatDispatch();
  const { messages: chatMessages, sessionId: currentSessionId } = useChatState();
  const { resolvePermission, pendingPermission } = useChat();
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [coworkOpen, setCoworkOpen] = useState(true);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [modelDialogTab, setModelDialogTab] = useState<ModelDialogTab>("main");
  const [cmdOpen, setCmdOpen] = useState(false);
  const [activeTab, setActiveTab] = useState("chat");

  // Editor tabs state — each open file gets its own tab with Monaco editor
  interface EditorTab {
    id: string;
    path: string;
    content: string;
  }
  const [editorTabs, setEditorTabs] = useState<EditorTab[]>([]);
  const openFileIdsRef = useRef<Set<string>>(new Set());

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

  // Mobile responsive
  useEffect(() => {
    const mq = window.matchMedia("(max-width: 767px)");
    let lastWasMobile = mq.matches;
    const handler = (e: MediaQueryListEvent) => {
      if (e.matches && !lastWasMobile) {
        setSidebarOpen(false);
      }
      lastWasMobile = e.matches;
    };
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, []);

  // Seed config values
  useEffect(() => {
    api
      .getAdvisorEnabled()
      .then((res) => dispatch({ type: "SET_ADVISOR_ENABLED", enabled: res.enabled }))
      .catch(console.error);
    api
      .getConfigModel()
      .then((res) => dispatch({ type: "SET_MODEL", model: res.model }))
      .catch(console.error);
    api
      .getSmallModelWithEnabled()
      .then((res) => dispatch({ type: "SET_SMALL_MODEL", model: res.model || "" }))
      .catch(console.error);
    api
      .getAdvisor()
      .then((res) => dispatch({ type: "SET_ADVISOR_MODEL", model: res.model || "" }))
      .catch(console.error);
    api
      .getTUIStatus()
      .then((res) => dispatch({ type: "SET_TUI_STATUS", status: res }))
      .catch(console.error);
  }, [dispatch]);

  useKeyboard({
    onNewSession: () => dispatch({ type: "RESET" }),
    onCommandPalette: () => setCmdOpen(true),
    onEscape: () => setCmdOpen(false),
  });

  const openModelDialog = (tab: ModelDialogTab = "main") => {
    setModelDialogTab(tab);
    setModelDialogOpen(true);
  };

  const handleCommand = async (cmd: string) => {
    const baseCmd = cmd.split(" ")[0];
    // Built-in quick actions that don't need the dispatch pipeline
    if (baseCmd === "/clear" || baseCmd === "/new") {
      dispatch({ type: "RESET" });
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
      getMessages: () => chatMessages,
      getSessionId: () => currentSessionId,
    });

    if (!result.handled) return false;

    // Apply result effects
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

  return (
    <div className="flex flex-col h-screen bg-zinc-950">
      {/* Session tabs bar (multi-project session management) */}
      <SessionTabs />

      {/* Main content area */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left sidebar - project roots */}
        <ProjectSidebar
          isOpen={sidebarOpen}
          onToggle={() => setSidebarOpen(!sidebarOpen)}
        />

        {/* Center content */}
        <main className="flex flex-1 flex-col overflow-hidden">
          {/* Content navigation tabs (chat/files/git/status/logs/assets + editor tabs) */}
          <TopTabs
            activeTab={activeTab}
            onTabChange={setActiveTab}
            editorTabs={editorTabs.map((t) => ({ id: t.id, path: t.path }))}
            onEditorTabClose={handleCloseEditorTab}
            onMenuToggle={() => setSidebarOpen(!sidebarOpen)}
          />

          {/* Tab content */}
          <div className="flex-1 overflow-hidden">
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
            {/* Main tabs */}
            {activeTab === "chat" && (
              <div className="flex flex-col h-full">
                <ChatPanel />
                <AgentPreview />
                <ChatInput onSlashCommand={handleCommand} />
              </div>
            )}
            {activeTab === "files" && <FileTree onOpenFile={handleOpenFile} />}
            {activeTab === "git" && <GitPanel onOpenFile={handleOpenFile} />}
            {activeTab === "status" && <StatusPanel onClose={() => setActiveTab("chat")} />}
            {activeTab === "logs" && <LogPanel />}
            {activeTab === "assets" && <AssetsPanel />}
          </div>

          {/* Status bar */}
          <StatusBar
            onCoworkToggle={() => setCoworkOpen(!coworkOpen)}
            onModelClick={() => openModelDialog("main")}
            onStatusClick={() => {
              setActiveTab("status");
            }}
          />
        </main>

        {/* Right sidebar - cowork panel (only on chat tab) */}
        {activeTab === "chat" && (
          <CoworkSidebar
            isOpen={coworkOpen}
            onClose={() => setCoworkOpen(false)}
            activeAgent="build"
            onModelClick={openModelDialog}
          />
        )}
      </div>

      {/* Dialogs */}
      <CommandPalette
        open={cmdOpen}
        onClose={() => setCmdOpen(false)}
        onExecute={handleCommand}
      />
      <ModelDialog
        open={modelDialogOpen}
        onClose={() => setModelDialogOpen(false)}
        initialTab={modelDialogTab}
      />

      {/* Permission Dialog */}
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

export default function App() {
  // Applies the server (terminal) theme to the CSS variables once on load.
  useTheme();
  return (
    <ErrorBoundary>
      <ChatProvider>
        <ProjectProvider>
          <StatusMetricsHydrator />
          <Routes>
            <Route path="/session/:id" element={<SessionPage />} />
            <Route path="*" element={<HomeApp />} />
          </Routes>
        </ProjectProvider>
      </ChatProvider>
    </ErrorBoundary>
  );
}
