import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { ExternalLink, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { api } from "../../api/client";
import { cn } from "../../lib/utils";
import { useJumpToSession } from "../../lib/jumpToSession";
import PermissionDialog, { type PermissionDecideResult } from "../Chat/PermissionDialog";
import QuestionDialog from "../Chat/QuestionDialog";
import {
  PulseStream,
  STATUS_META,
  TODO_MARK,
  jumpTargetFor,
  usePulseElapsed,
} from "./PulseCard";
import { pulseProjectBasename } from "./pulseFilter";
import { usePulseTail } from "./usePulseTail";
import { useStickToBottom } from "./useStickToBottom";
import type {
  PermissionDecision,
  PulseRow,
  QuestionAnswerPayload,
  QuestionPrompt,
  SSEPermissionEvent,
} from "../../api/types";

/**
 * PulseFocusPane — one session in detail, beside the compact list of the rest.
 *
 * Top to bottom: header, plan, the full activity feed (the same entries a card
 * streams, but filling the pane instead of a capped box), the pending ask
 * rendered as the real chat dialog, and a reply box.
 *
 * Pulse stopped being read-only here by the user's decision: it resolves asks
 * and sends replies inline. It is still not a session manager (no rename,
 * delete or archive).
 *
 * The ask dialogs are CONFINED to this pane (`container` = the pane element)
 * instead of the viewport: the whole point of focus mode is that the other
 * sessions stay visible and usable, and a viewport modal would black them out.
 * The pane is `relative`, which the scoped dialog needs for its scrim.
 *
 * Rows are never mutated optimistically. A resolved ask clears when the next
 * `/api/pulse` refresh drops `pending_ask`, so the dashboard has one source of
 * truth for status.
 *
 * Replies go through plain `api.sendMessage`. ChatInput's leading-`/` handling
 * lives in its parent's `onSlashCommand` prop, not an exported helper, so slash
 * commands are NOT interpreted here: the text is sent to the model as typed.
 */

interface LoadedAsks {
  permission?: SSEPermissionEvent;
  question?: { request_id: string; questions: QuestionPrompt[] };
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export function PulseFocusPane({ row, onClose }: { row: PulseRow; onClose: () => void }) {
  const jump = useJumpToSession();
  const sessionId = row.session_id;

  // State rather than a ref so the dialogs re-render once the element exists.
  const [container, setContainer] = useState<HTMLElement | null>(null);

  const tail = usePulseTail(sessionId, true, row.status);
  const streamRef = useRef<HTMLDivElement | null>(null);
  const { onScroll } = useStickToBottom(streamRef, tail.entries);
  const elapsedText = usePulseElapsed(row);

  // ── Pending ask ──
  const askKind = row.pending_ask ? row.pending_ask.kind : null;
  const askSummary = row.pending_ask ? row.pending_ask.summary : null;
  const [asks, setAsks] = useState<LoadedAsks | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [hiddenQuestionId, setHiddenQuestionId] = useState<string | null>(null);

  useEffect(() => {
    if (askKind === null) {
      setAsks(null);
      return;
    }
    // Generation guard: a response for a superseded row (another session, or the
    // same ask after a refresh) must not land into the pane that replaced it.
    let stale = false;
    api
      .getSessionState(sessionId)
      .then((state) => {
        if (stale) return;
        setAsks({
          permission: state.pending_asks?.permissions?.[0],
          question: state.pending_asks?.questions?.[0],
        });
      })
      .catch((err) => {
        console.error(`PulseFocusPane: loading pending ask failed for session ${sessionId}:`, err);
        if (!stale) setActionError(`Loading the pending request failed for ${sessionId}: ${errorText(err)}`);
      });
    return () => {
      stale = true;
    };
  }, [sessionId, askKind, askSummary, row.updated_at]);

  const decide = async (requestId: string, decision: PermissionDecision): Promise<PermissionDecideResult> => {
    try {
      await api.resolvePermission(requestId, sessionId, decision);
      setActionError(null);
      return { ok: true };
    } catch (err) {
      console.error(`PulseFocusPane: resolving permission failed for session ${sessionId}:`, err);
      const error = errorText(err);
      setActionError(`Permission decision failed for ${sessionId}: ${error}`);
      return { ok: false, error };
    }
  };

  const answer = async (requestId: string, answers: QuestionAnswerPayload[]): Promise<boolean> => {
    try {
      await api.answerQuestion(requestId, sessionId, answers);
      setActionError(null);
      return true;
    } catch (err) {
      console.error(`PulseFocusPane: answering question failed for session ${sessionId}:`, err);
      setActionError(`Answering failed for ${sessionId}: ${errorText(err)}`);
      return false;
    }
  };

  const dismissQuestion = async (requestId: string): Promise<boolean> => {
    try {
      await api.cancelQuestion(requestId, sessionId);
      setActionError(null);
      return true;
    } catch (err) {
      console.error(`PulseFocusPane: dismissing question failed for session ${sessionId}:`, err);
      setActionError(`Dismissing the question failed for ${sessionId}: ${errorText(err)}`);
      return false;
    }
  };

  // ── Reply ──
  const [reply, setReply] = useState("");
  const [sending, setSending] = useState(false);
  const [replyError, setReplyError] = useState<string | null>(null);

  const replyBlockedReason =
    row.status === "running"
      ? "Turn in progress"
      : row.pending_ask
        ? "Answer the pending request first"
        : null;
  const canSend = replyBlockedReason === null && !sending && reply.trim() !== "";

  const send = async () => {
    if (!canSend) return;
    setSending(true);
    setReplyError(null);
    try {
      await api.sendMessage(sessionId, reply.trim());
      setReply("");
    } catch (err) {
      console.error(`PulseFocusPane: sending reply failed for session ${sessionId}:`, err);
      setReplyError(`Sending failed for ${sessionId}: ${errorText(err)}`);
    } finally {
      setSending(false);
    }
  };

  const onReplyKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    // isComposing: Enter confirms an IME candidate and must not send.
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      void send();
    }
  };

  const status = STATUS_META[row.status];
  const label = row.title || sessionId;

  return (
    <div
      ref={setContainer}
      data-testid="pulse-focus-pane"
      className="relative flex h-full min-h-0 min-w-0 flex-col gap-3 rounded-md border border-border bg-card p-3"
    >
      <div className="flex min-w-0 items-center gap-2">
        <span aria-label={status.label} className={cn("shrink-0 text-sm leading-none", status.className)}>
          {status.glyph}
        </span>
        <span title={row.project_path} className="shrink-0 truncate text-xs text-muted-foreground">
          {pulseProjectBasename(row.project_path)}
        </span>
        <h2 className="min-w-0 flex-1 truncate text-sm font-medium">{label}</h2>
        <span data-testid="pulse-focus-elapsed" className="shrink-0 text-[11px] tabular-nums text-muted-foreground">
          {elapsedText}
        </span>
        <Button size="sm" variant="outline" onClick={() => jump(jumpTargetFor(row))}>
          <ExternalLink className="h-3.5 w-3.5" aria-hidden />
          Open session
        </Button>
        <Button size="icon" variant="ghost" aria-label="Close focus" onClick={onClose}>
          <X className="h-4 w-4" aria-hidden />
        </Button>
      </div>

      {row.todo && row.todo.items.length > 0 && (
        <div data-testid="pulse-focus-todo" className="flex max-h-32 shrink-0 flex-col gap-0.5 overflow-y-auto">
          {row.todo.items.map((item, i) => (
            <span
              key={`${i}-${item.text}`}
              className={cn("text-xs", item.state === "done" && "text-muted-foreground line-through")}
            >
              <span aria-hidden>{TODO_MARK[item.state]} </span>
              {item.text}
            </span>
          ))}
        </div>
      )}

      {/* The full feed. No max-height, unlike the card: this region takes all
          the height the pane has left and scrolls past it. */}
      <div
        ref={streamRef}
        data-testid="pulse-focus-stream"
        onScroll={onScroll}
        className="min-h-0 flex-1 overflow-y-auto overscroll-contain rounded-md border border-border bg-background p-2"
      >
        {tail.entries.length === 0 && tail.error === null ? (
          <p className="text-xs text-muted-foreground">{tail.loading ? "Loading…" : "No activity to show"}</p>
        ) : (
          <PulseStream tail={tail} wrap />
        )}
      </div>

      {actionError && (
        <p role="alert" data-testid="pulse-focus-error" className="text-xs text-destructive">
          {actionError}
        </p>
      )}

      {container && askKind === "permission" && asks?.permission && (
        <PermissionDialog
          open={true}
          container={container}
          tool={asks.permission.tool}
          command={asks.permission.command}
          args={asks.permission.args}
          rule={asks.permission.rule}
          summary={asks.permission.summary}
          denyReason={asks.permission.deny_reason}
          modelUnavailable={asks.permission.model_unavailable}
          scope={asks.permission.scope}
          prefix={asks.permission.prefix}
          outOfScopePath={asks.permission.out_of_scope_path}
          agentName={asks.permission.agent_name}
          requestId={asks.permission.request_id}
          onDecide={decide}
        />
      )}
      {container &&
        askKind === "question" &&
        asks?.question &&
        hiddenQuestionId !== asks.question.request_id && (
          <QuestionDialog
            key={asks.question.request_id}
            open={true}
            container={container}
            requestId={asks.question.request_id}
            questions={asks.question.questions}
            onSubmit={answer}
            onHide={setHiddenQuestionId}
            onCancel={dismissQuestion}
          />
        )}
      {askKind === "question" && asks?.question && hiddenQuestionId === asks.question.request_id && (
        <Button size="sm" variant="outline" onClick={() => setHiddenQuestionId(null)}>
          Show pending question
        </Button>
      )}

      <div className="flex shrink-0 flex-col gap-1">
        <div className="flex items-end gap-2">
          <textarea
            data-testid="pulse-focus-reply"
            aria-label="Reply to session"
            rows={2}
            value={reply}
            disabled={replyBlockedReason !== null || sending}
            onChange={(e) => setReply(e.target.value)}
            onKeyDown={onReplyKeyDown}
            placeholder="Reply… (Enter to send, Shift+Enter for a new line)"
            className="min-h-[2.5rem] flex-1 resize-none rounded-md border border-border bg-background px-2 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
          />
          <Button size="sm" disabled={!canSend} onClick={() => void send()}>
            Send
          </Button>
        </div>
        {replyBlockedReason && (
          <p data-testid="pulse-focus-disabled-reason" className="text-[11px] text-muted-foreground">
            {replyBlockedReason}
          </p>
        )}
        {replyError && (
          <p role="alert" data-testid="pulse-focus-reply-error" className="text-xs text-destructive">
            {replyError}
          </p>
        )}
      </div>
    </div>
  );
}
