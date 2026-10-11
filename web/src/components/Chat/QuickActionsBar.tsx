import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

/** One entry in the composer's quick-actions strip. */
export interface QuickActionItem {
  /** Stable id dispatched back through `onSelect`. */
  id: string;
  /** Visible pill text. */
  label: string;
  /** Leading lucide icon. */
  icon: LucideIcon;
  /** Tooltip + accessible name. Also doubles as the keyboard hint. */
  title: string;
  /** Render dimmed and non-interactive (e.g. Compact while one is running). */
  disabled?: boolean;
}

interface Props {
  actions: QuickActionItem[];
  onSelect: (id: string) => void;
}

/**
 * QuickActionsBar — the quick-action strip rendered directly below the
 * composer's send row. Purely presentational: the parent owns what each
 * action does (ChatInput routes them through the same dispatch/queue
 * pipeline a typed message or slash command uses) and whether the strip is
 * shown at all (ChatInput hides it until the session has conversation
 * content — see `hasConversation`).
 *
 * Deliberately not a dropdown: the actions are one click away, so the strip
 * keeps the common mid-conversation nudges out of the slash-command
 * autocomplete path.
 *
 * Which actions appear, in what order, and what each one does is entirely the
 * caller's business. It used to be a hardcoded trio (compact / continue /
 * recap) built in ChatInput; it is now the user's configured chips, read from
 * the quick_actions store. This component stayed unchanged and stays dumb: it
 * renders whatever `actions` it is handed and reports an id. Derived
 * presentation state -- dimming while a compaction runs, hiding chips that need
 * conversation history -- is decided by the caller before the items get here,
 * because it needs data this component does not have.
 */
export default function QuickActionsBar({ actions, onSelect }: Props) {
  return (
    <div
      role="toolbar"
      aria-label="Quick actions"
      className="mt-2 flex flex-wrap items-center gap-1"
    >
      {actions.map((action) => {
        const Icon = action.icon;
        return (
          <button
            key={action.id}
            type="button"
            onClick={() => onSelect(action.id)}
            disabled={action.disabled}
            title={action.title}
            aria-label={action.title}
            className={cn(
              "inline-flex items-center gap-1 rounded-full border border-border px-2.5 py-0.5 text-xs text-muted-foreground transition-colors",
              "hover:bg-accent hover:text-accent-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
              action.disabled &&
                "cursor-not-allowed opacity-50 hover:bg-transparent hover:text-muted-foreground"
            )}
          >
            <Icon className="w-3 h-3" />
            {action.label}
          </button>
        );
      })}
    </div>
  );
}
