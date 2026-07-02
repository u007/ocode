import { useEffect, useRef, useState } from "react";
import { Routes, Route } from "react-router-dom";
import { ChatProvider, useChatDispatch, useChatState } from "./stores/chatStore";
import { ProjectProvider } from "./stores/projectStore";
import { api } from "./api/client";
import ErrorBoundary from "./components/common/ErrorBoundary";
import ChatPanel from "./components/Chat/ChatPanel";
import AgentPreview from "./components/Chat/AgentPreview";
import ChatInput from "./components/Chat/ChatInput";
import StatusBar from "./components/common/StatusBar";
import StatusPanel from "./components/Status/StatusPanel";
import CommandPalette from "./components/common/CommandPalette";
import GitPanel from "./components/Git/GitPanel";
import FileTree from "./components/Files/FileTree";
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

function HomeApp() {
  const dispatch = useChatDispatch();
  const { resolvePermission, pendingPermission } = useChat();
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [coworkOpen, setCoworkOpen] = useState(true);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [modelDialogTab, setModelDialogTab] = useState<ModelDialogTab>("main");
  const [cmdOpen, setCmdOpen] = useState(false);
  const [activeTab, setActiveTab] = useState("chat");

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

  const handleCommand = (cmd: string) => {
    const baseCmd = cmd.split(" ")[0];
    if (baseCmd === "/clear" || baseCmd === "/new") {
      dispatch({ type: "RESET" });
      return true;
    }
    if (baseCmd === "/model") {
      openModelDialog("main");
      return true;
    }
    return false;
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
          {/* Content navigation tabs (chat/files/git/status/logs/assets) */}
          <TopTabs
            activeTab={activeTab}
            onTabChange={setActiveTab}
            onMenuToggle={() => setSidebarOpen(!sidebarOpen)}
          />

          {/* Tab content */}
          <div className="flex-1 overflow-hidden">
            {activeTab === "chat" && (
              <div className="flex flex-col h-full">
                <ChatPanel />
                <AgentPreview />
                <ChatInput onSlashCommand={handleCommand} />
              </div>
            )}
            {activeTab === "files" && <FileTree />}
            {activeTab === "git" && <GitPanel />}
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
