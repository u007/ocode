import { useLayoutEffect, useRef, useState } from "react";
import { MessageSquare, FolderGit2, GitBranch, ScrollText, Paperclip, Activity, FileCode, X, CalendarClock, History, Bot, MoreHorizontal } from "lucide-react";
import { TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import SyncStatusWidget from "./SyncStatusWidget";

export interface EditorTabInfo {
  id: string;
  path: string;
  isDirty?: boolean;
}

interface Props {
  editorTabs: EditorTabInfo[];
  onEditorTabClose: (id: string) => void;
  activeTab: string;
  onTabSelect: (value: string) => void;
}

const mainTabs = [
  { id: "chat", label: "Chat", icon: MessageSquare },
  { id: "agents", label: "Agents", icon: Bot },
  { id: "files", label: "Files", icon: FolderGit2 },
  { id: "changes", label: "Changes", icon: History },
  { id: "git", label: "Git", icon: GitBranch },
  { id: "status", label: "Status", icon: Activity },
  { id: "logs", label: "Logs", icon: ScrollText },
  { id: "cron", label: "Cron", icon: CalendarClock },
  { id: "assets", label: "Assets", icon: Paperclip },
];

function fileNameFromPath(path: string): string {
  return path.split("/").pop() || path;
}

export default function TopTabs({ editorTabs, onEditorTabClose, activeTab, onTabSelect }: Props) {
  const tabsRef = useRef<HTMLDivElement | null>(null);
  const activeRef = useRef<HTMLButtonElement | null>(null);
  const [overflowing, setOverflowing] = useState(false);

  // Detect whether the tab strip overflows its container so the "More" menu
  // can be shown. Re-measured on resize and whenever editor tabs change.
  useLayoutEffect(() => {
    const el = tabsRef.current;
    if (!el) return;
    const measure = () => setOverflowing(el.scrollWidth > el.clientWidth + 1);
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    window.addEventListener("resize", measure);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [editorTabs.length]);

  // Keep the active tab in view (Chrome-style) when it changes.
  useLayoutEffect(() => {
    const el = tabsRef.current;
    const tab = activeRef.current;
    if (!el || !tab) return;
    const left = tab.getBoundingClientRect().left - el.getBoundingClientRect().left;
    const right = left + tab.getBoundingClientRect().width;
    if (left < el.scrollLeft) {
      el.scrollTo({ left });
    } else if (right > el.scrollLeft + el.clientWidth) {
      el.scrollTo({ left: right - el.clientWidth });
    }
  }, [activeTab, editorTabs.length]);

  // Translate vertical mouse-wheel input into horizontal scrolling on the
  // strip. Trackpad horizontal gestures scroll natively.
  const handleWheel = (e: React.WheelEvent<HTMLDivElement>) => {
    const el = tabsRef.current;
    if (!el || el.scrollWidth <= el.clientWidth + 1) return;
    const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
    el.scrollLeft += delta;
  };

  return (
    <header className="flex items-center border-b border-zinc-700 bg-zinc-900 h-12 px-4 overflow-hidden">
      {/* Left: Logo */}
      <div className="flex items-center gap-2 mr-6 shrink-0">
        <div className="w-6 h-6 rounded bg-blue-600 flex items-center justify-center text-xs font-bold">
          o
        </div>
        <span className="font-semibold text-sm hidden sm:inline">ocode</span>
      </div>

      {/* Main + editor tabs — single row, horizontally scrollable */}
      <TabsList
        ref={tabsRef}
        onWheel={handleWheel}
        className="bg-transparent p-0 h-auto gap-1 justify-start flex-1 min-w-0 overflow-x-auto scrollbar-hide flex-nowrap"
      >
        {/* Main tabs */}
        {mainTabs.map((tab) => {
          const Icon = tab.icon;
          const isActive = activeTab === tab.id;
          return (
            <TabsTrigger
              key={tab.id}
              value={tab.id}
              ref={isActive ? activeRef : undefined}
              className="flex items-center gap-2 px-3 py-1.5 rounded-md text-sm font-medium transition-colors whitespace-nowrap data-[state=active]:bg-zinc-700 data-[state=active]:text-white data-[state=active]:shadow-none text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 shrink-0"
            >
              <Icon className="w-4 h-4" />
              <span className="hidden sm:inline">{tab.label}</span>
            </TabsTrigger>
          );
        })}

        {/* Editor tabs */}
        {editorTabs.length > 0 && (
          <div className="flex items-center gap-1 ml-2 pl-2 border-l border-zinc-700">
            {editorTabs.map((et) => {
              const isActive = activeTab === et.id;
              return (
                <div
                  key={et.id}
                  className="flex items-center gap-1 shrink-0"
                  onMouseDown={(e) => {
                    if (e.button === 1) {
                      e.preventDefault();
                      e.stopPropagation();
                      onEditorTabClose(et.id);
                    }
                  }}
                >
                  <TabsTrigger
                    value={et.id}
                    ref={isActive ? activeRef : undefined}
                    className="flex items-center gap-1.5 px-2 py-1.5 rounded-md text-xs font-medium transition-colors whitespace-nowrap data-[state=active]:bg-blue-600/20 data-[state=active]:text-blue-400 data-[state=active]:shadow-none text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 shrink-0"
                    title={et.path}
                  >
                    <FileCode className="w-3.5 h-3.5" />
                    <span className="max-w-[120px] truncate">{fileNameFromPath(et.path)}</span>
                    {et.isDirty && (
                      <span className="w-1.5 h-1.5 rounded-full bg-zinc-300 shrink-0" title="Unsaved changes" />
                    )}
                  </TabsTrigger>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      onEditorTabClose(et.id);
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
        )}
      </TabsList>

      {/* Overflow "More" menu — appears only when the tabs don't fit */}
      {overflowing && (
        <div className="ml-1 shrink-0">
          <Select value={activeTab} onValueChange={onTabSelect}>
            <SelectTrigger
              aria-label="More tabs"
              className="h-8 w-8 justify-center border-0 bg-transparent p-0 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200 [&>svg:last-child]:hidden"
            >
              <MoreHorizontal className="h-4 w-4" />
            </SelectTrigger>
            <SelectContent align="end" className="max-h-80">
              {mainTabs.map((tab) => {
                const Icon = tab.icon;
                return (
                  <SelectItem key={tab.id} value={tab.id}>
                    <span className="flex items-center gap-2">
                      <Icon className="w-3.5 h-3.5" />
                      {tab.label}
                    </span>
                  </SelectItem>
                );
              })}
            </SelectContent>
          </Select>
        </div>
      )}

      <div className="ml-auto flex items-center shrink-0">
        <SyncStatusWidget />
      </div>
    </header>
  );
}
