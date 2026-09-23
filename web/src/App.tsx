import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import pkg from "../package.json";
import { Routes, Route } from "react-router-dom";
import { PanelLeft, PanelLeftClose, PanelRight, PanelRightClose, Plus, X } from "lucide-react";
import { useIsMobile } from "./hooks/useIsMobile";
import { ChatProvider, useChatDispatch, useChatStateRef, getSessionSlice } from "./stores/chatStore";
import { ProjectProvider, findProjectPathForTab, useProjectState } from "./stores/projectStore";
import { TerminalProvider, useTerminalState } from "./stores/terminalStore";
import { BrowserTabsProvider, useAllBrowserTabs, useBrowserTabs, useBrowserTabsDispatch } from "./stores/browserTabsStore";
import { BrowserPanel } from "./components/Browser/BrowserPanel";
import PreviewHost from "./components/Preview/PreviewHost";
import PreviewTabPage from "./components/Preview/PreviewTabPage";
import { usePreviewActivation } from "./components/Preview/usePreviewActivation";
import { PREVIEW_CONTEXT_EVENT, type PreviewSelection } from "./lib/previewKind";
import { useBrowserStore, browserActions, type StateKey } from "./lib/browserStore";
import { loadViewStateForProject, saveViewStateForProject, type FocusedKind } from "./lib/viewPersistence";
import { sessionAskSurfaceVisible } from "./lib/dialogScope";
import { api, isRemoteSession, authToken, setAuthFailureHandler } from "./api/client";
import ErrorBoundary from "./components/common/ErrorBoundary";
import ActionErrorToast from "./components/common/ActionErrorToast";
import { reportActionError } from "./lib/actionErrors";
import AttentionSoundBridge from "./components/common/AttentionSoundBridge";
import RemoteReconnect from "./components/RemoteReconnect";
import ChatPanel from "./components/Chat/ChatPanel";
import RemoteVersionBanner from "./components/Chat/RemoteVersionBanner";
import AgentPreview from "./components/Chat/AgentPreview";
import AgentsPanel from "./components/Agents/AgentsPanel";
import ChatInput, { type SlashCommandResult } from "./components/Chat/ChatInput";
import StatusBar from "./components/common/StatusBar";
import StatusPanel from "./components/Status/StatusPanel";
import CommandPalette from "./components/common/CommandPalette";
import GitPanel from "./components/Git/GitPanel";
import ChangesPanel from "./components/Changes/ChangesPanel";
import FileTree from "./components/Files/FileTree";
import FileTabContent from "./components/Files/FileTabContent";
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
import UnifiedTabBar from "./components/Layout/UnifiedTabBar";
import SessionSubTabs from "./components/Layout/SessionSubTabs";
import SessionTabSync from "./components/Layout/SessionTabSync";
import CoworkSidebar from "./components/Layout/CoworkSidebar";
import { shouldRenderCoworkSidebar } from "./components/Layout/coworkSidebarVisibility";
import { shouldRenderSidePane } from "./lib/sidePaneVisibility";
import ModelDialog from "./components/Layout/ModelDialog";
import ShareDialog from "./components/Layout/ShareDialog";
import PermissionDialog from "./components/Chat/PermissionDialog";
import QuestionDialog from "./components/Chat/QuestionDialog";
import { useKeyboard } from "./hooks/useKeyboard";
import { useTheme } from "./hooks/useTheme";
import { useResizableSidebar } from "./hooks/useResizableSidebar";
import { useEditorTabs } from "./hooks/useEditorTabs";
import {
  resolveVisibleEditorTabId,
  visibleEditorTabs as visibleEditorTabsForProject,
} from "./components/Files/editorTabsPersistence";
import { useChat } from "./hooks/useChat";
import { dispatchCommand } from "./components/Chat/commands";
import SessionPage from "./pages/SessionPage";
import FilePicker from "./components/Files/FilePicker";
import ConfirmCloseDialog from "./components/Files/ConfirmCloseDialog";
import { isNewSessionTabEmpty, rekeyDraft } from "./lib/tabDrafts";
import { rekeyQueue, clearQueue } from "./lib/tabQueue";
import { rekeyInputHistory, clearInputHistory } from "./lib/tabInputHistory";
import { cancelLiveDeltas, closeSessionBackend } from "./lib/sessionEvents";
import { notifyWailsRuntimeReady } from "./lib/wails";
import { setPendingHighlight, peekPendingHighlight } from "./lib/fileSearchHighlight";
import { eventBus } from "./lib/eventBus";
import { OPEN_FILE_EVENT } from "./lib/fileLinks";
import { tabFocusActions, useTabFocusRequest } from "./lib/tabFocus";
import { useSessionStatus } from "./hooks/useSessionStatus";
import { useTurnWatchdogAll } from "./hooks/useTurnWatchdog";
import { useSessionRevisionSync } from "./hooks/useSessionRevisionSync";
import FrontendMemoryReporter from "./lib/debug/frontendMemoryReporter";
import { __setRevoker } from "./lib/browserStore";
import { revokeBrowseSession } from "./api/client";
import { getTrustedTerminalProject } from "./lib/trustedProject";
import { resolveSessionHost, useSessionHost } from "./hooks/useSessionHost";
import { SpeechProvider } from "./components/Speech/SpeechProvider";
import SpeechToolbar from "./components/Speech/SpeechToolbar";

/** Shared frozen empty array. Used as the default for props that would
 *  otherwise be a fresh `[]` on every render, which would defeat `memo` on the
 *  component receiving it. */
const EMPTY_STRING_ARRAY: string[] = [];

// Browse panel close → revoke the server-side browse session. Wired here
// (module scope, once) rather than inside browserStore.ts to avoid a
// client→store→client import cycle.
__setRevoker(revokeBrowseSession);

// Canonical union lives on ModelDialog (PURPOSE_TITLES keys) — re-aliased here
// so App and ModelDialog can never drift apart when a new purpose is added.
type ModelDialogTab = import("./components/Layout/ModelDialog").ModelDialogTab;

/**
 * Resolve terminal routing only from the latest successful project snapshot.
 * An absent path is deliberately not treated as local: persisted tabs can
 * outlive a project, and connecting them without trusted metadata could turn
 * a previously remote shell into a local one.
 *
 * Kept re-exported here for existing importers (App.browser.test.tsx); the
 * implementation lives in lib/trustedProject so the Port forwards panel can use
 * the same rule without an App ↔ Layout import cycle.
 */
export { getTrustedTerminalProject };

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
  useEffect(() => {
    const activeTab = activeTabId ? tabs.find((t) => t.id === activeTabId) || Object.values(projectState.tabsByProject).flat().find((t) => t.id === activeTabId) : null;
    const sessionTitle = activeTab?.title?.trim() || projectState.activeProject?.path?.split("/").pop() || "";
    document.title = sessionTitle ? "ocode - " + sessionTitle : ("ocode - " + (pkg.version || ""));
  }, [activeTabId, projectState.activeProject, tabs, projectState.tabsByProject]);
  const { resolvePermission, pendingPermission, pendingQuestion, askContext, submitQuestionAnswers, cancelQuestion } = useChat(activeTabId);
  // Host of the active session's project. The model dialog and the command
  // context route their session-scoped calls there so a remote session's model
  // list and context come from that host's server, never the local one.
  const activeSessionHost = useSessionHost(activeTabId ?? undefined);
  // Start collapsed on phones: the sidebar is an off-canvas drawer there, and
  // an inline open drawer would cover the whole viewport on first paint (the
  // media-query listener below only reacts to a breakpoint *change*, so a
  // direct load at ≤767px never fires it). Desktop keeps the previous default.
  const [sidebarOpen, setSidebarOpen] = useState(() =>
    typeof window === "undefined" ? true : window.innerWidth >= 768,
  );
  // Same phone default for the right rail: on mobile it is a fixed overlay
  // with a scrim, so opening by default would cover the workspace on load.
  const [coworkOpen, setCoworkOpen] = useState(() =>
    typeof window === "undefined" ? true : window.innerWidth >= 768,
  );
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
  useSessionStatus(activeTabId, activeSessionHost);
  // Watches every open tab (any project), not just the active one, so a
  // turn stalling in a backgrounded tab still gets detected and reconciled.
  const openSessionIds = useMemo(
    () => new Set(Object.values(projectState.tabsByProject).flat().map((t) => t.id)),
    [projectState.tabsByProject],
  );
  // Per-session SSH/WSL host for the watchdog's reconcile fetch. A remote
  // session's /state + transcript live on its host's server; without the host
  // the reconcile 404s and a stalled remote turn is never recovered.
  const sessionHosts = useMemo(() => {
    const map = new Map<string, string | undefined>();
    for (const tab of Object.values(projectState.tabsByProject).flat()) {
      map.set(tab.id, resolveSessionHost(projectState, tab.id));
    }
    return map;
  }, [projectState, projectState.tabsByProject]);
  useTurnWatchdogAll(openSessionIds, sessionHosts);
  // Cross-process sync: an ocode server sharing this project (desktop + dev
  // server, TUI) writes the same session files, but its live events never
  // reach our bus. Revalidate every open tab's stored revision so its writes
  // — e.g. a /compact in the other UI — converge here too.
  useSessionRevisionSync(openSessionIds, sessionHosts);

  // Declare the viewed projects on the shared bus (drives the server's
  // subscriber-aware git/spending emitters). All open tabs' projects count,
  // so background tabs in other projects keep receiving their events.
  useEffect(() => {
    const paths = [...new Set(Object.values(projectState.tabsByProject).flat().map((t) => t.projectPath))];
    eventBus.setProjects(paths);
  }, [projectState.tabsByProject]);

  // Declare the remote hosts with at least one open tab so the bus keeps a
  // per-host `/api/events` stream alongside the local one. Derived from every
  // open tab (not the active project): a host leaves the set only when its
  // LAST tab closes, so background tabs in a remote project keep receiving
  // their events. `resolveSessionHost` reads only `tabsByProject` and
  // `projects`, so both are the complete dependency set for this derivation.
  useEffect(() => {
    const hosts = new Set<string>();
    for (const tab of Object.values(projectState.tabsByProject).flat()) {
      const host = resolveSessionHost(projectState, tab.id);
      if (host) hosts.add(host);
    }
    eventBus.setHosts([...hosts]);
  }, [projectState.tabsByProject, projectState.projects]);
  const [cmdOpen, setCmdOpen] = useState(false);
  const [selectedAgentRunId, setSelectedAgentRunId] = useState<string | null>(null);
  const [activeView, setActiveView] = useState<
    "files" | "git" | "cron" | "assets" | "sessions" | "settings"
  >("sessions");
  // Which half of the merged Sessions tab (chat vs terminal) is currently
  // shown. Restored from per-project persistence on project switch.
  const [focusedKind, setFocusedKind] = useState<FocusedKind>("chat");
  const activeProjectPath = projectState.activeProject?.path ?? "";
  // Every open session tab (any project) + whether the chat half of the
  // Sessions view is actually on screen — feeds AttentionSoundBridge so a
  // backgrounded chat can chime when it finishes / stalls / waits on a dialog.
  const attentionTabs = useMemo(
    () =>
      Object.values(projectState.tabsByProject)
        .flat()
        .map((t) => ({ id: t.id, projectPath: t.projectPath })),
    [projectState.tabsByProject],
  );
  const chatVisible = activeView === "sessions" && focusedKind === "chat";
  const { activeId: activeBrowserId, closeBrowserTab } = useBrowserTabs(activeProjectPath);
  const allBrowserTabs = useAllBrowserTabs();
  const [activatedBrowserKeys, setActivatedBrowserKeys] = useState<Set<string>>(() => new Set());
  const browserTabsDispatch = useBrowserTabsDispatch();
  // browse_newtab: file the tab under its owning project when the event
  // names one; an unknown owner lands in the active project's strip.
  const openBackgroundBrowserTab = useCallback(
    (id: string, project?: string) =>
      browserTabsDispatch({ type: "OPEN_BACKGROUND", project: project || activeProjectPath, id }),
    [browserTabsDispatch, activeProjectPath],
  );
  // Closing the last browser tab (X button or Cmd+W) must not leave the
  // center region blank while "browser" is still the focused kind.
  useEffect(() => {
    if (focusedKind === "browser" && !activeBrowserId) setFocusedKind("chat");
  }, [focusedKind, activeBrowserId]);
  const activeBrowserKey = focusedKind === "browser" && activeBrowserId ? `tab:${activeBrowserId}` : null;
  useEffect(() => {
    if (!activeBrowserKey) return;
    setActivatedBrowserKeys((current) => {
      if (current.has(activeBrowserKey)) return current;
      const next = new Set(current);
      next.add(activeBrowserKey);
      return next;
    });
  }, [activeBrowserKey]);
  useEffect(() => {
    const liveKeys = new Set(allBrowserTabs.map(({ tab }) => `tab:${tab.id}`));
    setActivatedBrowserKeys((current) => {
      let changed = false;
      const next = new Set<string>();
      for (const key of current) {
        if (liveKeys.has(key)) next.add(key);
        else changed = true;
      }
      return changed ? next : current;
    });
  }, [allBrowserTabs]);
  // The side browser panel accompanies chat/terminal focus (never the
  // full-width browser *tab*, which has its own `tab:` state surface).
  const sidePanelKind = focusedKind === "terminal" ? "term" : "chat";
  const sideStateKey: StateKey | null =
    activeTabId && focusedKind !== "browser" ? `side:${sidePanelKind}:${activeTabId}` : null;
  const sideTabState = useBrowserStore(sideStateKey ?? ("side:chat:" as StateKey));
  // Tracks keys whose panel was explicitly closed by the user (via
  // the close button or toggle), so the tab-switch effect below does
  // not immediately reopen it.
  const panelClosedByUser = useRef<Set<string>>(new Set());
  const browserOpen = !!sideStateKey && !!sideTabState?.panelOpen;
  const browserPane = useResizableSidebar({
    storageKey: "ocode.ui.browser_width",
    defaultWidth: 480,
    minWidth: 320,
    maxWidth: 1400,
    collapsible: true,
  });
  // Restore per-project view state on project switch. Falls back to defaults
  // for new/unknown projects.
  //
  // useLayoutEffect, NOT useEffect: the active project changes on a normal
  // render, so a passive effect would let the browser PAINT the new project
  // with the outgoing project's tab (e.g. Files) before swapping to the saved
  // one (e.g. Sessions) — a visible one-frame flash on every project switch
  // whose saved view differs. A layout effect applies the restore in the same
  // commit, before paint, so the first frame is already correct. It runs on
  // mount too, which is what makes a deep-linked session open on the right tab
  // without a flash.
  useLayoutEffect(() => {
    const path = projectState.activeProject?.path;
    if (!path) return;
    const saved = loadViewStateForProject(path);
    if (saved) {
      setActiveView(saved.view);
      setFocusedKind(saved.focusedKind);
    } else {
      setActiveView("sessions");
      setFocusedKind("chat");
    }
  }, [projectState.activeProject?.path]);
  // Persist view state whenever it changes (debounced).
  useEffect(() => {
    const path = projectState.activeProject?.path;
    if (!path) return;
    const t = setTimeout(() => {
      saveViewStateForProject(path, { view: activeView, focusedKind });
    }, 300);
    return () => clearTimeout(t);
  }, [projectState.activeProject?.path, activeView, focusedKind]);

  // Reveal a tab opened from outside this component (the remote project's
  // chat/terminal inventory in the sidebar). Three properties make the reveal
  // win over the per-project view restore above:
  //  - it is declared AFTER that restore, so on a project switch the restore
  //    has already run in the same commit (layout effects run in declaration
  //    order); this pass then overwrites it (last writer wins — both setters
  //    batch into one re-render);
  //  - it is a PASSIVE effect, so it flushes before paint — no one-frame flash
  //    of the restored view;
  //  - it waits until the request's (path, host) is the active project,
  //    otherwise it stays queued, so it can't be applied against the outgoing
  //    project while the switch lands. The sidebar selects the project and
  //    binds the tab to it as part of the same click.
  const pendingTabFocus = useTabFocusRequest();
  const { setActiveId: setActiveTerminalId } = useTerminalState();
  useEffect(() => {
    if (!pendingTabFocus) return;
    if (pendingTabFocus.projectPath !== (projectState.activeProject?.path ?? "")) return;
    // Path alone is not identity: a local and a remote project can share an
    // absolute path. The producer (RemoteProjectStatus.revealTab) gates on
    // (path, host); mirror that here so a request for the remote `/srv` is
    // never applied to the local one.
    if ((pendingTabFocus.host ?? "") !== (projectState.activeProject?.host ?? "")) return;
    if (pendingTabFocus.kind === "terminal" && pendingTabFocus.terminalId) {
      setActiveTerminalId(pendingTabFocus.projectPath, pendingTabFocus.terminalId, pendingTabFocus.host);
    }
    setActiveView("sessions");
    setFocusedKind(pendingTabFocus.kind);
    tabFocusActions.clear();
  }, [pendingTabFocus, projectState.activeProject?.path, projectState.activeProject?.host, setActiveTerminalId]);
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
    toggleIncludeInContext,
    closeTabsForPaths,
    renameTabPath,
    pendingClose,
    confirmSaveAndClose,
    confirmDiscardAndClose,
    cancelClose,
    saveError,
    saveEditorTab,
    forceSaveEditorTab,
    reloadTabFromDisk,
    dismissExternalChange,
  } = useEditorTabs();

  // Editor tabs are global state (so switching projects preserves each
  // project's open files), but the Files tab must only ever SHOW the ACTIVE
  // project's tabs. Otherwise a file left open in a remote SSH project stays
  // on screen after switching to a local project (and vice versa) — the
  // "files tab shows remote files while a local project is focused" bug.
  //
  // Scoping is a visibility concern only, NOT a mounting one: every project's
  // panes stay mounted and are hidden with CSS (same convention the chat,
  // terminal, and browser surfaces use). Unmounting them reset the viewer
  // state to page 1 / zoom 100% / scroll 0 on every project switch — the
  // "PDF re-rendered from the start" bug. `visibleEditorTabs` still drives the
  // tab bar, the active id, context attachments, and Cmd+S/Cmd+W; only the
  // pane rendering below iterates the full `editorTabs`.
  const visibleEditorTabs = useMemo(
    () => visibleEditorTabsForProject(editorTabs, projectState.activeProject),
    [editorTabs, projectState.activeProject],
  );
  // The globally-active tab id may point at a tab that belongs to another
  // project (hidden). Everything user-facing (tab bar, panes, Cmd+S/Cmd+W,
  // chat attachment) must use an id that is actually visible here.
  const visibleActiveEditorTabId = useMemo(
    () => resolveVisibleEditorTabId(visibleEditorTabs, activeEditorTabId),
    [visibleEditorTabs, activeEditorTabId],
  );
  // Identity set of visible tab ids: pane rendering keeps every project's tabs
  // mounted, so a per-tab props decision (e.g. which session's diff
  // decorations to fetch) needs a fast "is this tab in the active project".
  const visibleEditorTabIds = useMemo(() => new Set(visibleEditorTabs.map((t) => t.id)), [visibleEditorTabs]);

  // Editor tabs opted into the LLM loop, preserving their project root so we
  // can filter per session-tab and avoid leaking files across projects. Omit
  // `includeInContext` (pre-feature) defaults to true.
  const contextFileEntries = useMemo(
    () => visibleEditorTabs.filter((t) => t.includeInContext !== false).map((t) => ({ path: t.path, projectRoot: t.projectRoot })),
    [visibleEditorTabs],
  );

  // Effective active editor context respects the include toggle. Unchecking
  // the file in the preview (EditorTabBar) must immediately remove it from
  // the chat attachment, otherwise the next send still includes it via
  // ChatInput's `activeEditorContext` even though the tab is excluded.
  const effectiveActiveEditorContext = useMemo(() => {
    if (!activeEditorContext) return null;
    const tab = visibleEditorTabs.find(
      (t) => t.path === activeEditorContext.path && (t.projectRoot ?? "") === (activeEditorContext.projectRoot ?? ""),
    );
    if (tab && tab.includeInContext === false) return null;
    // A context left over from a tab that is now hidden (project switched) must
    // not be attached to the newly-focused project's chat.
    if (!tab) return null;
    return activeEditorContext;
  }, [activeEditorContext, visibleEditorTabs]);

  // Mobile responsive
  const isMobile = useIsMobile();

  // ── Sidebar PreviewHost: AI/file-tree activation + highlight context ──
  // The `preview_open` agent tool and `ocode:open-preview` events (file tree
  // "Preview in sidebar", diagram node links) arrive as request + nonce; a
  // fresh nonce opens the side panel so the file is actually visible.
  const { request: previewRequest, nonce: previewNonce, consume: consumePreviewActivation } = usePreviewActivation(activeTabId);
  const [previewContext, setPreviewContext] = useState<PreviewSelection | null>(null);

  useEffect(() => {
    if (previewNonce === 0) return;
    // Mobile has no side pane, so surface the activation in the session's
    // Preview sub-tab instead — PreviewTabPage consumes the request + nonce.
    if (isMobile) {
      if (activeTabId) projectDispatch({ type: "SET_TAB_SUB_TAB", id: activeTabId, subTab: "preview" });
      return;
    }
    if (!sideStateKey) return;
    panelClosedByUser.current.delete(sideStateKey);
    browserActions.open(sideStateKey);
  }, [previewNonce, sideStateKey, isMobile, activeTabId, projectDispatch]);

  // Preview highlight (Ask LLM) → chat composer chip; cleared on tab switch.
  useEffect(() => {
    const onCtx = (e: Event) => {
      const detail = (e as CustomEvent<PreviewSelection>).detail;
      if (detail?.path) setPreviewContext(detail);
    };
    window.addEventListener(PREVIEW_CONTEXT_EVENT, onCtx as EventListener);
    return () => window.removeEventListener(PREVIEW_CONTEXT_EVENT, onCtx as EventListener);
  }, []);
  useEffect(() => {
    setPreviewContext(null);
  }, [activeTabId]);

  // When the active chat/terminal tab changes, the sideStateKey changes.
  // Propagate the browser open state so the right-pane browser doesn't
  // disappear just because the session/tab switched.
  const prevSideTabStateRef = useRef<ReturnType<typeof useBrowserStore>>(undefined);
  useEffect(() => {
    const prevState = prevSideTabStateRef.current;
    const prevOpen = prevState?.panelOpen ?? false;
    const prevCollapsed = prevState?.collapsed ?? false;
    if (!sideStateKey) {
      prevSideTabStateRef.current = sideTabState;
      return;
    }
    const currentExists = !!sideTabState;
    if (prevOpen && !currentExists && !panelClosedByUser.current.has(sideStateKey)) {
      browserActions.open(sideStateKey);
      if (prevCollapsed) browserActions.setCollapsed(sideStateKey, true);
    }
    panelClosedByUser.current.delete(sideStateKey);
    prevSideTabStateRef.current = sideTabState;
  }, [sideStateKey, sideTabState]);

  useEffect(() => {
    const onDelete = (e: Event) => {
      const d = (e as CustomEvent).detail as { paths: string[]; projectRoot?: string; host?: string };
      if (d?.paths?.length) closeTabsForPaths(d.paths, d.projectRoot, d.host);
    };
    const onRename = (e: Event) => {
      const d = (e as CustomEvent).detail as { oldPath: string; newPath: string; projectRoot?: string; host?: string };
      if (d?.oldPath && d?.newPath) renameTabPath(d.oldPath, d.newPath, d.projectRoot, d.host);
    };
    window.addEventListener("ocode:fs-delete", onDelete as EventListener);
    window.addEventListener("ocode:fs-rename", onRename as EventListener);
    return () => {
      window.removeEventListener("ocode:fs-delete", onDelete as EventListener);
      window.removeEventListener("ocode:fs-rename", onRename as EventListener);
    };
  }, [closeTabsForPaths, renameTabPath]);

  // Opening a file from anywhere (tree, git diff, file picker) shows the
  // Files view and selects the editor tab. When line/query are provided (content search),
  // highlight is set via pending store (for newly mounted editors) and via event
  // (for already-mounted editors) with bounded retry.
  const openFileAndShow = useCallback(
    async (path: string, projectRoot?: string, line?: number, query?: string) => {
      // Resolve the host for this project root. When the root is the ACTIVE
      // project's, the active project is authoritative — a path-only lookup
      // would be ambiguous if a remote SSH project and a local project share an
      // absolute path, and could route a local open through the remote host.
      const activeProject = projectState.activeProject;
      const host = activeProject && activeProject.path === projectRoot
        ? activeProject.host
        : projectState.projects.find((p) => p.path === projectRoot)?.host;
      if (query && query.trim()) {
        setPendingHighlight(path, query.trim(), line, projectRoot);
      } else if (line && line > 0) {
        // Line-only highlight (from chat file links) still needs dispatch
        setPendingHighlight(path, "", line, projectRoot);
      }
      await handleOpenFile(path, projectRoot, host);
      setActiveView("files");
      if ((query && query.trim()) || (line && line > 0)) {
        const detail = { path, query: query?.trim() ?? "", line, projectRoot };
        // Immediate dispatch for already-mounted editors
        window.dispatchEvent(new CustomEvent("ocode:highlight", { detail }));
        // Bounded retry for mount race: FileEditor consumes the pending highlight on mount
        // via consumePendingHighlight. If peek still returns a value for this path,
        // the editor hasn't mounted yet, so re-dispatch. Caps at 10 attempts (~1.6s)
        // to avoid indefinite retries if the file fails to open.
        let attempts = 0;
        const retry = () => {
          attempts++;
          if (attempts > 10) return;
          const stillPending = !!peekPendingHighlight(path, projectRoot);
          if (stillPending) {
            window.dispatchEvent(new CustomEvent("ocode:highlight", { detail }));
            setTimeout(retry, 150);
          }
        };
        setTimeout(retry, 100);
      }
    },
    [handleOpenFile, projectState.projects, projectState.activeProject],
  );

  // File links in chat (markdown + plain text) dispatch this event.
  // Also handles editor link clicks (which carry projectRoot).
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail as { path: string; line?: number; projectRoot?: string };
      if (!detail?.path) return;
      const projectRoot = detail.projectRoot ?? projectState.activeProject?.path;
      void openFileAndShow(detail.path, projectRoot, detail.line);
    };
    window.addEventListener(OPEN_FILE_EVENT, handler as EventListener);
    return () => window.removeEventListener(OPEN_FILE_EVENT, handler as EventListener);
  }, [openFileAndShow, projectState.activeProject?.path]);

  useEffect(() => {
    setSelectedAgentRunId(null);
  }, [activeTabId]);

  const openAgentDetail = (sessionId: string, runId: string) => {
    setSelectedAgentRunId(runId);
    projectDispatch({ type: "SET_TAB_SUB_TAB", id: sessionId, subTab: "agents" });
  };

  // Mobile responsive: auto-dismiss the project drawer when the viewport
  // crosses into mobile. (`isMobile` itself is derived once, near the preview
  // activation block, so both consumers share the same value.)
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

  // Open a new chat tab AND reveal it — the keyboard/UI entry point shared by
  // Ctrl/Cmd+N and Ctrl/Cmd+T's non-Sessions fallback. Mirrors UnifiedTabBar's
  // "new chat" button (`handleNewChat`), which switches focus to the chat half
  // before opening; without the view switch a shortcut pressed from
  // Files/Settings would add the tab invisibly. `reuseIfEmpty` keeps the active
  // blank `new-*` tab instead of stacking duplicates.
  const openNewChat = useCallback(() => {
    setActiveView("sessions");
    setFocusedKind("chat");
    openNewSessionTab(isNewSessionTabEmpty(activeTabId));
  }, [activeTabId, openNewSessionTab]);

  useKeyboard({
    focusedKind,
    activeBrowserId,
    onCloseBrowserTab: (id) => {
      // Mirrors the browser pill's X: strip identity + page state + session.
      closeBrowserTab(id);
      browserActions.close(`tab:${id}`);
    },
    onNewSession: () => {
      // Ctrl/Cmd+N: the new-chat shortcut. Also reveals the chat half so it
      // works from any view (the standard "new" binding; Ctrl/Cmd+T is the
      // terminal counterpart).
      openNewChat();
    },
    onNewTerminal: () => {
      // Ctrl/Cmd+T: on the merged sessions tab, always opens a new terminal
      // for the active project (terminal pills are reachable there
      // regardless of whether a chat or terminal tab currently has focus);
      // elsewhere it creates a new chat session (same as Ctrl/Cmd+N).
      // Terminal is project-scoped so the handle is keyed by project path.
      if (activeView === "sessions") {
        setFocusedKind("terminal");
        const proj = projectState.activeProject?.path ?? "";
        terminalRefs.current.get(proj)?.openTerminal();
      } else {
        openNewChat();
      }
    },
    onCommandPalette: () => setCmdOpen(true),
    onFilePicker: () => setFilePickerOpen(true),
    onSave: () => {
      if (visibleActiveEditorTabId) {
        saveEditorTab(visibleActiveEditorTabId);
      }
    },
    onCloseSession: () => {
      // Cmd/Ctrl+W: close whatever is frontmost. On the Files view that is
      // the active editor tab; on the merged sessions tab it is the active
      // terminal instance or the active chat tab, depending on which
      // currently has focus. Mirrors each tab bar's X button.
      if (activeView === "sessions" && focusedKind === "terminal") {
        const proj = projectState.activeProject?.path ?? "";
        if (terminalRefs.current.get(proj)?.closeActiveTerminal()) return;
        return;
      }
      if (activeView === "files") {
        if (visibleActiveEditorTabId) {
          requestCloseTab(visibleActiveEditorTabId);
        }
        return;
      }
      if (activeView !== "sessions" || focusedKind !== "chat" || !activeTabId) return;
      closeSessionBackend(activeTabId);
      closeSessionTab(activeTabId);
      cancelLiveDeltas(activeTabId);
      clearQueue(activeTabId);
      clearInputHistory(activeTabId);
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

  // `placeholderTitle` is set only for a brand-new `new-*` tab; a /reset-id
  // rekey passes nothing so it keeps the tab's existing (preserved) title.
  const rekeySession = useCallback((tempTabId: string, sessionId: string, placeholderTitle?: string) => {
    dispatch({ type: "REKEY_SESSION", oldId: tempTabId, newId: sessionId });
    rekeyQueue(tempTabId, sessionId);
    rekeyDraft(tempTabId, sessionId);
    rekeyInputHistory(tempTabId, sessionId);
    projectDispatch({
      type: "UPDATE_TAB_ID",
      oldId: tempTabId,
      newId: sessionId,
      newTitle: placeholderTitle,
    });
    // The REKEY/UPDATE_TAB_ID dispatches above are batched, so this render's
    // projectState still owns the tab under its OLD id. Resolve the host from
    // the temp tab (the rekeyed session inherits its project) so a remote
    // session's authoritative title is fetched from its own server, not the
    // local one (which would 404 and leave the tab titled "New session").
    const host = resolveSessionHost(projectState, tempTabId, { fallbackToActive: true });
    // Replace the placeholder with the authoritative session title once the
    // server has persisted it (auto title from first message). This covers
    // the race where the api.chat() 202 response wins before the
    // session_started SSE event's own fetch. Wrap in Promise.resolve for
    // test mocks that may return synchronously.
    void Promise.resolve(api.getSession(sessionId, undefined, host)).then((detail: any) => {
      const t = detail?.title?.trim() || "";
      if (t && t !== "New session") {
        projectDispatch({ type: "UPDATE_TAB_TITLE", id: sessionId, title: t });
      }
    }).catch(() => {});
  }, [dispatch, projectDispatch, projectState]);

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
        const host = resolveSessionHost(projectState, sessionId, { fallbackToActive: true });
        const result = await api.chat(content, undefined, undefined, sessionId, projectPath, host);
        rekeySession(sessionId, result.sessionId, "New session");
      } else {
        const host = resolveSessionHost(projectState, sessionId);
        await api.sendMessage(sessionId, content, host);
      }
      return true;
    } catch (err) {
      dispatch({ type: "SET_ERROR", sessionId, error: err instanceof Error ? err.message : "send failed" });
      dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
      return false;
    }
  }, [dispatch, projectState, rekeySession]);

  // Continue an interrupted turn (ChatPanel's notice): the same path as a typed
  // message, with the literal text "continue". ChatPanel owns the optimistic
  // hide; this owns the transport (busy-queueing, remote host, persistence).
  const handleContinueInterrupted = useCallback(
    (sessionId: string) => {
      void sendCommandToSession(sessionId, "continue");
    },
    [sendCommandToSession],
  );

  const handleCommand = async (cmd: string, targetSessionId: string | null = activeTabId): Promise<SlashCommandResult> => {
    const baseCmd = cmd.split(" ")[0];
    const targetProjectPath = targetSessionId
      ? findProjectPathForTab(projectState, targetSessionId) ?? projectState.activeProject?.path
      : projectState.activeProject?.path;
    const targetHost = resolveSessionHost(projectState, targetSessionId ?? undefined, { fallbackToActive: true });
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
        listSessions: (host) => api.listSessions(undefined, host).then((r) => r.sessions),
        getSession: (id, opts?, host?) => api.getSession(id, opts, host),
        getOcrConfig: () => api.getOcrConfig(),
        setOcrConfig: (cfg) => api.setOcrConfig(cfg),
        getComputerUseConfig: () => api.getComputerUseConfig(),
        setComputerUseConfig: (enabled) => api.setComputerUseConfig(enabled),
        getOcrModels: () => api.getOcrModels(),
        getOcrEnabled: () => api.getOcrEnabled(),
        setOcrEnabled: (enabled) => api.setOcrEnabled(enabled),
        setOcrModel: (model) => api.setOcrModel(model),
        compactSession: (id, host?, focus?) => api.compactSession(id, host, focus),
        recapSession: (id, host?) => api.recapSession(id, host),
        shareSession: (id, host?) => api.shareSession(id, host),
        btwSession: (id, content, host?) => api.btwSession(id, content, host),
        getMaskConfig: () => api.getMaskConfig(),
        setMaskEnabled: (enabled) => api.setMaskEnabled(enabled),
        setMaskMode: (mode) => api.setMaskMode(mode),
        setMaskModel: (model) => api.setMaskModel(model),
        getCommandContext: (name, args, project, host) => api.getCommandContext(name, args, project, host),
        getSessionContext: (id, host) => api.getSessionContext(id, host),
        getLSPStatuses: (host) => api.getLSPStatuses(host),
        listSkills: () => api.listSkills(),
        getMCP: (host) => api.getMCP(host),
        startMCPAuth: (name, host) => api.startMCPAuth(name, host),
        getMCPAuthStatus: (jobId, host) => api.getMCPAuthStatus(jobId, host),
        resetSessionId: (sessionId, host) => api.resetSessionId(sessionId, host),
        getGithubPR: (owner, repo, number) => api.getGithubPR(owner, repo, number),
        getGithubIssues: (owner, repo, state) => api.getGithubIssues(owner, repo, state),
        getAgentRuns: (host) => api.listAgentRuns(undefined, host),
        getCronJobs: () => api.listCronJobs().then((r) => r.jobs),
        getCronJob: (id) => api.getCronJob(id),
        deleteCronJob: (id) => api.deleteCronJob(id),
        getSmallModelWithEnabled: (host?) => api.getSmallModelWithEnabled(host),
        getAdvisor: (host?) => api.getAdvisor(host),
        getLimitsConfig: () => api.getLimitsConfig(),
        setLimitsConfig: (fields) => api.setLimitsConfig(fields),
        getThinkingBudget: (host?) => api.getThinkingBudget(host),
        setThinkingBudget: (budget, host?) => api.setThinkingBudget(budget, host),
        listModels: (host) => api.listModels(undefined, host),
        getConfigModel: (host?) => api.getConfigModel(host),
        setConfigModel: (model, host?) => api.setConfigModel(model, host),
        getFeaturesConfig: () => api.getFeaturesConfig(),
        setFeaturesConfig: (memoryEnabled, docPromptEnabled) => api.setFeaturesConfig(memoryEnabled, docPromptEnabled),
        getPathsInfo: () => api.getPathsInfo(),
        getPathsConfig: () => api.getPathsConfig(),
        setPathsConfig: (paths, uploadDir) => api.setPathsConfig(paths, uploadDir),
        getMemoryStatus: () => api.getMemoryStatus(),
        setBashRule: (prefix, level) => api.setBashRule(prefix, level),
        getPermissions: (sessionId, host) => api.getPermissions(sessionId, host),
        getAutoContinue: (host?) => api.getAutoContinue(host),
        setAutoContinue: (fields, host?) => api.setAutoContinue(fields, host),
        connectProvider: (provider, apiKey) => api.connectProvider(provider, apiKey),
        addProject: (path) => api.addProject(path),
        getDocsStatus: (project, host) => api.getDocsStatus(project, host),
        docsInit: (project, host) => api.docsInit(project, host),
        docsUpdate: (sessionId, focus, project, host) => api.docsUpdate(sessionId, focus, project, host),
        docsCleanup: (confirm, project, host) => api.docsCleanup(confirm, project, host),
        getImageGenConfig: () => api.getImageGenConfig(),
        setImageGenConfig: (cfg) => api.setImageGenConfig(cfg),
        getDiscoveryConfig: (host?) => api.getDiscoveryConfig(host),
        setDiscoveryConfig: (cfg, host?) => api.setDiscoveryConfig(cfg, host),
        getDiscoveryStatus: (id, host) => api.getDiscoveryStatus(id, host),
        getLocalModelsConfig: (host?) => api.getLocalModelsConfig(host),
        setLocalModelsConfig: (models, host?) => api.setLocalModelsConfig(models, host),
        syncLoginStart: () => api.syncLoginStart(),
        syncLogout: () => api.syncLogout(),
        getFakeAgent: () => api.getFakeAgentConfig(),
        setFakeAgent: (name) => api.setFakeAgentConfig(name),
        getEditorConfig: () => api.getEditorConfig(),
        setEditorConfig: (editor, editorMode, ideMode) => api.setEditorConfig(editor, editorMode, ideMode),
        getThemes: () => api.getThemes(),
        getTheme: (name) => api.getTheme(name),
        getTUISettings: () => api.getTUISettings(),
        setTUISettings: (cfg) => api.setTUISettings(cfg),
        getExplorerModel: (host?) => api.getExplorerModel(host),
        setExplorerModel: (model, host?) => api.setExplorerModel(model, host),
        setExplorerModelEnabled: (enabled, host?) => api.setExplorerModelEnabled(enabled, host),
        getContextModel: (host?) => api.getContextModel(host),
        setContextModel: (model, host?) => api.setContextModel(model, host),
        setContextModelEnabled: (enabled, host?) => api.setContextModelEnabled(enabled, host),
        getCliTools: (host) => api.getCliTools(host),
        startCliToolsInstall: (tool, host) => api.startCliToolsInstall(tool, host),
        getCliToolsInstallStatus: (jobId, host) => api.getCliToolsInstallStatus(jobId, host),
        getRecapConfig: (host) => api.getRecapConfig(host),
        setRecapConfig: (model, enabled, timeout, host) => api.setRecapConfig(model, enabled, timeout, host),
      },
      getMessages: () => getSessionSlice(chatStateRef.current, targetSessionId).messages,
      getSessionId: () => targetSessionId,
      // Delivers a command result that arrives AFTER the handler returned
      // (/tools install reports its outcome here once the background job
      // finishes). No-op without a target tab — a draft has no transcript yet.
      notify: (content) => {
        if (!targetSessionId) return;
        dispatch({
          type: "ADD_MESSAGE",
          sessionId: targetSessionId,
          message: { role: "assistant", content },
        });
      },
      setDraftPermissionMode: (mode) => {
        if (targetSessionId) {
          dispatch({ type: "SET_SESSION_PERMISSION_MODE", sessionId: targetSessionId, mode });
        }
      },
      host: targetHost,
      // Repo/docs-scoped commands (/lsp, /mcp, /changes, /review, /docs) must
      // read the tab's own project, not the server's default workdir.
      projectPath: targetProjectPath,
    });

    if (!result.handled) return { handled: false, accepted: true };

    if (result.openModelPicker) {
      openModelDialog(result.modelPickerPurpose ?? "main");
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
    if (result.rekeyTo) {
      // /reset-id: the server re-keyed the session under a new id and returned
      // the old/new pair. Reuse the same tab-rekey machinery a `new-*` tab uses
      // on first send, so the chat slice, draft, queue and project tab all
      // follow the new id. Re-key BEFORE returning so the tab the user is
      // looking at already points at the new session when the next render runs.
      rekeySession(result.rekeyTo.oldId, result.rekeyTo.newId);
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
    rekeySession(tempTabId, sessionId, "New session");
  };

  // --- Stable callbacks for the memoized per-tab children -------------------
  // A trampoline keeps the prop identity constant across renders while always
  // invoking the latest closure. Without it, every tab switch would hand
  // ChatInput a freshly-created callback and defeat its `memo`, re-rendering
  // every mounted (hidden) composer just because a sibling tab became active.
  const handleCommandRef = useRef(handleCommand);
  handleCommandRef.current = handleCommand;
  const stableHandleCommand = useCallback(
    (cmd: string, targetSessionId?: string | null) =>
      handleCommandRef.current(cmd, targetSessionId),
    [],
  );
  const handleSessionCreatedRef = useRef(handleSessionCreated);
  handleSessionCreatedRef.current = handleSessionCreated;
  const stableHandleSessionCreated = useCallback(
    (tempTabId: string, sessionId: string) =>
      handleSessionCreatedRef.current(tempTabId, sessionId),
    [],
  );

  // Continue an interrupted turn is handed to every mounted ChatPanel, so its
  // identity must be stable — an inline arrow would re-render all of them on
  // any App render (see the memo on ChatPanel).
  const handleContinueInterruptedRef = useRef(handleContinueInterrupted);
  handleContinueInterruptedRef.current = handleContinueInterrupted;
  const stableHandleContinueInterrupted = useCallback(
    (sessionId: string) => handleContinueInterruptedRef.current(sessionId),
    [],
  );
  const handleClearPreviewContext = useCallback(() => setPreviewContext(null), []);

  // Editor files opted into the loop, bucketed by project root so each tab's
  // ChatInput receives a stable array reference across renders. Filtering
  // inline in the tab map produced a fresh array per render per tab, which
  // would defeat the memo on every one of them.
  const contextFilePathsByProject = useMemo(() => {
    const byProject: Record<string, string[]> = {};
    for (const e of contextFileEntries) {
      const key = e.projectRoot ?? "";
      if (byProject[key]) byProject[key].push(e.path);
      else byProject[key] = [e.path];
    }
    return byProject;
  }, [contextFileEntries]);

  // Every project that owns a terminal, so the project-scoped TerminalTabs
  // instances stay mounted across both session-tab and project switches (their
  // ptys/WebSockets must never be torn down by a switch). Hoisted out of the
  // render-time IIFE below so this set is rebuilt only when the project/tab set
  // actually changes, not on every render.
  const terminalProjectPaths = useMemo(() => {
    const paths = [
      ...((projectState.projects ?? []) as { path: string }[]).map((p) => p.path),
      ...Object.keys(projectState.tabsByProject ?? {}),
      ...(projectState.activeProject ? [projectState.activeProject.path] : []),
    ].filter(Boolean) as string[];
    return Array.from(new Set(paths));
  }, [projectState.projects, projectState.tabsByProject, projectState.activeProject]);

  // Stable prop array for EditorTabBar (which is a plain child of App, so a
  // fresh array each render is a needless prop change). Scoped to the active
  // project so another project's tab never appears in the Files tab.
  const editorTabBarItems = useMemo(
    () =>
      visibleEditorTabs.map((t) => ({
        id: t.id,
        path: t.path,
        isDirty: t.isDirty,
        includeInContext: t.includeInContext,
      })),
    [visibleEditorTabs],
  );

  // Computed once per tab-set change instead of six times per render (one per
  // sub-tab block below) — each `Object.values(...).flat()` over every open tab
  // in every project was a real per-render cost on large tab counts.
  const allChatTabs = useMemo(
    () => Object.values(projectState.tabsByProject).flat(),
    [projectState.tabsByProject],
  );
  const activeSessionTab = tabs.find((t) => t.id === activeTabId);
  // The side pane accompanies a chat session (or the terminal); it must not
  // show over the session tab's non-chat sub-tabs (Agents/Changes/Logs/
  // Status/Preview). Pure gate, same pattern as shouldRenderCoworkSidebar.
  const sidePaneVisible = shouldRenderSidePane({
    activeView,
    activeSubTab: activeSessionTab?.activeSubTab,
    focusedKind,
    isMobile,
  });
  // Chat-session-bound dialogs (the permission/question asks, and any future
  // session-scoped prompt) may only mount while this session's Chat sub-tab is
  // actually on screen. Otherwise they render a full-screen Radix modal over a
  // view the user is not working in — blocking the whole app for a session they
  // cannot see. The pending ask stays in its per-session store slice, so it
  // re-opens on return; the sidebar Bell badge and attention chime cover the
  // out-of-sight case. See lib/dialogScope.
  const sessionAskVisible = sessionAskSurfaceVisible({
    activeView,
    focusedKind,
    activeSubTab: activeSessionTab?.activeSubTab,
  });
  // Lazy display:none: keep visited tabs mounted (hidden) so scroll/virtualizer
  // state survives switches (instant CSS toggle), but avoid mounting all 40
  // panels eagerly on first load. Only tabs that have been visited once are
  // kept in the DOM — the rest return null until first activated.
  const visitedTabsRef = useRef<Set<string>>(new Set());
  if (activeSessionTab) {
    visitedTabsRef.current.add(`${activeSessionTab.id}:${activeSessionTab.activeSubTab}`);
  }
  // Same lazy display:none policy for editor panes: a tab mounts once it has
  // been the visible active tab for its project, then stays mounted (hidden)
  // so its viewer/Monaco state survives tab AND project switches. The gate
  // matters because pane rendering iterates ALL projects' tabs now — without
  // it an app reload would eagerly spin up Monaco/pdf.js for every restored
  // tab in every project. A never-visited tab has no in-memory state to lose;
  // it restores its page/scroll from `previewViewState` when first shown.
  const visitedEditorTabsRef = useRef<Set<string>>(new Set());
  if (visibleActiveEditorTabId) visitedEditorTabsRef.current.add(visibleActiveEditorTabId);


  // Refs to TerminalTabs instances so Ctrl/Cmd+T can open a new terminal.
  // Keyed by project path (terminal is project-scoped, not session-scoped,
  // so switching chat sessions never kills the pty).
  const terminalRefs = useRef<Map<string, TerminalTabsHandle>>(new Map());
  const chatInputRefs = useRef<Map<string, import("./components/Chat/ChatInput").ChatInputHandle>>(new Map());

  return (
    <div className="flex flex-col h-screen bg-background">
      <SessionTabSync onNewTab={openBackgroundBrowserTab} />
      <AttentionSoundBridge
        tabs={attentionTabs}
        activeTabId={activeTabId}
        activeProjectPath={activeProjectPath}
        chatVisible={chatVisible}
      />

      {/* Main content area */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left sidebar - project roots */}
        <ProjectSidebar
          isOpen={sidebarOpen}
          onToggle={() => setSidebarOpen(!sidebarOpen)}
          width={sidebarOpen && !isMobile ? sidebar.width : undefined}
          isMobile={isMobile}
        />

        {/* Sidebar resize handle — desktop only: the mobile sidebar is a fixed
            overlay, so an inline handle would be a dead 1px divider at x=0. */}
        {sidebarOpen && !isMobile && (
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
        <main
          className="flex flex-1 flex-col overflow-hidden"
          data-active-view={activeView}
          data-focused-kind={focusedKind}
        >
          <Tabs value={activeView} onValueChange={(v) => setActiveView(v as typeof activeView)} className="flex flex-col flex-1 overflow-hidden">
            <div className="flex items-center justify-between gap-2 border-b pr-2">
              <div className="flex-1 min-w-0">
                <TopTabs
                  activeTab={activeView}
                  onTabSelect={(v) => setActiveView(v as typeof activeView)}
                  onMenuToggle={isMobile ? () => setSidebarOpen((open) => !open) : undefined}
                />
              </div>
              <ProfileSwitcher />
            </div>

              {/* The unified tab bar must live outside both the terminal container and the
                  chat content wrapper: each of those is hidden when the other kind is focused,
                  so a bar nested in either would vanish with it. It also sits above the
                  content+side-panel row so its width never depends on the per-tab side
                  browser panel: with the bar inside the center column, switching to a tab
                  whose side panel is open/closed/wider re-wrapped every pill. */}
              {activeView === "sessions" && (
                <div className="flex items-center p-1">
                  <div className="flex-1 min-w-0">
                    <UnifiedTabBar focusedKind={focusedKind} onFocusKindChange={setFocusedKind} />
                  </div>
                  {/* The side pane is desktop-only (shouldRenderSidePane gates
                      on isMobile), so its toggle is hidden on phones — the
                      browser lives in the tab strip's browser pills / "New
                      browser tab" button there. */}
                  {!isMobile && (
                    <button
                      type="button"
                      aria-label="Toggle browser panel"
                      disabled={focusedKind === "browser" || !sideStateKey || !sidePaneVisible}
                      title={sidePaneVisible ? "Toggle browser panel" : "Browser panel is available on the chat sub-tab"}
                      onClick={() => {
                        if (!sideStateKey) return;
                        if (browserOpen) {
                          panelClosedByUser.current.add(sideStateKey);
                          browserActions.close(sideStateKey);
                        } else {
                          panelClosedByUser.current.delete(sideStateKey);
                          browserActions.open(sideStateKey);
                        }
                      }}
                      className="mx-1 flex shrink-0 items-center rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground transition-colors border border-border disabled:opacity-40"
                    >
                      🌐
                    </button>
                  )}
                </div>
              )}
            <div className="flex flex-1 min-h-0">
            <div className="flex-1 min-w-0 overflow-hidden flex flex-col pb-2">
              {/* Terminal is project-scoped and must stay mounted even when not visible. It lives inside the Tabs root
                  so TopTabs (which uses TabsList/TabsTrigger) keeps its Radix context, but outside the non-terminal
                  content region so switching away never unmounts the WebSocket/pty. Visibility is toggled via CSS only. */}
              {(() => {
                const terminalFocused = activeView === "sessions" && focusedKind === "terminal";
                return (
                  <div className={terminalFocused ? "flex flex-1 overflow-hidden m-0 flex-col" : "hidden"}>
                    <div className="relative flex-1 min-h-0 overflow-hidden">
                      {projectState.projectsStatus !== "ready" ? (
                        terminalFocused && (
                          <div data-testid="terminal-project-metadata-status" className="p-4 text-sm text-muted-foreground">
                            Project metadata is unavailable; terminals are paused until it loads successfully.
                          </div>
                        )
                      ) : (
                        terminalProjectPaths.map((pp) => {
                          const metadata = getTrustedTerminalProject(projectState.projects, pp);
                          return (
                            <div
                              key={`${pp}:terminal`}
                              className={
                                pp === activeProjectPath ? "absolute inset-0" : "absolute inset-0 hidden"
                              }
                            >
                              {metadata.known ? (
                                <TerminalTabs
                                  ref={(handle) => {
                                    if (handle) terminalRefs.current.set(pp, handle);
                                    else terminalRefs.current.delete(pp);
                                  }}
                                  active={pp === activeProjectPath && terminalFocused}
                                  projectPath={pp}
                                  host={metadata.host}
                                />
                              ) : (
                                <div
                                  data-testid="terminal-project-unavailable"
                                  data-project-path={pp}
                                  className="p-4 text-sm text-muted-foreground"
                                >
                                  Project metadata is no longer available; this terminal was not opened locally.
                                </div>
                              )}
                            </div>
                          );
                        })
                      )}
                    </div>
                  </div>
                );
              })()}

              <div className={activeView === "sessions" && focusedKind === "terminal" ? "hidden" : "flex flex-1 overflow-hidden flex-col"}>
              <TabsContent value="files" forceMount className="flex-1 overflow-hidden m-0 flex">
                <div
                  className="relative shrink-0 h-full overflow-hidden border-r border-border transition-[width] duration-100"
                  style={{ width: fileTreePane.collapsed ? 0 : fileTreePane.width }}
                >
                  <div className="absolute inset-0" style={{ width: fileTreePane.width }}>
                    <FileTree onOpenFile={openFileAndShow} projectPath={projectState.activeProject?.path} projectHost={projectState.activeProject?.host} includedPaths={contextFileEntries.filter((e) => (e.projectRoot ?? "") === (projectState.activeProject?.path ?? "")).map((e) => e.path)} />
                  </div>
                </div>
                {!fileTreePane.collapsed && (
                  <div
                    ref={fileTreePane.handleRef}
                    onPointerDown={fileTreePane.onPointerDown}
                    onDoubleClick={fileTreePane.resetToDefault}
                    className="w-1 shrink-0 cursor-col-resize hover:bg-accent active:bg-accent"
                  />
                )}
                <button
                  onClick={fileTreePane.toggleCollapsed}
                  title={fileTreePane.collapsed ? "Show file tree" : "Hide file tree"}
                  className="shrink-0 self-start mt-1 p-0.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground"
                >
                  {fileTreePane.collapsed ? <PanelLeft className="w-3.5 h-3.5" /> : <PanelLeftClose className="w-3.5 h-3.5" />}
                </button>
                <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
                  <EditorTabBar
                    editorTabs={editorTabBarItems}
                    activeEditorTabId={visibleActiveEditorTabId}
                    onSelectTab={setActiveEditorTabId}
                    onCloseTab={requestCloseTab}
                    onToggleInclude={toggleIncludeInContext}
                  />
                  <div className="relative flex-1 overflow-hidden">
                    {visibleEditorTabs.length === 0 && (
                      <div className="absolute inset-0 flex items-center justify-center text-sm text-muted-foreground">
                        No file open
                      </div>
                    )}
                    {/* Each open tab mounts once it has been viewed, then stays
                        mounted (hidden) — so a viewer keeps its page/zoom/scroll
                        and Monaco its cursor/undo across tab AND project
                        switches. Tabs from other projects are kept too (hidden),
                        which is what makes a switch back instant and
                        state-preserving; the `visitedEditorTabsRef` gate avoids
                        eagerly mounting every restored tab on first load.

                        Memory tradeoff (accepted): because nothing unmounts, a
                        visited tab's parsed document / Monaco model stays
                        resident for the app's lifetime. The big per-page canvas
                        cost is NOT retained — `active` tells viewers when their
                        pane is hidden and PdfViewer releases its rasterized
                        window (rebuilt on show) while MediaViewer pauses
                        playback. The parsed pdf.js document and the blob copy
                        are deliberately kept (re-parsing on every switch-back
                        would be slow); that residual is bounded by the number
                        of PDF tabs actually opened. */}
                    {editorTabs.map((et) => {
                      if (!visitedEditorTabsRef.current.has(et.id) && et.id !== visibleActiveEditorTabId) return null;
                      return (
                        <div
                          key={et.id}
                          data-editor-pane={et.id}
                          className={et.id === visibleActiveEditorTabId ? "absolute inset-0" : "absolute inset-0 hidden"}
                        >
                          <FileTabContent
                            path={et.path}
                            projectRoot={et.projectRoot}
                            projectHost={et.projectHost}
                            persistKey={et.id}
                            content={et.content}
                            isBinary={et.isBinary}
                            active={et.id === visibleActiveEditorTabId}
                            onChange={(value) => handleEditorChange(et.id, value)}
                            onOpenFile={openFileAndShow}
                            readOnly={false}
                            session={visibleEditorTabIds.has(et.id) ? activeTabId ?? undefined : undefined}
                            diffVersion={et.diffVersion}
                            onSelectionChange={handleSelectionChange}
                            externalChange={et.externalChange}
                            onReloadFromDisk={() => reloadTabFromDisk(et.id)}
                            onDismissExternalChange={() => dismissExternalChange(et.id)}
                            onForceSave={() => forceSaveEditorTab(et.id)}
                            // Header Save button (touch devices have no Cmd/Ctrl+S).
                            // saveEditorTab sets its own conflict banner / dialog
                            // error and rethrows; surface the rest as a toast so a
                            // failed tap is never silent.
                            dirty={et.isDirty}
                            onSave={() => {
                              void saveEditorTab(et.id).catch((err) => reportActionError(err, "Save file"));
                            }}
                          />
                        </div>
                      );
                    })}
                  </div>
                </div>
              </TabsContent>

              <TabsContent value="git" forceMount className="flex-1 overflow-hidden m-0">
                <GitPanel onOpenFile={openFileAndShow} projectPath={projectState.activeProject?.path} projectHost={projectState.activeProject?.host} active={activeView === "git"} />
              </TabsContent>
              <TabsContent value="cron" forceMount className="flex-1 overflow-hidden m-0">
                <CronPanel active={activeView === "cron"} />
              </TabsContent>
              <TabsContent value="assets" forceMount className="flex-1 overflow-hidden m-0">
                <AssetsPanel />
              </TabsContent>
              <TabsContent value="settings" forceMount className="relative flex-1 min-h-0 overflow-hidden m-0">
                <SettingsPanel />
              </TabsContent>

              <TabsContent value="sessions" forceMount className="flex-1 overflow-hidden m-0">
                <div className="relative flex flex-col h-full">
                  <div className={focusedKind === "chat" ? "flex flex-col flex-1 min-h-0" : "hidden"}>
                  <SessionSubTabs />
                  <div className="relative flex-1 min-h-0 overflow-hidden">
                    {tabs.length === 0 && (
                      <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 text-muted-foreground">
                        <p className="text-sm">No open sessions for this project</p>
                        <button
                          onClick={() => openNewSessionTab(false)}
                          className="inline-flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-sm text-foreground hover:bg-muted hover:text-foreground transition-colors"
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
                          {/* Remote (SSH/WSL) server version mismatch for the
                              active chat tab's project. One instance only. */}
                          {isActive && <RemoteVersionBanner host={projectState.activeProject?.host} />}
                          <div className="relative flex-1 min-h-0 overflow-hidden">
                            <ChatPanel
                              sessionId={tab.id}
                              host={resolveSessionHost(projectState, tab.id)}
                              onContinueInterrupted={stableHandleContinueInterrupted}
                            />
                          </div>
                          <AgentPreview onOpenDetail={(runId) => openAgentDetail(tab.id, runId)} />
                          <ChatInput
                            ref={(handle) => {
                              if (handle) chatInputRefs.current.set(tab.id, handle);
                              else chatInputRefs.current.delete(tab.id);
                            }}
                            projectPath={tab.projectPath}
                            onSlashCommand={stableHandleCommand}
                            activeEditorContext={
                              effectiveActiveEditorContext && (effectiveActiveEditorContext.projectRoot ?? "") === (tab.projectPath ?? "") ? effectiveActiveEditorContext : null
                            }
                            contextFilePaths={contextFilePathsByProject[tab.projectPath ?? ""] ?? EMPTY_STRING_ARRAY}
                            previewContext={
                              previewContext && (previewContext.projectRoot ?? "") === (tab.projectPath ?? "") ? previewContext : null
                            }
                            onClearPreviewContext={handleClearPreviewContext}
                            sessionTabId={tab.id}
                            onSessionCreated={stableHandleSessionCreated}
                          />
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
                          <ChangesPanel session={tab.id} host={resolveSessionHost(projectState, tab.id)} active={isActive} />
                        </div>
                      );
                    })}
                    {allChatTabs.map((tab) => {
                      const isActive = tab.projectPath === projectState.activeProject?.path && tab.id === activeTabId && tab.activeSubTab === "logs";
                      const key = `${tab.id}:logs`;
                      if (!visitedTabsRef.current.has(key) && !isActive) return null;
                      return (
                        <div key={key} className={isActive ? "absolute inset-0" : "absolute inset-0 hidden"}>
                          <LogPanel active={isActive} sessionId={tab.id} host={resolveSessionHost(projectState, tab.id)} />
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
                    {allChatTabs.map((tab) => {
                      const isActive = tab.projectPath === projectState.activeProject?.path && tab.id === activeTabId && tab.activeSubTab === "preview";
                      const key = `${tab.id}:preview`;
                      if (!visitedTabsRef.current.has(key) && !isActive) return null;
                      // On mobile there is no side pane, so the preview
                      // activation (AI `preview_open` tool / "Preview in
                      // sidebar") is consumed by this full-width sub-tab
                      // instead. Desktop keeps it in the side pane.
                      const takesActivation = isMobile && tab.id === activeTabId;
                      return (
                        <div key={key} className={isActive ? "absolute inset-0" : "absolute inset-0 hidden"}>
                          <PreviewTabPage
                            projectRoot={tab.projectPath}
                            projectHost={projectState.projects.find((p) => p.path === tab.projectPath)?.host}
                            request={takesActivation ? previewRequest : null}
                            nonce={takesActivation ? previewNonce : 0}
                            onConsumeActivation={takesActivation ? consumePreviewActivation : undefined}
                          />
                        </div>
                      );
                    })}
                  </div>
                  </div>
                  <div
                    className={
                      activeView === "sessions" && focusedKind === "browser"
                        ? "absolute inset-0"
                        : "absolute inset-0 invisible pointer-events-none"
                    }
                  >
                    {allBrowserTabs.map(({ projectPath, tab }) => {
                      const stateKey = `tab:${tab.id}` as StateKey;
                      if (!activatedBrowserKeys.has(stateKey)) return null;
                      const isActive = activeView === "sessions" && focusedKind === "browser" &&
                        projectPath === activeProjectPath && tab.id === activeBrowserId;
                      return (
                        <div
                          key={stateKey}
                          data-testid={`browser-surface-${tab.id}`}
                          className={isActive ? "absolute inset-0" : "absolute inset-0 invisible pointer-events-none"}
                        >
                          <BrowserPanel stateKey={stateKey} mode="full" active={isActive} />
                        </div>
                      );
                    })}
                  </div>
                </div>
              </TabsContent>
              </div>
            </div>

          {/* Side browser panel — resizable/collapsible, accompanies the focused
              chat/terminal session (never the full-width browser tab). Its live
              page state lives in browserStore under the side: stateKey.
              Collapse keeps the surface mounted (BrowserPanel suppresses the
              iframe while `collapsed`, so the browse grant/session survives);
              close deletes the state and revokes the server session. */}
          {sideStateKey && browserOpen && sidePaneVisible && (
            <>
              {sideTabState?.collapsed ? (
                <button
                  type="button"
                  onClick={() => browserActions.setCollapsed(sideStateKey, false)}
                  title="Show browser panel"
                  aria-label="Show browser panel"
                  className="flex shrink-0 items-center justify-center self-stretch w-7 border-l border-border text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
                >
                  <PanelRight className="w-4 h-4" />
                </button>
              ) : (
                <>
                  <div
                    ref={browserPane.handleRef}
                    role="separator"
                    aria-orientation="vertical"
                    aria-valuemin={browserPane.minWidth}
                    aria-valuemax={browserPane.maxWidth}
                    aria-valuenow={browserPane.width}
                    aria-label="Resize browser panel"
                    tabIndex={0}
                    className="w-1 flex-shrink-0 cursor-col-resize bg-transparent hover:bg-primary/40 active:bg-primary/60 transition-colors"
                    onPointerDown={browserPane.onPointerDown}
                    onDoubleClick={browserPane.resetToDefault}
                    onKeyDown={(e) => {
                      const step = e.shiftKey ? 50 : 10;
                      if (e.key === "ArrowLeft") {
                        e.preventDefault();
                        browserPane.setWidth(browserPane.width - step);
                      } else if (e.key === "ArrowRight") {
                        e.preventDefault();
                        browserPane.setWidth(browserPane.width + step);
                      } else if (e.key === "Home") {
                        e.preventDefault();
                        browserPane.resetToDefault();
                      }
                    }}
                  />
                  <div
                    style={{ width: browserPane.width }}
                    className="flex-shrink-0 h-full min-h-0 flex flex-col border-l border-border"
                  >
                    <div className="flex shrink-0 items-center justify-between h-8 pl-3 pr-1.5 border-b border-border">
                      <span className="min-w-0 truncate text-xs font-medium text-muted-foreground">Browser / Preview</span>
                      <div className="flex shrink-0 items-center gap-0.5">
                        <button
                          type="button"
                          onClick={() => browserActions.setCollapsed(sideStateKey, true)}
                          title="Collapse browser panel"
                          aria-label="Collapse browser panel"
                          className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                        >
                          <PanelRightClose className="w-3.5 h-3.5" />
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            panelClosedByUser.current.add(sideStateKey);
                            browserActions.close(sideStateKey);
                          }}
                          title="Close browser panel"
                          aria-label="Close browser panel"
                          className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                        >
                          <X className="w-3.5 h-3.5" />
                        </button>
                      </div>
                    </div>
                    <div className="flex-1 min-h-0">
                      <PreviewHost
                        key={sideStateKey}
                        stateKey={sideStateKey}
                        projectRoot={projectState.activeProject?.path}
                        projectHost={projectState.activeProject?.host}
                        request={previewRequest}
                        nonce={previewNonce}
                        onConsumeActivation={consumePreviewActivation}
                        sessionId={sidePanelKind === "chat" ? activeTabId : null}
                      />
                    </div>
                  </div>
                </>
              )}
            </>
          )}
            </div>
          </Tabs>

          {/* Status bar — only on chat sub-tab; hidden when terminal is focused */}
          {activeView === "sessions" && activeSessionTab?.activeSubTab === "chat" && focusedKind === "chat" && (
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

        {/* Right sidebar - cowork panel. Only mounted on the chat sub-tab
            (shouldRenderCoworkSidebar) and only while open, so terminal and every
            other view get the full width. */}
        {shouldRenderCoworkSidebar({
          activeView,
          activeSubTab: activeSessionTab?.activeSubTab,
          focusedKind,
        }) && coworkOpen && (
          // On mobile CoworkSidebar renders itself as a `fixed` right overlay,
          // so the wrapper must NOT reserve its desktop width — a `w-72`
          // flex-shrink-0 box would still eat 288px of the flex row and squeeze
          // the session tab list to zero width.
          <div className={isMobile ? "" : "w-72 flex-shrink-0 flex flex-col min-h-0 self-stretch overflow-hidden"}>
            <CoworkSidebar
              isOpen={coworkOpen}
              onClose={() => setCoworkOpen(false)}
              activeAgent="build"
              onModelClick={openModelDialog}
              isMobile={isMobile}
            />
          </div>
        )}
      </div>

      {/* Dialogs */}
      <SessionDialog />
      <ShareDialog />
      <CommandPalette
        open={cmdOpen}
        onClose={() => setCmdOpen(false)}
        onExecute={handleCommand}
      />
      <ModelDialog
        open={modelDialogOpen}
        onClose={() => setModelDialogOpen(false)}
        purpose={modelDialogTab}
        sessionId={activeTabId ?? undefined}
        host={activeSessionHost}
      />

      {/* Permission Dialog — only while this session's Chat sub-tab is on
          screen, so an ask never blocks a view the user is not working in. */}
      {pendingPermission && sessionAskVisible && (
        <PermissionDialog
          open={true}
          tool={pendingPermission.tool}
          command={pendingPermission.command}
          args={pendingPermission.args}
          rule={pendingPermission.rule}
          summary={pendingPermission.summary}
          denyReason={pendingPermission.deny_reason}
          modelUnavailable={pendingPermission.model_unavailable}
          scope={pendingPermission.scope}
          prefix={pendingPermission.prefix}
          outOfScopePath={pendingPermission.out_of_scope_path}
          context={askContext}
          requestId={pendingPermission.request_id}
          onDecide={resolvePermission}
        />
      )}

      {/* Question Dialog (agent `question` tool prompt) — same surface gate as
          the permission dialog above. */}
      {pendingQuestion && sessionAskVisible && (
        <QuestionDialog
          key={pendingQuestion.request_id}
          open={true}
          requestId={pendingQuestion.request_id}
          questions={pendingQuestion.questions}
          onSubmit={submitQuestionAnswers}
          onCancel={cancelQuestion}
          context={askContext}
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

  // `!authToken()` alone only catches "no token at all" (a tab that never
  // had one). The realistic failure is the remote server restarting: the
  // cached token goes stale, every API call starts 401ing, but authToken()
  // still returns that (now-invalid) cached value. reportAuthFailure (in
  // client.ts) detects that 401 and calls this handler so the app can
  // switch to RemoteReconnect immediately instead of the user staring at a
  // silently broken app.
  const [remoteAuthFailed, setRemoteAuthFailed] = useState(false);
  useEffect(() => {
    if (!isRemoteSession()) return;
    setAuthFailureHandler(() => {
      eventBus.stop(); // stop retrying the stream with the now-dead token
      setRemoteAuthFailed(true);
    });
    return () => setAuthFailureHandler(null);
  }, []);

  if (isRemoteSession() && (!authToken() || remoteAuthFailed)) {
    return <RemoteReconnect />;
  }
	return (
	    <ErrorBoundary>
	      <ChatProvider>
	        <ProjectProvider>
	          <TerminalProvider>
	            <BrowserTabsProvider>
	              <SpeechProvider>
	              <FrontendMemoryReporter />
              <StatusMetricsHydrator />
              <Routes>
                <Route path="/session/:id" element={<SessionPage />} />
	                <Route path="*" element={<HomeApp />} />
	              </Routes>
              <SpeechToolbar />
              {/* Settings-action failures (sidebar toggles, model picks) are
                  otherwise only console.error'd. Root-mounted so it survives
                  the ModelDialog closing on pick. */}
              <ActionErrorToast />
	              </SpeechProvider>
	            </BrowserTabsProvider>
          </TerminalProvider>
        </ProjectProvider>
      </ChatProvider>
    </ErrorBoundary>
  );
}
