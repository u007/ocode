import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { X, List, Plus, Loader2, Bell, ChevronDown, Pause } from "lucide-react";
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
  rectSortingStrategy,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useChatDispatch, useChatSelector, getSessionSlice, type ChatState, type SessionSlice } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { useTerminalConfig } from "@/hooks/useTerminalConfig";
import { useTerminalState, getProjectTerminals, terminalDisplayTitle, PROCESSES_TAB_ID } from "../../stores/terminalStore";
import { useBrowserTabs } from "../../stores/browserTabsStore";
import { useBrowserPersistence } from "../Browser/browserPersistence";
import { browserActions, useBrowserStore, type StateKey } from "../../lib/browserStore";
import type { FocusedKind } from "../../lib/viewPersistence";
import { isNewSessionTabEmpty } from "../../lib/tabDrafts";
import { clearQueue } from "../../lib/tabQueue";
import { cancelLiveDeltas, closeSessionBackend } from "../../lib/sessionEvents";
import { resolveSessionHost } from "../../hooks/useSessionHost";
import { prefetchSession } from "../../lib/sessionPrefetch";
import { api } from "../../api/client";
import { loadTabOrder, saveTabOrder, reconcileTabOrder, type UnifiedTabKey } from "./tabOrderPersistence";
import { focusTerminalById } from "../Terminal/terminalFocus";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Button } from "../ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "../ui/popover";

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

type ChatTurnState = "idle" | "running" | "stalled";

/** Live turn status of a chat, mirroring the project sidebar's streaming/stalled
 *  signals: `stalled` (no heartbeat while a turn is active) wins over `running`
 *  (turn in flight), otherwise idle. */
function deriveTurnState(slice: SessionSlice): ChatTurnState {
  if (slice.turnStalled) return "stalled";
  if (slice.isStreaming || slice.turnActive) return "running";
  return "idle";
}

/** Compact glyph for a chat's live turn state — a blue spinner while running,
 *  an amber pause when the stream stalled. Null when idle so callers can render
 *  an empty fixed-size slot. */
function TurnStateGlyph({ state }: { state: ChatTurnState }) {
  if (state === "running") {
    return <Loader2 className="h-3 w-3 animate-spin motion-reduce:animate-none text-blue-500" />;
  }
  if (state === "stalled") {
    return <Pause className="h-3 w-3 text-amber-500" />;
  }
  return null;
}

function turnStateTitle(state: ChatTurnState): string | undefined {
  if (state === "running") return "Running";
  if (state === "stalled") return "Stalled — streaming stopped";
  return undefined;
}

interface ChatDerived {
  id: string;
  initialized: boolean;
  hasPending: boolean;
  displayTitle: string;
  turnState: ChatTurnState;
}
function chatDerivedEqual(a: ChatDerived[], b: ChatDerived[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    const x = a[i];
    const y = b[i];
    if (x.id !== y.id || x.initialized !== y.initialized || x.hasPending !== y.hasPending || x.displayTitle !== y.displayTitle || x.turnState !== y.turnState) {
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
  /** Chat-only: live turn status (running/stalled) shown as a compact badge.
   *  The slot is always rendered so toggling it never resizes the pill. */
  turnState?: ChatTurnState;
  /** Terminal-only: a backgrounded terminal emitted a bell/notification. Drives
   *  the "unread activity" badge above the pill. */
  hasAlert?: boolean;
  isEditing: boolean;
  editValue: string;
  onEditValueChange: (v: string) => void;
  onClick: (e: React.MouseEvent) => void;
  onStartRename: () => void;
  onCommitRename: () => void;
  onCancelRename: () => void;
  onClose: (e: React.MouseEvent) => void;
  onAuxClose?: (e: React.MouseEvent) => void;
  /** Fired when the pill is hovered or focused — a cheap "the user is about to
   *  open this tab" signal used to warm its data. Chat tabs use it to prefetch
   *  the transcript; other kinds pass nothing. */
  onHover?: () => void;
}

function TabPill({
  sortId,
  emoji,
  title,
  isActive,
  isLoading,
  hasPending,
  turnState,
  hasAlert,
  isEditing,
  editValue,
  onEditValueChange,
  onClick,
  onStartRename,
  onCommitRename,
  onCancelRename,
  onClose,
  onAuxClose,
  onHover,
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
      onMouseEnter={onHover}
      onFocus={onHover}
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
      className={`relative flex w-full lg:w-52 items-center gap-1 overflow-hidden px-2.5 py-1 rounded-md text-[13px] leading-4 cursor-pointer shrink-0 touch-none transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring ${
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
      {/* Leading icon slot: fixed-size box so the pill width is identical
          whether the tab's glyph or the loading spinner is shown (browser
          favicon convention — the identity icon yields to the in-flight
          state). Without this, spinner↔emoji swaps resize the pill and, in a
          wrapping bar, reshuffle every other tab's row on each switch. */}
      <span aria-hidden data-testid="tab-icon" className="flex h-4 w-4 shrink-0 items-center justify-center">
        {isLoading ? (
          <Loader2 className="h-3 w-3 animate-spin motion-reduce:animate-none text-muted-foreground" />
        ) : (
          <span className="text-[13px] leading-none">{emoji}</span>
        )}
      </span>
      {/* Pending dot slot: always rendered (transparent when idle) so toggling
          the "waiting for response" indicator never resizes the pill. The dot
          flips on exactly the two tabs involved in a tab switch (the old tab
          gains it, the new one loses it), so a conditional render reflowed the
          wrapping bar on every single switch. */}
      <span
        aria-hidden
        data-testid="tab-pending"
        data-active={hasPending ? "true" : "false"}
        title={hasPending ? "Waiting for a response in this tab" : undefined}
        className={`h-1.5 w-1.5 rounded-full shrink-0 ${hasPending ? "bg-amber-400" : "bg-transparent"}`}
      />
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
          className="min-w-0 flex-1 bg-background text-foreground rounded px-1 outline-none border border-blue-500"
        />
      ) : (
        <span
          className="min-w-0 flex-1 truncate whitespace-nowrap"
          title={displayTitle}
          onDoubleClick={(e) => {
            e.stopPropagation();
            onStartRename();
          }}
        >
          {displayTitle}
        </span>
      )}
      {/* Live turn-status slot: always rendered (empty when idle) so a chat
          flipping running↔stalled never resizes the pill and reshuffles the
          wrapping bar — same rationale as the pending-dot slot. */}
      <span
        aria-hidden
        data-testid="tab-turn-state"
        data-state={turnState ?? "idle"}
        title={turnStateTitle(turnState ?? "idle")}
        className="flex h-3.5 w-3.5 shrink-0 items-center justify-center"
      >
        <TurnStateGlyph state={turnState ?? "idle"} />
      </span>
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

/** Browser pills read live page state from the browser store (server-driven
 *  via nav events); the browserTabsStore strip owns tab identity, the manual
 *  rename, and the fallback title. Display precedence: manual rename >
 *  page title > strip fallback. A top-level navigation clears the manual
 *  override so the next page title takes effect. */
function BrowserTabPill({
  id,
  manualTitle,
  fallbackTitle,
  onNavigated,
  ...props
}: {
  id: string;
  manualTitle: string | null;
  fallbackTitle: string;
  onNavigated: (id: string) => void;
} & Omit<TabPillProps, "isLoading" | "title">) {
  const s = useBrowserStore(`tab:${id}` as StateKey);
  const seenUrl = useRef<string | null>(null);
  useEffect(() => {
    const url = s?.url ?? null;
    if (seenUrl.current === null) {
      seenUrl.current = url;
      return;
    }
    if (url !== seenUrl.current) {
      seenUrl.current = url;
      onNavigated(id);
    }
  }, [s?.url, id, onNavigated]);
  const displayTitle = manualTitle ?? s?.pageTitle ?? fallbackTitle ?? "New tab";
  return <TabPill {...props} title={displayTitle} isLoading={!!s?.loading} />;
}

/** One row of the mobile/tablet tab-switcher dropdown. `key` is the same
 *  composite key the pill strip uses (plus the synthetic "term:processes"
 *  entry for the Processes pseudo-tab, which has no pill). */
type TabEntryKind = "chat" | "terminal" | "browser";

interface TabEntry {
  key: UnifiedTabKey | "term:processes";
  kind: TabEntryKind;
  id: string;
  emoji: string;
  title: string;
  isActive: boolean;
  hasPending?: boolean;
  hasAlert?: boolean;
  isLoading?: boolean;
  turnState?: ChatTurnState;
}

interface MobileTabDropdownProps {
  entries: TabEntry[];
  onActivate: (e: React.MouseEvent, entry: TabEntry) => void;
  onRequestClose: (e: React.MouseEvent, entry: TabEntry) => void;
  editing: { kind: FocusedKind; id: string } | null;
  editValue: string;
  onEditValueChange: (v: string) => void;
  onStartRename: (kind: FocusedKind, id: string, title: string) => void;
  onCommitRename: () => void;
  onCancelRename: () => void;
}

/** Phone/tablet presentation of the tab strip: a single dropdown whose closed
 *  trigger shows the ACTIVE tab (emoji + title + chevron), opening into a
 *  list with full pill parity (pending dot, unread bell, close button, rename
 *  via double-click). Select a row with the exact same activation handlers
 *  the desktop pills use, so switching never unmounts the keep-alive surface.
 */
function MobileTabDropdown({
  entries,
  onActivate,
  onRequestClose,
  editing,
  editValue,
  onEditValueChange,
  onStartRename,
  onCommitRename,
  onCancelRename,
}: MobileTabDropdownProps) {
  const [open, setOpen] = useState(false);
  const active = entries.find((e) => e.isActive) ?? entries[0] ?? null;
  if (!active) return null;
  const activeTitle = active.title || active.id;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          data-testid="mobile-tab-dropdown-trigger"
          aria-haspopup="listbox"
          aria-expanded={open}
          aria-label={`Sessions: ${activeTitle}`}
          title={activeTitle}
          className="flex min-w-0 w-full items-center gap-1 rounded-md border border-border bg-card px-2 py-1 text-[13px] leading-4 text-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        >
          <span aria-hidden data-testid="mobile-tab-dropdown-active-icon" className="flex h-4 w-4 shrink-0 items-center justify-center text-[13px]">
            {active.isLoading ? (
              <Loader2 className="h-3 w-3 animate-spin motion-reduce:animate-none text-muted-foreground" />
            ) : (
              active.emoji
            )}
          </span>
          <span className="min-w-0 flex-1 truncate text-left">{activeTitle}</span>
          <span
            aria-hidden
            data-testid="mobile-tab-dropdown-active-pending"
            data-active={active.hasPending ? "true" : "false"}
            className={`h-1.5 w-1.5 shrink-0 rounded-full ${active.hasPending ? "bg-amber-400" : "bg-transparent"}`}
          />
          {active.turnState && active.turnState !== "idle" && (
            <span
              aria-hidden
              data-testid="mobile-tab-dropdown-active-turn-state"
              data-state={active.turnState}
              title={turnStateTitle(active.turnState)}
              className="flex h-3.5 w-3.5 shrink-0 items-center justify-center"
            >
              <TurnStateGlyph state={active.turnState} />
            </span>
          )}
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={4} className="w-72 max-w-[calc(100vw-1.25rem)] p-1.5">
        <div role="listbox" aria-label="Session tabs" className="flex max-h-[60vh] flex-col gap-0.5 overflow-y-auto">
          {entries.map((entry) => {
            const title = entry.title || entry.id;
            const isEditingThis = editing?.kind === entry.kind && editing.id === entry.id;
            return (
              <div
                key={entry.key}
                role="option"
                aria-selected={entry.isActive}
                data-testid="mobile-tab-dropdown-item"
                tabIndex={0}
                title={title}
                className={`flex cursor-pointer items-center gap-1.5 rounded-md px-2 py-1.5 text-[13px] leading-4 outline-none focus-visible:ring-1 focus-visible:ring-ring ${
                  entry.isActive
                    ? "bg-accent text-accent-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                }`}
                onClick={(e) => {
                  onActivate(e, entry);
                  setOpen(false);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onActivate(e as unknown as React.MouseEvent, entry);
                    setOpen(false);
                  }
                }}
                onDoubleClick={(e) => {
                  if (entry.key === "term:processes") return;
                  e.stopPropagation();
                  onStartRename(entry.kind, entry.id, title);
                }}
              >
                <span aria-hidden className="flex h-4 w-4 shrink-0 items-center justify-center text-[13px]">
                  {entry.isLoading ? (
                    <Loader2 className="h-3 w-3 animate-spin motion-reduce:animate-none text-muted-foreground" />
                  ) : (
                    entry.emoji
                  )}
                </span>
                {isEditingThis ? (
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
                    className="min-w-0 flex-1 rounded bg-background px-1 text-[13px] text-foreground outline-none border border-blue-500"
                  />
                ) : (
                  <span className="min-w-0 flex-1 truncate">{title}</span>
                )}
                {entry.hasAlert && (
                  <span
                    aria-hidden
                    title="Unread activity (terminal bell or notification)"
                    data-testid="mobile-tab-dropdown-alert"
                    className="flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-full bg-red-500 text-white"
                  >
                    <Bell className="h-2 w-2" />
                  </span>
                )}
                {entry.turnState && entry.turnState !== "idle" && (
                  <span
                    aria-hidden
                    data-testid="mobile-tab-dropdown-turn-state"
                    data-state={entry.turnState}
                    title={turnStateTitle(entry.turnState)}
                    className="flex h-3.5 w-3.5 shrink-0 items-center justify-center"
                  >
                    <TurnStateGlyph state={entry.turnState} />
                  </span>
                )}
                <span
                  aria-hidden
                  data-testid="mobile-tab-dropdown-pending"
                  data-active={entry.hasPending ? "true" : "false"}
                  className={`h-1.5 w-1.5 shrink-0 rounded-full ${entry.hasPending ? "bg-amber-400" : "bg-transparent"}`}
                />
                {entry.key !== "term:processes" && (
                  <button
                    type="button"
                    aria-label={`Close ${title}`}
                    title={`Close ${title}`}
                    className="shrink-0 rounded p-0.5 text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
                    onClick={(e) => {
                      e.stopPropagation();
                      onRequestClose(e, entry);
                    }}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        e.stopPropagation();
                        onRequestClose(e as unknown as React.MouseEvent, entry);
                      }
                    }}
                  >
                    <X className="h-3 w-3" />
                  </button>
                )}
              </div>
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
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
  const activeProjectHost = projectState.activeProject?.host;
  useBrowserPersistence(activeProjectPath);

  const chatDispatch = useChatDispatch();
  const { available: terminalAvailable } = useTerminalConfig();
  const { state: terminalState, openTerminal, closeTerminal, setActiveId: setActiveTerminalId, renameTerminal, clearAlert } =
    useTerminalState();
  const { terminals, activeId: activeTerminalId } = useMemo(
    () => getProjectTerminals(terminalState, activeProjectPath, activeProjectHost),
    [terminalState, activeProjectPath, activeProjectHost],
  );
  const {
    tabs: browserTabs,
    activeId: activeBrowserId,
    openBrowserTab,
    closeBrowserTab,
    renameBrowserTab,
    activateBrowserTab,
    clearManualTitle,
  } = useBrowserTabs(activeProjectPath);

  const chatDerived = useChatSelector(
    (s: ChatState): ChatDerived[] =>
      chatTabs.map((tab) => {
        const slice = getSessionSlice(s, tab.id);
        return {
          id: tab.id,
          initialized: slice.initialized,
          hasPending: activeChatId !== tab.id && (slice.pendingPermission !== null || slice.pendingQuestion !== null),
          displayTitle: deriveChatTabTitle(tab, slice),
          turnState: deriveTurnState(slice),
        };
      }),
    chatDerivedEqual,
  );

  const [editing, setEditing] = useState<{ kind: FocusedKind; id: string } | null>(null);
  const [editValue, setEditValue] = useState("");

  // When a backgrounded terminal is focused (clicked), keep its "unread
  // activity" badge visible for 3s and then clear it. The effect keys off the
  // memoized `terminals` array: getProjectTerminals rebuilds it whenever
  // alerts exist, so depending on a freshly-built array re-ran the effect on
  // every unrelated render and restarted the 3s timer — a component that
  // re-rendered more often than that never cleared the alert. Memoizing keeps
  // the array stable across unrelated renders while a real bell (which mutates
  // the terminal store) still produces a new array and restarts the window.
  useEffect(() => {
    if (focusedKind !== "terminal") return;
    if (!activeTerminalId || activeTerminalId === PROCESSES_TAB_ID) return;
    const active = terminals.find((t) => t.id === activeTerminalId);
    if (!active?.alerted) return;
    const timer = setTimeout(() => clearAlert(activeProjectPath, activeTerminalId, activeProjectHost), 3000);
    return () => clearTimeout(timer);
  }, [focusedKind, activeTerminalId, terminals, activeProjectPath, activeProjectHost, clearAlert]);

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
      setActiveTerminalId(activeProjectPath, id, activeProjectHost);
      // Single left-click on the already-active terminal should still focus
      // the shell (active effect won't re-fire). Double-click's second click
      // has detail === 2, so it won't steal focus from the rename input.
      if (alreadyActive && e.detail === 1) {
        focusTerminalById(id);
      }
    },
    [activeProjectPath, activeProjectHost, setActiveTerminalId, onFocusKindChange, focusedKind, activeTerminalId],
  );

  const handleBrowserClick = useCallback(
    (e: React.MouseEvent, id: string) => {
      if (e.button !== 0) return;
      onFocusKindChange("browser");
      activateBrowserTab(id);
    },
    [onFocusKindChange, activateBrowserTab],
  );

  // A top-level navigation (new document) clears the manual rename so the
  // next page title takes effect. Same-URL status updates don't reach here
  // (the pill only fires when the surface URL actually changes).
  const handleBrowserNavigated = useCallback(
    (id: string) => clearManualTitle(id),
    [clearManualTitle],
  );

  const [pendingClose, setPendingClose] = useState<PendingTabClose>(null);

  const doCloseChat = useCallback((id: string) => {
    closeSessionBackend(id, resolveSessionHost(projectState, id));
    cancelLiveDeltas(id);
    chatDispatch({ type: "RESET", sessionId: id });
    closeSessionTab(id);
    clearQueue(id);
  }, [closeSessionTab, chatDispatch, projectState]);

  const doCloseBrowser = useCallback((id: string) => {
    closeBrowserTab(id);
    // Drop the tab's page state (URL/history/console) and revoke its server
    // browse session — browserTabsStore owns only the strip identity.
    browserActions.close(`tab:${id}`);
  }, [closeBrowserTab]);

  const doCloseTerminal = useCallback((id: string) => {
    closeTerminal(activeProjectPath, id, activeProjectHost);
  }, [closeTerminal, activeProjectPath, activeProjectHost]);

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
    const title = t ? terminalDisplayTitle(t) : id;
    setPendingClose({ kind: "terminal", id, title });
  }, [terminals, doCloseTerminal]);

  const handleImmediateCloseTerminal = useCallback((e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    doCloseTerminal(id);
  }, [doCloseTerminal]);

  const goProcesses = useCallback(() => {
    onFocusKindChange("terminal");
    setActiveTerminalId(activeProjectPath, PROCESSES_TAB_ID, activeProjectHost);
  }, [onFocusKindChange, setActiveTerminalId, activeProjectPath, activeProjectHost]);

  // Mobile/tablet dropdown routing: same handlers as the desktop pills, so the
  // keep-alive surfaces are merely switched, never unmounted.
  const activateEntry = useCallback(
    (e: React.MouseEvent, entry: TabEntry) => {
      if (entry.key === "term:processes") {
        goProcesses();
        return;
      }
      if (entry.kind === "chat") handleChatClick(e, entry.id, entry.title);
      else if (entry.kind === "terminal") handleTerminalClick(e, entry.id);
      else handleBrowserClick(e, entry.id);
    },
    [handleChatClick, handleTerminalClick, handleBrowserClick, goProcesses],
  );

  const requestCloseEntry = useCallback(
    (e: React.MouseEvent, entry: TabEntry) => {
      e.stopPropagation();
      if (entry.key === "term:processes") return;
      if (entry.kind === "chat") handleRequestCloseChat(e, entry.id);
      else if (entry.kind === "browser") handleRequestCloseBrowser(e, entry.id);
      else handleRequestCloseTerminal(e, entry.id);
    },
    [handleRequestCloseChat, handleRequestCloseBrowser, handleRequestCloseTerminal],
  );

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
        api.setSessionTitle(target.id, title, resolveSessionHost(projectState, target.id)).catch((err) => {
          console.error("failed to save renamed tab title", err);
        });
      }
    } else if (target.kind === "browser") {
      renameBrowserTab(target.id, title);
    } else {
      renameTerminal(activeProjectPath, target.id, title, activeProjectHost);
    }
  }, [editing, editValue, projectDispatch, projectState, renameTerminal, renameBrowserTab, activeProjectPath, activeProjectHost]);

  const handleNewChat = useCallback(() => {
    onFocusKindChange("chat");
    openNewSessionTab(isNewSessionTabEmpty(activeChatId));
  }, [activeChatId, openNewSessionTab, onFocusKindChange]);

  const handleNewTerminal = useCallback(() => {
    onFocusKindChange("terminal");
    openTerminal(activeProjectPath, activeProjectHost);
  }, [activeProjectPath, activeProjectHost, openTerminal, onFocusKindChange]);

  const isLoadingChatTab = (tabId: string, initialized: boolean) => !tabId.startsWith("new-") && !initialized;

  if (!projectState.activeProject) return null;

  const chatById = new Map(chatTabs.map((t) => [t.id, t]));
  const terminalById = new Map(terminals.map((t) => [t.id, t]));

  // One metadata row per tab, shared by the desktop pills (renderPill) and the
  // mobile/tablet dropdown entries below. Keep the two in lockstep when adding
  // a tab kind. title/emoji/active mirror what the pill renders.
  const tabEntryFor = (key: UnifiedTabKey): TabEntry | null => {
    if (key.startsWith("chat:")) {
      const id = key.slice("chat:".length);
      const tab = chatById.get(id);
      if (!tab) return null;
      const derived = chatDerived.find((d) => d.id === id);
      return {
        key,
        kind: "chat" as const,
        id,
        emoji: "💬",
        title: derived?.displayTitle ?? tab.title,
        isActive: focusedKind === "chat" && activeChatId === id,
        hasPending: derived?.hasPending ?? false,
        isLoading: isLoadingChatTab(id, derived?.initialized ?? false),
        turnState: derived?.turnState ?? "idle",
      };
    }
    if (key.startsWith("browser:")) {
      const id = key.slice("browser:".length);
      const tab = browserTabs.find((t) => t.id === id);
      if (!tab) return null;
      return {
        key,
        kind: "browser" as const,
        id,
        emoji: "🌐",
        title: tab.manualTitle ?? tab.title,
        isActive: focusedKind === "browser" && activeBrowserId === id,
      };
    }
    const id = key.slice("term:".length);
    const term = terminalById.get(id);
    if (!term) return null;
    return {
      key,
      kind: "terminal" as const,
      id,
      emoji: "⌨️",
      title: terminalDisplayTitle(term),
      isActive: focusedKind === "terminal" && activeTerminalId === id,
      hasAlert: !!term.alerted,
    };
  };

  // The Processes pseudo-tab has no pill, but mobile users still need to reach
  // it from the dropdown (and the trigger must show it while it is focused).
  const processesActive = focusedKind === "terminal" && activeTerminalId === PROCESSES_TAB_ID;
  const tabEntries: TabEntry[] = [];
  for (const key of order) {
    const entry = tabEntryFor(key);
    if (entry) tabEntries.push(entry);
  }
  if (terminalAvailable) {
    tabEntries.push({
      key: "term:processes",
      kind: "terminal",
      id: PROCESSES_TAB_ID,
      emoji: "⌨️",
      title: "Processes",
      isActive: processesActive,
    });
  }

  const renderPill = (key: string) => {
    if (key.startsWith("chat:")) {
      const id = key.slice("chat:".length);
      const tab = chatById.get(id);
      if (!tab) return null;
      const derived = chatDerived.find((d) => d.id === id);
      const displayTitle = derived?.displayTitle ?? tab.title;
      return (
        <TabPill
          key={id}
          sortId={key}
          emoji="💬"
          title={displayTitle}
          isActive={focusedKind === "chat" && activeChatId === id}
          isLoading={isLoadingChatTab(id, derived?.initialized ?? false)}
          hasPending={derived?.hasPending ?? false}
          turnState={derived?.turnState ?? "idle"}
          isEditing={editing?.kind === "chat" && editing.id === id}
          editValue={editValue}
          onEditValueChange={setEditValue}
          onClick={(e) => handleChatClick(e, id, displayTitle)}
          onHover={() => prefetchSession(id, resolveSessionHost(projectState, id))}
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
          key={id}
          id={id}
          sortId={key}
          emoji="🌐"
          fallbackTitle={tab.title}
          manualTitle={tab.manualTitle ?? null}
          onNavigated={handleBrowserNavigated}
          isActive={focusedKind === "browser" && activeBrowserId === id}
          isEditing={editing?.kind === "browser" && editing.id === id}
          editValue={editValue}
          onEditValueChange={setEditValue}
          onClick={(e) => handleBrowserClick(e, id)}
          onStartRename={() => startRename("browser", id, tab.manualTitle ?? tab.title)}
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
        key={id}
        sortId={key}
        emoji="⌨️"
        title={terminalDisplayTitle(term)}
        isActive={focusedKind === "terminal" && activeTerminalId === id}
        hasAlert={!!term.alerted}
        isEditing={editing?.kind === "terminal" && editing.id === id}
        editValue={editValue}
        onEditValueChange={setEditValue}
        onClick={(e) => handleTerminalClick(e, id)}
        onStartRename={() => startRename("terminal", id, terminalDisplayTitle(term))}
        onCommitRename={commitRename}
        onCancelRename={() => setEditing(null)}
        onClose={(e) => handleRequestCloseTerminal(e, id)}
        onAuxClose={(e) => handleImmediateCloseTerminal(e, id)}
      />
    );
  };

  // Phones/tablets (<lg) collapse the tab strip into a single dropdown whose
  // trigger shows the ACTIVE tab, with the new-tab/Processes/All-sessions
  // buttons staying on the RIGHT of that same row. ≥lg restores the original
  // two-column grid: wrapping 208px pills + a right-aligned action column.
  const actions = (
    <>
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
          className="flex shrink-0 items-center gap-0.5 px-2 py-1 rounded-md text-xs text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
        >
          <span aria-hidden>⌨️</span>
          <Plus className="w-3 h-3" />
        </button>
      )}

      {terminalAvailable && (
        <button
          onClick={goProcesses}
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
        <span className="hidden lg:inline">All sessions</span>
      </button>
    </>
  );

  return (
    <div className="flex items-center gap-1 lg:grid lg:grid-cols-[minmax(0,1fr)_auto] lg:gap-2 lg:items-start px-2 pt-2 bg-card border-b border-border min-w-0 w-full">
      <div className="min-w-0 flex-1 lg:flex-none">
        {/* Phones/tablets: session tabs → dropdown, buttons stay on the right
            of the same row (the actions cluster below is shrink-0). */}
        <div className="lg:hidden flex items-center gap-1 py-1.5">
          <MobileTabDropdown
            entries={tabEntries}
            onActivate={activateEntry}
            onRequestClose={requestCloseEntry}
            editing={editing}
            editValue={editValue}
            onEditValueChange={setEditValue}
            onStartRename={startRename}
            onCommitRename={commitRename}
            onCancelRename={() => setEditing(null)}
          />
        </div>
        {/* Desktop ≥lg: the drag-and-drop pill strip. */}
        <div className="hidden lg:flex lg:flex-wrap lg:gap-x-0.5 lg:gap-y-1 items-stretch lg:items-start py-1.5">
          <DndContext sensors={dndSensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
            <SortableContext items={order} strategy={rectSortingStrategy}>
              {order.map(renderPill)}
            </SortableContext>
          </DndContext>
        </div>
      </div>

      <div className="shrink-0 flex flex-wrap justify-start lg:justify-end items-center gap-0.5 py-1.5">{actions}</div>

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
              <Button variant="ghost" onClick={cancelPendingClose} data-dialog-default-action>
                Cancel
              </Button>
              <Button variant="destructive" onClick={confirmPendingClose}>Close tab</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
