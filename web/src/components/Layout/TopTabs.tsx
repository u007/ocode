import { MessageSquare, FolderGit2, GitBranch, ScrollText, Menu } from "lucide-react";

interface Props {
  activeTab: string;
  onTabChange: (tab: string) => void;
  onMenuToggle?: () => void;
}

const tabs = [
  { id: "chat", label: "Chat", icon: MessageSquare },
  { id: "files", label: "Files", icon: FolderGit2 },
  { id: "git", label: "Git", icon: GitBranch },
  { id: "logs", label: "Logs", icon: ScrollText },
];

export default function TopTabs({ activeTab, onTabChange, onMenuToggle }: Props) {
  return (
    <header className="flex items-center border-b border-zinc-700 bg-zinc-900 h-12 px-4">
      {/* Left: Logo */}
      <div className="flex items-center gap-2 mr-6">
        <div className="w-6 h-6 rounded bg-blue-600 flex items-center justify-center text-xs font-bold">
          o
        </div>
        <span className="font-semibold text-sm">ocode</span>
      </div>

      {/* Center: Tabs */}
      <nav className="flex items-center gap-1 flex-1">
        {tabs.map((tab) => {
          const Icon = tab.icon;
          const isActive = activeTab === tab.id;
          return (
            <button
              key={tab.id}
              onClick={() => onTabChange(tab.id)}
              className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
                isActive
                  ? "bg-zinc-700 text-white"
                  : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800"
              }`}
            >
              <Icon className="w-4 h-4" />
              <span className="hidden sm:inline">{tab.label}</span>
            </button>
          );
        })}
      </nav>

      {/* Right: Burger menu (always visible, toggles session sidebar) */}
      {onMenuToggle && (
        <button
          onClick={onMenuToggle}
          className="p-2 rounded-md text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition-colors"
          title="Toggle session history"
        >
          <Menu className="w-5 h-5" />
        </button>
      )}
    </header>
  );
}
