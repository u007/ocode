import { useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  ScopedDialog,
  ScopedDialogContent,
  ScopedDialogHeader,
  ScopedDialogTitle,
  type ScopedDialogContainer,
} from "@/components/ui/scoped-dialog";
import {
  AlertTriangle,
  Terminal,
  FileCode,
  Check,
  X,
  ShieldCheck,
} from "lucide-react";
import type { PermissionDecision } from "@/api/types";
import type { AskContext } from "@/stores/chatStore";
import type { ContentGuardScore } from "@/stores/chatStore";
import AskContextPreview from "./AskContextPreview";

// Shell control-flow keywords are not real commands, so an "always allow
// prefix" rule for them is meaningless. Mirrors agent.ShellControlKeywords.
const SHELL_CONTROL_KEYWORDS = new Set([
  "if", "else", "elif", "fi",
  "then", "while", "do", "done",
  "for", "case", "esac", "until",
  "function", "select", "time",
]);

interface Props {
  open: boolean;
  /**
   * Element the dialog is confined to — the session's chat surface, so an ask
   * never covers the project list, the tab bar or another session. Required
   * (not optional) so a call site cannot silently render an invisible dialog.
   */
  container: ScopedDialogContainer;
  tool: string;
  command?: string;
  /** Complete JSON execution parameters; absent on legacy requests. */
  args?: unknown;
  rule?: string;
  summary?: string;
  denyReason?: string;
  modelUnavailable?: string;
  /** "tool" | "bash_prefix" — drives always-allow button availability. */
  scope?: string;
  /** Bash prefix for bash_prefix-scope asks (e.g. "rm"). */
  prefix?: string;
  /** Out-of-workspace target path for path-scope asks. */
  outOfScopePath?: string;
  /** Content-guardrail ask (scope "content"): the flagged result text. */
  untrustedContent?: string;
  /** Content-guardrail ask: one-line origin of the flagged content. */
  untrustedSource?: string;
  /** Content-guardrail ask: the guardrail's headline (concern + confidence). */
  untrustedSummary?: string;
  /** Content-guardrail ask: the per-question judge scores, one per judged chunk. */
  untrustedScores?: ContentGuardScore[];
  /** Content-guardrail ask: why the guardrail could not clear this result. */
  untrustedFailure?: string;
  /** Sub-agent that raised the ask; absent for a main-agent ask. Named in the
   *  dialog so the user can tell WHICH sub-agent is blocked when several run. */
  agentName?: string;
  /** Assistant message (prose + reasoning) that led to this ask. */
  context?: AskContext | null;
  requestId: string;
  onDecide: (requestId: string, decision: PermissionDecision) => Promise<PermissionDecideResult>;
}

/** Outcome of a decision submit. `ok: false` carries the message the dialog
 *  shows inline; whether the dialog stays mounted after a failure is the
 *  parent's call (useChat dismisses asks the server no longer holds). */
export type PermissionDecideResult = { ok: true } | { ok: false; error: string };

/** Whether the "Always allow this rule" choice is offered, mirroring the
 *  TUI's permAlwaysRuleAvailable / agent.AlwaysRuleChoiceAvailable: git
 *  prefixes would blanket-approve every future invocation of that subcommand
 *  (e.g. all git push), and shell control keywords are not real commands. */
function alwaysRuleAvailable(
  tool: string,
  scope?: string,
  prefix?: string,
): boolean {
  if (scope === "content") return false;
  if (tool === "bash" && scope === "bash_prefix") {
    const p = (prefix ?? "").trim();
    if (!p) return false;
    if (p.startsWith("git ") || p === "git") return false;
    if (SHELL_CONTROL_KEYWORDS.has(p)) return false;
  }
  return true;
}

/** Whether the "Always allow this tool" choice is offered, mirroring the
 *  TUI's permAlwaysToolAvailable / agent.AlwaysToolChoiceAvailable: a
 *  tool-level bash allow blanket-approves every future shell command. */
function alwaysToolAvailable(tool: string, scope?: string): boolean {
  if (scope === "content") return false;
  return tool !== "bash";
}

export default function PermissionDialog({
  container,
  open,
  tool,
  command,
  args,
  rule,
  summary,
  denyReason,
  modelUnavailable,
  scope,
  prefix,
  outOfScopePath,
  untrustedContent,
  untrustedSource,
  untrustedSummary,
  untrustedScores,
  untrustedFailure,
  agentName,
  context,
  requestId,
  onDecide,
}: Props) {
  // A content-guardrail ask is about an ALREADY-EXECUTED result, not a pending
  // command, so neither always-allow choice is offered — mirroring
  // agent.AlwaysRuleChoiceAvailable / AlwaysToolChoiceAvailable, which both
  // return false for PermissionScopeContent. The Go side enforces the same rule
  // on resolve, so this is presentation rather than the security boundary.
  const isContentAsk = scope === "content";
  const [loading, setLoading] = useState(false);
  const parameters = useMemo(
    () => args === undefined ? undefined : JSON.stringify(args, null, 2),
    [args],
  );
  // Which always-allow choice is pending confirmation ("a"/"t" step in the
  // TUI); empty when the main button row is shown.
  const [confirming, setConfirming] = useState<"always_rule" | "always_tool" | null>(null);
  // Why the last decision did not go through — shown inline so a failed
  // submit is never silent (the store's per-session error is not rendered in
  // the chat view). Cleared on the next attempt or a new request.
  const [submitError, setSubmitError] = useState<string | null>(null);

  const canAlwaysRule = useMemo(
    () => alwaysRuleAvailable(tool, scope, prefix),
    [tool, scope, prefix],
  );
  const canAlwaysTool = useMemo(() => alwaysToolAvailable(tool, scope), [tool, scope]);

  // When a new permission request arrives while the dialog is still mounted
  // (the queue resurfaces the next ask after the previous one resolves), the
  // previous decision's loading/confirming state must not carry over — it
  // left the buttons disabled and the confirm UI stale for the new request.
  useEffect(() => {
    setLoading(false);
    setConfirming(null);
    setSubmitError(null);
  }, [requestId, open]);

  const handleResponse = async (decision: PermissionDecision) => {
    setLoading(true);
    setSubmitError(null);
    try {
      const result = await onDecide(requestId, decision);
      if (!result.ok) {
        setSubmitError(result.error);
        setLoading(false);
        setConfirming(null);
      }
    } catch (err) {
      console.error("PermissionDialog: decision submit threw:", err);
      setSubmitError(err instanceof Error ? err.message : "permission decision failed");
      setLoading(false);
      setConfirming(null);
    }
  };

  const toolIcon =
    tool === "bash" || tool === "bash_output" ? Terminal : FileCode;

  // Describes exactly what confirming will persist — mirrors the TUI's
  // renderPermConfirmBody so both surfaces describe the same write.
  const confirmExplanation = (() => {
    if (confirming === "always_tool") {
      return (
        <>
          Persist a tool rule: always allow ALL uses of the{" "}
          <span className="font-mono">{tool}</span> tool. This is broad — every
          future call to this tool is auto-allowed, regardless of arguments.
        </>
      );
    }
    if (outOfScopePath || (rule && /\.(out_of_scope|path_pattern)$/.test(rule))) {
      const root = outOfScopePath || "(detected path)";
      return (
        <>
          Persist out-of-workspace path access for{" "}
          <span className="font-mono">{root}</span>. Adds this directory to
          extra_allowed_paths. No bash-prefix or tool rule is persisted.
        </>
      );
    }
    if (tool === "webfetch" && rule?.startsWith("webfetch.domain.")) {
      const domain = rule.slice("webfetch.domain.".length);
      return (
        <>
          Always allow fetching from domain{" "}
          <span className="font-mono">{domain}</span> for this session.
        </>
      );
    }
    if (tool === "bash" && scope === "bash_prefix" && prefix) {
      if (prefix.startsWith("bash.interpreter.")) {
        const lang = prefix.slice("bash.interpreter.".length);
        return (
          <>
            Persist an interpreter rule: always allow{" "}
            <span className="font-mono">{lang}</span> interpreter executions.
          </>
        );
      }
      return (
        <>
          Persist a bash-prefix rule: always allow{" "}
          <span className="font-mono">{prefix} ...</span> (all commands starting
          with <span className="font-mono">{prefix}</span>).
        </>
      );
    }
    return (
      <>
        Persist a tool rule: always allow the{" "}
        <span className="font-mono">{tool}</span> tool.
      </>
    );
  })();

  return (
    <ScopedDialog
      container={container}
      open={open}
      onOpenChange={(isOpen) => {
        if (!isOpen && !loading) {
          setConfirming(null);
          void handleResponse("deny");
        }
      }}
    >
      {/* `dialog-viewport-max` is deliberately gone: it capped the panel to the
          WINDOW, which is wrong once the dialog is confined to a panel that can
          be shorter than the viewport. ScopedDialogContent supplies the
          container-relative `max-h-[calc(100%-2rem)]` instead, and two
          `max-height` declarations do NOT take the min — last one wins. */}
      <ScopedDialogContent className="w-[calc(100%-2rem)] overflow-x-clip overflow-y-auto sm:max-w-2xl bg-card border-border">
        <ScopedDialogHeader>
          <ScopedDialogTitle className="flex items-center gap-2 text-foreground">
            {confirming ? (
              <ShieldCheck className="w-5 h-5 text-yellow-400" />
            ) : (
              <AlertTriangle className="w-5 h-5 text-yellow-400" />
            )}
            {confirming
              ? "Confirm always-allow"
              : isContentAsk
                ? "Content guardrail — review before it reaches the model"
                : agentName
                  ? `Permission Required — ${agentName}`
                  : "Permission Required"}
          </ScopedDialogTitle>
        </ScopedDialogHeader>

        <div className="space-y-4">
          {!confirming && (
            <>
              <AskContextPreview context={context} />

              {isContentAsk && (
                <section aria-label="Flagged tool result" className="min-w-0 max-w-full space-y-2">
                  <div className="max-w-full rounded-lg border border-yellow-900/60 bg-yellow-950/30 p-3 text-sm text-yellow-200">
                    <div className="font-medium">This tool already ran. Its result was flagged:</div>
                    {untrustedSummary && (
                      <div className="mt-1 break-words [overflow-wrap:anywhere]">{untrustedSummary}</div>
                    )}
                    {untrustedSource && (
                      <div className="mt-1 text-xs">
                        Source:
                        {/* A bash source is the whole command, unclipped and
                            possibly multi-line: keep its line breaks and scroll
                            rather than truncate. */}
                        <pre
                          tabIndex={0}
                          data-testid="content-guard-source"
                          className="mt-1 max-h-40 max-w-full overflow-y-auto font-mono whitespace-pre-wrap break-words [overflow-wrap:anywhere]"
                        >
                          {untrustedSource}
                        </pre>
                      </div>
                    )}
                  </div>
                  {untrustedFailure && (
                    <div
                      role="status"
                      className="max-w-full rounded-lg border border-orange-900/60 bg-orange-950/30 p-3 text-sm text-orange-200"
                    >
                      <div className="font-medium">
                        Guardrail could not clear this result:
                      </div>
                      <div className="mt-1 break-words [overflow-wrap:anywhere]">
                        {untrustedFailure}
                      </div>
                    </div>
                  )}
                  {untrustedScores && untrustedScores.length > 0 && (
                    <div aria-label="Guardrail judge scores" className="max-w-full text-xs text-muted-foreground">
                      <div className="mb-1 font-medium text-foreground">Judge scores:</div>
                      <ul className="space-y-1">
                        {untrustedScores.map((sc) => (
                          <li key={`${sc.chunk}-${sc.total}`} className="break-words [overflow-wrap:anywhere]">
                            <span className="font-mono">
                              chunk {sc.chunk}/{sc.total}
                            </span>{" "}
                            — {sc.verdict} (confidence {sc.verdict_confidence.toFixed(2)})
                            {sc.concern && (
                              <>
                                {" · "}
                                {sc.concern} (confidence {(sc.concern_confidence ?? 0).toFixed(2)})
                              </>
                            )}
                            {sc.probabilities && Object.keys(sc.probabilities).length > 0 && (
                              <div className="ml-4 font-mono opacity-80">
                                p:{" "}
                                {Object.keys(sc.probabilities)
                                  .sort()
                                  .map((k) => `${k} ${sc.probabilities![k].toFixed(2)}`)
                                  .join(" / ")}
                              </div>
                            )}
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}
                  <div className="text-sm text-muted-foreground">
                    The complete result is below — scroll to review it all. Delivering it
                    lets the assistant read the text, including anything in it written as
                    an instruction. Secrets are already masked in this view.
                  </div>
                  {/* The result can be arbitrarily large, so it is scrollable
                      rather than rendered in full: an uncapped block inside the
                      dialog would push the buttons off-screen and make the
                      decision unreachable. */}
                  <pre
                    tabIndex={0}
                    data-testid="content-guard-result"
                    className="max-h-96 max-w-full overflow-y-auto rounded-lg border border-border bg-muted p-3 font-mono text-xs whitespace-pre-wrap break-words text-foreground [overflow-wrap:anywhere]"
                  >
                    {untrustedContent}
                  </pre>
                </section>
              )}

              {denyReason && (
                <div className="max-w-full rounded-lg border border-red-900/60 bg-red-950/30 p-3 text-sm text-red-200">
                  <div className="font-medium">Auto-denied by LLM permission model:</div>
                  <div className="mt-1 break-words [overflow-wrap:anywhere]">{denyReason}</div>
                </div>
              )}

              {modelUnavailable && (
                <div className="max-w-full rounded-lg border border-yellow-900/60 bg-yellow-950/30 p-3 text-sm text-yellow-200">
                  <div className="font-medium">Permission model unavailable — asking you instead:</div>
                  <div className="mt-1 break-words [overflow-wrap:anywhere]">{modelUnavailable}</div>
                </div>
              )}

              {summary && (
                <div className="max-w-full rounded-lg border border-border bg-muted/60 p-3 text-sm text-foreground">
                  <div className="font-medium text-foreground">Model summary:</div>
                  <div className="mt-1 break-words [overflow-wrap:anywhere]">{summary}</div>
                </div>
              )}

              {agentName && !isContentAsk && (
                <div
                  data-testid="permission-asking-agent"
                  className="max-w-full rounded-lg border border-blue-900/60 bg-blue-950/30 px-3 py-2 text-sm text-blue-200"
                >
                  The{" "}
                  <span className="font-mono break-words [overflow-wrap:anywhere]">
                    {agentName}
                  </span>{" "}
                  sub-agent is blocked on this. Answering resumes its tool call
                  directly — nothing else needs to run first.
                </div>
              )}

              {rule && (
                <div className="max-w-full text-xs text-muted-foreground">
                  Permission rule:{" "}
                  <span className="font-mono break-words [overflow-wrap:anywhere]">{rule}</span>
                </div>
              )}

              <div className="flex max-w-full items-center gap-3 rounded-lg border border-border bg-muted p-3">
                {(() => {
                  const Icon = toolIcon;
                  return <Icon className="w-5 h-5 text-blue-400 flex-shrink-0" />;
                })()}
                <div className="min-w-0 max-w-full">
                  <div className="text-sm font-medium text-foreground">
                    The agent wants to use{" "}
                    <span className="font-mono break-words [overflow-wrap:anywhere] text-blue-400">{tool}</span>
                  </div>
                  {command && (
                    <div className="mt-2 max-h-32 overflow-y-auto rounded bg-card p-2 font-mono text-xs whitespace-pre-wrap break-words text-muted-foreground [overflow-wrap:anywhere]">
                      {command}
                    </div>
                  )}
                </div>
              </div>
              {parameters !== undefined && (
                <section aria-label="Tool execution parameters" className="min-w-0 max-w-full">
                  <div className="mb-2 text-sm font-medium text-foreground">Tool execution parameters</div>
                  <pre tabIndex={0} className="max-h-64 max-w-full overflow-y-auto rounded-lg border border-border bg-muted p-3 font-mono text-xs whitespace-pre-wrap break-words text-foreground [overflow-wrap:anywhere]">
                    {parameters}
                  </pre>
                </section>
              )}
            </>
          )}

          {confirming && (
            <div className="max-w-full rounded-lg border border-yellow-900/60 bg-yellow-950/20 p-3 text-sm break-words text-foreground [overflow-wrap:anywhere]">
              {confirmExplanation}
            </div>
          )}

          {submitError && (
            <div
              role="alert"
              className="max-w-full rounded-lg border border-red-900/60 bg-red-950/30 p-3 text-sm text-red-200"
            >
              <div className="font-medium">Could not submit decision:</div>
              <div className="mt-1 break-words [overflow-wrap:anywhere]">{submitError}</div>
            </div>
          )}

          {confirming ? (
            <div className="flex flex-wrap justify-end gap-3">
              <Button
                type="button"
                variant="outline"
                onClick={() => setConfirming(null)}
                disabled={loading}
              >
                Back
              </Button>
              <Button
                type="button"
                autoFocus
                onClick={() => void handleResponse(confirming)}
                disabled={loading}
              >
                <Check className="w-4 h-4 mr-2" />
                Confirm
              </Button>
            </div>
          ) : (
            <div className="flex flex-wrap justify-end gap-3">
              <Button
                type="button"
                variant="destructive"
                onClick={() => void handleResponse("deny")}
                disabled={loading}
              >
                <X className="w-4 h-4 mr-2" />
                {isContentAsk ? "Withhold" : "Deny"}
              </Button>
              {canAlwaysRule && (
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => setConfirming("always_rule")}
                  disabled={loading}
                  title="Persist a rule so this exact request is auto-approved in future"
                >
                  Always allow rule
                </Button>
              )}
              {canAlwaysTool && (
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => setConfirming("always_tool")}
                  disabled={loading}
                  title="Persist a tool rule so every use of this tool is auto-approved"
                >
                  Always allow tool
                </Button>
              )}
              <Button
                type="button"
                onClick={() => void handleResponse("allow")}
                disabled={loading}
                data-dialog-default-action
              >
                <Check className="w-4 h-4 mr-2" />
                {isContentAsk ? "Deliver to model" : "Allow once"}
              </Button>
            </div>
          )}
        </div>
      </ScopedDialogContent>
    </ScopedDialog>
  );
}
