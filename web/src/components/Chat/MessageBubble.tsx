import { memo, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Message } from "../../api/types";
import {
  rehypeFileLinks,
  FileLinkFromNode,
  linkifyPlainText,
} from "../../lib/fileLinks";
import { ThinkingBlock, ToolBlock } from "./TurnParts";
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
}

// AssistantText renders markdown assistant output. Shared by committed messages
// and the live text stream so rendering stays consistent.
export function AssistantText({ content }: { content: string }) {
  return (
    <div className="flex justify-start mb-3">
      <div className="max-w-[95%] md:max-w-[80%] rounded-lg px-4 py-2 bg-zinc-800 text-zinc-100">
        <div className="prose prose-invert prose-sm max-w-none text-sm">
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            rehypePlugins={[rehypeFileLinks]}
            components={{
              // @ts-expect-error custom hast element produced by rehypeFileLinks
              filelink: FileLinkFromNode,
              pre: ({ children }) => (
                <pre className="rounded-md bg-zinc-900 p-3 overflow-x-auto text-xs">
                  {children}
                </pre>
              ),
              code: ({ className, children, ...props }) => {
                const isInline = !className;
                if (isInline) {
                  return (
                    <code
                      className="rounded bg-zinc-700 px-1.5 py-0.5 text-xs"
                      {...props}
                    >
                      {children}
                    </code>
                  );
                }
                // Fenced block: react-markdown puts the fence tag in the
                // className as `language-xxx`, and appends a trailing newline
                // that would render as a blank last line once highlighted.
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
              ul: ({ children }) => (
                <ul className="list-disc pl-4 mb-2">{children}</ul>
              ),
              ol: ({ children }) => (
                <ol className="list-decimal pl-4 mb-2">{children}</ol>
              ),
              li: ({ children }) => <li className="mb-1">{children}</li>,
              h1: ({ children }) => (
                <h1 className="text-lg font-bold mb-2">{children}</h1>
              ),
              h2: ({ children }) => (
                <h2 className="text-base font-bold mb-2">{children}</h2>
              ),
              h3: ({ children }) => (
                <h3 className="text-sm font-bold mb-2">{children}</h3>
              ),
              blockquote: ({ children }) => (
                <blockquote className="border-l-4 border-zinc-600 pl-3 italic text-zinc-400 mb-2">
                  {children}
                </blockquote>
              ),
              a: ({ href, children }) => (
                <a
                  href={href}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-blue-400 hover:underline"
                >
                  {children}
                </a>
              ),
              table: ({ children }) => (
                <div className="overflow-x-auto mb-2">
                  <table className="border-collapse text-xs">{children}</table>
                </div>
              ),
              th: ({ children }) => (
                <th className="border border-zinc-600 px-2 py-1 text-left bg-zinc-700">
                  {children}
                </th>
              ),
              td: ({ children }) => (
                <td className="border border-zinc-600 px-2 py-1">{children}</td>
              ),
              hr: () => <hr className="border-zinc-600 my-3" />,
              strong: ({ children }) => (
                <strong className="font-bold">{children}</strong>
              ),
              em: ({ children }) => <em className="italic">{children}</em>,
            }}
          >
            {content}
          </ReactMarkdown>
        </div>
      </div>
    </div>
  );
}

function MessageBubble({ message, highlight = "", toolName = "", sessionId }: Props) {
  // Tool result message (role "tool"): no tool name is carried on the message
  // itself, only tool_call_id — the caller resolves toolName from that.
  if (message.role === "tool") {
    return (
      <ToolBlock
        tool={toolName || "result"}
        output={message.content}
        highlight={highlight}
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
          <ThinkingBlock text={message.reasoning_content} highlight={highlight} />
        ) : null}
        {message.tool_calls?.map((tc, i) => (
          <ToolBlock
            key={i}
            tool={tc.function.name}
            command={tc.function.arguments}
            output=""
            highlight={highlight}
          />
        ))}
        {message.content ? <AssistantText content={message.content} /> : null}
      </>
    );
  }

  if (message.role === "user") {
    return (
      <UserBubble message={message} highlight={highlight} sessionId={sessionId} />
    );
  }

  return <AssistantText content={message.content} />;
}

function UserBubble({
  message,
  highlight,
  sessionId,
}: {
  message: Message;
  highlight: string;
  sessionId?: string;
}) {
  const [confirmOpen, setConfirmOpen] = useState(false);

  const handleConfirm = () => {
    if (!sessionId) return;
    dispatchRestore(sessionId, message.content);
    setConfirmOpen(false);
  };

  return (
    <>
      <div className="group flex justify-end mb-3 gap-1 items-start">
        <button
          type="button"
          aria-label="Restore to input"
          title="Restore to input"
          onClick={() => setConfirmOpen(true)}
          className="opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 shrink-0 mt-1 p-1.5 rounded text-zinc-400 hover:text-zinc-100 hover:bg-zinc-700 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring transition-opacity"
        >
          <RotateCcw className="w-3.5 h-3.5" />
        </button>
        <div className="max-w-[95%] md:max-w-[80%] rounded-lg px-4 py-2 bg-blue-600 text-white">
          <pre className="whitespace-pre-wrap font-sans text-sm">
            {highlight.trim()
              ? highlightMatches(message.content, highlight)
              : linkifyPlainText(message.content)}
          </pre>
        </div>
      </div>

      <Dialog open={confirmOpen} onOpenChange={(o) => !o && setConfirmOpen(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Restore message to input?</DialogTitle>
            <DialogDescription>
              This will replace the current draft in the input field with the selected message. You can edit it before sending.
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-40 overflow-auto rounded bg-zinc-800 p-3 text-sm text-zinc-200 whitespace-pre-wrap">
            {message.content}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleConfirm}>Restore</Button>
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
