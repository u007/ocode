import { useState, type KeyboardEvent, useRef, useEffect } from "react";
import { useChat } from "../../hooks/useChat";
import { Button } from "@/components/ui/button";
import SlashCommandMenu from "./SlashCommandMenu";

interface ChatInputProps {
  /** Called when a slash command is entered. Return true if handled. */
  onSlashCommand?: (command: string) => boolean;
}

export default function ChatInput({ onSlashCommand }: ChatInputProps) {
  const [input, setInput] = useState("");
  const [showSlashMenu, setShowSlashMenu] = useState(false);
  const [slashQuery, setSlashQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);
  // shellInFlight is true while a `!cmd` shell command is being executed by
  // the server. We block new sends during this window so the user can't fire
  // a second message that interleaves with the in-flight shell result — the
  // agent would otherwise answer msg2 first, then re-engage with the shell
  // output of msg1, producing confusing turn ordering.
  const [shellInFlight, setShellInFlight] = useState(false);
  const { sendMessage, executeShell, stop, isStreaming } = useChat();
  const textareaRef = useRef<HTMLTextAreaElement>(null);

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

  const filteredCount = showSlashMenu
    ? [
        "/new",
        "/clear",
        "/model",
        "/compact",
        "/recap",
        "/export",
        "/share",
        "/help",
      ].filter((cmd) => cmd.includes(slashQuery.toLowerCase())).length
    : 0;

  const handleSend = async () => {
    const trimmed = input.trim();
    if (!trimmed || isStreaming || shellInFlight) return;
    setInput("");
    setShowSlashMenu(false);

    // Check if this is a slash command
    if (trimmed.startsWith("/") && onSlashCommand) {
      const handled = onSlashCommand(trimmed);
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

    sendMessage(trimmed);
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
        const commands = [
          "/new",
          "/clear",
          "/model",
          "/compact",
          "/recap",
          "/export",
          "/share",
          "/help",
        ].filter((cmd) => cmd.includes(slashQuery.toLowerCase()));
        if (commands[selectedIndex]) {
          handleSlashSelect(commands[selectedIndex]);
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
      <div className="flex items-end gap-2">
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
