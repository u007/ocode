import { useRef } from "react";
import { MessageSquare, Bot, History, ScrollText, Activity, Terminal as TerminalIcon, Gauge } from "lucide-react";
import { useProjectState, type SessionSubTabId } from "../../stores/projectStore";

const subTabs: { id: SessionSubTabId; label: string; icon: typeof MessageSquare }[] = [
  { id: "chat", label: "Chat", icon: MessageSquare },
  { id: "agents", label: "Agents", icon: Bot },
  { id: "changes", label: "Changes", icon: History },
  { id: "logs", label: "Logs", icon: ScrollText },
  { id: "status", label: "Status", icon: Activity },
  { id: "terminal", label: "Terminal", icon: TerminalIcon },
  { id: "processes", label: "Processes", icon: Gauge },
];

export default function SessionSubTabs() {
  const { tabs, activeTabId, dispatch } = useProjectState();
  // The terminal sub-tab is always shown: the interactive terminal is always
  // enabled, there is no config to hide it.
  const activeSessionTab = tabs.find((t) => t.id === activeTabId);
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

  if (!activeSessionTab) return null;

  return (
    <div
      ref={scrollRef}
      onWheel={handleWheel}
      className="flex items-center h-9 px-2 gap-1 bg-zinc-900 border-b border-zinc-700 overflow-x-auto overflow-y-hidden scrollbar-hide flex-nowrap min-w-0 w-full touch-pan-x overscroll-x-contain"
      style={{ WebkitOverflowScrolling: "touch" } as React.CSSProperties}
    >
      {subTabs.map((tab) => {
        const Icon = tab.icon;
        const isActive = activeSessionTab.activeSubTab === tab.id;
        return (
          <button
            key={tab.id}
            onClick={() => dispatch({ type: "SET_TAB_SUB_TAB", id: activeSessionTab.id, subTab: tab.id })}
            className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-sm font-medium transition-colors whitespace-nowrap shrink-0 ${
              isActive
                ? "bg-zinc-700 text-white"
                : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800"
            }`}
          >
            <Icon className="w-4 h-4" />
            {tab.label}
          </button>
        );
      })}
    </div>
  );
}
