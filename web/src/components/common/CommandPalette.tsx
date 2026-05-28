import { useState, useEffect, useRef } from "react";

interface Props {
  open: boolean;
  onClose: () => void;
  onExecute: (command: string) => void;
}

const COMMANDS = [
  { name: "/clear", description: "Clear chat history" },
  { name: "/model", description: "Switch model" },
  { name: "/session", description: "Switch session" },
  { name: "/help", description: "Show help" },
];

export default function CommandPalette({ open, onClose, onExecute }: Props) {
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  const filtered = COMMANDS.filter((c) =>
    c.name.toLowerCase().includes(query.toLowerCase())
  );

  useEffect(() => {
    if (open) {
      setQuery("");
      setSelected(0);
      inputRef.current?.focus();
    }
  }, [open]);

  useEffect(() => {
    setSelected(0);
  }, [query]);

  if (!open) return null;

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      onClose();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelected((s) => Math.min(s + 1, filtered.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelected((s) => Math.max(s - 1, 0));
    } else if (e.key === "Enter" && filtered[selected]) {
      onExecute(filtered[selected].name);
      onClose();
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[20vh] bg-black/50">
      <div className="w-full max-w-md rounded-lg border border-zinc-700 bg-zinc-900 shadow-xl">
        <input
          ref={inputRef}
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Type a command..."
          className="w-full border-b border-zinc-700 bg-transparent px-4 py-3 text-sm text-zinc-100 placeholder-zinc-500 focus:outline-none"
        />
        <div className="max-h-60 overflow-y-auto">
          {filtered.map((cmd, i) => (
            <button
              key={cmd.name}
              onClick={() => { onExecute(cmd.name); onClose(); }}
              className={`w-full px-4 py-2 text-left text-sm ${
                i === selected ? "bg-zinc-800 text-white" : "text-zinc-400"
              }`}
            >
              <span className="font-mono text-blue-400">{cmd.name}</span>
              <span className="ml-2 text-zinc-500">{cmd.description}</span>
            </button>
          ))}
          {filtered.length === 0 && (
            <div className="px-4 py-3 text-sm text-zinc-500">No commands found</div>
          )}
        </div>
      </div>
    </div>
  );
}
