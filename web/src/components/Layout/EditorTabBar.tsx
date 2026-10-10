import { useRef } from "react";
import { Check, FileCode, FolderTree, X } from "lucide-react";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu";

export interface EditorTabInfo {
  id: string;
  path: string;
  isDirty?: boolean;
  /** When true (default) this opened file is attached to the chat/LLM loop. */
  includeInContext?: boolean;
  /** Root this tab's path is relative to (or lives under). Needed to locate the
   *  file in the file tree when it is not the project's own root. */
  projectRoot?: string;
}

interface Props {
  editorTabs: EditorTabInfo[];
  activeEditorTabId: string | null;
  onSelectTab: (id: string) => void;
  onCloseTab: (id: string) => void;
  onToggleInclude?: (id: string) => void;
  /** Expand + scroll the file tree to this tab's file. Omitted when the tree
   *  cannot be driven (no Files tab wiring), which hides the menu item. */
  onRevealInTree?: (tab: EditorTabInfo) => void;
}

function fileNameFromPath(path: string): string {
  return path.split("/").pop() || path;
}

export default function EditorTabBar({
  editorTabs,
  activeEditorTabId,
  onSelectTab,
  onCloseTab,
  onToggleInclude,
  onRevealInTree,
}: Props) {
  if (editorTabs.length === 0) return null;

  const scrollRef = useRef<HTMLDivElement>(null);
  const handleWheel = (e: React.WheelEvent<HTMLDivElement>) => {
    const el = scrollRef.current;
    if (!el || el.scrollWidth <= el.clientWidth + 1) return;
    const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
    if (delta === 0) return;
    const atLeft = el.scrollLeft <= 0;
    const atRight = el.scrollLeft + el.clientWidth >= el.scrollWidth - 1;
    if ((delta < 0 && atLeft) || (delta > 0 && atRight)) return;
    e.preventDefault();
    el.scrollLeft += delta;
  };

  return (
    <div
      ref={scrollRef}
      onWheel={handleWheel}
      className="flex items-center h-8 px-2 gap-1 bg-card border-b border-border overflow-x-auto overflow-y-hidden scrollbar-hide flex-nowrap min-w-0 w-full touch-pan-x overscroll-x-contain"
      style={{ WebkitOverflowScrolling: "touch" } as React.CSSProperties}
    >
      {editorTabs.map((et) => {
        const isActive = activeEditorTabId === et.id;
        const included = et.includeInContext !== false;
        // One tab = one context menu, so the whole pill (label, checkbox, close)
        // is the trigger. Right-clicking makes the tab active first: the reveal
        // is about the file the user just pointed at, and an inactive tab's row
        // would otherwise highlight a file the Files pane isn't showing.
        return (
          <ContextMenu key={et.id} onOpenChange={(open) => open && onSelectTab(et.id)}>
            <ContextMenuTrigger asChild>
              <div
                className="flex items-center gap-1 shrink-0"
                onMouseDown={(e) => {
                  if (e.button === 1) {
                    e.preventDefault();
                    e.stopPropagation();
                    onCloseTab(et.id);
                  }
                }}
              >
                <button
                  type="button"
                  role="checkbox"
                  aria-checked={included}
                  aria-label={`${fileNameFromPath(et.path)} — ${included ? "included in chat context" : "excluded from chat context"}`}
                  title={included ? "Included in chat context — click to exclude from the LLM loop" : "Excluded from chat context — click to include in the LLM loop"}
                  onClick={() => onToggleInclude?.(et.id)}
                  className={`shrink-0 w-3.5 h-3.5 rounded-sm border flex items-center justify-center transition-colors ${
                    included
                      ? "border-blue-500/60 text-blue-400"
                      : "border-border text-transparent hover:text-muted-foreground"
                  }`}
                >
                  {included && <Check className="w-3 h-3" />}
                </button>
                <button
                  onClick={() => onSelectTab(et.id)}
                  className={`flex items-center gap-1.5 px-2 py-1 rounded-md text-xs font-medium transition-colors whitespace-nowrap shrink-0 border ${
                    isActive
                      ? "bg-blue-600/20 text-blue-400 border-blue-500/30"
                      : "border-border text-muted-foreground hover:text-foreground hover:bg-muted"
                  }`}
                  title={et.path}
                >
                  <FileCode className="w-3.5 h-3.5" />
                  <span className="max-w-[120px] truncate">{fileNameFromPath(et.path)}</span>
                  {et.isDirty && (
                    <span className="w-1.5 h-1.5 rounded-full bg-muted shrink-0" title="Unsaved changes" />
                  )}
                </button>
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onCloseTab(et.id);
                  }}
                  className="p-0.5 rounded hover:bg-accent text-muted-foreground hover:text-accent-foreground transition-colors"
                  title="Close"
                >
                  <X className="w-3.5 h-3.5" />
                </button>
              </div>
            </ContextMenuTrigger>
            {onRevealInTree && (
              <ContextMenuContent>
                <ContextMenuItem onSelect={() => onRevealInTree(et)}>
                  <FolderTree className="w-3.5 h-3.5 mr-2" /> Show in file tree
                </ContextMenuItem>
              </ContextMenuContent>
            )}
          </ContextMenu>
        );
      })}
    </div>
  );
}