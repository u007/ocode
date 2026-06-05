import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";

interface Props {
  open: boolean;
  onClose: () => void;
  onExecute: (command: string) => void;
}

const COMMANDS = [
  { name: "/clear", description: "Clear chat history" },
  { name: "/model", description: "Switch model" },
  { name: "/compact", description: "Compact conversation context" },
  { name: "/recap", description: "Generate session recap" },
  { name: "/export", description: "Export session as JSON" },
  { name: "/share", description: "Share session link" },
  { name: "/session", description: "Switch session" },
  { name: "/help", description: "Show help" },
];

export default function CommandPalette({ open, onClose, onExecute }: Props) {
  return (
    <CommandDialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <CommandInput placeholder="Type a command..." />
      <CommandList>
        <CommandEmpty>No commands found</CommandEmpty>
        <CommandGroup heading="Commands">
          {COMMANDS.map((cmd) => (
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
