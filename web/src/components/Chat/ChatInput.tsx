import { useState, type KeyboardEvent, useRef, useEffect, useCallback } from "react";
import { useChat } from "../../hooks/useChat";
import { getDraft, setDraft, clearDraft } from "../../lib/tabDrafts";
import { getQueue, pushQueued, shiftUndispatched, unshiftQueued, popLastQueued, removeQueuedItem, type QueuedItem } from "../../lib/tabQueue";
import { Button } from "@/components/ui/button";
import SlashCommandMenu from "./SlashCommandMenu";
import { COMMANDS } from "./commands";
import { Paperclip, X } from "lucide-react";
import { apiPath, authHeaders } from "@/api/client";
import { useProjectState } from "../../stores/projectStore";
import EditorContextChip from "./EditorContextChip";
import { RESTORE_EVENT } from "../../lib/inputRestore";

interface ChatInputProps {
  /** Called when a slash command is entered. */
  onSlashCommand?: (command: string, sessionTabId?: string | null) => boolean | SlashCommandResult | Promise<boolean | SlashCommandResult>;
  /** Active editor context (file path + optional selection) to display as a chip
   *  and attach to the outgoing message. When provided, a chip is shown above
   *  the input and the ref is prepended to the message on send. */
  activeEditorContext?: { path: string; selection?: { startLine: number; endLine: number } } | null;
  /** Paths of opened editor tabs opted into the chat/LLM loop. Each is attached
   *  as an `@path` ref on send (unless it duplicates the active editor context). */
  contextFilePaths?: string[];
  /** Temporary session tab ID that should be remapped once the first message creates a real session. */
  sessionTabId?: string | null;
  /** Called when a temporary tab has been replaced by the real session ID. */
  onSessionCreated?: (tempTabId: string, sessionId: string) => void;
}

export interface SlashCommandResult {
  handled: boolean;
  startedTurn?: boolean;
  accepted?: boolean;
}

export default function ChatInput({
  onSlashCommand,
  activeEditorContext,
  contextFilePaths,
  sessionTabId,
  onSessionCreated,
}: ChatInputProps) {
  const [input, setInput] = useState("");
  const [showSlashMenu, setShowSlashMenu] = useState(false);
  const [slashQuery, setSlashQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [attachedFiles, setAttachedFiles] = useState<string[]>([]);
  // shellInFlight is true while a `!cmd` shell command is being executed by
  // the server. We block new sends during this window so the user can't fire
  // a second message that interleaves with the in-flight shell result — the
  // agent would otherwise answer msg2 first, then re-engage with the shell
  // output of msg1, producing confusing turn ordering.
  const [shellInFlight, setShellInFlight] = useState(false);
  const [queueCount, setQueueCount] = useState(0);
  const [isDragging, setIsDragging] = useState(false);
  const dragCounterRef = useRef(0);
  // In-flight submission guard: an accidental double-fire of the same physical
  // submit (duplicate keydown/click, IME confirm + keydown, or a duplicated
  // synthetic event from the desktop shell) must not send the message twice.
  // submittingRef is set synchronously at the start of handleSend and cleared
  // when the send settles, so a duplicate that arrives within the same submit
  // is dropped — while a later, DISTINCT message (different tick, after the
  // previous send resolved and busy flipped) is never blocked.
  const submittingRef = useRef(false);
  const { sendMessage, executeShell, stop, resume, wasInterrupted, isStreaming, pendingPermission } = useChat(sessionTabId ?? null, {
    onNewSession: (sessionId) => {
      if (sessionTabId?.startsWith("new-")) {
        onSessionCreated?.(sessionTabId, sessionId);
      }
    },
  });
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const attachRef = useRef<HTMLInputElement>(null);

  // Per-tab draft: restore this tab's typed-but-unsent text whenever the active
  // session tab changes (the draft map also lets the "New session" button tell
  // whether the active tab is "completely empty").
  useEffect(() => {
    setInput(getDraft(sessionTabId));
    setQueueCount(getQueue(sessionTabId).length);
  }, [sessionTabId]);

  // "Restore older input" — ChatPanel's user bubble dispatches a typed window
  // event after confirmation. Only the matching sessionTabId applies it, so
  // hidden tabs don't mutate. Replace semantics: the restored text replaces
  // the current draft entirely, focus moves to textarea with cursor at end.
  useEffect(() => {
    const handler = (e: Event) => {
      const ce = e as CustomEvent<{ sessionId: string; text: string }>;
      if (!ce.detail || ce.detail.sessionId !== sessionTabId) return;
      const text = ce.detail.text ?? "";
      setInput(text);
      setDraft(sessionTabId, text);
      // Focus after state applies; queue microtask to ensure DOM updated.
      requestAnimationFrame(() => {
        const el = textareaRef.current;
        if (el) {
          el.focus();
          el.setSelectionRange(text.length, text.length);
        }
      });
    };
    window.addEventListener(RESTORE_EVENT, handler as EventListener);
    return () => window.removeEventListener(RESTORE_EVENT, handler as EventListener);
  }, [sessionTabId]);

  // Auto-drain the queue once the turn (streaming or shell exec) frees up,
  // in FIFO order — mirrors the TUI's drainQueuedItems. Stops as soon as an
  // item starts a new turn; the next busy->free transition continues it.
  //
  // `shiftUndispatched` is the backstop against double-send: messages typed
  // while streaming are BOTH injected into the live loop (so they're answered
  // within the same turn, matching the TUI) and kept in the client queue for
  // up-arrow recall. Those entries are flagged `dispatched`, so the drain
  // discards them instead of sending them a second time once the turn ends.
  //
  // pendingPermission counts as busy even though the server already marked
  // the turn inactive (turn_done fires as soon as Step pauses on the
  // PERMISSION_ASK sentinel) — otherwise the composer unlocks and the queue
  // drains while the dialog is still up, starting a new turn on top of an
  // unresolved permission ask.
  const busy = isStreaming || shellInFlight || !!pendingPermission;
  const effectiveBusy = busy || wasInterrupted;
  const prevBusyRef = useRef(busy);
  const prevWasInterruptedRef = useRef(wasInterrupted);

  // Dispatch a `/command` or `!shell` command, replaying the same logic used
  // for an immediate send. Returns true if it started a new agent turn
  // (i.e. draining must stop until that turn ends).
  const dispatchCommand = async (text: string): Promise<{ startedTurn: boolean; accepted: boolean }> => {
    if (text.startsWith("/") && onSlashCommand) {
      const response = await onSlashCommand(text, sessionTabId);
      if (typeof response === "boolean") {
        if (response) return { startedTurn: false, accepted: true };
      } else if (response.handled) {
        return {
          startedTurn: response.startedTurn === true,
          accepted: response.accepted !== false,
        };
      }
    }
    if (text.startsWith("!")) {
      const command = text.slice(1).trim();
      if (command) {
        setShellInFlight(true);
        try {
          const result = await executeShell(command);
          const outputMessage = result.exitCode === 0
            ? `Shell command executed successfully:\n\`\`\`\n${result.output}\n\`\`\``
            : `Shell command failed (exit code ${result.exitCode}):\n\`\`\`\n${result.error || result.output}\n\`\`\``;
          const accepted = await sendMessage(outputMessage);
          return { startedTurn: true, accepted };
        } finally {
          setShellInFlight(false);
        }
      }
      return { startedTurn: false, accepted: true };
    }
    const accepted = await sendMessage(text);
    return { startedTurn: true, accepted };
  };

  const drainQueue = useCallback(async () => {
    for (;;) {
      const item = shiftUndispatched(sessionTabId);
      setQueueCount(getQueue(sessionTabId).length);
      if (!item) break;
      const outcome = item.kind === "command"
        ? await dispatchCommand(item.text)
        : { startedTurn: true, accepted: await sendMessage(item.text) };
      if (!outcome.accepted) {
        unshiftQueued(sessionTabId, item);
        setQueueCount(getQueue(sessionTabId).length);
        break;
      }
      if (outcome.startedTurn) break;
    }
  }, [sessionTabId, sendMessage]);

  useEffect(() => {
    const wasInterruptedJustCleared = prevWasInterruptedRef.current && !wasInterrupted;
    prevWasInterruptedRef.current = wasInterrupted;
    if (wasInterrupted) {
      prevBusyRef.current = busy;
      return;
    }
    if ((prevBusyRef.current && !busy) || (wasInterruptedJustCleared && !busy)) {
      void drainQueue();
    }
    prevBusyRef.current = busy;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [busy, wasInterrupted, sessionTabId]);

  const handleResume = useCallback(() => {
    resume();
  }, [resume]);

  const updateDraft = (value: string) => {
    setInput(value);
    setDraft(sessionTabId, value);
  };

  // Shared upload logic for both file input and drag-and-drop. Uploads are
  // project-scoped: they land in <project>/.ocode/uploads so the relative
  // @.ocode/uploads/<name> refs below resolve against the session's project.
  const projectPath = useProjectState().state.activeProject?.path;
  const uploadFiles = async (files: File[]) => {
    if (files.length === 0) return;
    const fd = new FormData();
    files.forEach((f) => fd.append("file", f));
    try {
      const query = projectPath ? `?project=${encodeURIComponent(projectPath)}` : "";
      const r = await fetch(apiPath(`/api/uploads${query}`), {
        method: "POST",
        headers: authHeaders(),
        body: fd,
      });
      const raw: unknown = await r.json();
      const saved: { name: string }[] = Array.isArray(raw) ? (raw as { name: string }[]) : [];
      if (!Array.isArray(raw)) console.warn("upload response was not an array:", raw);
      setAttachedFiles((prev) => [...prev, ...saved.map((f) => f.name)]);
    } catch (err) {
      console.error("upload failed:", err);
    }
  };

  const handleAttach = async (e: React.ChangeEvent<HTMLInputElement>) => {
    if (!e.target.files) return;
    await uploadFiles(Array.from(e.target.files));
    e.target.value = "";
  };

  // Drag-and-drop handlers
  const handleDragEnter = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragCounterRef.current++;
    if (e.dataTransfer.types.includes("Files")) {
      setIsDragging(true);
    }
  }, []);

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragCounterRef.current--;
    if (dragCounterRef.current === 0) {
      setIsDragging(false);
    }
  }, []);

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
  }, []);

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragCounterRef.current = 0;
    setIsDragging(false);
    const files = Array.from(e.dataTransfer.files);
    if (files.length > 0) {
      uploadFiles(files);
    }
  };

  useEffect(() => {
    const value = input;
    if (value.startsWith("/") && !value.includes(" ")) {
      setShowSlashMenu(true);
      setSlashQuery(value);
      setSelectedIndex(0);
    } else {
      setShowSlashMenu(false);
    }
  }, [input]);

  const filteredCommandNames = showSlashMenu
    ? COMMANDS.filter((cmd) => cmd.name.includes(slashQuery.toLowerCase())).map((c) => c.name)
    : [];
  const filteredCount = filteredCommandNames.length;

  const handleSend = async () => {
    // In-flight guard — must run before any async gap and before reading
    // `input`, so a duplicate physical submit (same tick) is dropped instead of
    // sending the same text twice. Distinct later submits (different tick, after
    // this send settles) are never blocked.
    if (submittingRef.current) return;
    const trimmed = input.trim();
    if (!trimmed) return;
    submittingRef.current = true;
    try {
      setInput("");
      clearDraft(sessionTabId);
      setShowSlashMenu(false);

    // While a turn is busy (streaming, shell, permission, or interrupted
    // barrier), queue instead of blocking — mirrors the TUI's unified
    // queuedItems, drained by the effect once the turn frees up and the
    // interrupt barrier is cleared via Resume.
      if (trimmed.startsWith("/") || trimmed.startsWith("!")) {
        if (effectiveBusy) {
          pushQueued(sessionTabId, { kind: "command", text: trimmed });
          setQueueCount(getQueue(sessionTabId).length);
          return;
        }
        const outcome = await dispatchCommand(trimmed);
        if (!outcome.accepted) {
          setInput(trimmed);
          setDraft(sessionTabId, trimmed);
        }
        return;
      }

    // Build refs: attached files + active editor context + opened files opted
    // into the LLM loop. Resolved now (not at drain time) so queueing a message
    // doesn't leave stale attachment chips hanging around in the composer.
      const refs = attachedFiles.map((n) => `@.ocode/uploads/${n}`).join(" ");
      const contextRef = activeEditorContext
        ? activeEditorContext.selection
          ? `@${activeEditorContext.path}#L${activeEditorContext.selection.startLine}-L${activeEditorContext.selection.endLine}`
          : `@${activeEditorContext.path}`
        : "";

      // Opened editor tabs opted into the chat context: attach each as an
      // @path ref, skipping the active editor (already covered by contextRef)
      // and de-duplicating against uploaded refs.
      const seen = new Set<string>();
      if (contextRef) seen.add(activeEditorContext!.path);
      const contextRefs = (contextFilePaths ?? [])
        .filter((p) => !seen.has(p))
        .map((p) => `@${p}`)
        .join(" ");

      const parts = [contextRef, contextRefs, refs, trimmed].filter(Boolean);
      const finalMessage = parts.join(" ");
      setAttachedFiles([]);

      if (wasInterrupted) {
        // Interrupted barrier: queue without dispatched flag so FIFO order is
        // preserved and nothing is injected into the live agent loop while
        // paused. Resume will drain in order.
        pushQueued(sessionTabId, { kind: "message", text: finalMessage });
        setQueueCount(getQueue(sessionTabId).length);
        return;
      }
      if (isStreaming) {
        // A turn is actively running. Mirror the TUI: inject into the live agent
        // loop at the next tool-call boundary (so it's answered within the same
        // turn) AND keep a client queue entry flagged `dispatched` so the message
        // is still recallable via up-arrow before the turn ends. The drain
        // backstop discards dispatched entries, so it is never sent a second time.
        const item: QueuedItem = { kind: "message", text: finalMessage, dispatched: true };
        pushQueued(sessionTabId, item);
        setQueueCount(getQueue(sessionTabId).length);
        const ok = await sendMessage(finalMessage);
        if (!ok) {
          // Submit was rejected (network/validation) — the message never reached
          // the live loop, so drop the phantom queue entry instead of letting the
          // drain backstop silently skip it. The error is already surfaced in the
          // store for the user to retry.
          removeQueuedItem(sessionTabId, item);
          setQueueCount(getQueue(sessionTabId).length);
        }
        return;
      }
      if (busy) {
        // Shell command in flight, no agent turn running yet — nothing to
        // inject into, so queue as before until it frees up.
        pushQueued(sessionTabId, { kind: "message", text: finalMessage });
        setQueueCount(getQueue(sessionTabId).length);
        return;
      }
      const ok = await sendMessage(finalMessage);
      if (!ok) {
        // Restore the draft so the user can retry without retyping. The
        // error is already set in the store's SET_ERROR path.
        setInput(trimmed);
        setDraft(sessionTabId, trimmed);
      }
    } finally {
      // Release the in-flight guard. The previous send has now settled, so a
      // subsequent, distinct submit is allowed (even if it repeats the text).
      submittingRef.current = false;
    }
  };

  // Recall the most recently queued item into the input box for editing —
  // web equivalent of the TUI's up-arrow queue restore.
  const restoreLastQueued = () => {
    const item = popLastQueued(sessionTabId);
    if (!item) return false;
    setQueueCount(getQueue(sessionTabId).length);
    updateDraft(item.text);
    return true;
  };

  const handleSlashSelect = (command: string) => {
    updateDraft(command + " ");
    setShowSlashMenu(false);
    textareaRef.current?.focus();
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    // IME composition Enter should not send — it only confirms the composition.
    if ((e.nativeEvent as unknown as { isComposing?: boolean }).isComposing) return;
    // Key repeat from holding Enter would otherwise queue duplicate sends
    // before isStreaming flips.
    if (e.repeat) return;
    if (showSlashMenu) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIndex((i) => Math.min(i + 1, filteredCount - 1));
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIndex((i) => Math.max(i - 1, 0));
        return;
      }
      if (e.key === "Enter" && filteredCount > 0) {
        e.preventDefault();
        if (filteredCommandNames[selectedIndex]) {
          handleSlashSelect(filteredCommandNames[selectedIndex]);
        }
        return;
      }
      if (e.key === "Escape") {
        setShowSlashMenu(false);
        return;
      }
    }

    if (e.key === "ArrowUp" && input === "") {
      if (restoreLastQueued()) {
        e.preventDefault();
        return;
      }
    }

    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void handleSend();
    }
  };

  return (
    <div
      className={`border-t border-border p-4 relative transition-colors ${
        isDragging ? "bg-blue-500/10 border-blue-500" : ""
      }`}
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
    >
      {isDragging && (
        <div className="absolute inset-0 flex items-center justify-center bg-blue-500/20 border-2 border-dashed border-blue-400 rounded-lg z-10 pointer-events-none">
          <span className="text-blue-300 text-sm font-medium">Drop files here</span>
        </div>
      )}
      {showSlashMenu && (
        <SlashCommandMenu
          query={slashQuery}
          selectedIndex={selectedIndex}
          onSelect={handleSlashSelect}
          onHover={setSelectedIndex}
        />
      )}
      <div className="flex flex-wrap gap-1 mb-2">
        {attachedFiles.map((name) => (
          <span
            key={name}
            className="inline-flex items-center gap-1 text-xs bg-accent text-foreground rounded px-2 py-0.5"
          >
            {name}
            <button
              type="button"
              onClick={() => setAttachedFiles((prev) => prev.filter((n) => n !== name))}
              className="text-muted-foreground hover:text-foreground"
            >
              <X className="w-3 h-3" />
            </button>
          </span>
        ))}
        {activeEditorContext && (
          <EditorContextChip
            path={activeEditorContext.path}
            selection={activeEditorContext.selection ?? null}
          />
        )}
      </div>
      {queueCount > 0 && (
        <div className="text-xs text-muted-foreground mb-1">
          {queueCount} queued — press ↑ in an empty box to edit the last one
        </div>
      )}
      <input
        type="file"
        multiple
        ref={attachRef}
        className="hidden"
        onChange={handleAttach}
      />
      <div className="flex items-end gap-2">
        <button
          type="button"
          onClick={() => attachRef.current?.click()}
          className="shrink-0 p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-accent"
          title="Attach files"
        >
          <Paperclip className="w-4 h-4" />
        </button>
        <textarea
          ref={textareaRef}
          className="flex-1 resize-none rounded-lg border border-border bg-muted p-3 text-sm text-foreground placeholder-muted-foreground focus:border-blue-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
          rows={2}
          placeholder="Type a message... (Enter to send, Shift+Enter for newline, / for commands, ! for shell)"
          value={input}
          onChange={(e) => updateDraft(e.target.value)}
          onKeyDown={handleKeyDown}
        />
        {isStreaming ? (
          <Button
            type="button"
            variant="destructive"
            size="sm"
            className="shrink-0"
            onClick={stop}
          >
            Stop
          </Button>
        ) : shellInFlight ? (
          // Shell command is mid-execution on the server. Show "Running..."
          // so the user understands the input is being processed, even
          // though isStreaming is still false (the result hasn't been
          // streamed to the agent yet).
          <Button
            type="button"
            size="sm"
            className="shrink-0"
            disabled
          >
            Running…
          </Button>
        ) : pendingPermission ? (
          // A permission dialog is up. isStreaming is already false (the
          // server marks the turn inactive the moment it pauses), so without
          // this branch Send would look available while the ask is still
          // unresolved.
          <Button
            type="button"
            size="sm"
            className="shrink-0"
            disabled
          >
            Waiting for permission…
          </Button>
        ) : wasInterrupted ? (
          <Button
            type="button"
            size="sm"
            className="shrink-0"
            onClick={handleResume}
            title={queueCount > 0 ? `Resume — ${queueCount} queued` : "Resume"}
          >
            Resume{queueCount > 0 ? ` (${queueCount})` : ""}
          </Button>
        ) : (
          <Button
            type="button"
            size="sm"
            className="shrink-0"
            onClick={handleSend}
            disabled={!input.trim()}
          >
            Send
          </Button>
        )}
      </div>
    </div>
  );
}
