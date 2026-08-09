import { MessageSquare, Bot, History } from "lucide-react";
import { useProjectState, type SessionSubTabId } from "../../stores/projectStore";

const subTabs: { id: SessionSubTabId; label: string; icon: typeof MessageSquare }[] = [
  { id: "chat", label: "Chat", icon: MessageSquare },
  { id: "agents", label: "Agents", icon: Bot },
  { id: "changes", label: "Changes", icon: History },
];

export default function SessionSubTabs() {
  const { tabs, activeTabId, dispatch } = useProjectState();
  const activeSessionTab = tabs.find((t) => t.id === activeTabId);

  if (!activeSessionTab) return null;

  return (
    <div className="flex items-center h-9 px-2 gap-1 bg-zinc-900 border-b border-zinc-700">
      {subTabs.map((tab) => {
        const Icon = tab.icon;
        const isActive = activeSessionTab.activeSubTab === tab.id;
        return (
          <button
            key={tab.id}
            onClick={() => dispatch({ type: "SET_TAB_SUB_TAB", id: activeSessionTab.id, subTab: tab.id })}
            className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-sm font-medium transition-colors whitespace-nowrap ${
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
