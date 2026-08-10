import { useCallback, useEffect, useRef, useState } from "react";
import { Routes, Route } from "react-router-dom";
import { useIsMobile } from "./hooks/useIsMobile";
import { ChatProvider, useChatDispatch, useChatState, getSessionSlice } from "./stores/chatStore";
import { ProjectProvider, useProjectState } from "./stores/projectStore";
import { api } from "./api/client";
import ErrorBoundary from "./components/common/ErrorBoundary";
import ChatPanel from "./components/Chat/ChatPanel";
import AgentPreview from "./components/Chat/AgentPreview";
import AgentsPanel from "./components/Agents/AgentsPanel";
import ChatInput from "./components/Chat/ChatInput";
import StatusBar from "./components/common/StatusBar";
import StatusPanel from "./components/Status/StatusPanel";
import CommandPalette from "./components/common/CommandPalette";
import GitPanel from "./components/Git/GitPanel";
import ChangesPanel from "./components/Changes/ChangesPanel";
import FileTree from "./components/Files/FileTree";
import FileEditor from "./components/Files/FileEditor";
import LogPanel from "./components/Logs/LogPanel";
import AssetsPanel from "./components/Assets/AssetsPanel";
import CronPanel from "./components/Cron/CronPanel";
import { Tabs, TabsContent } from "@/components/ui/tabs";
import TopTabs from "./components/Layout/TopTabs";
import EditorTabBar from "./components/Layout/EditorTabBar";
import ProjectSidebar from "./components/Layout/ProjectSidebar";
import SessionDialog from "./components/Layout/SessionDialog";
import OpenSessionBar from "./components/Layout/OpenSessionBar";
import SessionSubTabs from "./components/Layout/SessionSubTabs";
import SessionTabSync from "./components/Layout/SessionTabSync";
import CoworkSidebar from "./components/Layout/CoworkSidebar";
import ModelDialog from "./components/Layout/ModelDialog";
import PermissionDialog from "./components/Chat/PermissionDialog";
import { useKeyboard } from "./hooks/useKeyboard";
import { useTheme } from "./hooks/useTheme";
import { useEditorTabs } from "./hooks/useEditorTabs";
import { useChat } from "./hooks/useChat";
import { dispatchCommand } from "./components/Chat/commands";
import SessionPage from "./pages/SessionPage";
import FilePicker from "./components/Files/FilePicker";
import ConfirmCloseDialog from "./components/Files/ConfirmCloseDialog";
import { isNewSessionTabEmpty } from "./lib/tabDrafts";

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
  const chatState = useChatState();
  const { state: projectState, tabs, activeTabId, dispatch: projectDispatch, openSessionTab, openNewSessionTab } = useProjectState();
  const { resolvePermission, pendingPermission } = useChat(activeTabId);
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [coworkOpen, setCoworkOpen] = useState(true);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [modelDialogTab, setModelDialogTab] = useState<ModelDialogTab>("main");
  const [cmdOpen, setCmdOpen] = useState(false);
  const [selectedAgentRunId, setSelectedAgentRunId] = useState<string | null>(null);
  const [activeView, setActiveView] = useState<
    "files" | "git" | "cron" | "assets" | "status" | "logs" | "sessions"
  >("sessions");
  const {
    editorTabs,
    activeEditorTabId,
    setActiveEditorTabId,
    handleOpenFile,
    handleEditorChange,
    handleSelectionChange,
    activeEditorContext,
    requestCloseTab,
    pendingClose,
    confirmSaveAndClose,
    confirmDiscardAndClose,
    cancelClose,
    saveError,
    saveEditorTab,
  } = useEditorTabs();

  // Opening a file from anywhere (tree, git diff, file picker) shows the
  // Files view and selects the editor tab.
  const openFileAndShow = useCallback(
    async (path: string) => {
      await handleOpenFile(path);
      setActiveView("files");
    },
    [handleOpenFile],
  );

  useEffect(() => {
    setSelectedAgentRunId(null);
  }, [activeTabId]);

  const openAgentDetail = (sessionId: string, runId: string) => {
    setSelectedAgentRunId(runId);
    projectDispatch({ type: "SET_TAB_SUB_TAB", id: sessionId, subTab: "agents" });
  };

  // Mobile responsive
  const isMobile = useIsMobile();
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
      .then((res) => {
        dispatch({ type: "SET_SMALL_MODEL", model: res.model || "" });
        dispatch({ type: "SET_SMALL_MODEL_ENABLED", enabled: res.enabled });
      })
      .catch(console.error);
    api
      .getAdvisor()
      .then((res) => dispatch({ type: "SET_ADVISOR_MODEL", model: res.model || "" }))
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
  }, [dispatch]);

  const [filePickerOpen, setFilePickerOpen] = useState(false);

  useKeyboard({
    onNewSession: () => {
      openNewSessionTab(isNewSessionTabEmpty(activeTabId));
    },
    onCommandPalette: () => setCmdOpen(true),
    onFilePicker: () => setFilePickerOpen(true),
    onSave: () => {
      if (activeEditorTabId) {
        saveEditorTab(activeEditorTabId);
      }
    },
    onEscape: () => {
      setCmdOpen(false);
      setFilePickerOpen(false);
    },
  });

  const openModelDialog = (tab: ModelDialogTab = "main") => {
    setModelDialogTab(tab);
    setModelDialogOpen(true);
  };

  const handleCommand = async (cmd: string) => {
    const baseCmd = cmd.split(" ")[0];
    // Built-in quick actions that don't need the dispatch pipeline
    if (baseCmd === "/clear" || baseCmd === "/new") {
      openNewSessionTab(isNewSessionTabEmpty(activeTabId));
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
        listSessions: () => api.listSessions().then((r) => r.sessions),
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
        getCommandContext: (name, args) => api.getCommandContext(name, args),
        getSessionContext: (id) => api.getSessionContext(id),
        getLSPStatuses: () => api.getLSPStatuses(),
        listSkills: () => api.listSkills(),
        getMCP: () => api.getMCP(),
        getGithubPR: (owner, repo, number) => api.getGithubPR(owner, repo, number),
        getGithubIssues: (owner, repo, state) => api.getGithubIssues(owner, repo, state),
        getAgentRuns: () => api.listAgentRuns(),
        getCronJobs: () => api.listCronJobs().then((r) => r.jobs),
        getSmallModelWithEnabled: () => api.getSmallModelWithEnabled(),
        getAdvisor: () => api.getAdvisor(),
      },
      getMessages: () => getSessionSlice(chatState, activeTabId).messages,
      getSessionId: () => activeTabId,
    });

    if (!result.handled) return false;

    // Apply result effects
    if (result.messages) {
      for (const msg of result.messages) {
        if (activeTabId) {
          dispatch({ type: "ADD_MESSAGE", sessionId: activeTabId, message: msg });
        }
      }
    }
    if (result.sessionId) {
      openSessionTab(result.sessionId, result.sessionId);
    }
    if (result.newSession) {
      openNewSessionTab(isNewSessionTabEmpty(activeTabId));
    }
    if (result.download) {
      triggerDownload(result.download.filename, result.download.content, result.download.mimeType);
    }
    return true;
  };

  // Direct fallback for the temp-tab → real-session rename: SessionTabSync
  // does the same rekey off the "session_started" SSE event, whichever
  // arrives first. REKEY_SESSION/UPDATE_TAB_ID are both idempotent (no-op if
  // the old id is already gone), so running this twice is safe.
  const handleSessionCreated = (tempTabId: string, sessionId: string) => {
    dispatch({ type: "REKEY_SESSION", oldId: tempTabId, newId: sessionId });
    projectDispatch({
      type: "UPDATE_TAB_ID",
      oldId: tempTabId,
      newId: sessionId,
      newTitle: "New session",
    });
  };

  const allChatTabs = Object.values(projectState.tabsByProject).flat();
  const activeSessionTab = tabs.find((t) => t.id === activeTabId);

  return (
    <div className="flex flex-col h-screen bg-zinc-950">
      <SessionTabSync />

      {/* Main content area */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left sidebar - project roots */}
        <ProjectSidebar
          isOpen={sidebarOpen}
          onToggle={() => setSidebarOpen(!sidebarOpen)}
        />

        {/* Center content */}
        <main className="flex flex-1 flex-col overflow-hidden">
          {/* Tabs context wraps header triggers + content panels */}
          <Tabs value={activeView} onValueChange={(v) => setActiveView(v as typeof activeView)} className="flex flex-col flex-1 overflow-hidden">
            <TopTabs activeTab={activeView} onTabSelect={(v) => setActiveView(v as typeof activeView)} />

            <div className="flex-1 overflow-hidden flex flex-col">
              <TabsContent value="files" forceMount className="flex-1 overflow-hidden m-0 flex flex-col">
                <EditorTabBar
                  editorTabs={editorTabs.map((t) => ({ id: t.id, path: t.path, isDirty: t.isDirty }))}
                  activeEditorTabId={activeEditorTabId}
                  onSelectTree={() => setActiveEditorTabId(null)}
                  onSelectTab={setActiveEditorTabId}
                  onCloseTab={requestCloseTab}
                />
                <div className="relative flex-1 overflow-hidden">
                  <div className={activeEditorTabId === null ? "absolute inset-0" : "absolute inset-0 hidden"}>
                    <FileTree onOpenFile={openFileAndShow} />
                  </div>
                  {editorTabs.map((et) => (
                    <div
                      key={et.id}
                      className={et.id === activeEditorTabId ? "absolute inset-0" : "absolute inset-0 hidden"}
                    >
                      <FileEditor
                        path={et.path}
                        content={et.content}
                        onChange={(value) => handleEditorChange(et.id, value)}
                        readOnly={false}
                        session={activeTabId ?? undefined}
                        diffVersion={et.diffVersion}
                        onSelectionChange={handleSelectionChange}
                      />
                    </div>
                  ))}
                </div>
              </TabsContent>

              <TabsContent value="git" forceMount className="flex-1 overflow-hidden m-0">
                <GitPanel onOpenFile={openFileAndShow} />
              </TabsContent>
              <TabsContent value="cron" forceMount className="flex-1 overflow-hidden m-0">
                <CronPanel />
              </TabsContent>
              <TabsContent value="assets" forceMount className="flex-1 overflow-hidden m-0">
                <AssetsPanel />
              </TabsContent>
              <TabsContent value="status" forceMount className="flex-1 overflow-hidden m-0">
                <StatusPanel onClose={() => setActiveView("sessions")} />
              </TabsContent>
              <TabsContent value="logs" forceMount className="flex-1 overflow-hidden m-0">
                <LogPanel active={activeView === "logs"} />
              </TabsContent>

              <TabsContent value="sessions" forceMount className="flex-1 overflow-hidden m-0">
                <div className="flex flex-col h-full">
                  <OpenSessionBar />
                  <SessionSubTabs />
                  <div className="relative flex-1 min-h-0 overflow-hidden">
                    {allChatTabs.map((tab) => (
                      <div
                        key={`${tab.id}:chat`}
                        className={
                          tab.projectPath === projectState.activeProject?.path &&
                          tab.id === activeTabId &&
                          tab.activeSubTab === "chat"
                            ? "absolute inset-0 flex flex-col"
                            : "absolute inset-0 hidden"
                        }
                      >
                        <div className="relative flex-1 min-h-0 overflow-hidden">
                          <ChatPanel sessionId={tab.id} />
                        </div>
                        <AgentPreview onOpenDetail={(runId) => openAgentDetail(tab.id, runId)} />
                        <ChatInput
                          onSlashCommand={handleCommand}
                          activeEditorContext={activeEditorContext}
                          sessionTabId={tab.id}
                          onSessionCreated={handleSessionCreated}
                        />
                      </div>
                    ))}
                    {allChatTabs.map((tab) => (
                      <div
                        key={`${tab.id}:agents`}
                        className={
                          tab.projectPath === projectState.activeProject?.path &&
                          tab.id === activeTabId &&
                          tab.activeSubTab === "agents"
                            ? "absolute inset-0"
                            : "absolute inset-0 hidden"
                        }
                      >
                        <AgentsPanel
                          sessionId={tab.id}
                          selectedRunId={selectedAgentRunId}
                          onSelectRun={setSelectedAgentRunId}
                        />
                      </div>
                    ))}
                    {allChatTabs.map((tab) => (
                      <div
                        key={`${tab.id}:changes`}
                        className={
                          tab.projectPath === projectState.activeProject?.path &&
                          tab.id === activeTabId &&
                          tab.activeSubTab === "changes"
                            ? "absolute inset-0"
                            : "absolute inset-0 hidden"
                        }
                      >
                        <ChangesPanel session={tab.id} />
                      </div>
                    ))}
                  </div>
                </div>
              </TabsContent>
            </div>
          </Tabs>

          {/* Status bar */}
          <StatusBar
            onCoworkToggle={() => setCoworkOpen(!coworkOpen)}
            onStatusClick={() => setActiveView("status")}
          />
        </main>

        {/* Right sidebar - cowork panel (only on Sessions view with the active session's sub-tab on Chat) */}
        {activeView === "sessions" && activeSessionTab?.activeSubTab === "chat" && (
          <CoworkSidebar
            isOpen={coworkOpen}
            onClose={() => setCoworkOpen(false)}
            activeAgent="build"
            onModelClick={openModelDialog}
            isMobile={isMobile}
          />
        )}
      </div>

      {/* Dialogs */}
      <SessionDialog />
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

      <FilePicker
        open={filePickerOpen}
        onClose={() => setFilePickerOpen(false)}
        onOpenFile={openFileAndShow}
      />
      <ConfirmCloseDialog
        path={pendingClose?.path ?? ""}
        open={pendingClose !== null}
        error={saveError}
        onSave={confirmSaveAndClose}
        onDiscard={confirmDiscardAndClose}
        onCancel={cancelClose}
      />
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
