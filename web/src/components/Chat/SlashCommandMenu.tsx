import { useEffect, useRef } from "react";
import { useCommands } from "./commands";

interface Props {
  query: string;
  selectedIndex: number;
  onSelect: (command: string) => void;
  onHover: (index: number) => void;
}

export default function SlashCommandMenu({ query, selectedIndex, onSelect, onHover }: Props) {
  const menuRef = useRef<HTMLDivElement>(null);
  const commands = useCommands();

  const filtered = commands.filter((cmd) =>
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
      className="absolute bottom-full left-0 right-0 mb-2 max-h-60 overflow-y-auto rounded-lg border border-border bg-card shadow-xl z-50"
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
            className={`w-full flex items-center gap-3 px-4 py-2.5 text-left text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background ${
              isSelected
                ? "bg-accent text-accent-foreground"
                : "text-foreground hover:bg-muted"
            }`}
            onMouseEnter={() => onHover(i)}
            onClick={() => onSelect(cmd.name)}
          >
            <Icon className="w-4 h-4 text-muted-foreground flex-shrink-0" />
            <div className="flex-1 min-w-0">
              <div className="font-mono text-blue-400">{cmd.name}</div>
              <div className="text-xs text-muted-foreground truncate">{cmd.description}</div>
            </div>
          </button>
        );
      })}
    </div>
  );
}
