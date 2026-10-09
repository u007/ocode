import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { Settings2, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { api } from "../../api/client";
import { useChat } from "../../hooks/useChat";
import { useTurnWatchdogAll } from "../../hooks/useTurnWatchdog";
import ChatPanel from "../Chat/ChatPanel";
import ChatInput from "../Chat/ChatInput";
import PermissionDialog from "../Chat/PermissionDialog";
import QuestionDialog from "../Chat/QuestionDialog";
import ModelDialog from "../Layout/ModelDialog";
import { OPEN_PULSE_ASSISTANT_SETTINGS_EVENT, shortModelName } from "../../lib/pulseAssistant";
import type { PulseAssistantInfo } from "../../api/types";

/**
 * PulseAssistantDrawer — the Pulse assistant's chat, docked to the right of the
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

const KEY_OPEN = "pulse.assistant.open";
const KEY_WIDTH = "pulse.assistant.width";
export const ASSISTANT_MIN_WIDTH = 320;
export const ASSISTANT_DEFAULT_WIDTH = 420;
/** Fraction of the dashboard the drawer may take at most. */
export const ASSISTANT_MAX_FRACTION = 0.6;
const KEYBOARD_RESIZE_STEP = 16;

function readPref(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch (err) {
    console.warn(`PulseAssistantDrawer: reading localStorage key ${key} failed:`, err);
    return null;
  }
}

function writePref(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch (err) {
    console.warn(`PulseAssistantDrawer: writing localStorage key ${key} failed:`, err);
  }
}

/**
 * Open state and width of the drawer.
 *
 * localStorage on purpose, not the pulse store: both are per-viewer
 * conveniences (a second window or device wants its own layout), nothing else
 * reads them, and they must not leak into per-project view state. A blocked or
 * throwing storage only costs the remembered layout, so reads and writes are
 * guarded and logged.
 */
export function usePulseAssistantPrefs() {
  const [open, setOpenState] = useState(() => readPref(KEY_OPEN) === "1");
  const [width, setWidthState] = useState(() => {
    const stored = Number(readPref(KEY_WIDTH));
    return Number.isFinite(stored) && stored >= ASSISTANT_MIN_WIDTH ? stored : ASSISTANT_DEFAULT_WIDTH;
  });
  const setOpen = useCallback((next: boolean) => {
    setOpenState(next);
    writePref(KEY_OPEN, next ? "1" : "0");
  }, []);
  const setWidth = useCallback((next: number) => {
    const clamped = Math.max(ASSISTANT_MIN_WIDTH, Math.round(next));
    setWidthState(clamped);
    writePref(KEY_WIDTH, String(clamped));
  }, []);
  return { open, setOpen, width, setWidth };
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

interface DrawerProps {
  width: number;
  onWidthChange: (width: number) => void;
  onClose: () => void;
}

export function PulseAssistantDrawer({ width, onWidthChange, onClose }: DrawerProps) {
  const [info, setInfo] = useState<PulseAssistantInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [modelError, setModelError] = useState<string | null>(null);
  // State, not a ref: the ask dialogs are confined to this element and must
  // re-render once it exists.
  const [surface, setSurface] = useState<HTMLElement | null>(null);
  const rootRef = useRef<HTMLElement | null>(null);

  const load = useCallback(async () => {
    try {
      return await api.getPulseAssistant();
    } catch (err) {
      console.error("PulseAssistantDrawer: starting the assistant failed:", err);
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
      console.error("PulseAssistantDrawer: changing the assistant model failed:", err);
      setModelError(`Changing the model failed: ${errorText(err)}`);
    }
  };

  // ── Resize ──
  const dragging = useRef(false);
  const onHandlePointerDown = (e: PointerEvent<HTMLDivElement>) => {
    dragging.current = true;
    e.currentTarget.setPointerCapture(e.pointerId);
  };
  const onHandlePointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (!dragging.current) return;
    const root = rootRef.current;
    const parent = root?.parentElement;
    if (!root || !parent) return;
    const right = root.getBoundingClientRect().right;
    const max = parent.getBoundingClientRect().width * ASSISTANT_MAX_FRACTION;
    onWidthChange(Math.min(Math.max(right - e.clientX, ASSISTANT_MIN_WIDTH), Math.max(max, ASSISTANT_MIN_WIDTH)));
  };
  const onHandlePointerUp = (e: PointerEvent<HTMLDivElement>) => {
    dragging.current = false;
    e.currentTarget.releasePointerCapture(e.pointerId);
  };
  const onHandleKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "ArrowLeft") onWidthChange(width + KEYBOARD_RESIZE_STEP);
    else if (e.key === "ArrowRight") onWidthChange(width - KEYBOARD_RESIZE_STEP);
    else return;
    e.preventDefault();
  };

  return (
    <aside
      ref={rootRef}
      aria-label="Assistant"
      data-testid="pulse-assistant-drawer"
      // maxWidth is the 60% ceiling; minWidth the floor. Both beat `width`.
      style={{ width, minWidth: ASSISTANT_MIN_WIDTH, maxWidth: `${ASSISTANT_MAX_FRACTION * 100}%` }}
      className="relative flex shrink-0 flex-col border-l border-border bg-background"
    >
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize assistant"
        aria-valuemin={ASSISTANT_MIN_WIDTH}
        aria-valuenow={width}
        tabIndex={0}
        data-testid="pulse-assistant-resize"
        onPointerDown={onHandlePointerDown}
        onPointerMove={onHandlePointerMove}
        onPointerUp={onHandlePointerUp}
        onKeyDown={onHandleKeyDown}
        className="absolute inset-y-0 -left-1 z-10 w-2 cursor-col-resize hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
      />

      <div className="flex shrink-0 items-center gap-2 border-b border-border px-3 py-2">
        <h2 className="text-sm font-medium">Assistant</h2>
        {info && (
          <Button
            size="sm"
            variant="ghost"
            className="min-w-0 max-w-[60%] justify-start font-mono text-xs"
            title={info.model ? `${info.model} — change the assistant model` : "Change the assistant model"}
            onClick={() => setModelDialogOpen(true)}
          >
            <span className="truncate">{shortModelName(info.model)}</span>
          </Button>
        )}
        <Button
          size="icon"
          variant="ghost"
          className="ml-auto"
          aria-label="Assistant settings"
          title="Assistant settings"
          onClick={() => window.dispatchEvent(new CustomEvent(OPEN_PULSE_ASSISTANT_SETTINGS_EVENT))}
        >
          <Settings2 className="h-4 w-4" aria-hidden />
        </Button>
        <Button size="icon" variant="ghost" aria-label="Close assistant" onClick={onClose}>
          <X className="h-4 w-4" aria-hidden />
        </Button>
      </div>

      {modelError && (
        <p role="alert" data-testid="pulse-assistant-model-error" className="px-3 py-1 text-xs text-destructive">
          {modelError}
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
        <AssistantChat sessionId={sessionId} surface={surface} setSurface={setSurface} />
      )}

      <ModelDialog
        open={modelDialogOpen}
        onClose={() => setModelDialogOpen(false)}
        purpose="pulse"
        currentValues={{ pulse: info ? info.model : "" }}
        onPick={(_purpose, modelId) => void pickModel(modelId)}
      />
    </aside>
  );
}

function AssistantChat({
  sessionId,
  surface,
  setSurface,
}: {
  sessionId: string;
  surface: HTMLElement | null;
  setSurface: (el: HTMLElement | null) => void;
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
