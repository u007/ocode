import { Menu, Minimize2 } from "lucide-react";

/**
 * Header strip above the chat column while reader mode is on. The menu button
 * brings the sessions sidebar back, and the minimize button leaves reader mode.
 */
export function ReaderChatHeader({
  onToggleSidebar,
  onExit,
}: {
  onToggleSidebar: () => void;
  onExit: () => void;
}) {
  return (
    <div className="shrink-0 h-8 flex items-center border-b border-border px-2 gap-1">
      <button
        type="button"
        onClick={onToggleSidebar}
        title="Show sessions sidebar"
        aria-label="Show sessions sidebar"
        className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
      >
        <Menu className="w-3.5 h-3.5" />
      </button>
      <span className="min-w-0 flex-1 truncate text-xs font-medium text-muted-foreground">Chat</span>
      <button
        type="button"
        onClick={onExit}
        title="Exit reader mode"
        aria-label="Exit reader mode"
        className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
      >
        <Minimize2 className="w-3.5 h-3.5" />
      </button>
    </div>
  );
}
