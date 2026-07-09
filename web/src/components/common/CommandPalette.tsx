import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { useCommands } from "../Chat/commands";

interface Props {
  open: boolean;
  onClose: () => void;
  onExecute: (command: string) => void;
}

export default function CommandPalette({ open, onClose, onExecute }: Props) {
  const commands = useCommands();
  return (
    <CommandDialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <CommandInput placeholder="Type a command..." />
      <CommandList>
        <CommandEmpty>No commands found</CommandEmpty>
        <CommandGroup heading="Commands">
          {commands.map((cmd) => (
            <CommandItem
              key={cmd.name}
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
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
