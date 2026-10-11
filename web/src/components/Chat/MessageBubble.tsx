import { createContext, memo, useContext, useRef, useState } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Message } from "../../api/types";
import {
  rehypeFileLinks,
  FileLinkFromNode,
  linkifyPlainText,
} from "../../lib/fileLinks";
import { ThinkingBlock, ToolBlock } from "./TurnParts";
import CompactionNotice, { isCompactionSummary } from "./CompactionNotice";
import { renderedSpeechText } from "../Speech/speechUtils";
import { highlightMatches } from "./ChatSearchBar";
import HighlightedCode from "./HighlightedCode";
import { dispatchRestore } from "../../lib/inputRestore";
import { RotateCcw } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import { Button } from "../ui/button";
import MarkdownLink from "../common/MarkdownLink";
import { requestSpeech } from "../Speech/SpeechProvider";
import { SpeakButton, type SpeakAction } from "../Speech/SpeakButton";
import { renderedCopyText } from "../../lib/copyText";
import { BlockCopyControl } from "./BlockCopyControl";

interface Props {
  message: Message;
  // Active find-bar query. When set, plaintext regions (user text, thinking,
  // tool args/output) wrap matches in <mark>. Empty string = no highlight.
  highlight?: string;
  // Name of the tool that produced this message, resolved by the caller from
  // the preceding assistant message's tool_calls (role "tool" messages carry
  // only tool_call_id, not the name). Used to pick a syntax-highlight language.
  toolName?: string;
  /** Session/tab id that owns this message — required for restore dispatch validation. */
  sessionId?: string;
  /** Local transcript position used only to build a stable fallback key. */
  messageIndex?: number;
  /** Absolute server index for the durable rewind resource. */
  restoreTargetIndex?: number;
  /** Stable identity supplied by the virtualized transcript renderer. */
  entryKey?: string;
  /** The last thinking block in the latest assistant turn stays expanded. */
  isLatestThinking?: boolean;
  /** Search selection temporarily opens disclosure regions. */
  forceOpen?: boolean;
}

// hasRenderableText reports whether an assistant content string will actually
// produce visible output. Thinking models (e.g. DeepSeek) emit bare newlines
// ("\n\n") as the content of a tool-calling step while the real prose lives in
// `reasoning_content` and the final answer; react-markdown renders those as an
// empty document, so guarding on truthiness alone left a blank `bg-muted`
// bubble carrying nothing but its "Speak" button. Every AssistantText render
// site (committed + live, grouped + single) must gate on this instead.
export function hasRenderableText(content: string | undefined | null): boolean {
  return !!content && content.trim().length > 0;
}

/**
 * A chat message body can be enormous (e.g. a standup prompt pasted with full
 * commit diffs, or an attached sheet). Rendering the whole string as one text
 * node forces the browser to lay out every character on EVERY reflow — measured
 * ~390 ms for 1.4 MB versus ~14 ms for a few KB. Bound the DEFAULT render of an
 * oversized message; the full text stays reachable via the expander and the
 * copy button, and the stored transcript is never modified (search, restore and
 * copy still use the full content).
 */
export const OVERSIZED_MESSAGE_CHARS = 20_000;

// --- Markdown rendering configuration ---------------------------------------
// These MUST be module-scope constants, not inline JSX props. React compares
// element types by identity, so defining `components` (or the plugin arrays)
// inside AssistantText created brand-new component functions on every render;
// react-markdown's `pre`/`code`/`p`/… elements then changed TYPE and React
// unmounted and remounted the whole rendered subtree. AssistantText re-renders
// on every streaming delta and on every ChatPanel render (both the virtualized
// rows and the live tail render it directly), and a remount destroys any
// in-progress text selection — the transcript "flickered to other places" while
// the user dragged. Stable identities let React reconcile the DOM in place.
const BlockCodeContext = createContext(false);

const MARKDOWN_REMARK_PLUGINS = [remarkGfm];
const MARKDOWN_REHYPE_PLUGINS = [rehypeFileLinks];

const MARKDOWN_COMPONENTS: Components = {
  // Custom hast element emitted by rehypeFileLinks — not a key of Components.
  // @ts-expect-error filelink is produced by our rehype plugin
  filelink: FileLinkFromNode,
  // The `pre` override marks its whole subtree as a fenced (block) code region.
  // Inline code never passes through `pre`, so its `code` sees the default
  // (false) and renders the inline chip.
  pre: ({ children }) => (
    <pre className="rounded-md bg-card p-3 overflow-x-auto text-xs">
      <BlockCodeContext.Provider value={true}>
        {children}
      </BlockCodeContext.Provider>
    </pre>
  ),
  code: ({ className, children, node: _node, ...props }) => {
    // A fenced block with no info string gets NO `className` from react-markdown,
    // so the old `!className` test misclassified it as inline and wrapped the
    // entire block in the inline chip: its border painted on every line fragment
    // (reading as an underline per line), inline padding, and no highlighting.
    const inBlock = useContext(BlockCodeContext);
    if (!inBlock) {
      return (
        <code
          // Same surface as fenced blocks: bg-card is the page background, so
          // chips read as a dark inset on dark themes (bg-accent is the
          // palette's highlight colour — solid white on github-dark). text-link
          // is contrast-checked against background, so file links keep it.
          // before/after:content-none override @tailwindcss/typography's default
          // of wrapping inline code in literal backticks — redundant now that
          // the chip has its own border.
          className="rounded border border-border bg-card text-foreground px-1.5 py-0.5 text-xs before:content-none after:content-none [&_.file-link]:underline"
          {...props}
        >
          {children}
        </code>
      );
    }
    // Fenced block: react-markdown puts the fence tag in the className as
    // `language-xxx`, and appends a trailing newline that would render as a
    // blank last line once highlighted.
    const lang = /language-([\w+-]+)/.exec(className ?? "")?.[1] ?? "";
    return (
      <code className={className} {...props}>
        <HighlightedCode
          code={String(children).replace(/\n$/, "")}
          lang={lang}
        />
      </code>
    );
  },
  p: ({ children }) => <p className="mb-2 last:mb-0">{children}</p>,
  ul: ({ children }) => <ul className="list-disc pl-4 mb-2">{children}</ul>,
  ol: ({ children }) => <ol className="list-decimal pl-4 mb-2">{children}</ol>,
  li: ({ children }) => <li className="mb-1">{children}</li>,
  h1: ({ children }) => <h1 className="text-lg font-bold mb-2">{children}</h1>,
  h2: ({ children }) => (
    <h2 className="text-base font-bold mb-2">{children}</h2>
  ),
  h3: ({ children }) => <h3 className="text-sm font-bold mb-2">{children}</h3>,
  blockquote: ({ children }) => (
    <blockquote className="border-l-4 border-border pl-3 italic text-muted-foreground mb-2">
      {children}
    </blockquote>
  ),
  a: (props) => (
    <MarkdownLink className="text-link hover:underline" {...props} />
  ),
  table: ({ children }) => (
    <div className="overflow-x-auto mb-2">
      <table className="border-collapse text-xs">{children}</table>
    </div>
  ),
  th: ({ children }) => (
    <th className="border border-border px-2 py-1 text-left font-semibold bg-card text-foreground [&_.file-link]:underline">
      {children}
    </th>
  ),
  td: ({ children }) => (
    <td className="border border-border px-2 py-1">{children}</td>
  ),
  hr: () => <hr className="border-border my-3" />,
  strong: ({ children }) => <strong className="font-bold">{children}</strong>,
  em: ({ children }) => <em className="italic">{children}</em>,
};

// AssistantText renders markdown assistant output. Shared by committed messages
// and the live text stream so rendering stays consistent.
//
// `onSpeak` receives the RENDERED text of this block (extracted from the DOM in
// `speechRef`), never the raw markdown `content`: speaking the source would read
// heading hashes, `**` markers, backticks and link targets aloud.
export function AssistantText({
  content,
  onSpeak,
}: {
  content: string;
  onSpeak?: SpeakAction;
}) {
  const speechRef = useRef<HTMLDivElement>(null);
  return (
    <div className="flex justify-start mb-3">
      <div className="max-w-[95%] md:max-w-[80%] rounded-lg px-4 py-2 bg-muted text-foreground">
        <div
          ref={speechRef}
          data-speech-content=""
          data-copy-content=""
          className="relative prose prose-invert prose-sm max-w-none text-sm"
        >
          <ReactMarkdown
            remarkPlugins={MARKDOWN_REMARK_PLUGINS}
            rehypePlugins={MARKDOWN_REHYPE_PLUGINS}
            components={MARKDOWN_COMPONENTS}
          >
            {content}
          </ReactMarkdown>
        </div>
        {onSpeak && (
          // Rendered by a sibling of the `.prose` block, not inside it: the
          // extractor reads `[data-speech-content]`, so a control inside that
          // subtree would have its own label ("Speak") read aloud. The button
          // also opts out explicitly for any container-wide extraction.
          // SpeakButton disables itself and shows a spinner while the request
          // is prepared, and re-enables with an inline error if it fails.
          <SpeakButton
            getText={() => renderedSpeechText(speechRef.current)}
            onSpeak={onSpeak}
            ariaLabel="Speak message"
            title="Speak message"
            className="mt-2 mr-2 inline-flex items-center gap-1 rounded px-1.5 py-1 text-xs text-muted-foreground hover:bg-background hover:text-foreground disabled:cursor-not-allowed disabled:opacity-60"
          />
        )}
        <BlockCopyControl
          rawText={content}
          getRenderedText={() => renderedCopyText(speechRef.current)}
          className="mt-2"
        />
      </div>
    </div>
  );
}

function MessageBubble({
  message,
  highlight = "",
  toolName = "",
  sessionId,
  messageIndex,
  restoreTargetIndex,
  entryKey,
  isLatestThinking = false,
  forceOpen = false,
}: Props) {
  // ChatPanel supplies a stable object identity. The fallback keeps isolated
  // component tests and legacy callers deterministic without using a row index
  // in the production transcript path.
  const baseKey =
    entryKey ??
    `${message.role}:${messageIndex ?? 0}:${message.content.slice(0, 32)}`;
  // Synthetic compaction summary (spliced by the agent): render it as a
  // dedicated inline notice rather than the raw `[ocode:compaction-summary]`
  // marker. This is the durable "compact notice" — the composer's transient
  // status bar is not retained after completion.
  if (isCompactionSummary(message.content)) {
    return <CompactionNotice content={message.content} />;
  }

  // Tool result message (role "tool"): no tool name is carried on the message
  // itself, only tool_call_id — the caller resolves toolName from that.
  if (message.role === "tool") {
    return (
      <ToolBlock
        tool={toolName || "result"}
        output={message.content}
        highlight={highlight}
        callKey={`${baseKey}:call:${message.tool_call_id ?? "result"}`}
        outputKey={`${baseKey}:output:${message.tool_call_id ?? "result"}`}
        forceOpen={forceOpen}
      />
    );
  }

  // Assistant turn that issued tool calls and/or carried reasoning.
  if (
    message.role === "assistant" &&
    (message.tool_calls?.length || message.reasoning_content)
  ) {
    return (
      <>
        {message.reasoning_content ? (
          <ThinkingBlock
            text={message.reasoning_content}
            highlight={highlight}
            onSpeak={() => requestSpeech(message.reasoning_content || "")}
            blockKey={`${baseKey}:thinking`}
            isLatest={isLatestThinking}
            forceOpen={forceOpen}
          />
        ) : null}
        {message.tool_calls?.map((tc) => (
          <ToolBlock
            key={tc.id}
            tool={tc.function.name}
            command={tc.function.arguments}
            output=""
            highlight={highlight}
            callKey={`${baseKey}:call:${tc.id}`}
            outputKey={`${baseKey}:output:${tc.id}`}
            forceOpen={forceOpen}
          />
        ))}
        {hasRenderableText(message.content) ? (
          <AssistantText content={message.content} onSpeak={requestSpeech} />
        ) : null}
      </>
    );
  }

  if (message.role === "user") {
    return (
      <UserBubble
        message={message}
        highlight={highlight}
        sessionId={sessionId}
        restoreTargetIndex={restoreTargetIndex}
      />
    );
  }

  if (!hasRenderableText(message.content)) return null;

  return <AssistantText content={message.content} onSpeak={requestSpeech} />;
}

function UserBubble({
  message,
  highlight,
  sessionId,
  restoreTargetIndex,
}: {
  message: Message;
  highlight: string;
  sessionId?: string;
  restoreTargetIndex?: number;
}) {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [restoreError, setRestoreError] = useState<string | null>(null);
  // Oversized bodies render a bounded prefix by default (see
  // OVERSIZED_MESSAGE_CHARS); the expander reveals the full text. The full
  // content is still what copy/restore use.
  const [showFull, setShowFull] = useState(false);
  const oversized = message.content.length > OVERSIZED_MESSAGE_CHARS;
  const shownContent =
    oversized && !showFull
      ? message.content.slice(0, OVERSIZED_MESSAGE_CHARS)
      : message.content;

  const handleConfirm = () => {
    if (!sessionId || restoreTargetIndex === undefined) {
      setConfirmOpen(false);
      setRestoreError(
        "This message cannot be restored because its full-transcript position is unknown. Reload the chat and try again.",
      );
      return;
    }
    dispatchRestore({
      sessionId,
      text: message.content,
      targetIndex: restoreTargetIndex,
      userSeq: message.user_seq || undefined,
    });
    setConfirmOpen(false);
    setRestoreError(null);
  };

  return (
    <>
      <div className="group flex justify-end mb-3 gap-1 items-start">
        <button
          type="button"
          aria-label="Restore to input"
          title="Restore to input"
          onClick={() => setConfirmOpen(true)}
          className="opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 shrink-0 mt-1 p-1.5 rounded text-muted-foreground hover:text-accent-foreground hover:bg-accent focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring transition-opacity"
        >
          <RotateCcw className="w-3.5 h-3.5" />
        </button>
        <div className="max-w-[95%] md:max-w-[80%] rounded-lg px-4 py-2 bg-primary text-primary-foreground [&_.file-link]:text-primary-foreground [&_.file-link]:underline">
          <pre className="whitespace-pre-wrap font-sans text-sm">
            {highlight.trim()
              ? highlightMatches(shownContent, highlight)
              : linkifyPlainText(shownContent)}
          </pre>
          {oversized && (
            <button
              type="button"
              data-testid="expand-oversized-message"
              onClick={() => setShowFull((v) => !v)}
              className="mt-1 block text-xs underline opacity-80 hover:opacity-100"
            >
              {showFull
                ? "Show less"
                : `Show full message (${Math.round(message.content.length / 1024)} KB)`}
            </button>
          )}
        </div>
        <BlockCopyControl
          rawText={message.content}
          getRenderedText={() => message.content}
          className="mt-1 shrink-0"
        />
      </div>
      {restoreError && (
        <div
          role="alert"
          className="mb-3 rounded border border-destructive/50 bg-destructive/10 px-3 py-2 text-xs text-destructive"
        >
          {restoreError}
        </div>
      )}

      <Dialog
        open={confirmOpen}
        onOpenChange={(o) => !o && setConfirmOpen(false)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Restore message to input?</DialogTitle>
            <DialogDescription>
              This replaces the current draft with this message. The selected
              message and following history stay in history until you send the
              restored draft.
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-40 overflow-auto rounded bg-muted p-3 text-sm text-foreground whitespace-pre-wrap">
            {message.content}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleConfirm} data-dialog-default-action>
              Restore
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

// Historical messages don't change once committed, but every LIVE_DELTA
// dispatched during streaming (thinking tokens included) creates a new
// ChatPanel render pass. Without memo, each of those re-runs ReactMarkdown
// for every prior bubble in the session — the CPU cost that made "thinking"
// streams (many more, smaller deltas than plain text) visibly spike CPU.
export default memo(MessageBubble);
