import { useEffect, useRef, useState } from "react";
import { Routes, Route, useNavigate } from "react-router-dom";
import { ChatProvider, useChatDispatch, useChatState } from "./stores/chatStore";
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
import SessionSidebar from "./components/Layout/SessionSidebar";
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

    if (!sessionId) {
      return () => {
        cancelled = true;
      };
    }

    api
      .getSessionContext(sessionId)
      .then((res) => {
        if (cancelled) return;
        dispatch({
          type: "SET_SESSION_CONTEXT",
          context: {
            currentTokens: res.estimated_tokens,
            maxTokens: res.max_tokens ?? 0,
            model: res.model ?? "",
          },
        });
      })
      .catch((err) => console.error(err));

    return () => {
      cancelled = true;
    };
  }, [dispatch, tuiStatus?.session_id, tuiStatus?.updated_at]);

  return null;
}

function HomeApp() {
  const [activeTab, setActiveTab] = useState("chat");
  const [cmdOpen, setCmdOpen] = useState(false);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [modelDialogTab, setModelDialogTab] = useState<ModelDialogTab>("main");
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [coworkOpen, setCoworkOpen] = useState(true);
  const [isMobile, setIsMobile] = useState(() => window.innerWidth < 768);
  const navigate = useNavigate();
  const dispatch = useChatDispatch();
  const { resolvePermission, pendingPermission } = useChat({
    onNewSession: (sessionId) => navigate(`/session/${sessionId}`),
  });

  // Mobile breakpoint detection. We only auto-close the sidebar on the
  // RISING edge (desktop → mobile) so resizing back to desktop doesn't
  // hide a sidebar the user was just looking at, and resizing mobile →
  // desktop → mobile doesn't snap a deliberately open sidebar shut. The
  // listener still updates isMobile every time; the auto-close is gated
  // on the previous-vs-current transition in lastWasMobile.
  useEffect(() => {
    const mq = window.matchMedia("(max-width: 767px)");
    let lastWasMobile = mq.matches;
    const handler = (e: MediaQueryListEvent) => {
      setIsMobile(e.matches);
      if (e.matches && !lastWasMobile) {
        setSidebarOpen(false);
      }
      lastWasMobile = e.matches;
    };
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, []);

  // Seed the advisor on/off, model, small model, advisor model, and the
  // consolidated TUI status snapshot so the status bar is correct on load.
  // The SSE "status" event from the bridged TUI session will overwrite these
  // as updates arrive.
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
      .then((res) => {
        dispatch({ type: "SET_SMALL_MODEL", model: res.model || "" });
      })
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
    // Extract the base command (first word)
    const baseCmd = cmd.split(" ")[0];
    
    if (baseCmd === "/clear" || baseCmd === "/new") {
      dispatch({ type: "RESET" });
      return true;
    }
    if (baseCmd === "/model") {
      openModelDialog("main");
      return true;
    }
    // For other commands, let them pass through to the LLM
    return false;
  };

  return (
    <div className="flex flex-col h-screen bg-zinc-950">
      {/* Top navigation with tabs */}
      <TopTabs
        activeTab={activeTab}
        onTabChange={setActiveTab}
        onMenuToggle={() => setSidebarOpen(!sidebarOpen)}
      />

      {/* Main content area */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left sidebar - session history (collapsible) */}
        <SessionSidebar
          isOpen={sidebarOpen}
          onToggle={() => setSidebarOpen(!sidebarOpen)}
          isMobile={isMobile}
        />

        {/* Center content */}
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
          {activeTab === "files" && <FileTree />}
          {activeTab === "git" && <GitPanel />}
          {activeTab === "status" && <StatusPanel onClose={() => setActiveTab("chat")} />}
          {activeTab === "logs" && <LogPanel />}
          {activeTab === "assets" && <AssetsPanel />}
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
        <StatusMetricsHydrator />
        <Routes>
          <Route path="/session/:id" element={<SessionPage />} />
          <Route path="*" element={<HomeApp />} />
        </Routes>
      </ChatProvider>
    </ErrorBoundary>
  );
}
