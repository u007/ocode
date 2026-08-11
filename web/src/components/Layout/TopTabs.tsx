import { useLayoutEffect, useRef, useState } from "react";
import { FolderGit2, GitBranch, Paperclip, CalendarClock, MessageSquare, MoreHorizontal, Settings } from "lucide-react";
import { TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select";
import SyncStatusWidget from "./SyncStatusWidget";

interface Props {
  activeTab: string;
  onTabSelect: (value: string) => void;
}

const mainTabs = [
  { id: "sessions", label: "Sessions", icon: MessageSquare },
  { id: "files", label: "Files", icon: FolderGit2 },
  { id: "git", label: "Git", icon: GitBranch },
  { id: "cron", label: "Cron", icon: CalendarClock },
  { id: "assets", label: "Assets", icon: Paperclip },
  { id: "settings", label: "Settings", icon: Settings },
];

export default function TopTabs({ activeTab, onTabSelect }: Props) {
  const tabsRef = useRef<HTMLDivElement | null>(null);
  const activeRef = useRef<HTMLButtonElement | null>(null);
  const [overflowing, setOverflowing] = useState(false);

  // Detect whether the tab strip overflows its container so the "More" menu
  // can be shown. Re-measured on resize.
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
  }, []);

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
  }, [activeTab]);

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

      {/* Main tabs — single row, horizontally scrollable */}
      <TabsList
        ref={tabsRef}
        onWheel={handleWheel}
        className="bg-transparent p-0 h-auto gap-1 justify-start flex-1 min-w-0 overflow-x-auto scrollbar-hide flex-nowrap"
      >
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
