import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { X, List, Plus, Loader2, Bell } from "lucide-react";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  horizontalListSortingStrategy,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useChatDispatch, useChatSelector, getSessionSlice, type ChatState, type SessionSlice } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { useTerminalConfig } from "@/hooks/useTerminalConfig";
import { useTerminalState, getProjectTerminals, PROCESSES_TAB_ID } from "../../stores/terminalStore";
import { useBrowserTabs } from "../../stores/browserTabsStore";
import { browserActions, useBrowserStore, type StateKey } from "../../lib/browserStore";
import type { FocusedKind } from "../../lib/viewPersistence";
import { isNewSessionTabEmpty } from "../../lib/tabDrafts";
import { clearQueue } from "../../lib/tabQueue";
import { cancelLiveDeltas, closeSessionBackend } from "../../lib/sessionEvents";
import { api } from "../../api/client";
import { loadTabOrder, saveTabOrder, reconcileTabOrder, type UnifiedTabKey } from "./tabOrderPersistence";
import { focusTerminalById } from "../Terminal/terminalFocus";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Button } from "../ui/button";

// While a tab's session has an in-flight turn, show what it's doing as a
// badge alongside its title (never replacing the title). Reverts once idle.
function activeProcessLabel(slice: SessionSlice): string | null {
  if (!slice.turnActive) return null;
  for (let i = slice.live.length - 1; i >= 0; i--) {
    const part = slice.live[i];
    if (part.kind === "tool") return shortCommandLabel(part.command, part.tool);
  }
  return "Running…";
}

function shortCommandLabel(command: string | undefined, tool: string): string {
  if (!command) return tool;
  let candidate: string = tool;
  try {
    const parsed = JSON.parse(command) as Record<string, unknown>;
    if (typeof parsed.command === "string") candidate = parsed.command;
    else if (typeof parsed.description === "string") candidate = parsed.description;
    else {
      const firstString = Object.values(parsed).find((v): v is string => typeof v === "string");
      if (firstString) candidate = firstString;
    }
  } catch {
    // Not valid JSON — fall back to the tool name.
  }
  return candidate.length > 40 ? `${candidate.slice(0, 40)}…` : candidate;
}

function truncateTitle(s: string, maxLen: number): string {
  s = s.replace(/\n/g, " ").trim();
  const runes = Array.from(s);
  if (runes.length <= maxLen) return s;
  return runes.slice(0, maxLen - 3).join("") + "...";
}

function deriveChatTabTitle(tab: { title: string; titleManual?: boolean }, slice: SessionSlice): string {
  if (tab.titleManual) return tab.title;
  if (tab.title && tab.title !== "New session") return tab.title;
  const raw = slice.tuiStatus?.session_title?.trim() || "";
  if (raw) return truncateTitle(raw, 80);
  for (const m of slice.messages) {
    const text = m.content?.trim() || "";
    if (m.role === "user" && text && !text.startsWith("/")) {
      return truncateTitle(text, 80);
    }
  }
  if (slice.messages.length === 0) return "New session";
  return tab.title || "New session";
}

interface ChatDerived {
  id: string;
  initialized: boolean;
  hasPending: boolean;
  processLabel: string | null;
  displayTitle: string;
}

function chatDerivedEqual(a: ChatDerived[], b: ChatDerived[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    const x = a[i];
    const y = b[i];
    if (x.id !== y.id || x.initialized !== y.initialized || x.hasPending !== y.hasPending || x.processLabel !== y.processLabel || x.displayTitle !== y.displayTitle) {
      return false;
    }
  }
  return true;
}

interface TabPillProps {
  sortId: string;
  emoji: string;
  title: string;
  isActive: boolean;
  isLoading?: boolean;
  hasPending?: boolean;
  /** Terminal-only: a backgrounded terminal emitted a bell/notification. Drives
   *  the "unread activity" badge above the pill. */
  hasAlert?: boolean;
  processLabel?: string | null;
  isEditing: boolean;
  editValue: string;
  onEditValueChange: (v: string) => void;
  onClick: (e: React.MouseEvent) => void;
  onStartRename: () => void;
  onCommitRename: () => void;
  onCancelRename: () => void;
  onClose: (e: React.MouseEvent) => void;
  onAuxClose?: (e: React.MouseEvent) => void;
}

function TabPill({
  sortId,
  emoji,
  title,
  isActive,
  isLoading,
  hasPending,
  hasAlert,
  processLabel,
  isEditing,
  editValue,
  onEditValueChange,
  onClick,
  onStartRename,
  onCommitRename,
  onCancelRename,
  onClose,
  onAuxClose,
}: TabPillProps) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: sortId });
  const style = { transform: CSS.Transform.toString(transform), transition, opacity: isDragging ? 0.5 : 1 };
  const displayTitle = title || sortId;

  return (
    <div
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      role="tab"
      tabIndex={0}
      aria-selected={isActive}
      aria-label={`${displayTitle}${hasAlert ? " (has unread activity)" : ""}`}
      onClick={onClick}
      onContextMenu={(e) => {
        // Preserve native context menu / right-click behavior: don't select
        // or focus the terminal on right-click. onClick only fires for button
        // 0, but explicitly stopping here prevents any future click routing
        // from being repurposed for focus.
        e.stopPropagation();
      }}
      onAuxClick={(e) => {
        if (e.button === 1) {
          if (onAuxClose) onAuxClose(e);
          else onClose(e);
        }
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick({ button: 0, detail: 1 } as unknown as React.MouseEvent);
        }
      }}
      className={`relative flex items-center gap-1 px-2.5 py-1 rounded-md text-[13px] leading-4 cursor-pointer shrink-0 touch-none transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring ${
        isActive ? "bg-muted/80 text-foreground border border-border/70 shadow-sm" : "bg-card/20 text-muted-foreground border border-transparent hover:bg-muted/50 hover:text-foreground"
      }`}
    >
      {hasAlert && (
        <span
          aria-hidden
          title="Unread activity (terminal bell or notification)"
          className="pointer-events-none absolute -top-2 left-1/2 z-10 flex h-3.5 w-3.5 -translate-x-1/2 items-center justify-center rounded-full bg-red-500 text-white shadow-sm"
        >
          <Bell className="h-2.5 w-2.5" />
        </span>
      )}
      {/* Leading icon slot: while loading, the tab's glyph is replaced by a
          spinner (browser favicon convention — the identity icon yields to
          the in-flight state) instead of rendering both side by side. */}
      {isLoading ? (
        <Loader2
          aria-hidden
          className="w-3 h-3 animate-spin motion-reduce:animate-none text-muted-foreground shrink-0"
        />
      ) : (
        <span aria-hidden className="shrink-0">
          {emoji}
        </span>
      )}
      {hasPending && (
        <span className="h-1.5 w-1.5 rounded-full bg-amber-400 shrink-0" title="Waiting for a response in this tab" />
      )}
      {isEditing ? (
        <input
          autoFocus
          value={editValue}
          onChange={(e) => onEditValueChange(e.target.value)}
          onClick={(e) => e.stopPropagation()}
          onBlur={onCommitRename}
          onKeyDown={(e) => {
            if (e.key === "Enter") onCommitRename();
            else if (e.key === "Escape") onCancelRename();
          }}
          className="max-w-48 w-44 bg-background text-foreground rounded px-1 outline-none border border-blue-500"
        />
      ) : (
        <span
          className="max-w-48 truncate shrink-0"
          title={displayTitle}
          onDoubleClick={(e) => {
            e.stopPropagation();
            onStartRename();
          }}
        >
          {displayTitle}
        </span>
      )}
      {processLabel && (
        <span
          className="max-w-24 truncate text-[10px] leading-none px-1 py-0.5 rounded bg-amber-500/20 text-amber-300 border border-amber-500/30 shrink-0"
          title={processLabel}
        >
          {processLabel}
        </span>
      )}
      <span
        role="button"
        tabIndex={0}
        aria-label={`Close ${displayTitle}`}
        title={`Close ${displayTitle}`}
      className="p-0.5 rounded hover:bg-accent text-muted-foreground hover:text-accent-foreground transition-colors shrink-0"
        onClick={onClose}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            e.stopPropagation();
            onClose(e as unknown as React.MouseEvent);
          }
        }}
      >
        <X className="w-3 h-3" />
      </span>
    </div>
  );
}

/** Browser pills read live loading state from the browser store (server-driven
 *  via nav events); the browserTabsStore strip only owns tab identity/title. */
function BrowserTabPill({ id, ...props }: { id: string } & Omit<TabPillProps, "isLoading">) {
  const s = useBrowserStore(`tab:${id}` as StateKey);
  return <TabPill {...props} isLoading={!!s?.loading} />;
}

type PendingTabClose = { kind: "chat" | "browser" | "terminal"; id: string; title: string } | null;

interface Props {
  focusedKind: FocusedKind;
  onFocusKindChange: (kind: FocusedKind) => void;
}

export default function UnifiedTabBar({ focusedKind, onFocusKindChange }: Props) {
  const {
    state: projectState,
    tabs: chatTabs,
    activeTabId: activeChatId,
    openSessionTab,
    closeSessionTab,
    openNewSessionTab,
    toggleSessionPicker,
    dispatch: projectDispatch,
  } = useProjectState();
  const activeProjectPath = projectState.activeProject?.path ?? "";
  const chatDispatch = useChatDispatch();
  const { available: terminalAvailable } = useTerminalConfig();
  const { state: terminalState, openTerminal, closeTerminal, setActiveId: setActiveTerminalId, renameTerminal, clearAlert } =
    useTerminalState();
  const { terminals, activeId: activeTerminalId } = getProjectTerminals(terminalState, activeProjectPath);
  const {
    tabs: browserTabs,
    activeId: activeBrowserId,
    openBrowserTab,
    closeBrowserTab,
    renameBrowserTab,
    activateBrowserTab,
  } = useBrowserTabs(activeProjectPath);

  const chatDerived = useChatSelector(
    (s: ChatState): ChatDerived[] =>
      chatTabs.map((tab) => {
        const slice = getSessionSlice(s, tab.id);
        return {
          id: tab.id,
          initialized: slice.initialized,
          hasPending: activeChatId !== tab.id && (slice.pendingPermission !== null || slice.pendingQuestion !== null),
          processLabel: activeProcessLabel(slice),
          displayTitle: deriveChatTabTitle(tab, slice),
        };
      }),
    chatDerivedEqual,
  );

  const [editing, setEditing] = useState<{ kind: FocusedKind; id: string } | null>(null);
  const [editValue, setEditValue] = useState("");

  // When a backgrounded terminal is focused (clicked), keep its "unread
  // activity" badge visible for 3s and then clear it. Re-runs if a fresh bell
  // arrives mid-window (resets the timer) and cleans up on tab/project change.
  useEffect(() => {
    if (focusedKind !== "terminal") return;
    if (!activeTerminalId || activeTerminalId === PROCESSES_TAB_ID) return;
    const active = terminals.find((t) => t.id === activeTerminalId);
    if (!active?.alerted) return;
    const timer = setTimeout(() => clearAlert(activeProjectPath, activeTerminalId), 3000);
    return () => clearTimeout(timer);
  }, [focusedKind, activeTerminalId, terminals, activeProjectPath, clearAlert]);

  const chatIds = useMemo(() => chatTabs.map((t) => t.id), [chatTabs]);
  const terminalIds = useMemo(() => terminals.map((t) => t.id), [terminals]);
  const browserIds = useMemo(() => browserTabs.map((t) => t.id), [browserTabs]);

  // Persisted merged tab order. Seeded from localStorage and re-synced whenever
  // the live tab set or active project changes (tabs added/removed, project
  // switch). Drag-reorder updates this state *and* localStorage so the move
  // registers immediately instead of snapping back — previously `order` was a
  // memo derived from localStorage, so a drag that only wrote localStorage
  // never re-rendered the bar into its new order.
  const [order, setOrder] = useState<UnifiedTabKey[]>(() =>
    reconcileTabOrder(loadTabOrder(activeProjectPath), chatIds, terminalIds, browserIds),
  );
  const orderRef = useRef(order);

  useEffect(() => {
    const next = reconcileTabOrder(loadTabOrder(activeProjectPath), chatIds, terminalIds, browserIds);
    if (next.join("") === orderRef.current.join("")) return;
    orderRef.current = next;
    setOrder(next);
  }, [activeProjectPath, chatIds, terminalIds, browserIds]);

  const dndSensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      setOrder((prev) => {
        const oldIndex = prev.indexOf(active.id as UnifiedTabKey);
        const newIndex = prev.indexOf(over.id as UnifiedTabKey);
        if (oldIndex === -1 || newIndex === -1) return prev;
        const next = arrayMove(prev, oldIndex, newIndex);
        orderRef.current = next;
        saveTabOrder(activeProjectPath, next);
        return next;
      });
    },
    [activeProjectPath],
  );

  const scrollRef = useRef<HTMLDivElement>(null);
  const handleWheel = (e: React.WheelEvent<HTMLDivElement>) => {
    const el = scrollRef.current;
    if (!el || el.scrollWidth <= el.clientWidth + 1) return;
    const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
    if (delta === 0) return;
    const atLeft = el.scrollLeft <= 0;
    const atRight = el.scrollLeft + el.clientWidth >= el.scrollWidth - 1;
    if ((delta < 0 && atLeft) || (delta > 0 && atRight)) return;
    e.preventDefault();
    el.scrollLeft += delta;
  };

  const handleChatClick = useCallback(
    (e: React.MouseEvent, id: string, title: string) => {
      if (e.button !== 0) return;
      onFocusKindChange("chat");
      if (activeChatId === id) return;
      openSessionTab(id, title);
    },
    [activeChatId, openSessionTab, onFocusKindChange],
  );

  const handleTerminalClick = useCallback(
    (e: React.MouseEvent, id: string) => {
      if (e.button !== 0) return;
      const alreadyActive = focusedKind === "terminal" && activeTerminalId === id;
      onFocusKindChange("terminal");
      setActiveTerminalId(activeProjectPath, id);
      // Single left-click on the already-active terminal should still focus
      // the shell (active effect won't re-fire). Double-click's second click
      // has detail === 2, so it won't steal focus from the rename input.
      if (alreadyActive && e.detail === 1) {
        focusTerminalById(id);
      }
    },
    [activeProjectPath, setActiveTerminalId, onFocusKindChange, focusedKind, activeTerminalId],
  );

  const handleBrowserClick = useCallback(
    (e: React.MouseEvent, id: string) => {
      if (e.button !== 0) return;
      onFocusKindChange("browser");
      activateBrowserTab(id);
    },
    [onFocusKindChange, activateBrowserTab],
  );

  const [pendingClose, setPendingClose] = useState<PendingTabClose>(null);

  const doCloseChat = useCallback((id: string) => {
    closeSessionBackend(id);
    cancelLiveDeltas(id);
    chatDispatch({ type: "RESET", sessionId: id });
    closeSessionTab(id);
    clearQueue(id);
  }, [closeSessionTab, chatDispatch]);

  const doCloseBrowser = useCallback((id: string) => {
    closeBrowserTab(id);
    // Drop the tab's page state (URL/history/console) and revoke its server
    // browse session — browserTabsStore owns only the strip identity.
    browserActions.close(`tab:${id}`);
  }, [closeBrowserTab]);

  const doCloseTerminal = useCallback((id: string) => {
    closeTerminal(activeProjectPath, id);
  }, [closeTerminal, activeProjectPath]);

  const confirmPendingClose = useCallback(() => {
    if (!pendingClose) return;
    const req = pendingClose;
    setPendingClose(null);
    if (req.kind === "chat") doCloseChat(req.id);
    else if (req.kind === "browser") doCloseBrowser(req.id);
    else doCloseTerminal(req.id);
  }, [pendingClose, doCloseChat, doCloseBrowser, doCloseTerminal]);

  const cancelPendingClose = useCallback(() => setPendingClose(null), []);

  // X button → confirm first; middle-click → immediate close (no confirmation)
  const handleRequestCloseChat = useCallback((e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    const tab = chatTabs.find((t) => t.id === id);
    const title = tab?.title || id;
    setPendingClose({ kind: "chat", id, title });
  }, [chatTabs]);

  const handleImmediateCloseChat = useCallback((e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    doCloseChat(id);
  }, [doCloseChat]);

  const handleRequestCloseBrowser = useCallback((e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    const tab = browserTabs.find((t) => t.id === id);
    const title = tab?.title || id;
    setPendingClose({ kind: "browser", id, title });
  }, [browserTabs]);

  const handleImmediateCloseBrowser = useCallback((e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    doCloseBrowser(id);
  }, [doCloseBrowser]);

  const handleRequestCloseTerminal = useCallback((e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    // If the terminal has no running app, close directly without confirmation.
    // We have no reliable "running" signal in the tab metadata, so treat idle
    // terminals as directly closable. A future enhancement can check a live
    // busy flag before showing the dialog.
    const t = terminals.find((term) => term.id === id);
    // Heuristic: if we ever track busy state, gate on it here. For now all
    // terminals are considered idle → close immediately (no confirmation).
    // To preserve the "confirm when busy" contract, keep the pending path
    // reachable by checking t?.alerted or similar when available.
    const hasRunningApp = false; // TODO: wire to actual busy detection when available
    if (!hasRunningApp) {
      doCloseTerminal(id);
      return;
    }
    const title = t?.title || id;
    setPendingClose({ kind: "terminal", id, title });
  }, [terminals, doCloseTerminal]);

  const handleImmediateCloseTerminal = useCallback((e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    doCloseTerminal(id);
  }, [doCloseTerminal]);

  const handleNewBrowser = useCallback(() => {
    const id = openBrowserTab();
    // The panel renders nothing without a store slice — open it up front.
    browserActions.open(`tab:${id}`);
    onFocusKindChange("browser");
  }, [openBrowserTab, onFocusKindChange]);

  const startRename = useCallback((kind: FocusedKind, id: string, currentTitle: string) => {
    setEditing({ kind, id });
    setEditValue(currentTitle);
  }, []);

  const commitRename = useCallback(() => {
    const target = editing;
    setEditing(null);
    if (!target) return;
    const title = editValue.trim();
    if (!title) return;
    if (target.kind === "chat") {
      projectDispatch({ type: "UPDATE_TAB_TITLE", id: target.id, title, manual: true });
      if (!target.id.startsWith("new-")) {
        api.setSessionTitle(target.id, title).catch((err) => {
          console.error("failed to save renamed tab title", err);
        });
      }
    } else if (target.kind === "browser") {
      renameBrowserTab(target.id, title);
    } else {
      renameTerminal(activeProjectPath, target.id, title);
    }
  }, [editing, editValue, projectDispatch, renameTerminal, renameBrowserTab, activeProjectPath]);

  const handleNewChat = useCallback(() => {
    onFocusKindChange("chat");
    openNewSessionTab(isNewSessionTabEmpty(activeChatId));
  }, [activeChatId, openNewSessionTab, onFocusKindChange]);

  const handleNewTerminal = useCallback(() => {
    onFocusKindChange("terminal");
    openTerminal(activeProjectPath);
  }, [activeProjectPath, openTerminal, onFocusKindChange]);

  const isLoadingChatTab = (tabId: string, initialized: boolean) => !tabId.startsWith("new-") && !initialized;

  if (!projectState.activeProject) return null;

  const chatById = new Map(chatTabs.map((t) => [t.id, t]));
  const terminalById = new Map(terminals.map((t) => [t.id, t]));

  return (
    <div
      ref={scrollRef}
      onWheel={handleWheel}
      className="flex items-center h-9 px-2 gap-0.5 bg-card border-b border-border overflow-x-auto overflow-y-hidden scrollbar-hide flex-nowrap min-w-0 w-full touch-pan-x overscroll-x-contain pt-2"
      style={{ WebkitOverflowScrolling: "touch" } as React.CSSProperties}
    >
      <DndContext sensors={dndSensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        <SortableContext items={order} strategy={horizontalListSortingStrategy}>
          {order.map((key) => {
            if (key.startsWith("chat:")) {
              const id = key.slice("chat:".length);
              const tab = chatById.get(id);
              if (!tab) return null;
              const derived = chatDerived.find((d) => d.id === id);
              const displayTitle = derived?.displayTitle ?? tab.title;
          return (
                <TabPill
                  key={key}
                  sortId={key}
                  emoji="💬"
                  title={displayTitle}
                  isActive={focusedKind === "chat" && activeChatId === id}
                  isLoading={isLoadingChatTab(id, derived?.initialized ?? false)}
                  hasPending={derived?.hasPending ?? false}
                  processLabel={derived?.processLabel ?? null}
                  isEditing={editing?.kind === "chat" && editing.id === id}
                  editValue={editValue}
                  onEditValueChange={setEditValue}
                  onClick={(e) => handleChatClick(e, id, displayTitle)}
                  onStartRename={() => startRename("chat", id, displayTitle || "")}
                  onCommitRename={commitRename}
                  onCancelRename={() => setEditing(null)}
                  onClose={(e) => handleRequestCloseChat(e, id)}
                  onAuxClose={(e) => handleImmediateCloseChat(e, id)}
                />
              );
            }
            if (key.startsWith("browser:")) {
              const id = key.slice("browser:".length);
              const tab = browserTabs.find((t) => t.id === id);
              if (!tab) return null;
              return (
                <BrowserTabPill
                  key={key}
                  id={id}
                  sortId={key}
                  emoji="🌐"
                  title={tab.title}
                  isActive={focusedKind === "browser" && activeBrowserId === id}
                  isEditing={editing?.kind === "browser" && editing.id === id}
                  editValue={editValue}
                  onEditValueChange={setEditValue}
                  onClick={(e) => handleBrowserClick(e, id)}
                  onStartRename={() => startRename("browser", id, tab.title)}
                  onCommitRename={commitRename}
                  onCancelRename={() => setEditing(null)}
                  onClose={(e) => handleRequestCloseBrowser(e, id)}
                  onAuxClose={(e) => handleImmediateCloseBrowser(e, id)}
                />
              );
            }
            const id = key.slice("term:".length);
            const term = terminalById.get(id);
            if (!term) return null;
            return (
              <TabPill
                key={key}
                sortId={key}
                emoji="⌨️"
                title={term.title}
                isActive={focusedKind === "terminal" && activeTerminalId === id}
                hasAlert={!!term.alerted}
                isEditing={editing?.kind === "terminal" && editing.id === id}
                editValue={editValue}
                onEditValueChange={setEditValue}
                onClick={(e) => handleTerminalClick(e, id)}
                onStartRename={() => startRename("terminal", id, term.title)}
                onCommitRename={commitRename}
                onCancelRename={() => setEditing(null)}
                onClose={(e) => handleRequestCloseTerminal(e, id)}
                onAuxClose={(e) => handleImmediateCloseTerminal(e, id)}
              />
            );
          })}
        </SortableContext>
      </DndContext>

      <div className="w-px h-4 bg-border mx-1 shrink-0" aria-hidden="true" />

      <button
        onClick={handleNewChat}
        aria-label="New chat session"
        title="New chat session"
        className="flex shrink-0 items-center gap-0.5 h-6 px-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
      >
        <span aria-hidden className="text-[13px]">💬</span>
        <Plus className="w-3 h-3" />
      </button>

      <button
        onClick={handleNewBrowser}
        aria-label="New browser tab"
        title="New browser tab"
        className="flex shrink-0 items-center gap-0.5 h-6 px-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
      >
        <span aria-hidden className="text-[13px]">🌐</span>
        <Plus className="w-3 h-3" />
      </button>

      {terminalAvailable && (
        <button
          onClick={handleNewTerminal}
          aria-label="New terminal"
          title="New terminal"
          className="flex shrink-0 items-center gap-0.5 px-2 py-1 rounded-md text-xs text-muted-foreground hover:text-foreground hover:bg-muted transition-colors border border-border"
        >
          <span aria-hidden>⌨️</span>
          <Plus className="w-3 h-3" />
        </button>
      )}

      {terminalAvailable && (
        <button
          onClick={() => {
            onFocusKindChange("terminal");
            setActiveTerminalId(activeProjectPath, PROCESSES_TAB_ID);
          }}
          className={`flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-xs transition-colors border ${
            focusedKind === "terminal" && activeTerminalId === PROCESSES_TAB_ID
              ? "bg-accent text-accent-foreground "
              : " text-muted-foreground hover:bg-muted hover:text-foreground"
          }`}
        >
          Processes
        </button>
      )}

      <button
        onClick={toggleSessionPicker}
        className="flex items-center gap-1 px-2 py-1 rounded-md text-xs text-muted-foreground hover:text-foreground hover:bg-muted transition-colors shrink-0 border border-border"
        title="Browse all sessions"
      >
        <List className="w-3.5 h-3.5" />
        <span className="hidden sm:inline">All sessions</span>
      </button>
      {pendingClose && (
        <Dialog open onOpenChange={(o) => !o && cancelPendingClose()}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle className="text-sm">Close {pendingClose.kind} tab?</DialogTitle>
            </DialogHeader>
            <p className="text-sm text-muted-foreground">
              Close <span className="font-medium text-foreground">{pendingClose.title || pendingClose.id}</span>? This cannot be undone.
            </p>
            <DialogFooter className="gap-2">
              <Button variant="ghost" onClick={cancelPendingClose}>Cancel</Button>
              <Button variant="destructive" onClick={confirmPendingClose}>Close tab</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
