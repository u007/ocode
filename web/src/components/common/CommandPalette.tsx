import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { useRef, useState, useMemo } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useCommands } from "../Chat/commands";

interface Props {
  open: boolean;
  onClose: () => void;
  onExecute: (command: string) => void;
}

export default function CommandPalette({ open, onClose, onExecute }: Props) {
  const commands = useCommands();
  const [query, setQuery] = useState("");
  const scrollRef = useRef<HTMLDivElement>(null);
  const filtered = useMemo(() => {
    if (!query.trim()) return commands;
    const q = query.toLowerCase();
    return commands.filter((c) => c.name.toLowerCase().includes(q) || c.description.toLowerCase().includes(q));
  }, [commands, query]);
  const virtualizer = useVirtualizer({
    count: filtered.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 42,
    overscan: 8,
  });
  return (
    <CommandDialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <CommandInput placeholder="Type a command..." value={query} onValueChange={setQuery} />
      <CommandList ref={scrollRef}>
        <CommandEmpty>No commands found</CommandEmpty>
        <CommandGroup heading="Commands">
          <div
            style={{ height: virtualizer.getTotalSize(), width: "100%", position: "relative" }}
          >
            {virtualizer.getVirtualItems().map((item) => {
              const cmd = filtered[item.index];
              return (
                <div
                  key={item.key}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    transform: `translateY(${item.start}px)`,
                    height: item.size,
                  }}
                >
                  <CommandItem
                    value={cmd.name}
                    onSelect={() => {
                      onExecute(cmd.name);
                      onClose();
                    }}
                  >
                    <span className="font-mono text-blue-400">{cmd.name}</span>
                    <span className="ml-2 text-muted-foreground">
                      {cmd.description}
                    </span>
                  </CommandItem>
                </div>
              );
            })}
          </div>
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
