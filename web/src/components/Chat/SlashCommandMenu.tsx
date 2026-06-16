import { useEffect, useRef } from "react";
import {
  Trash2,
  Settings,
  Archive,
  FileText,
  Share2,
  HelpCircle,
  FileDown,
  Plus,
} from "lucide-react";

interface Command {
  name: string;
  description: string;
  icon: React.ElementType;
}

const COMMANDS: Command[] = [
  { name: "/new", description: "Start a new session", icon: Plus },
  { name: "/clear", description: "Clear conversation history", icon: Trash2 },
  { name: "/model", description: "Open model selector", icon: Settings },
  { name: "/compact", description: "Compact conversation context", icon: Archive },
  { name: "/recap", description: "Generate session recap", icon: FileText },
  { name: "/export", description: "Export session as JSON", icon: FileDown },
  { name: "/share", description: "Share session link", icon: Share2 },
  { name: "/help", description: "Show available commands", icon: HelpCircle },
];

interface Props {
  query: string;
  selectedIndex: number;
  onSelect: (command: string) => void;
  onHover: (index: number) => void;
}

export default function SlashCommandMenu({ query, selectedIndex, onSelect, onHover }: Props) {
  const menuRef = useRef<HTMLDivElement>(null);

  const filtered = COMMANDS.filter((cmd) =>
    cmd.name.toLowerCase().includes(query.toLowerCase())
  );

  useEffect(() => {
    if (menuRef.current) {
      const selected = menuRef.current.children[selectedIndex] as HTMLElement;
      selected?.scrollIntoView({ block: "nearest" });
    }
  }, [selectedIndex]);

  if (filtered.length === 0) return null;

  return (
    <div
      ref={menuRef}
      role="listbox"
      className="absolute bottom-full left-0 right-0 mb-2 max-h-60 overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 shadow-xl z-50"
    >
      {filtered.map((cmd, i) => {
        const Icon = cmd.icon;
        const isSelected = i === selectedIndex;
        return (
          <button
            key={cmd.name}
            type="button"
            role="option"
            aria-selected={isSelected}
            className={`w-full flex items-center gap-3 px-4 py-2.5 text-left text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-900 ${
              isSelected
                ? "bg-zinc-700 text-white"
                : "text-zinc-300 hover:bg-zinc-800"
            }`}
            onMouseEnter={() => onHover(i)}
            onClick={() => onSelect(cmd.name)}
          >
            <Icon className="w-4 h-4 text-zinc-500 flex-shrink-0" />
            <div className="flex-1 min-w-0">
              <div className="font-mono text-blue-400">{cmd.name}</div>
              <div className="text-xs text-zinc-500 truncate">{cmd.description}</div>
            </div>
          </button>
        );
      })}
    </div>
  );
}
