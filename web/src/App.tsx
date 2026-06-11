import { useEffect, useState } from "react";
import { Routes, Route } from "react-router-dom";
import { ChatProvider, useChatDispatch } from "./stores/chatStore";
import { api } from "./api/client";
import ErrorBoundary from "./components/common/ErrorBoundary";
import ChatPanel from "./components/Chat/ChatPanel";
import AgentPreview from "./components/Chat/AgentPreview";
import ChatInput from "./components/Chat/ChatInput";
import StatusBar from "./components/common/StatusBar";
import CommandPalette from "./components/common/CommandPalette";
import GitPanel from "./components/Git/GitPanel";
import FileTree from "./components/Files/FileTree";
import LogPanel from "./components/Logs/LogPanel";
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

function HomeApp() {
  const [activeTab, setActiveTab] = useState("chat");
  const [cmdOpen, setCmdOpen] = useState(false);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [modelDialogTab, setModelDialogTab] = useState<ModelDialogTab>("main");
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [coworkOpen, setCoworkOpen] = useState(true);
  const dispatch = useChatDispatch();
  const { resolvePermission, pendingPermission } = useChat();

  // Seed the advisor on/off state from the server so the status bar is correct on load.
  useEffect(() => {
    api
      .getAdvisorEnabled()
      .then((res) => dispatch({ type: "SET_ADVISOR_ENABLED", enabled: res.enabled }))
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
    if (cmd === "/clear") dispatch({ type: "RESET" });
    if (cmd === "/model") openModelDialog("main");
    // /compact, /recap, /export, /share are handled by the chat input
  };

  return (
    <div className="flex flex-col h-screen bg-zinc-950">
      {/* Top navigation with tabs */}
      <TopTabs
        activeTab={activeTab}
        onTabChange={setActiveTab}
      />

      {/* Main content area */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left sidebar - session history (collapsible) */}
        <SessionSidebar
          isOpen={sidebarOpen}
          onToggle={() => setSidebarOpen(!sidebarOpen)}
        />

        {/* Center content */}
        <main className="flex flex-1 flex-col overflow-hidden">
          {activeTab === "chat" && (
            <>
              <ChatPanel />
              <AgentPreview />
              <ChatInput />
              <StatusBar 
                onCoworkToggle={() => setCoworkOpen(!coworkOpen)} 
                onModelClick={() => openModelDialog("main")}
              />
            </>
          )}
          {activeTab === "files" && <FileTree />}
          {activeTab === "git" && <GitPanel />}
          {activeTab === "logs" && <LogPanel />}
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
        <Routes>
          <Route path="/session/:id" element={<SessionPage />} />
          <Route path="*" element={<HomeApp />} />
        </Routes>
      </ChatProvider>
    </ErrorBoundary>
  );
}
