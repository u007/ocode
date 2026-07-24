import { MessageSquare, FolderGit2, GitBranch, ScrollText, Paperclip, Activity, FileCode, X, CalendarClock, History } from "lucide-react";

export interface EditorTabInfo {
  id: string;
  path: string;
  isDirty?: boolean;
}

interface Props {
  activeTab: string;
  onTabChange: (tab: string) => void;
  editorTabs: EditorTabInfo[];
  onEditorTabClose: (id: string) => void;
}

const mainTabs = [
  { id: "chat", label: "Chat", icon: MessageSquare },
  { id: "files", label: "Files", icon: FolderGit2 },
  { id: "changes", label: "Changes", icon: History },
  { id: "git", label: "Git", icon: GitBranch },
  { id: "status", label: "Status", icon: Activity },
  { id: "logs", label: "Logs", icon: ScrollText },
  { id: "cron", label: "Cron", icon: CalendarClock },
  { id: "assets", label: "Assets", icon: Paperclip },
];

function tabClass(isActive: boolean): string {
  return `flex items-center gap-2 px-3 py-1.5 rounded-md text-sm font-medium transition-colors whitespace-nowrap ${
    isActive
      ? "bg-zinc-700 text-white"
      : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800"
  }`;
}

function fileNameFromPath(path: string): string {
  return path.split("/").pop() || path;
}

export default function TopTabs({ activeTab, onTabChange, editorTabs, onEditorTabClose }: Props) {
  return (
    <header className="flex items-center border-b border-zinc-700 bg-zinc-900 h-12 px-4 overflow-hidden">
      {/* Left: Logo */}
      <div className="flex items-center gap-2 mr-6 shrink-0">
        <div className="w-6 h-6 rounded bg-blue-600 flex items-center justify-center text-xs font-bold">
          o
        </div>
        <span className="font-semibold text-sm">ocode</span>
      </div>

      {/* Main tabs */}
      <nav className="flex items-center gap-1 shrink-0">
        {mainTabs.map((tab) => {
          const Icon = tab.icon;
          const isActive = activeTab === tab.id;
          return (
            <button
              key={tab.id}
              onClick={() => onTabChange(tab.id)}
              className={tabClass(isActive)}
            >
              <Icon className="w-4 h-4" />
              <span className="hidden sm:inline">{tab.label}</span>
            </button>
          );
        })}
      </nav>

      {/* Editor tabs */}
      {editorTabs.length > 0 && (
        <>
          <div className="w-px h-6 bg-zinc-700 mx-2 shrink-0" />

          <div className="flex items-center gap-1 overflow-x-auto min-w-0 scrollbar-none">
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
                  <button
                    onClick={() => onTabChange(et.id)}
                    className={`flex items-center gap-1.5 px-2 py-1.5 rounded-md text-xs font-medium transition-colors whitespace-nowrap ${
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
        </>
      )}


    </header>
  );
}
