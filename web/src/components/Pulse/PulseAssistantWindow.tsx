import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent, type PointerEvent } from "react";
import { DndContext, PointerSensor, useDndMonitor, useDraggable, useSensor, useSensors } from "@dnd-kit/core";
import { CSS } from "@dnd-kit/utilities";
import { ChevronUp, GripVertical, History, Maximize2, Minimize2, Minus, Plus, MessageSquare, Settings2, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { api } from "../../api/client";
import { useChat } from "../../hooks/useChat";
import { useTurnWatchdogAll } from "../../hooks/useTurnWatchdog";
import ChatPanel from "../Chat/ChatPanel";
import ChatInput from "../Chat/ChatInput";
import PermissionDialog from "../Chat/PermissionDialog";
import QuestionDialog from "../Chat/QuestionDialog";
import ModelDialog from "../Layout/ModelDialog";
import { OPEN_PULSE_ASSISTANT_SETTINGS_EVENT, shortModelName } from "../../lib/pulseAssistant";
import { cn } from "../../lib/utils";
import type { PulseAssistantInfo, PulseChatSummary } from "../../api/types";
import { formatAgo } from "./PulseCard";
import {
  ASSISTANT_BAR_HEIGHT,
  ASSISTANT_EDGE,
  ASSISTANT_MIN_WIDTH,
  clampRect,
  defaultRect,
  isNarrowViewport,
  toggleAssistantWindow,
  usePulseAssistantPrefs,
  type AssistantRect,
  type Viewport,
} from "./pulseAssistantPrefs";
import { isMacPlatform } from "../../lib/platform";

/**
 * PulseAssistantWindow — the Pulse assistant's chat, docked to the right of the
 * dashboard.
 *
 * It reuses the real chat surface rather than rendering a second transcript or
 * a second composer: `ChatPanel` for the transcript and `ChatInput` for sending
 * (ChatInput's own `useChat` does the send, so the turn is marked streaming the
 * same way as in any tab).
 *
 * HOW THE SESSION IS TRACKED FOR SSE. The router (lib/sessionEvents.ts
 * `sessionIsTracked`) applies a session's frames when it has an open tab OR
 * "a slice already exists". The assistant has no tab and must not get one (it
 * would land in a project's tab list and the Sessions tab bar), so it is
 * tracked through the second clause: mounting `ChatPanel` for the id hydrates
 * the session's chat slice, and sending dispatches SET_STREAMING, which creates
 * it too. No store change is needed. Two things a tab would have given it are
 * restored explicitly instead: the stall watchdog (below), and host resolution
 * (`resolveSessionHost` returns undefined for assistant ids, so the draft-tab
 * fallback cannot route it to a remote active project).
 * Known limit: SessionTabSync's reconnect reconcile only walks open tabs, so a
 * turn that finishes during an SSE outage is repaired by the watchdog, not
 * instantly.
 *
 * Slash commands: ChatInput hands a leading `/` to its parent's
 * `onSlashCommand`. No such prop is passed here, so everything goes to the
 * model through `sendMessage`; local instant commands do not apply to a
 * model-side helper.
 */

/**
 * The app-level toggle in the top bar (TopTabs), before the port-map and sync
 * widgets. Its title names the shortcut the platform actually uses. The window is
 * global, so this button works from every view.
 */
export function AssistantToggleButton() {
  const { open } = usePulseAssistantPrefs();
  const shortcut = isMacPlatform() ? "⌘⇧A" : "Ctrl+Shift+A";
  return (
    <Button
      variant={open ? "secondary" : "ghost"}
      size="sm"
      className="h-8 w-8 p-0 shrink-0"
      aria-label="Toggle assistant"
      aria-pressed={open}
      title={`Toggle assistant (${shortcut})`}
      onClick={toggleAssistantWindow}
    >
      <MessageSquare className="w-4 h-4" aria-hidden />
    </Button>
  );
}

/** `a` toggles the drawer, unless a key is being typed into a field or dialog. */
export function usePulseAssistantHotkey(toggle: () => void): void {
  const toggleRef = useRef(toggle);
  toggleRef.current = toggle;
  useEffect(() => {
    const onKeyDown = (e: globalThis.KeyboardEvent) => {
      if (e.key !== "a" || e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
      const target = e.target;
      if (
        target instanceof Element &&
        target.closest('input, textarea, select, [contenteditable="true"], [role="dialog"]')
      ) {
        return;
      }
      e.preventDefault();
      toggleRef.current();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

const DRAG_ID = "pulse-assistant-window";
const KEYBOARD_RESIZE_STEP = 16;

/** The window's viewport, kept current on resize. */
function useViewport(): Viewport {
  const [vp, setVp] = useState<Viewport>(() => ({ width: window.innerWidth, height: window.innerHeight }));
  useEffect(() => {
    const onResize = () => setVp({ width: window.innerWidth, height: window.innerHeight });
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  return vp;
}

/**
 * PulseAssistantWindow — the Pulse assistant's chat, in a floating window at app
 * level, so it stays available on every view.
 *
 * Modes: normal (floating, draggable by its grip, resizable from the corner),
 * minimised (only the header bar stays), maximised (fills the app inside a
 * margin). Below ASSISTANT_NARROW_BREAKPOINT it is an inset sheet and neither
 * drags nor resizes. The window keeps ONE DOM tree across modes: minimising only
 * hides the body, so the composer draft, the hydrated asks and the chat's scoped
 * dialog surface all survive. Unmounting would drop them.
 *
 * It reuses the real chat surface rather than rendering a second transcript or
 * a second composer: `ChatPanel` for the transcript and `ChatInput` for sending
 * (ChatInput's own `useChat` does the send, so the turn is marked streaming the
 * same way as in any tab).
 *
 * A pending ask while the window is minimised restores it, so a tool waiting on
 * approval is never blocked behind a hidden dialog. The bar also shows
 * "Needs approval" for an ask raised while the window is minimised by hand.
 *
 * HOW THE SESSION IS TRACKED FOR SSE. The router (lib/sessionEvents.ts
 * `sessionIsTracked`) applies a session's frames when it has an open tab OR
 * "a slice already exists". The assistant has no tab and must not get one (it
 * would land in a project's tab list and the Sessions tab bar), so it is
 * tracked through the second clause: mounting `ChatPanel` for the id hydrates
 * the session's chat slice, and sending dispatches SET_STREAMING, which creates
 * it too. No store change is needed. Two things a tab would have given it are
 * restored explicitly instead: the stall watchdog (below), and host resolution
 * (`resolveSessionHost` returns undefined for assistant ids, so the draft-tab
 * fallback cannot route it to a remote active project).
 * Known limit: SessionTabSync's reconnect reconcile only walks open tabs, so a
 * turn that finishes during an SSE outage is repaired by the watchdog, not
 * instantly.
 *
 * Slash commands: ChatInput hands a leading `/` to its parent's
 * `onSlashCommand`. No such prop is passed here, so everything goes to the
 * model through `sendMessage`; local instant commands do not apply to a
 * model-side helper.
 */
export function PulseAssistantWindow() {
  const { open } = usePulseAssistantPrefs();
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));
  if (!open) return null;
  return (
    <DndContext sensors={sensors}>
      <AssistantWindowFrame />
    </DndContext>
  );
}

function AssistantWindowFrame() {
  const prefs = usePulseAssistantPrefs();
  const vp = useViewport();
  const narrow = isNarrowViewport(vp);
  const modeRef = useRef(prefs.mode);
  modeRef.current = prefs.mode;

  const [info, setInfo] = useState<PulseAssistantInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [modelError, setModelError] = useState<string | null>(null);
  const [chatError, setChatError] = useState<string | null>(null);
  const [chatBusy, setChatBusy] = useState(false);
  const [askPending, setAskPending] = useState(false);
  // State, not a ref: the ask dialogs are confined to this element and must
  // re-render once it exists.
  const [surface, setSurface] = useState<HTMLElement | null>(null);
  // Live rectangle while a corner resize is in progress. It is committed to the
  // store, and so to localStorage, once on pointer up, not on every move.
  const [draft, setDraft] = useState<AssistantRect | null>(null);
  const draftRef = useRef<AssistantRect | null>(null);
  const resizeStart = useRef<{ x: number; y: number; rect: AssistantRect } | null>(null);

  const rect = clampRect(draft ?? prefs.rect ?? defaultRect(vp), vp);
  const dragAllowed = !narrow && prefs.mode !== "maximized";
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({
    id: DRAG_ID,
    disabled: !dragAllowed,
  });
  useDndMonitor({
    onDragEnd: ({ delta }) => {
      if (!dragAllowed) return;
      prefs.setRect(clampRect({ ...rect, x: rect.x + delta.x, y: rect.y + delta.y }, vp));
    },
  });

  // An ask that arrives while the window is minimised brings it back: the turn
  // is waiting on a dialog that only the open window can show.
  useEffect(() => {
    if (askPending && modeRef.current === "minimized") prefs.setMode("normal");
  }, [askPending, prefs.setMode]);

  const load = useCallback(async () => {
    try {
      return await api.getPulseAssistant();
    } catch (err) {
      console.error("PulseAssistantWindow: starting the assistant failed:", err);
      throw err;
    }
  }, []);

  useEffect(() => {
    // Generation guard: Retry or unmount must drop a superseded response.
    let stale = false;
    setError(null);
    setInfo(null);
    load()
      .then((res) => {
        if (!stale) setInfo(res);
      })
      .catch((err) => {
        if (!stale) setError(errorText(err));
      });
    return () => {
      stale = true;
    };
  }, [load, attempt]);

  const sessionId = info ? info.session_id : null;

  // The stall watchdog normally covers open tabs only (App passes tab ids).
  // The assistant has no tab, so it is registered with its own instance.
  const watched = useMemo(() => new Set(sessionId ? [sessionId] : []), [sessionId]);
  useTurnWatchdogAll(watched);

  const pickModel = async (model: string) => {
    setModelError(null);
    try {
      await api.setPulseModel(model);
      // The effective model (slot, else server default) is the server's call,
      // so re-read it rather than echoing the pick; this also covers a clear.
      setInfo(await load());
    } catch (err) {
      console.error("PulseAssistantWindow: changing the assistant model failed:", err);
      setModelError(`Changing the model failed: ${errorText(err)}`);
    }
  };

  // Starting a new chat or switching back is refused by the server (409) while
  // the current chat is mid-turn; the reason is shown in the window.
  const runChatAction = async (action: () => Promise<PulseAssistantInfo>, failure: string) => {
    setChatError(null);
    setChatBusy(true);
    try {
      setInfo(await action());
      return true;
    } catch (err) {
      console.error(`PulseAssistantWindow: ${failure}:`, err);
      setChatError(`${failure}: ${errorText(err)}`);
      return false;
    } finally {
      setChatBusy(false);
    }
  };
  const startNewChat = () => runChatAction(() => api.newPulseChat(), "Starting a new chat failed");
  const switchChat = (id: string) => {
    if (id === sessionId) return Promise.resolve(true);
    return runChatAction(() => api.selectPulseChat(id), "Switching chat failed");
  };

  // ── Corner resize ──
  const setDraftRect = (next: AssistantRect | null) => {
    draftRef.current = next;
    setDraft(next);
  };
  const onResizePointerDown = (e: PointerEvent<HTMLDivElement>) => {
    resizeStart.current = { x: e.clientX, y: e.clientY, rect };
    e.currentTarget.setPointerCapture(e.pointerId);
  };
  const onResizePointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const start = resizeStart.current;
    if (!start) return;
    setDraftRect(
      clampRect(
        {
          ...start.rect,
          width: start.rect.width + (e.clientX - start.x),
          height: start.rect.height + (e.clientY - start.y),
        },
        vp,
      ),
    );
  };
  const onResizePointerUp = (e: PointerEvent<HTMLDivElement>) => {
    resizeStart.current = null;
    e.currentTarget.releasePointerCapture(e.pointerId);
    if (draftRef.current) prefs.setRect(draftRef.current);
    setDraftRect(null);
  };
  const onResizeKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = KEYBOARD_RESIZE_STEP;
    const moves: Record<string, [number, number]> = {
      ArrowLeft: [-step, 0],
      ArrowRight: [step, 0],
      ArrowUp: [0, -step],
      ArrowDown: [0, step],
    };
    const move = moves[e.key];
    if (!move) return;
    e.preventDefault();
    prefs.setRect(clampRect({ ...rect, width: rect.width + move[0], height: rect.height + move[1] }, vp));
  };

  const minimised = prefs.mode === "minimized";
  const maximised = prefs.mode === "maximized";
  const inset = `${ASSISTANT_EDGE}px`;
  let frame: CSSProperties;
  if (narrow || maximised) {
    frame =
      minimised && narrow
        ? { left: inset, right: inset, bottom: inset, height: ASSISTANT_BAR_HEIGHT }
        : { left: inset, right: inset, top: inset, bottom: inset };
  } else if (minimised) {
    frame = { left: rect.x, top: rect.y, width: rect.width, height: ASSISTANT_BAR_HEIGHT };
  } else {
    frame = { left: rect.x, top: rect.y, width: rect.width, height: rect.height };
  }
  if (transform) frame.transform = CSS.Translate.toString(transform);

  return (
    <section
      ref={setNodeRef}
      role="region"
      aria-label="Assistant"
      data-testid="pulse-assistant-window"
      data-mode={prefs.mode}
      style={frame}
      className={cn(
        "fixed z-40 flex min-w-0 flex-col overflow-hidden rounded-md border border-border bg-background shadow-lg",
        isDragging && "opacity-90",
      )}
    >
      <header className="flex shrink-0 items-center gap-1 border-b border-border px-2 py-1.5">
        {dragAllowed && (
          <div
            {...listeners}
            {...attributes}
            aria-label="Move assistant"
            data-testid="pulse-assistant-grip"
            className="flex h-7 w-5 shrink-0 cursor-grab items-center justify-center text-muted-foreground hover:text-foreground active:cursor-grabbing"
          >
            <GripVertical className="h-4 w-4" aria-hidden />
          </div>
        )}
        <h2 className="shrink-0 text-sm font-medium">Assistant</h2>
        {info && (
          <Button
            size="sm"
            variant="ghost"
            className="min-w-0 max-w-[40%] justify-start font-mono text-xs"
            title={info.model ? `${info.model} — change the assistant model` : "Change the assistant model"}
            onClick={() => setModelDialogOpen(true)}
          >
            <span className="truncate">{shortModelName(info.model)}</span>
          </Button>
        )}
        {minimised && askPending && (
          <Badge variant="secondary" role="status" data-testid="pulse-assistant-ask-badge" className="shrink-0">
            Needs approval
          </Badge>
        )}
        <div className="ml-auto flex shrink-0 items-center gap-0.5">
          {info && (
            <>
              <Button
                size="icon"
                variant="ghost"
                aria-label="New chat"
                title="New chat"
                disabled={chatBusy}
                onClick={() => void startNewChat()}
              >
                <Plus className="h-4 w-4" aria-hidden />
              </Button>
              <ChatHistory currentId={info.session_id} disabled={chatBusy} onPick={(id) => switchChat(id)} />
            </>
          )}
          <Button
            size="icon"
            variant="ghost"
            aria-label="Assistant settings"
            title="Assistant settings"
            onClick={() => window.dispatchEvent(new CustomEvent(OPEN_PULSE_ASSISTANT_SETTINGS_EVENT))}
          >
            <Settings2 className="h-4 w-4" aria-hidden />
          </Button>
          {minimised ? (
            <Button size="icon" variant="ghost" aria-label="Restore assistant" title="Restore" onClick={() => prefs.setMode("normal")}>
              <ChevronUp className="h-4 w-4" aria-hidden />
            </Button>
          ) : (
            <Button size="icon" variant="ghost" aria-label="Minimise assistant" title="Minimise" onClick={() => prefs.setMode("minimized")}>
              <Minus className="h-4 w-4" aria-hidden />
            </Button>
          )}
          {maximised ? (
            <Button size="icon" variant="ghost" aria-label="Restore size" title="Restore size" onClick={() => prefs.setMode("normal")}>
              <Minimize2 className="h-4 w-4" aria-hidden />
            </Button>
          ) : (
            <Button size="icon" variant="ghost" aria-label="Maximise assistant" title="Maximise" onClick={() => prefs.setMode("maximized")}>
              <Maximize2 className="h-4 w-4" aria-hidden />
            </Button>
          )}
          <Button size="icon" variant="ghost" aria-label="Close assistant" onClick={() => prefs.setOpen(false)}>
            <X className="h-4 w-4" aria-hidden />
          </Button>
        </div>
      </header>

      <div hidden={minimised} className="flex min-h-0 flex-1 flex-col">
        {modelError && (
          <p role="alert" data-testid="pulse-assistant-model-error" className="px-3 py-1 text-xs text-destructive">
            {modelError}
          </p>
        )}
        {chatError && (
          <p role="alert" data-testid="pulse-assistant-chat-error" className="px-3 py-1 text-xs text-destructive">
            {chatError}
          </p>
        )}

        {error ? (
          <div role="alert" className="flex flex-col items-start gap-2 p-3 text-sm text-destructive">
            <span data-testid="pulse-assistant-error">Couldn’t start the assistant: {error}</span>
            <Button size="sm" variant="outline" onClick={() => setAttempt((n) => n + 1)}>
              Retry
            </Button>
          </div>
        ) : sessionId === null ? (
          <p className="p-3 text-sm text-muted-foreground">Starting assistant…</p>
        ) : (
          // Keyed by id: a switched chat must not keep the previous chat's
          // dialogs, draft or hydrated asks.
          <AssistantChat
            key={sessionId}
            sessionId={sessionId}
            surface={surface}
            setSurface={setSurface}
            onAskChange={setAskPending}
          />
        )}
      </div>

      {dragAllowed && !minimised && (
        <div
          role="separator"
          aria-orientation="horizontal"
          aria-label="Resize assistant"
          aria-valuemin={ASSISTANT_MIN_WIDTH}
          aria-valuenow={rect.width}
          tabIndex={0}
          data-testid="pulse-assistant-resize"
          onPointerDown={onResizePointerDown}
          onPointerMove={onResizePointerMove}
          onPointerUp={onResizePointerUp}
          onKeyDown={onResizeKeyDown}
          className="absolute bottom-0 right-0 z-10 h-4 w-4 cursor-nwse-resize rounded-tl hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
        />
      )}

      <ModelDialog
        open={modelDialogOpen}
        onClose={() => setModelDialogOpen(false)}
        purpose="pulse"
        currentValues={{ pulse: info ? info.model : "" }}
        onPick={(_purpose, modelId) => void pickModel(modelId)}
      />
    </section>
  );
}

const CHAT_PAGE_SIZE = 20;

/**
 * The chat history popover: earlier Pulse assistant chats, newest first, paged.
 * Loads the first page each time it opens so the list reflects the latest turns.
 */
function ChatHistory({
  currentId,
  disabled,
  onPick,
}: {
  currentId: string;
  disabled: boolean;
  onPick: (id: string) => Promise<boolean>;
}) {
  const [open, setOpen] = useState(false);
  const [chats, setChats] = useState<PulseChatSummary[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadPage = useCallback(async (offset: number) => {
    setLoading(true);
    setError(null);
    try {
      const page = await api.listPulseChats(offset, CHAT_PAGE_SIZE);
      setTotal(page.total);
      setChats((prev) => (offset === 0 ? page.chats : [...prev, ...page.chats]));
    } catch (err) {
      console.error("PulseAssistantWindow: listing chats failed:", err);
      setError(`Listing chats failed: ${errorText(err)}`);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) void loadPage(0);
  }, [open, loadPage]);

  const pick = async (id: string) => {
    if (await onPick(id)) setOpen(false);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          size="icon"
          variant="ghost"
          aria-label="Chat history"
          title="Chat history"
          disabled={disabled}
        >
          <History className="h-4 w-4" aria-hidden />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-64 p-1" data-testid="pulse-assistant-history">
        {error && (
          <p role="alert" className="px-2 py-1 text-xs text-destructive">
            {error}
          </p>
        )}
        <ul className="flex max-h-72 flex-col overflow-y-auto">
          {chats.map((chat) => {
            const isCurrent = chat.session_id === currentId;
            return (
              <li key={chat.session_id}>
                <button
                  type="button"
                  aria-current={isCurrent ? "true" : undefined}
                  disabled={disabled}
                  onClick={() => void pick(chat.session_id)}
                  className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
                >
                  <span className="min-w-0 flex-1 truncate">{chat.title || "Pulse assistant"}</span>
                  <span className="shrink-0 tabular-nums text-muted-foreground">
                    {isCurrent ? "current" : formatAgo(chat.updated_at, Date.now())}
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
        {!loading && chats.length === 0 && !error && (
          <p className="px-2 py-1 text-xs text-muted-foreground">No earlier chats.</p>
        )}
        {chats.length < total && (
          <Button
            size="sm"
            variant="ghost"
            className="mt-1 w-full"
            disabled={loading}
            onClick={() => void loadPage(chats.length)}
          >
            {loading ? "Loading…" : "Load more"}
          </Button>
        )}
      </PopoverContent>
    </Popover>
  );
}

function AssistantChat({
  sessionId,
  surface,
  setSurface,
  onAskChange,
}: {
  sessionId: string;
  surface: HTMLElement | null;
  setSurface: (el: HTMLElement | null) => void;
  onAskChange: (pending: boolean) => void;
}) {
  // Asks raised by the assistant's own tools. App renders these only for the
  // active tab, so without them here a paused turn would wait on a dialog
  // nobody can see. Confined to the chat surface like every session ask.
  const {
    pendingPermission,
    pendingQuestion,
    hiddenQuestionRequestId,
    askContext,
    resolvePermission,
    submitQuestionAnswers,
    cancelQuestion,
    hideQuestion,
    hydratePendingAsks,
  } = useChat(sessionId);

  // An ask raised before this drawer mounted (a reload mid-turn, or the drawer
  // opened later) was never delivered to the slice. Recover it from the live
  // session state, the same path useChat takes after a 409, instead of waiting
  // for the next send to trip it.
  useEffect(() => {
    void hydratePendingAsks();
  }, [hydratePendingAsks]);

  // Reported up so the window can restore itself, or badge its bar, while it is
  // minimised. A hidden dialog would otherwise hold the turn with nothing shown.
  const askPending = pendingPermission !== null || pendingQuestion !== null;
  useEffect(() => {
    onAskChange(askPending);
  }, [askPending, onAskChange]);

  return (
    <div ref={setSurface} data-testid="pulse-assistant-surface" className="relative flex min-h-0 flex-1 flex-col">
      <div className="relative min-h-0 flex-1 overflow-hidden">
        <ChatPanel sessionId={sessionId} host={undefined} />
      </div>
      <ChatInput sessionTabId={sessionId} isActive quickActions={false} />

      {surface && pendingPermission && (
        <PermissionDialog
          open={true}
          container={surface}
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
          untrustedContent={pendingPermission.untrusted_content}
          untrustedSource={pendingPermission.untrusted_source}
          untrustedSummary={pendingPermission.untrusted_summary}
          untrustedScores={pendingPermission.untrusted_scores}
          untrustedFailure={pendingPermission.untrusted_failure}
          agentName={pendingPermission.agent_name}
          context={askContext}
          requestId={pendingPermission.request_id}
          onDecide={resolvePermission}
        />
      )}
      {surface && pendingQuestion && hiddenQuestionRequestId !== pendingQuestion.request_id && (
        <QuestionDialog
          key={pendingQuestion.request_id}
          open={true}
          container={surface}
          requestId={pendingQuestion.request_id}
          questions={pendingQuestion.questions}
          onSubmit={submitQuestionAnswers}
          onHide={hideQuestion}
          onCancel={cancelQuestion}
          context={askContext}
        />
      )}
    </div>
  );
}
