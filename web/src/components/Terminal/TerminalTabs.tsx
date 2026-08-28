import { useEffect, useImperativeHandle, useRef, useState, forwardRef, type RefObject } from "react";
import { Plus, X, Pencil, Gauge } from "lucide-react";
import TerminalPanel from "./TerminalPanel";
import ProcessesPanel from "./ProcessesPanel";
import { useTerminalConfig } from "@/hooks/useTerminalConfig";
import { loadProjectTerminals, saveProjectTerminals } from "./terminalPersistence";
import { ContextMenu } from "@/components/Layout/ContextMenu";
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

const PROCESSES_TAB_ID = "processes";

interface TerminalInstance {
  id: string;
  title: string;
}

let nextTerminalSeq = 1;

function newTerminal(): TerminalInstance {
  const n = nextTerminalSeq++;
  return { id: `term-${n}-${Date.now()}`, title: `Terminal ${n}` };
}

/** Keeps the module-level title counter ahead of any restored terminal
 *  numbers, so a newly opened terminal after a restore doesn't reuse a
 *  "Terminal N" title that's already on screen. */
function bumpSeqPast(titles: string[]) {
  for (const title of titles) {
    const m = /^Terminal (\d+)$/.exec(title);
    if (m) nextTerminalSeq = Math.max(nextTerminalSeq, Number(m[1]) + 1);
  }
}

/**
 * Tab strip over N independent terminals. Each tab owns a TerminalPanel, which
 * owns one WebSocket and therefore one pty process on the server; closing a
 * tab unmounts its panel, which closes the socket, which kills the pty.
 *
 * Inactive tabs stay mounted (hidden with `display: none`) so background
 * shells keep running and keep their scrollback. State is local to this
 * component on purpose — it is scoped to the *project* (not the session tab)
 * so switching chat sessions within the same project never hides or kills the
 * pty. This matches the server's project-scoped terminal model.
 */
export interface TerminalTabsHandle {
  openTerminal: () => void;
  /** Close the active terminal instance. Returns false when there is none,
   *  so the caller (Cmd/Ctrl+W) can fall through to closing the session tab. */
  closeActiveTerminal: () => boolean;
}

/** One draggable tab. Drag activation needs a few pixels of movement
 *  (see `sensors` below) so a plain click still activates/renames the tab
 *  instead of being swallowed as a zero-distance drag. */
function SortableTerminalTab({
  t,
  isActive,
  isRenaming,
  renameValue,
  renameInputRef,
  onActivate,
  onStartRename,
  onRenameChange,
  onCommitRename,
  onCancelRename,
  onClose,
}: {
  t: TerminalInstance;
  isActive: boolean;
  isRenaming: boolean;
  renameValue: string;
  renameInputRef: RefObject<HTMLInputElement>;
  onActivate: () => void;
  onStartRename: () => void;
  onRenameChange: (v: string) => void;
  onCommitRename: () => void;
  onCancelRename: () => void;
  onClose: () => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: t.id });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  return (
    <ContextMenu
      items={[{ label: "Rename", icon: <Pencil className="w-3.5 h-3.5" />, onClick: onStartRename }]}
    >
      <div
        ref={setNodeRef}
        style={style}
        {...attributes}
        {...listeners}
        onAuxClick={(e) => {
          if (e.button === 1) onClose();
        }}
        className={`flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-sm transition-colors touch-none ${
          isActive
            ? "bg-zinc-700 text-white"
            : "text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
        }`}
      >
        {isRenaming ? (
          <input
            ref={renameInputRef}
            value={renameValue}
            onChange={(e) => onRenameChange(e.target.value)}
            onBlur={onCommitRename}
            onKeyDown={(e) => {
              if (e.key === "Enter") onCommitRename();
              if (e.key === "Escape") onCancelRename();
            }}
            className="w-24 rounded bg-zinc-800 px-1 text-sm text-white outline-none"
          />
        ) : (
          <button
            type="button"
            onClick={onActivate}
            onDoubleClick={onStartRename}
            title="Double-click to rename, drag to reorder"
            className="whitespace-nowrap"
          >
            {t.title}
          </button>
        )}
        <button
          type="button"
          onClick={onClose}
          aria-label={`Close ${t.title}`}
          title={`Close ${t.title}`}
          className="rounded p-0.5 text-zinc-500 hover:bg-zinc-600 hover:text-white"
        >
          <X className="h-3 w-3" />
        </button>
      </div>
    </ContextMenu>
  );
}

const TerminalTabs = forwardRef<TerminalTabsHandle, { active: boolean; projectPath: string }>(
  function TerminalTabs({ active, projectPath }, ref) {
    const { available, loading, error, scrollbackLines, fontFamily, fontSize } = useTerminalConfig();
    const [terminals, setTerminals] = useState<TerminalInstance[]>([]);
    const [activeId, setActiveId] = useState<string>("");
    const [renamingId, setRenamingId] = useState<string | null>(null);
    const [renameValue, setRenameValue] = useState("");
    const renameInputRef = useRef<HTMLInputElement>(null);
    // This panel is force-mounted for every project (hidden with `display: none`
    // when not active) so the first shell is spawned only once the Terminal
    // sub-tab is actually opened — otherwise every project would start a pty on
    // page load. Project-scoped so switching chat sessions never kills the pty.
    const autoOpened = useRef(false);
    // Set once a restore (or its no-op fallback) has run, so the persistence
    // effect below never overwrites storage with an empty in-flight state.
    const restored = useRef(false);
    // The restore effect's setTerminals/setActiveId calls don't take effect
    // until the next render, but the persistence effect below runs in the
    // same commit with the pre-restore (empty) `terminals` closure — so skip
    // exactly one save right after restoring to avoid clobbering storage with
    // that stale empty state before the real state renders.
    const skipNextSave = useRef(false);

    // Reorder activation needs a few pixels of movement so a plain click
    // still activates/renames a tab (mirrors ProjectSidebar's drag setup).
    const dndSensors = useSensors(
      useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
      useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
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

    const handleTabDragEnd = (event: DragEndEvent) => {
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      setTerminals((prev) => {
        const oldIndex = prev.findIndex((t) => t.id === active.id);
        const newIndex = prev.findIndex((t) => t.id === over.id);
        if (oldIndex === -1 || newIndex === -1) return prev;
        return arrayMove(prev, oldIndex, newIndex);
      });
    };

    const openTerminal = () => {
      const term = newTerminal();
      setTerminals((prev) => [...prev, term]);
      setActiveId(term.id);
    };

    useImperativeHandle(ref, () => ({
      openTerminal,
      closeActiveTerminal: () => {
        if (!activeId || !terminals.some((t) => t.id === activeId)) return false;
        closeTerminal(activeId);
        return true;
      },
    }));

    // Restore this project's previously open terminal tabs (fresh shells, prior
    // scrollback replayed by TerminalPanel) instead of always starting with one
    // new terminal. Runs once the terminal sub-tab is actually opened, matching
    // the original lazy-open behaviour. Project-scoped so switching chat
    // sessions within the same project never hides or kills the pty.
    useEffect(() => {
      if (!active || !available || autoOpened.current) return;
      autoOpened.current = true;
      const saved = loadProjectTerminals(projectPath);
      if (saved) {
        skipNextSave.current = true;
        bumpSeqPast(saved.terminals.map((t) => t.title));
        setTerminals(saved.terminals);
        setActiveId(
          saved.activeId && (saved.activeId === PROCESSES_TAB_ID || saved.terminals.some((t) => t.id === saved.activeId))
            ? saved.activeId
            : saved.terminals[saved.terminals.length - 1].id,
        );
        restored.current = true;
        return;
      }
      restored.current = true;
      openTerminal();
    }, [active, available, projectPath]);

    useEffect(() => {
      if (!restored.current) return;
      if (skipNextSave.current) {
        skipNextSave.current = false;
        return;
      }
      saveProjectTerminals(projectPath, terminals, activeId);
    }, [projectPath, terminals, activeId]);

    // Keep this project's terminal list in sync across same-origin windows.
    // When another tab opens or closes a terminal, `saveProjectTerminals`
    // writes to localStorage — the other tab receives a `storage` event.
    const terminalsRef = useRef(terminals);
    const activeIdRef = useRef(activeId);
    useEffect(() => {
      terminalsRef.current = terminals;
      activeIdRef.current = activeId;
    }, [terminals, activeId]);
    useEffect(() => {
      const handler = (e: StorageEvent) => {
        if (e.key !== "ocode.ui.terminals.project.v1") return;
        if (!restored.current) return;
        const saved = loadProjectTerminals(projectPath);
        const curTerminals = terminalsRef.current;
        const curActive = activeIdRef.current;
        if (!saved) {
          if (curTerminals.length !== 0) {
            skipNextSave.current = true;
            setTerminals([]);
            setActiveId("");
          }
          return;
        }
        const same =
          saved.terminals.length === curTerminals.length &&
          saved.terminals.every((t, i) => t.id === curTerminals[i]?.id && t.title === curTerminals[i]?.title) &&
          saved.activeId === curActive;
        if (same) return;
        skipNextSave.current = true;
        bumpSeqPast(saved.terminals.map((t) => t.title));
        setTerminals(saved.terminals);
        const nextActive =
          saved.activeId && (saved.activeId === PROCESSES_TAB_ID || saved.terminals.some((t) => t.id === saved.activeId))
            ? saved.activeId
            : saved.terminals[saved.terminals.length - 1]?.id ?? "";
        setActiveId(nextActive);
      };
      window.addEventListener("storage", handler);
      return () => window.removeEventListener("storage", handler);
    }, [projectPath]);

    const startRename = (t: TerminalInstance) => {
      setRenameValue(t.title);
      setRenamingId(t.id);
    };

    useEffect(() => {
      if (renamingId) {
        renameInputRef.current?.focus();
        renameInputRef.current?.select();
      }
    }, [renamingId]);

    const commitRename = () => {
      const id = renamingId;
      if (!id) return;
      const trimmed = renameValue.trim();
      setRenamingId(null);
      if (!trimmed) return;
      setTerminals((prev) => prev.map((t) => (t.id === id ? { ...t, title: trimmed } : t)));
    };

    const cancelRename = () => setRenamingId(null);

    const closeTerminal = (id: string) => {
      // Buffer cleanup happens via GC in the persistence effect once this id
      // drops out of `terminals` — not here, since TerminalPanel's own unmount
      // (which fires after this state update) re-saves its buffer one last
      // time, which would race an eager clear.
      setTerminals((prev) => {
        const next = prev.filter((t) => t.id !== id);
        if (id === activeId) {
          setActiveId(next.length > 0 ? next[next.length - 1].id : "");
        }
        return next;
      });
    };

    if (loading || (available && scrollbackLines <= 0)) {
      return <div className="p-4 text-sm text-zinc-500">Checking terminal availability…</div>;
    }

    if (error) {
      return <div className="p-4 text-sm text-red-400">Failed to read terminal setting: {error}</div>;
    }

    if (!available) {
      return (
        <div className="p-4 text-sm text-zinc-400">
          The interactive terminal is unavailable on this server: it requires server
          authentication or a loopback bind address.
        </div>
      );
    }

    return (
      <div className="flex h-full flex-col bg-zinc-900">
        <div
          ref={scrollRef}
          onWheel={handleWheel}
          className="flex h-9 shrink-0 items-center gap-1 overflow-x-auto overflow-y-hidden scrollbar-hide flex-nowrap min-w-0 w-full touch-pan-x overscroll-x-contain border-b border-zinc-700 bg-zinc-900 px-2"
          style={{ WebkitOverflowScrolling: "touch" } as React.CSSProperties}
        >
          <DndContext sensors={dndSensors} collisionDetection={closestCenter} onDragEnd={handleTabDragEnd}>
            <SortableContext items={terminals.map((t) => t.id)} strategy={horizontalListSortingStrategy}>
              {terminals.map((t) => (
                <SortableTerminalTab
                  key={t.id}
                  t={t}
                  isActive={t.id === activeId}
                  isRenaming={renamingId === t.id}
                  renameValue={renameValue}
                  renameInputRef={renameInputRef}
                  onActivate={() => setActiveId(t.id)}
                  onStartRename={() => startRename(t)}
                  onRenameChange={setRenameValue}
                  onCommitRename={commitRename}
                  onCancelRename={cancelRename}
                  onClose={() => closeTerminal(t.id)}
                />
              ))}
            </SortableContext>
          </DndContext>
          <button
            type="button"
            onClick={() => setActiveId(PROCESSES_TAB_ID)}
            className={`flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-sm transition-colors ${
              activeId === PROCESSES_TAB_ID
                ? "bg-zinc-700 text-white"
                : "text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
            }`}
          >
            <Gauge className="h-4 w-4" />
            Processes
          </button>
          <button
            type="button"
            onClick={openTerminal}
            aria-label="New terminal"
            title="New terminal"
            className="shrink-0 rounded p-1 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200"
          >
            <Plus className="h-4 w-4" />
          </button>
        </div>

        <div className="relative flex-1">
          {/* Process telemetry is mounted only for the active project; terminal
              panels below remain mounted so their WebSockets/PTYs survive tab switches. */}
          {active && (
            <div className={activeId === PROCESSES_TAB_ID ? "absolute inset-0" : "absolute inset-0 hidden"}>
              <ProcessesPanel projectPath={projectPath} />
            </div>
          )}

          {/* TerminalPanels are always mounted to preserve WebSocket/PTY state;
              only the active tab is visible. Empty state shown when no terminals exist. */}
          {terminals.map((t) => (
            <div
              key={t.id}
              className={t.id === activeId ? "absolute inset-0" : "absolute inset-0 hidden"}
            >
              <TerminalPanel
                id={t.id}
                active={active && t.id === activeId}
                scrollbackLines={scrollbackLines}
                fontFamily={fontFamily}
                fontSize={fontSize}
                projectPath={projectPath}
              />
            </div>
          ))}
          {terminals.length === 0 && activeId !== PROCESSES_TAB_ID && (
            <div className="p-4 text-sm text-zinc-500">
              No terminals open. Use + to start one.
            </div>
          )}
        </div>
      </div>
    );
  },
);

export default TerminalTabs;
