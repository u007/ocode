import { useRef } from "react";
import { FileCode, Files as FilesIcon, X } from "lucide-react";

export interface EditorTabInfo {
  id: string;
  path: string;
  isDirty?: boolean;
}

interface Props {
  editorTabs: EditorTabInfo[];
  activeEditorTabId: string | null;
  onSelectTree: () => void;
  onSelectTab: (id: string) => void;
  onCloseTab: (id: string) => void;
}

function fileNameFromPath(path: string): string {
  return path.split("/").pop() || path;
}

export default function EditorTabBar({
  editorTabs,
  activeEditorTabId,
  onSelectTree,
  onSelectTab,
  onCloseTab,
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
      className="flex items-center h-8 px-2 gap-1 bg-zinc-900 border-b border-zinc-700 overflow-x-auto overflow-y-hidden scrollbar-hide flex-nowrap min-w-0 w-full touch-pan-x overscroll-x-contain"
      style={{ WebkitOverflowScrolling: "touch" } as React.CSSProperties}
    >
      <button
        onClick={onSelectTree}
        className={`flex items-center gap-1.5 px-2 py-1 rounded text-xs font-medium transition-colors whitespace-nowrap shrink-0 ${
          activeEditorTabId === null
            ? "bg-zinc-700 text-white"
            : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800"
        }`}
      >
        <FilesIcon className="w-3.5 h-3.5" />
        File tree
      </button>
      {editorTabs.map((et) => {
        const isActive = activeEditorTabId === et.id;
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
              onClick={() => onSelectTab(et.id)}
              className={`flex items-center gap-1.5 px-2 py-1 rounded-md text-xs font-medium transition-colors whitespace-nowrap shrink-0 ${
                isActive
                  ? "bg-blue-600/20 text-blue-400"
                  : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800"
              }`}
              title={et.path}
            >
              <FileCode className="w-3.5 h-3.5" />
              <span className="max-w-[120px] truncate">{fileNameFromPath(et.path)}</span>
              {et.isDirty && (
                <span className="w-1.5 h-1.5 rounded-full bg-zinc-300 shrink-0" title="Unsaved changes" />
              )}
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation();
                onCloseTab(et.id);
              }}
              className="p-0.5 rounded hover:bg-zinc-700 text-zinc-500 hover:text-zinc-300 transition-colors"
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
