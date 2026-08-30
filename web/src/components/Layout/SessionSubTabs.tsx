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

  const contextCurrent = chatSlice.tuiStatus?.context_current_tokens ?? 0;
  const contextMax = chatSlice.tuiStatus?.context_max_tokens ?? 0;
  const contextPct =
    contextMax > 0 ? Math.min(100, Math.round((contextCurrent / contextMax) * 100)) : null;

  function formatTokenCount(n: number): string {
    if (n <= 0) return "0";
    if (n < 1000) return String(n);
    if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`;
    return `${(n / 1_000_000).toFixed(1)}M`;
  }

  // Human-readable memory usage for the Chat tab badge.
  // When context window is known: "45%" (with tooltip showing tokens).
  // When only current tokens known: formatted count e.g. "12.3k".
  // When nothing known: badge hidden (null).
  const chatMemoryLabel: string | null =
    contextPct !== null
      ? `${contextPct}%`
      : contextCurrent > 0
        ? formatTokenCount(contextCurrent)
        : null;
  const chatMemoryTooltip =
    contextMax > 0
      ? `${formatTokenCount(contextCurrent)} / ${formatTokenCount(contextMax)} tokens${contextPct !== null ? ` (${contextPct}%)` : ""}`
      : contextCurrent > 0
        ? `${formatTokenCount(contextCurrent)} tokens`
        : undefined;

  return (
    <div
      ref={scrollRef}
      onWheel={handleWheel}
      className="flex items-center h-9 px-2 gap-1 bg-card border-b border-border overflow-x-auto overflow-y-hidden scrollbar-hide flex-nowrap min-w-0 w-full touch-pan-x overscroll-x-contain"
      style={{ WebkitOverflowScrolling: "touch" } as React.CSSProperties}
    >
      {subTabs.map((tab) => {
        const Icon = tab.icon;
        const isActive = activeSessionTab.activeSubTab === tab.id;
        const isChat = tab.id === "chat";
        const badgeLabel = isChat ? chatMemoryLabel : undefined;
        const badgeTooltip = isChat ? chatMemoryTooltip : undefined;
        // Color-code memory pressure similar to CoworkSidebar: green <65%, yellow <85%, red >=85%
        const memoryBadgeColor =
          isChat && contextPct !== null
            ? contextPct >= 85
              ? isActive
                ? "bg-red-500 text-white"
                : "bg-red-900/60 text-red-200"
              : contextPct >= 65
                ? isActive
                  ? "bg-yellow-500 text-foreground"
                  : "bg-yellow-900/60 text-yellow-200"
                : isActive
                  ? "bg-accent text-accent-foreground"
                  : "bg-muted text-foreground"
            : isActive
              ? "bg-accent text-accent-foreground"
              : "bg-muted text-foreground";
        return (
          <button
            key={tab.id}
            onClick={() => dispatch({ type: "SET_TAB_SUB_TAB", id: activeSessionTab.id, subTab: tab.id })}
            className={`flex items-center gap-2 px-[13px] py-[7px] rounded-md text-[15px] leading-5 font-medium transition-colors whitespace-nowrap shrink-0 border ${
              isActive
                ? "bg-accent text-accent-foreground border-border"
                : "border-border text-muted-foreground hover:text-foreground hover:bg-muted"
            }`}
          >
            <Icon className="w-4 h-4" />
            {tab.label}
            {badgeLabel !== undefined && badgeLabel !== null && (
              <span
                className={`inline-flex items-center justify-center min-w-[1.25rem] h-5 px-1 rounded-full text-xs font-semibold leading-none ${memoryBadgeColor}`}
                aria-label={`${tab.label} memory ${badgeLabel}`}
                title={badgeTooltip}
              >
                {badgeLabel}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
