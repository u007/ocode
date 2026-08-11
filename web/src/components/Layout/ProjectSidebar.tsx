import { useState, useMemo } from "react";
import { useProjectState } from "../../stores/projectStore";
import { useChatState } from "../../stores/chatStore";
import { FolderGit2, Plus, Trash2, ChevronLeft, FolderOpen, Loader2, FolderSearch } from "lucide-react";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { ScrollArea } from "../ui/scroll-area";
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from "../ui/tooltip";
import { Separator } from "../ui/separator";
import DirectoryBrowser from "./DirectoryBrowser";

type SessionStatus = "none" | "idle" | "running";

/** Derive per-project session status from open tabs + chat slices.
 *  "running" = any tab is streaming; "idle" = any tab has a real session
 *  (non-new-* id); "none" = no sessions. */
function useProjectSessionStatus(projectPath: string): SessionStatus {
  const { state: projectState } = useProjectState();
  const chatState = useChatState();
  const tabs = projectState.tabsByProject[projectPath];

  return useMemo(() => {
    if (!tabs || tabs.length === 0) return "none";
    for (const tab of tabs) {
      if (tab.id.startsWith("new-")) continue;
      const slice = chatState.sessions[tab.id];
      if (slice?.isStreaming) return "running";
    }
    // Any non-new tab means a real session exists → idle.
    const hasRealSession = tabs.some((t) => !t.id.startsWith("new-"));
    return hasRealSession ? "idle" : "none";
  }, [tabs, chatState.sessions]);
}

function SessionDot({ status }: { status: SessionStatus }) {
  if (status === "none") return null;
  return (
    <span
      className={`shrink-0 h-2 w-2 rounded-full ${
        status === "running"
          ? "bg-blue-500 animate-pulse"
          : "bg-zinc-500"
      }`}
    />
  );
}

interface ProjectRowProps {
  project: { path: string; name: string };
  isActive: boolean;
  onSelect: () => void;
  onRemove: () => void;
}

// A dedicated component (rather than inlining this in a .map()) so
// useProjectSessionStatus is called at a stable top level per project
// instance, not a variable number of times per ProjectSidebar render.
function ProjectRow({ project, isActive, onSelect, onRemove }: ProjectRowProps) {
  const status = useProjectSessionStatus(project.path);
  return (
    <div className="group relative px-1">
      <Button
        variant="ghost"
        className={`w-full justify-start gap-3 px-3 h-auto py-2.5 text-sm ${
          isActive
            ? "bg-accent text-accent-foreground border-l-2 border-primary"
            : "text-muted-foreground border-l-2 border-transparent"
        }`}
        onClick={onSelect}
      >
        <FolderGit2 className="w-4 h-4 shrink-0 text-muted-foreground/70" />
        <div className="min-w-0 flex-1 text-left">
          <div className="truncate font-medium text-foreground/90">
            {project.name}
          </div>
          <div className="truncate text-xs text-muted-foreground/60">
            {project.path}
          </div>
        </div>
        <SessionDot status={status} />
        <Button
          variant="ghost"
          size="sm"
          className="p-1 h-6 w-6 opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-destructive"
          onClick={(e) => {
            e.stopPropagation();
            onRemove();
          }}
        >
          <Trash2 className="w-3.5 h-3.5" />
        </Button>
      </Button>
    </div>
  );
}

interface Props {
  isOpen: boolean;
  onToggle: () => void;
}

export default function ProjectSidebar({ isOpen, onToggle }: Props) {
  const { state, selectProject, addProject, removeProject } = useProjectState();
  const [adding, setAdding] = useState(false);
  const [newPath, setNewPath] = useState("");
  const [browserOpen, setBrowserOpen] = useState(false);

  const handleAdd = async () => {
    const path = newPath.trim();
    if (!path) return;
    await addProject(path);
    setNewPath("");
    setAdding(false);
  };

  // Collapsed state: icon-only sidebar with tooltips
  if (!isOpen) {
    return (
      <TooltipProvider delayDuration={300}>
        <div className="flex flex-col items-center py-2 w-10 border-r border-border bg-background">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                className="p-2 h-9 w-9"
                onClick={onToggle}
              >
                <ChevronLeft className="w-4 h-4 rotate-180" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right">Show project sidebar</TooltipContent>
          </Tooltip>

          <Separator className="my-2 w-6" />

          {state.projects.length > 0 && (
            <div className="flex flex-col gap-1">
              {state.projects.slice(0, 5).map((p) => (
                <Tooltip key={p.path}>
                  <TooltipTrigger asChild>
                    <Button
                      variant="ghost"
                      size="sm"
                      className={`p-2 h-9 w-9 ${
                        state.activeProject?.path === p.path
                          ? "bg-primary/15 text-primary"
                          : "text-muted-foreground"
                      }`}
                      onClick={() => selectProject(p)}
                    >
                      <FolderGit2 className="w-4 h-4" />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent side="right">{p.name}</TooltipContent>
                </Tooltip>
              ))}
            </div>
          )}
        </div>
      </TooltipProvider>
    );
  }

  // Expanded state
  return (
    <div className="flex flex-col w-60 border-r border-border bg-background">
      {/* Header */}
      <div className="flex items-center justify-between px-4 h-12 border-b border-border">
        <h2 className="text-sm font-semibold text-foreground">Projects</h2>
        <Button variant="ghost" size="sm" className="h-8 w-8 p-0" onClick={onToggle}>
          <ChevronLeft className="w-4 h-4" />
        </Button>
      </div>

      {/* Project list */}
      <ScrollArea className="flex-1">
        {state.loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
          </div>
        ) : state.projects.length === 0 ? (
          <div className="px-4 py-12 text-center text-xs text-muted-foreground">
            <FolderOpen className="w-8 h-8 mx-auto mb-2 text-muted-foreground/60" />
            <p>No projects yet</p>
            <p className="mt-1">Add a project root below</p>
          </div>
        ) : (
          <div className="py-1">
            {state.projects.map((project) => (
              <ProjectRow
                key={project.path}
                project={project}
                isActive={state.activeProject?.path === project.path}
                onSelect={() => selectProject(project)}
                onRemove={() => removeProject(project.path)}
              />
            ))}
          </div>
        )}
      </ScrollArea>

      {/* Add project */}
      <div className="border-t border-border p-3">
        {adding ? (
          <div className="flex flex-col gap-2">
            <div className="flex gap-2">
              <Input
                type="text"
                value={newPath}
                onChange={(e) => setNewPath(e.target.value)}
                placeholder="/path/to/project"
                className="h-8 text-xs flex-1"
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleAdd();
                  if (e.key === "Escape") setAdding(false);
                }}
                autoFocus
              />
              <Button
                variant="outline"
                size="sm"
                className="h-8 w-8 p-0 shrink-0"
                onClick={() => setBrowserOpen(true)}
                title="Browse for folder"
              >
                <FolderSearch className="w-3.5 h-3.5" />
              </Button>
            </div>
            <div className="flex gap-2">
              <Button size="sm" className="flex-1 h-7 text-xs" onClick={handleAdd}>
                Add
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-xs"
                onClick={() => setAdding(false)}
              >
                Cancel
              </Button>
            </div>
          </div>
        ) : (
          <Button
            variant="ghost"
            size="sm"
            className="w-full justify-start gap-2 h-8 text-xs text-muted-foreground"
            onClick={() => setAdding(true)}
          >
            <Plus className="w-3.5 h-3.5" />
            <span>Add Project</span>
          </Button>
        )}
      </div>
      <DirectoryBrowser
        open={browserOpen}
        onOpenChange={setBrowserOpen}
        onSelect={(path) => {
          setNewPath(path);
          setBrowserOpen(false);
        }}
      />
    </div>
  );
}
