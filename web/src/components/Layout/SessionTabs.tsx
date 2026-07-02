import { useChatState, useChatDispatch } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { api } from "../../api/client";
import { X, Loader2, MessageSquare, Plus } from "lucide-react";
import { Button } from "../ui/button";
import { ScrollArea } from "../ui/scroll-area";
import { Tabs, TabsList, TabsTrigger } from "../ui/tabs";

export default function SessionTabs() {
  const { state: projectState, openSessionTab, closeSessionTab } = useProjectState();
  const chatState = useChatState();
  const chatDispatch = useChatDispatch();
  const { projectSessions, tabs, activeTabId } = projectState;

  const handleSessionClick = async (sessionId: string, title: string) => {
    openSessionTab(sessionId, title);
    try {
      const session = await api.getSession(sessionId);
      chatDispatch({ type: "SET_SESSION", sessionId });
      chatDispatch({ type: "SET_MESSAGES", messages: session.messages || [] });
    } catch (err) {
      console.error("Failed to load session:", err);
    }
  };

  const handleNewSession = () => {
    chatDispatch({ type: "RESET" });
  };

  // If no project is selected, show a lean bar
  if (!projectState.activeProject) {
    return (
      <div className="flex items-center h-10 px-4 border-b border-border bg-muted/30">
        <span className="text-xs text-muted-foreground">
          Select a project to view sessions
        </span>
      </div>
    );
  }

  return (
    <div className="flex flex-col border-b border-border bg-background">
      {/* Session tabs row */}
      {tabs.length > 0 && (
        <div className="flex items-center px-2 pt-2 overflow-x-auto">
          <Tabs value={activeTabId || undefined} className="flex-1">
            <TabsList className="h-8 bg-transparent p-0 gap-0.5">
              {tabs.map((tab) => (
                <TabsTrigger
                  key={tab.id}
                  value={tab.id}
                  onClick={() => handleSessionClick(tab.id, tab.title)}
                  className="h-7 px-3 text-xs data-[state=active]:bg-accent data-[state=active]:shadow-none rounded-t-md rounded-b-none border-b-2 border-transparent data-[state=active]:border-primary gap-1.5"
                >
                  <MessageSquare className="w-3 h-3 shrink-0" />
                  <span className="max-w-28 truncate">{tab.title || tab.id}</span>
                  <span
                    role="button"
                    tabIndex={0}
                    className="ml-1 p-0.5 rounded-sm hover:bg-muted-foreground/20"
                    onClick={(e) => {
                      e.stopPropagation();
                      closeSessionTab(tab.id);
                      if (chatState.sessionId === tab.id) {
                        chatDispatch({ type: "RESET" });
                      }
                    }}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        closeSessionTab(tab.id);
                        if (chatState.sessionId === tab.id) {
                          chatDispatch({ type: "RESET" });
                        }
                      }
                    }}
                  >
                    <X className="w-3 h-3" />
                  </span>
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs text-muted-foreground shrink-0 ml-1"
            onClick={handleNewSession}
          >
            <Plus className="w-3.5 h-3.5 mr-0.5" />
            New
          </Button>
        </div>
      )}

      {/* Project sessions quick-picker */}
      <ScrollArea className="max-w-full">
        <div className="flex items-center gap-2 px-4 py-2">
          <span className="text-xs font-medium text-muted-foreground whitespace-nowrap shrink-0">
            {projectState.activeProject.name}:
          </span>
          {projectState.sessionsLoading ? (
            <Loader2 className="w-3.5 h-3.5 text-muted-foreground animate-spin" />
          ) : projectSessions.length === 0 ? (
            <span className="text-xs text-muted-foreground/60">No sessions yet</span>
          ) : (
            <div className="flex items-center gap-1 flex-wrap">
              {projectSessions.slice(0, 20).map((session) => (
                <Button
                  key={session.id}
                  variant={activeTabId === session.id ? "secondary" : "ghost"}
                  size="sm"
                  className={`h-6 px-2 text-xs ${
                    activeTabId === session.id ? "" : "text-muted-foreground"
                  }`}
                  onClick={() => handleSessionClick(session.id, session.title)}
                >
                  {session.title || session.id.slice(0, 16)}
                </Button>
              ))}
            </div>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}
