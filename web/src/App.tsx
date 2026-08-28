import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Routes, Route } from "react-router-dom";
import { PanelLeft, PanelLeftClose, Plus } from "lucide-react";
import { useIsMobile } from "./hooks/useIsMobile";
import { ChatProvider, useChatDispatch, useChatStateRef, getSessionSlice } from "./stores/chatStore";
import { ProjectProvider, findProjectPathForTab, useProjectState } from "./stores/projectStore";
import { api } from "./api/client";
import ErrorBoundary from "./components/common/ErrorBoundary";
import ChatPanel from "./components/Chat/ChatPanel";
import AgentPreview from "./components/Chat/AgentPreview";
import AgentsPanel from "./components/Agents/AgentsPanel";
import ChatInput, { type SlashCommandResult } from "./components/Chat/ChatInput";
import StatusBar from "./components/common/StatusBar";
import StatusPanel from "./components/Status/StatusPanel";
import CommandPalette from "./components/common/CommandPalette";
import GitPanel from "./components/Git/GitPanel";
import ChangesPanel from "./components/Changes/ChangesPanel";
import FileTree from "./components/Files/FileTree";
import FileEditor from "./components/Files/FileEditor";
import LogPanel from "./components/Logs/LogPanel";
import TerminalTabs, { type TerminalTabsHandle } from "./components/Terminal/TerminalTabs";
import AssetsPanel from "./components/Assets/AssetsPanel";
import CronPanel from "./components/Cron/CronPanel";
import { Tabs, TabsContent } from "@/components/ui/tabs";
import TopTabs from "./components/Layout/TopTabs";
import { ProfileSwitcher } from "./components/ProfileSwitcher";
import SettingsPanel from "./components/Settings/SettingsPanel";
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
import { useResizableSidebar } from "./hooks/useResizableSidebar";
import { useEditorTabs } from "./hooks/useEditorTabs";
import { useChat } from "./hooks/useChat";
import { dispatchCommand } from "./components/Chat/commands";
import SessionPage from "./pages/SessionPage";
import FilePicker from "./components/Files/FilePicker";
import ConfirmCloseDialog from "./components/Files/ConfirmCloseDialog";
import { isNewSessionTabEmpty, rekeyDraft } from "./lib/tabDrafts";
import { rekeyQueue, clearQueue } from "./lib/tabQueue";
import { cancelLiveDeltas } from "./lib/sessionEvents";
import { notifyWailsRuntimeReady } from "./lib/wails";
import { eventBus } from "./lib/eventBus";
import { useSessionStatus } from "./hooks/useSessionStatus";
import { useTurnWatchdogAll } from "./hooks/useTurnWatchdog";
import FrontendMemoryReporter from "./lib/debug/frontendMemoryReporter";

type ModelDialogTab = "main" | "small" | "advisor" | "permission" | "recap" | "ocr" | "mask" | "commit" | "summary";

function StatusMetricsHydrator() {
  const dispatch = useChatDispatch();

  // Spending is server-pushed on the unified bus (`spending` envelopes) — the
  // 60s client poll is gone. Seed with one fetch so the value renders before
  // the first event (the emitter only publishes on change).
  useEffect(() => {
    let cancelled = false;
    api
      .getSpending()
      .then((res) => {
        if (!cancelled) dispatch({ type: "SET_SPENDING", spendingUSD: res.spending_usd });
      })
      .catch(console.error);
    const off = eventBus.on("spending", (env) => {
      const data = env.data as { spending_usd?: number };
      if (typeof data.spending_usd === "number") {
        dispatch({ type: "SET_SPENDING", spendingUSD: data.spending_usd });
      }
    });
    return () => {
      cancelled = true;
      off();
    };
  }, [dispatch]);

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
  // Imperative-only: getMessages() below reads this at call time, not during
  // render, so HomeApp must not re-render on every dispatch (every streamed
  // token) just to keep this reference "fresh" — see useChatStateRef's docs.
  const chatStateRef = useChatStateRef();
  const { state: projectState, tabs, activeTabId, dispatch: projectDispatch, openSessionTab, openNewSessionTab, closeSessionTab } = useProjectState();
  const { resolvePermission, pendingPermission } = useChat(activeTabId);
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [coworkOpen, setCoworkOpen] = useState(true);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [modelDialogTab, setModelDialogTab] = useState<ModelDialogTab>("main");
  const sidebar = useResizableSidebar();
  const fileTreePane = useResizableSidebar({
    storageKey: "ocode.ui.filetree_width",
    defaultWidth: 260,
    minWidth: 160,
    maxWidth: 500,
    collapsible: true,
  });

  // Part 05: per-session status on tab activation + streaming watchdog.
  useSessionStatus(activeTabId);
  // Watches every open tab (any project), not just the active one, so a
  // turn stalling in a backgrounded tab still gets detected and reconciled.
  const openSessionIds = useMemo(
    () => new Set(Object.values(projectState.tabsByProject).flat().map((t) => t.id)),
    [projectState.tabsByProject],
  );
  useTurnWatchdogAll(openSessionIds);

  // Declare the viewed projects on the shared bus (drives the server's
  // subscriber-aware git/spending emitters). All open tabs' projects count,
  // so background tabs in other projects keep receiving their events.
  useEffect(() => {
    const paths = [...new Set(Object.values(projectState.tabsByProject).flat().map((t) => t.projectPath))];
    eventBus.setProjects(paths);
  }, [projectState.tabsByProject]);
  const [cmdOpen, setCmdOpen] = useState(false);
  const [selectedAgentRunId, setSelectedAgentRunId] = useState<string | null>(null);
  const [activeView, setActiveView] = useState<
    "files" | "git" | "cron" | "assets" | "sessions" | "settings" | "terminal"
  >("sessions");
  useEffect(() => {
    const h = () => setActiveView("settings")
    window.addEventListener("ocode:open-settings-profiles", h)
    return () => window.removeEventListener("ocode:open-settings-profiles", h)
  }, [])
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
    reloadTabFromDisk,
    dismissExternalChange,
  } = useEditorTabs();

  // Opening a file from anywhere (tree, git diff, file picker) shows the
  // Files view and selects the editor tab.
  const openFileAndShow = useCallback(
    async (path: string, projectRoot?: string) => {
      await handleOpenFile(path, projectRoot);
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
    // OCR settings are global config, not session state — seed them here.
    // (Session status itself arrives via session-tagged bus events; the old
    // full SET_TUI_STATUS seed stays removed.)
    api
      .getTUIStatus()
      .then((res) => {
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

  // Desktop-only: complete the Wails "runtime ready" handshake by hand.
  // Every navigation gets a minimal native bridge unconditionally injected
  // as window._wails.invoke (see wails/v3 internal/runtime/runtime_*.go) —
  // that's why JS→Go calls already work. But WebviewWindow.ExecJS (used
  // below and by the native "Settings…" menu handler in
  // cmd/ocode-desktop/main.go) only runs immediately once the Go side's
  // internal `runtimeLoaded` flag is set; until then every ExecJS call is
  // queued forever. That flag is only set when the page posts the literal
  // message "wails:runtime:ready" — normally done by Wails' own
  // @wailsio/runtime JS module, reachable only via /wails/runtime.js on
  // Wails' own asset server. This SPA is served by ocode's own HTTP server
  // at an external origin and never loads that module, so nothing ever
  // sends that message and runtimeLoaded stays false for the app's entire
  // lifetime. Sending it ourselves via the already-injected invoke bridge
  // completes the same handshake without pulling in the full runtime.
  useEffect(() => {
    let timer: number | undefined;
    let attempts = 0;
    const maxAttempts = 200; // Five seconds; enough for desktop navigation, bounded in browsers.
    const attemptHandshake = () => {
      if (notifyWailsRuntimeReady()) {
        return;
      }
      attempts++;
      if (attempts < maxAttempts) {
        timer = window.setTimeout(attemptHandshake, 25);
      }
    };
    attemptHandshake();
    return () => {
      if (timer !== undefined) {
        window.clearTimeout(timer);
      }
    };
  }, []);

  // The native "Settings…" menu item's OnClick handler runs window.ExecJS to
  // dispatch this plain DOM CustomEvent directly into the page (see
  // cmd/ocode-desktop/main.go buildAppMenu). This does not depend on any
  // Wails-injected runtime global beyond the handshake above —
  // window.EmitEvent/window.wails.Events remain structurally unavailable
  // since this page never loads the full runtime module. No-op in the
  // browser (the event is simply never dispatched there).
  useEffect(() => {
    const handler = () => setActiveView("settings");
    window.addEventListener("ocode:open-settings", handler);
    return () => window.removeEventListener("ocode:open-settings", handler);
  }, []);

  const [filePickerOpen, setFilePickerOpen] = useState(false);

  useKeyboard({
    onNewSession: () => {
      openNewSessionTab(isNewSessionTabEmpty(activeTabId));
    },
    onNewTerminal: () => {
      // Ctrl/Cmd+T: if on terminal top-level tab, open a new terminal instance;
      // otherwise create a new session tab (same as Ctrl/Cmd+N). Terminal is
      // project-scoped so the handle is keyed by project path.
      if (activeView === "terminal") {
        const proj = projectState.activeProject?.path ?? "";
        terminalRefs.current.get(proj)?.openTerminal();
      } else {
        openNewSessionTab(isNewSessionTabEmpty(activeTabId));
      }
    },
    onCommandPalette: () => setCmdOpen(true),
    onFilePicker: () => setFilePickerOpen(true),
    onSave: () => {
      if (activeEditorTabId) {
        saveEditorTab(activeEditorTabId);
      }
    },
    onCloseSession: () => {
      // Cmd/Ctrl+W: close whatever is frontmost. On the Files view that is
      // the active editor tab; on the terminal top-level tab it is the active
      // terminal instance; otherwise the session tab itself. Mirrors each tab bar's
      // X button.
      if (activeView === "terminal") {
        const proj = projectState.activeProject?.path ?? "";
        if (terminalRefs.current.get(proj)?.closeActiveTerminal()) return;
        return;
      }
      if (activeView === "files") {
        if (activeEditorTabId) {
          requestCloseTab(activeEditorTabId);
        }
        return;
      }
      if (activeView !== "sessions" || !activeTabId) return;
      closeSessionTab(activeTabId);
      cancelLiveDeltas(activeTabId);
      clearQueue(activeTabId);
      dispatch({ type: "RESET", sessionId: activeTabId });
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

  const rekeySession = useCallback((tempTabId: string, sessionId: string) => {
    dispatch({ type: "REKEY_SESSION", oldId: tempTabId, newId: sessionId });
    rekeyQueue(tempTabId, sessionId);
    rekeyDraft(tempTabId, sessionId);
    projectDispatch({
      type: "UPDATE_TAB_ID",
      oldId: tempTabId,
      newId: sessionId,
      newTitle: "New session",
    });
  }, [dispatch, projectDispatch]);

  // Commands may be drained by a hidden ChatInput belonging to a background
  // tab. Keep the originating session explicit rather than routing a command
  // through whichever tab is active when its async work completes.
  const sendCommandToSession = useCallback(async (sessionId: string | null, content: string): Promise<boolean> => {
    if (!sessionId) return false;
    dispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
    dispatch({ type: "SET_ERROR", sessionId, error: null });
    try {
      if (sessionId.startsWith("new-")) {
        const projectPath = findProjectPathForTab(projectState, sessionId) ?? projectState.activeProject?.path;
        if (!projectPath) throw new Error("Select a project before starting a chat.");
        const result = await api.chat(content, undefined, undefined, sessionId, projectPath);
        rekeySession(sessionId, result.sessionId);
      } else {
        await api.sendMessage(sessionId, content);
      }
      return true;
    } catch (err) {
      dispatch({ type: "SET_ERROR", sessionId, error: err instanceof Error ? err.message : "send failed" });
      dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
      return false;
    }
  }, [dispatch, projectState, rekeySession]);

  const handleCommand = async (cmd: string, targetSessionId: string | null = activeTabId): Promise<SlashCommandResult> => {
    const baseCmd = cmd.split(" ")[0];
    const targetProjectPath = targetSessionId
      ? findProjectPathForTab(projectState, targetSessionId) ?? projectState.activeProject?.path
      : projectState.activeProject?.path;
    // Built-in quick actions that don't need the dispatch pipeline
    if (baseCmd === "/clear" || baseCmd === "/new") {
      openNewSessionTab(isNewSessionTabEmpty(targetSessionId), targetProjectPath);
      return { handled: true, accepted: true };
    }
    // Bare /model opens the model dialog. With a name argument it falls
    // through to the shared dispatch (TUI /models <name> parity).
    if (baseCmd === "/model" && !cmd.slice(baseCmd.length).trim()) {
      openModelDialog("main");
      return { handled: true, accepted: true };
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
        getLimitsConfig: () => api.getLimitsConfig(),
        setLimitsConfig: (fields) => api.setLimitsConfig(fields),
        getThinkingBudget: () => api.getThinkingBudget(),
        setThinkingBudget: (budget) => api.setThinkingBudget(budget),
        listModels: () => api.listModels(),
        getConfigModel: () => api.getConfigModel(),
        setConfigModel: (model) => api.setConfigModel(model),
        getFeaturesConfig: () => api.getFeaturesConfig(),
        setFeaturesConfig: (memoryEnabled, docPromptEnabled) => api.setFeaturesConfig(memoryEnabled, docPromptEnabled),
        getPathsInfo: () => api.getPathsInfo(),
        getPathsConfig: () => api.getPathsConfig(),
        setPathsConfig: (paths, uploadDir) => api.setPathsConfig(paths, uploadDir),
        getMemoryStatus: () => api.getMemoryStatus(),
        setBashRule: (prefix, level) => api.setBashRule(prefix, level),
        getPermissions: () => api.getPermissions(),
        getAutoContinue: () => api.getAutoContinue(),
        setAutoContinue: (fields) => api.setAutoContinue(fields),
        connectProvider: (provider, apiKey) => api.connectProvider(provider, apiKey),
        addProject: (path) => api.addProject(path),
        getDocsStatus: () => api.getDocsStatus(),
        docsInit: () => api.docsInit(),
        docsUpdate: (sessionId, focus) => api.docsUpdate(sessionId, focus),
        docsCleanup: (confirm) => api.docsCleanup(confirm),
        getImageGenConfig: () => api.getImageGenConfig(),
        setImageGenConfig: (cfg) => api.setImageGenConfig(cfg),
        getDiscoveryConfig: () => api.getDiscoveryConfig(),
        setDiscoveryConfig: (cfg) => api.setDiscoveryConfig(cfg),
        getLocalModelsConfig: () => api.getLocalModelsConfig(),
        setLocalModelsConfig: (models) => api.setLocalModelsConfig(models),
        syncLoginStart: () => api.syncLoginStart(),
        syncLogout: () => api.syncLogout(),
      },
      getMessages: () => getSessionSlice(chatStateRef.current, targetSessionId).messages,
      getSessionId: () => targetSessionId,
    });

    if (!result.handled) return { handled: false, accepted: true };

    if (result.openModelPicker) {
      openModelDialog("main");
      return { handled: true, accepted: true };
    }

    // Apply result effects
    if (result.messages) {
      for (const msg of result.messages) {
        if (targetSessionId) {
          dispatch({ type: "ADD_MESSAGE", sessionId: targetSessionId, message: msg });
        }
      }
    }
    if (result.prompt) {
      // Server-assembled prompt (/standup, /changes, /review, /learn,
      // /doc-sync, /mem update, /docs init): send through the normal chat
      // pipeline exactly like the TUI dispatching the same command.
      const accepted = await sendCommandToSession(targetSessionId, result.prompt);
      return { handled: true, startedTurn: true, accepted };
    }
    if (result.sessionId) {
      openSessionTab(result.sessionId, result.sessionId);
    }
    if (result.newSession) {
      openNewSessionTab(isNewSessionTabEmpty(targetSessionId), targetProjectPath);
    }
    if (result.download) {
      triggerDownload(result.download.filename, result.download.content, result.download.mimeType);
    }
    return { handled: true, accepted: true };
  };

  // Direct fallback for the temp-tab → real-session rename: SessionTabSync
  // does the same rekey off the "session_started" SSE event, whichever
  // arrives first. REKEY_SESSION/UPDATE_TAB_ID are both idempotent (no-op if
  // the old id is already gone), so running this twice is safe.
  const handleSessionCreated = (tempTabId: string, sessionId: string) => {
    rekeySession(tempTabId, sessionId);
  };

  const allChatTabs = Object.values(projectState.tabsByProject).flat();
  const activeSessionTab = tabs.find((t) => t.id === activeTabId);
  // Lazy display:none: keep visited tabs mounted (hidden) so scroll/virtualizer
  // state survives switches (instant CSS toggle), but avoid mounting all 40
  // panels eagerly on first load. Only tabs that have been visited once are
  // kept in the DOM — the rest return null until first activated.
  const visitedTabsRef = useRef<Set<string>>(new Set());
  if (activeSessionTab) {
    visitedTabsRef.current.add(`${activeSessionTab.id}:${activeSessionTab.activeSubTab}`);
  }

  // Refs to TerminalTabs instances so Ctrl/Cmd+T can open a new terminal.
  // Keyed by project path (terminal is project-scoped, not session-scoped,
  // so switching chat sessions never kills the pty).
  const terminalRefs = useRef<Map<string, TerminalTabsHandle>>(new Map());

  return (
    <div className="flex flex-col h-screen bg-zinc-950">
      <SessionTabSync />

      {/* Main content area */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left sidebar - project roots */}
        <ProjectSidebar
          isOpen={sidebarOpen}
          onToggle={() => setSidebarOpen(!sidebarOpen)}
          width={sidebarOpen ? sidebar.width : undefined}
        />

        {/* Sidebar resize handle */}
        {sidebarOpen && (
          <div
            ref={sidebar.handleRef}
            role="separator"
            aria-orientation="vertical"
            aria-valuemin={sidebar.minWidth}
            aria-valuemax={sidebar.maxWidth}
            aria-valuenow={sidebar.width}
            tabIndex={0}
            className="w-1 flex-shrink-0 cursor-col-resize bg-transparent hover:bg-primary/40 active:bg-primary/60 transition-colors"
            onPointerDown={sidebar.onPointerDown}
            onDoubleClick={sidebar.resetToDefault}
            onKeyDown={(e) => {
              const step = e.shiftKey ? 50 : 10;
              if (e.key === "ArrowLeft") {
                e.preventDefault();
                sidebar.setWidth(sidebar.width - step);
              } else if (e.key === "ArrowRight") {
                e.preventDefault();
                sidebar.setWidth(sidebar.width + step);
              } else if (e.key === "Home") {
                e.preventDefault();
                sidebar.resetToDefault();
              }
            }}
          />
        )}

        {/* Center content */}
        <main className="flex flex-1 flex-col overflow-hidden">
          <Tabs value={activeView} onValueChange={(v) => setActiveView(v as typeof activeView)} className="flex flex-col flex-1 overflow-hidden">
            <div className="flex items-center justify-between gap-2 border-b pr-2">
              <div className="flex-1 min-w-0">
                <TopTabs activeTab={activeView} onTabSelect={(v) => setActiveView(v as typeof activeView)} />
              </div>
              <ProfileSwitcher />
            </div>

            <div className="flex-1 overflow-hidden flex flex-col pb-2">
              {/* Terminal is project-scoped and must stay mounted even when not visible. It lives inside the Tabs root
                  so TopTabs (which uses TabsList/TabsTrigger) keeps its Radix context, but outside the non-terminal
                  content region so switching away never unmounts the WebSocket/pty. Visibility is toggled via CSS only. */}
              {(() => {
                const projectPaths = Array.from(
                  new Set(
                    [
                      ...((projectState.projects ?? []) as { path: string }[]).map((p) => p.path),
                      ...Object.keys(projectState.tabsByProject ?? {}),
                      ...(projectState.activeProject ? [projectState.activeProject.path] : []),
                    ].filter(Boolean) as string[],
                  ),
                );
                const activeProjectPath = projectState.activeProject?.path ?? "";
                return (
                  <div className={activeView === "terminal" ? "flex flex-1 overflow-hidden m-0 flex-col" : "hidden"}>
                    <div className="relative flex-1 min-h-0 overflow-hidden">
                      {projectPaths.map((pp) => (
                        <div
                          key={`${pp}:terminal`}
                          className={
                            pp === activeProjectPath ? "absolute inset-0" : "absolute inset-0 hidden"
                          }
                        >
                          <TerminalTabs
                            ref={(handle) => {
                              if (handle) terminalRefs.current.set(pp, handle);
                              else terminalRefs.current.delete(pp);
                            }}
                            active={pp === activeProjectPath && activeView === "terminal"}
                            projectPath={pp}
                          />
                        </div>
                      ))}
                    </div>
                  </div>
                );
              })()}

              <div className={activeView === "terminal" ? "hidden" : "flex flex-1 overflow-hidden flex-col"}>
              <TabsContent value="files" forceMount className="flex-1 overflow-hidden m-0 flex">
                <div
                  className="relative shrink-0 h-full overflow-hidden border-r border-zinc-800 transition-[width] duration-100"
                  style={{ width: fileTreePane.collapsed ? 0 : fileTreePane.width }}
                >
                  <div className="absolute inset-0" style={{ width: fileTreePane.width }}>
                    <FileTree onOpenFile={openFileAndShow} projectPath={projectState.activeProject?.path} />
                  </div>
                </div>
                {!fileTreePane.collapsed && (
                  <div
                    ref={fileTreePane.handleRef}
                    onPointerDown={fileTreePane.onPointerDown}
                    onDoubleClick={fileTreePane.resetToDefault}
                    className="w-1 shrink-0 cursor-col-resize hover:bg-zinc-700 active:bg-zinc-600"
                  />
                )}
                <button
                  onClick={fileTreePane.toggleCollapsed}
                  title={fileTreePane.collapsed ? "Show file tree" : "Hide file tree"}
                  className="shrink-0 self-start mt-1 p-0.5 rounded hover:bg-zinc-800 text-zinc-500 hover:text-zinc-300"
                >
                  {fileTreePane.collapsed ? <PanelLeft className="w-3.5 h-3.5" /> : <PanelLeftClose className="w-3.5 h-3.5" />}
                </button>
                <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
                  <EditorTabBar
                    editorTabs={editorTabs.map((t) => ({ id: t.id, path: t.path, isDirty: t.isDirty }))}
                    activeEditorTabId={activeEditorTabId}
                    onSelectTab={setActiveEditorTabId}
                    onCloseTab={requestCloseTab}
                  />
                  <div className="relative flex-1 overflow-hidden">
                    {editorTabs.length === 0 && (
                      <div className="absolute inset-0 flex items-center justify-center text-sm text-zinc-500">
                        No file open
                      </div>
                    )}
                    {editorTabs.map((et) => (
                      <div
                        key={et.id}
                        className={et.id === activeEditorTabId ? "absolute inset-0" : "absolute inset-0 hidden"}
                      >
                        <FileEditor
                          path={et.path}
                          persistKey={et.id}
                          content={et.content}
                          onChange={(value) => handleEditorChange(et.id, value)}
                          readOnly={false}
                          session={activeTabId ?? undefined}
                          diffVersion={et.diffVersion}
                          onSelectionChange={handleSelectionChange}
                          externalChange={et.externalChange}
                          onReloadFromDisk={() => reloadTabFromDisk(et.id)}
                          onDismissExternalChange={() => dismissExternalChange(et.id)}
                        />
                      </div>
                    ))}
                  </div>
                </div>
              </TabsContent>

              <TabsContent value="git" forceMount className="flex-1 overflow-hidden m-0">
                <GitPanel onOpenFile={openFileAndShow} projectPath={projectState.activeProject?.path} active={activeView === "git"} />
              </TabsContent>
              <TabsContent value="cron" forceMount className="flex-1 overflow-hidden m-0">
                <CronPanel active={activeView === "cron"} />
              </TabsContent>
              <TabsContent value="assets" forceMount className="flex-1 overflow-hidden m-0">
                <AssetsPanel />
              </TabsContent>
              <TabsContent value="settings" forceMount className="flex-1 overflow-hidden m-0">
                <SettingsPanel />
              </TabsContent>

              <TabsContent value="sessions" forceMount className="flex-1 overflow-hidden m-0">
                <div className="flex flex-col h-full">
                  <OpenSessionBar />
                  <SessionSubTabs />
                  <div className="relative flex-1 min-h-0 overflow-hidden">
                    {tabs.length === 0 && (
                      <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 text-zinc-500">
                        <p className="text-sm">No open sessions for this project</p>
                        <button
                          onClick={() => openNewSessionTab(false)}
                          className="inline-flex items-center gap-1.5 rounded-md border border-zinc-700 px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100 transition-colors"
                        >
                          <Plus className="w-3.5 h-3.5" />
                          New session
                        </button>
                      </div>
                   )}
                    {allChatTabs.map((tab) => {
                      const isActive = tab.projectPath === projectState.activeProject?.path && tab.id === activeTabId && tab.activeSubTab === "chat";
                      const key = `${tab.id}:chat`;
                      if (!visitedTabsRef.current.has(key) && !isActive) return null;
                      return (
                        <div
                          key={key}
                          className={isActive ? "absolute inset-0 flex flex-col" : "absolute inset-0 hidden"}
                        >
                          <div className="relative flex-1 min-h-0 overflow-hidden">
                            <ChatPanel sessionId={tab.id} />
                          </div>
                          <AgentPreview onOpenDetail={(runId) => openAgentDetail(tab.id, runId)} />
                          <ChatInput onSlashCommand={handleCommand} activeEditorContext={activeEditorContext} sessionTabId={tab.id} onSessionCreated={handleSessionCreated} />
                        </div>
                      );
                    })}
                    {allChatTabs.map((tab) => {
                      const isActive = tab.projectPath === projectState.activeProject?.path && tab.id === activeTabId && tab.activeSubTab === "agents";
                      const key = `${tab.id}:agents`;
                      if (!visitedTabsRef.current.has(key) && !isActive) return null;
                      return (
                        <div key={key} className={isActive ? "absolute inset-0" : "absolute inset-0 hidden"}>
                          <AgentsPanel sessionId={tab.id} selectedRunId={selectedAgentRunId} onSelectRun={setSelectedAgentRunId} />
                        </div>
                      );
                    })}
                    {allChatTabs.map((tab) => {
                      const isActive = tab.projectPath === projectState.activeProject?.path && tab.id === activeTabId && tab.activeSubTab === "changes";
                      const key = `${tab.id}:changes`;
                      if (!visitedTabsRef.current.has(key) && !isActive) return null;
                      return (
                        <div key={key} className={isActive ? "absolute inset-0" : "absolute inset-0 hidden"}>
                          <ChangesPanel session={tab.id} active={isActive} />
                        </div>
                      );
                    })}
                    {allChatTabs.map((tab) => {
                      const isActive = tab.projectPath === projectState.activeProject?.path && tab.id === activeTabId && tab.activeSubTab === "logs";
                      const key = `${tab.id}:logs`;
                      if (!visitedTabsRef.current.has(key) && !isActive) return null;
                      return (
                        <div key={key} className={isActive ? "absolute inset-0" : "absolute inset-0 hidden"}>
                          <LogPanel active={isActive} sessionId={tab.id} />
                        </div>
                      );
                    })}
                    {allChatTabs.map((tab) => {
                      const isActive = tab.projectPath === projectState.activeProject?.path && tab.id === activeTabId && tab.activeSubTab === "status";
                      const key = `${tab.id}:status`;
                      if (!visitedTabsRef.current.has(key) && !isActive) return null;
                      return (
                        <div key={key} className={isActive ? "absolute inset-0" : "absolute inset-0 hidden"}>
                          <StatusPanel onClose={() => projectDispatch({ type: "SET_TAB_SUB_TAB", id: tab.id, subTab: "chat" })} />
                        </div>
                      );
                    })}
                  </div>
                </div>
              </TabsContent>
            </div>
          </div>
          </Tabs>

          {/* Status bar — only on chat sub-tab */}
          {activeView === "sessions" && activeSessionTab?.activeSubTab === "chat" && (
            <StatusBar
              onCoworkToggle={() => setCoworkOpen(!coworkOpen)}
              onStatusClick={() => {
                setActiveView("sessions");
                if (activeTabId) {
                  projectDispatch({ type: "SET_TAB_SUB_TAB", id: activeTabId, subTab: "status" });
                } else {
                  const newId = openNewSessionTab(true);
                  if (newId) {
                    projectDispatch({ type: "SET_TAB_SUB_TAB", id: newId, subTab: "status" });
                  }
                }
              }}
            />
          )}
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
        purpose={modelDialogTab}
      />

      {/* Permission Dialog */}
      {pendingPermission && (
        <PermissionDialog
          open={true}
          tool={pendingPermission.tool}
          command={pendingPermission.command}
          rule={pendingPermission.rule}
          summary={pendingPermission.summary}
          denyReason={pendingPermission.deny_reason}
          modelUnavailable={pendingPermission.model_unavailable}
          scope={pendingPermission.scope}
          prefix={pendingPermission.prefix}
          outOfScopePath={pendingPermission.out_of_scope_path}
          requestId={pendingPermission.request_id}
          onDecide={resolvePermission}
        />
      )}

      <FilePicker
        open={filePickerOpen}
        onClose={() => setFilePickerOpen(false)}
        onOpenFile={openFileAndShow}
        projectPath={projectState.activeProject?.path}
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
          <FrontendMemoryReporter />
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
