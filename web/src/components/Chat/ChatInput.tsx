import { useState, type KeyboardEvent, useRef, useEffect } from "react";
import { useChat } from "../../hooks/useChat";
import { Button } from "@/components/ui/button";
import SlashCommandMenu from "./SlashCommandMenu";
import { COMMANDS } from "./commands";
import { Paperclip, X } from "lucide-react";
import { apiPath, authHeaders } from "@/api/client";
import EditorContextChip from "./EditorContextChip";

interface ChatInputProps {
  /** Called when a slash command is entered. Return true if handled (async). */
  onSlashCommand?: (command: string) => boolean | Promise<boolean>;
  /** Active editor context (file path + optional selection) to display as a chip
   *  and attach to the outgoing message. When provided, a chip is shown above
   *  the input and the ref is prepended to the message on send. */
  activeEditorContext?: { path: string; selection?: { startLine: number; endLine: number } } | null;
}

export default function ChatInput({ onSlashCommand, activeEditorContext }: ChatInputProps) {
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
  const { sendMessage, executeShell, stop, isStreaming } = useChat();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const attachRef = useRef<HTMLInputElement>(null);

  const handleAttach = async (e: React.ChangeEvent<HTMLInputElement>) => {
    if (!e.target.files) return;
    const fd = new FormData();
    Array.from(e.target.files).forEach((f) => fd.append("file", f));
    try {
      const r = await fetch(apiPath("/api/uploads"), {
        method: "POST",
        headers: authHeaders(),
        body: fd,
      });
      const saved: { name: string }[] = await r.json();
      setAttachedFiles((prev) => [...prev, ...saved.map((f) => f.name)]);
    } catch (err) {
      console.error("upload failed:", err);
    }
    e.target.value = "";
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
    const trimmed = input.trim();
    if (!trimmed || isStreaming || shellInFlight) return;
    setInput("");
    setShowSlashMenu(false);

    // Check if this is a slash command (handler may be async)
    if (trimmed.startsWith("/") && onSlashCommand) {
      const handled = await onSlashCommand(trimmed);
      if (handled) return;
    }

    // Check if this is a shell command (! prefix). The shell call is async
    // and can take many seconds; we hold shellInFlight for the duration so
    // the user can't fire a second message in the gap. sendMessage() below
    // enqueues the result onto the chat stream, which the agent will pick up
    // after the current turn ends (handled by isStreaming in the store).
    if (trimmed.startsWith("!")) {
      const command = trimmed.slice(1).trim();
      if (command) {
        setShellInFlight(true);
        try {
          const result = await executeShell(command);
          // Send the result as a message to the agent for display
          const outputMessage = result.exitCode === 0
            ? `Shell command executed successfully:\n\`\`\`\n${result.output}\n\`\`\``
            : `Shell command failed (exit code ${result.exitCode}):\n\`\`\`\n${result.error || result.output}\n\`\`\``;
          sendMessage(outputMessage);
        } finally {
          setShellInFlight(false);
        }
        return;
      }
    }

    // Build refs: attached files + active editor context
    const refs = attachedFiles.map((n) => `@.ocode/uploads/${n}`).join(" ");
    const contextRef = activeEditorContext
      ? activeEditorContext.selection
        ? `@${activeEditorContext.path}#L${activeEditorContext.selection.startLine}-L${activeEditorContext.selection.endLine}`
        : `@${activeEditorContext.path}`
      : "";

    const parts = [contextRef, refs, trimmed].filter(Boolean);
    const finalMessage = parts.join(" ");
    setAttachedFiles([]);
    sendMessage(finalMessage);
  };

  const handleSlashSelect = (command: string) => {
    setInput(command + " ");
    setShowSlashMenu(false);
    textareaRef.current?.focus();
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
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

    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  return (
    <div className="border-t border-zinc-700 p-4 relative">
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
            className="inline-flex items-center gap-1 text-xs bg-zinc-700 text-zinc-300 rounded px-2 py-0.5"
          >
            {name}
            <button
              type="button"
              onClick={() => setAttachedFiles((prev) => prev.filter((n) => n !== name))}
              className="text-zinc-500 hover:text-zinc-300"
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
          className="shrink-0 p-1.5 rounded text-zinc-500 hover:text-zinc-300 hover:bg-zinc-700"
          title="Attach files"
        >
          <Paperclip className="w-4 h-4" />
        </button>
        <textarea
          ref={textareaRef}
          className="flex-1 resize-none rounded-lg border border-zinc-600 bg-zinc-800 p-3 text-sm text-zinc-100 placeholder-zinc-500 focus:border-blue-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-900"
          rows={2}
          placeholder="Type a message... (Enter to send, Shift+Enter for newline, / for commands, ! for shell)"
          value={input}
          onChange={(e) => setInput(e.target.value)}
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
