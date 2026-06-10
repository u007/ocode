import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Message } from "../../api/types";
import {
  rehypeFileLinks,
  FileLinkFromNode,
  linkifyPlainText,
} from "../../lib/fileLinks";
import { ThinkingBlock, ToolBlock } from "./TurnParts";

interface Props {
  message: Message;
}

// AssistantText renders markdown assistant output. Shared by committed messages
// and the live text stream so rendering stays consistent.
export function AssistantText({ content }: { content: string }) {
  return (
    <div className="flex justify-start mb-3">
      <div className="max-w-[80%] rounded-lg px-4 py-2 bg-zinc-800 text-zinc-100">
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
                return (
                  <code className={className} {...props}>
                    {children}
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

export default function MessageBubble({ message }: Props) {
  // Tool result message (role "tool"): no tool name is carried, only the output.
  if (message.role === "tool") {
    return <ToolBlock tool="result" output={message.content} />;
  }

  // Assistant turn that issued tool calls and/or carried reasoning.
  if (
    message.role === "assistant" &&
    (message.tool_calls?.length || message.reasoning_content)
  ) {
    return (
      <>
        {message.reasoning_content ? (
          <ThinkingBlock text={message.reasoning_content} />
        ) : null}
        {message.tool_calls?.map((tc, i) => (
          <ToolBlock
            key={i}
            tool={tc.function.name}
            command={tc.function.arguments}
            output=""
          />
        ))}
        {message.content ? <AssistantText content={message.content} /> : null}
      </>
    );
  }

  if (message.role === "user") {
    return (
      <div className="flex justify-end mb-3">
        <div className="max-w-[80%] rounded-lg px-4 py-2 bg-blue-600 text-white">
          <pre className="whitespace-pre-wrap font-sans text-sm">
            {linkifyPlainText(message.content)}
          </pre>
        </div>
      </div>
    );
  }

  return <AssistantText content={message.content} />;
}
