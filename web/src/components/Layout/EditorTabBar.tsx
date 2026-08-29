import { useRef } from "react";
import { Check, FileCode, X } from "lucide-react";

export interface EditorTabInfo {
  id: string;
  path: string;
  isDirty?: boolean;
  /** When true (default) this opened file is attached to the chat/LLM loop. */
  includeInContext?: boolean;
}

interface Props {
  editorTabs: EditorTabInfo[];
  activeEditorTabId: string | null;
  onSelectTab: (id: string) => void;
  onCloseTab: (id: string) => void;
  onToggleInclude?: (id: string) => void;
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
        return (
          <div
            key={et.id}
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
              className={`flex items-center gap-1.5 px-2 py-1 rounded-md text-xs font-medium transition-colors whitespace-nowrap shrink-0 ${
                isActive
                  ? "bg-blue-600/20 text-blue-400"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted"
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
              className="p-0.5 rounded hover:bg-accent text-muted-foreground hover:text-foreground transition-colors"
              title="Close"
            >
              <X className="w-3 h-3" />
            </button>
          </div>
        );
      })}
    </div>
  );
}
