import { useRef } from "react";
import { MessageSquare, Bot, History, ScrollText, Activity } from "lucide-react";
import { useProjectState, type SessionSubTabId } from "../../stores/projectStore";
import { getSessionSlice, useChatSelector } from "../../stores/chatStore";

const subTabs: { id: SessionSubTabId; label: string; icon: typeof MessageSquare }[] = [
  { id: "chat", label: "Chat", icon: MessageSquare },
  { id: "agents", label: "Agents", icon: Bot },
  { id: "changes", label: "Changes", icon: History },
  { id: "logs", label: "Logs", icon: ScrollText },
  { id: "status", label: "Status", icon: Activity },
];

export default function SessionSubTabs() {
  const { tabs, activeTabId, dispatch } = useProjectState();
  // Selector must run unconditionally (before the early return below), so
  // it's keyed on activeTabId directly rather than activeSessionTab.id.
  const chatSlice = useChatSelector((s) => getSessionSlice(s, activeTabId));
  // The terminal is now a project-level top tab (beside Sessions), not a
  // session sub-tab.
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

  const chatCount = chatSlice.totalMessages > 0 ? chatSlice.totalMessages : chatSlice.messages.length;

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
        const count = tab.id === "chat" ? chatCount : undefined;
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
            {count !== undefined && (
              <span
                className={`inline-flex items-center justify-center min-w-[1.25rem] h-5 px-1 rounded-full text-xs font-semibold leading-none ${
                  isActive ? "bg-zinc-600 text-white" : "bg-zinc-800 text-zinc-300"
                }`}
                aria-label={`${tab.label} count ${count}`}
              >
                {count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
